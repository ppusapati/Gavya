package uow

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"p9e.in/samavaya/packages/database/rlssession"
	"p9e.in/samavaya/packages/errors"
	"p9e.in/samavaya/packages/p9context"
)

// RLSFactory creates UnitOfWork instances that automatically set RLS session variables.
// It extracts RLSScope from context and sets PostgreSQL session variables
// (app.tenant_id, app.company_id, app.branch_id) at the start of each transaction.
type RLSFactory struct {
	pool *pgxpool.Pool
}

// NewRLSFactory creates a new RLS-aware Factory from a connection pool.
func NewRLSFactory(pool *pgxpool.Pool) Factory {
	return &RLSFactory{pool: pool}
}

// Begin starts a new transaction and sets RLS session variables from context.
func (f *RLSFactory) Begin(ctx context.Context) (UnitOfWork, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return nil, errors.InternalServer(
			"TRANSACTION_BEGIN_FAILED",
			fmt.Sprintf("Failed to begin transaction: %v", err),
		)
	}

	// Extract RLS scope from context
	scope := p9context.MustRLSScope(ctx)

	// Set RLS session variables
	if err := setRLSVariables(ctx, tx, scope); err != nil {
		// Rollback and return error
		_ = tx.Rollback(ctx)
		return nil, err
	}

	return NewUnitOfWork(tx), nil
}

// New is an alias for Begin for backward compatibility.
func (f *RLSFactory) New(ctx context.Context) (UnitOfWork, error) {
	return f.Begin(ctx)
}

// setRLSVariables sets PostgreSQL session variables (app.tenant_id,
// app.company_id, app.branch_id) on the current transaction. Thin wrapper
// over rlssession.SetLocal that wraps any error with the platform's
// errors.InternalServer for backward-compat with the call site (Begin
// translates the wrapped error into a tx rollback + caller-visible error).
func setRLSVariables(ctx context.Context, tx interface{ Exec(context.Context, string, ...any) (pgconn.CommandTag, error) }, scope p9context.RLSScope) error {
	if err := rlssession.SetLocal(ctx, execAdapter{tx}, scope); err != nil {
		return errors.InternalServer("RLS_SET_FAILED", err.Error())
	}
	return nil
}

// execAdapter bridges the in-package interface{} parameter to the
// rlssession.Execer interface. Required because we can't change the
// signature of setRLSVariables (the interface{} type was already public)
// without a breaking change to anything in the build that depends on it.
type execAdapter struct {
	inner interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	}
}

func (a execAdapter) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return a.inner.Exec(ctx, sql, args...)
}

// WithRLSTransaction executes a function within a transaction with RLS variables set.
// Automatically commits on success or rolls back on error.
func WithRLSTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(uow UnitOfWork) error) error {
	factory := NewRLSFactory(pool)
	return WithTx(ctx, factory, fn)
}

// SetRLSOnPool sets RLS session variables on a pooled connection for non-transactional reads.
// This acquires a connection, sets variables, executes the function, and releases the connection.
// Note: The session variables are reset when the connection is returned to the pool.
func SetRLSOnPool(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return errors.InternalServer(
			"POOL_ACQUIRE_FAILED",
			fmt.Sprintf("Failed to acquire connection: %v", err),
		)
	}
	defer conn.Release()

	// Extract RLS scope from context
	scope := p9context.MustRLSScope(ctx)

	// Set GUCs at SESSION scope (no transaction here). The connection is
	// pooled — pgx resets session state on release, so the next request
	// gets a clean connection. If pool config disables that reset, this
	// path leaks tenant scope to the next consumer; document and audit.
	if err := rlssession.SetSession(ctx, conn, scope); err != nil {
		return errors.InternalServer("RLS_SET_FAILED", err.Error())
	}

	return fn(ctx)
}
