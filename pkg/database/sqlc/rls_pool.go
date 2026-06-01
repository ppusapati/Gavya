package sqlc

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"p9e.in/samavaya/packages/database/rlssession"
	"p9e.in/samavaya/packages/p9context"
)

// RLSPool wraps a *pgxpool.Pool and satisfies the sqlc-generated `DBTX`
// interface (Exec, Query, QueryRow). On every call, it extracts the
// RLSScope from the request context and runs the SQL inside an implicit
// single-statement transaction with `SET LOCAL app.tenant_id = ...` (and
// company/branch if present) so PostgreSQL FORCE RLS policies admit the
// query.
//
// **Why this exists.** Migration 000219 created `samavaya_app` as
// NOSUPERUSER NOBYPASSRLS. Under that role, every read against a table
// with FORCE RLS + tenant_isolation policy returns zero rows unless the
// session variable matching the policy's `current_setting('app.tenant_id')`
// expression is set. The application's repositories use sqlc-generated
// `*db.Queries` constructed once at fx graph build time via `db.New(pool)`
// — which means every call goes straight through the pool with no
// session-variable setup.
//
// Refactoring all 872 `business/*/internal/repository/*.go` files to wrap
// each call in a uow transaction would be invasive. Wrapping the pool
// once (here) is the smallest change: every existing `r.queries.Foo(ctx, ...)`
// call now flows through `rlsPool.Query/Exec/QueryRow`, which transparently
// sets up RLS for the duration of the SQL.
//
// **Tradeoffs.** Each query does 4 round-trips instead of 1: BEGIN; SET
// LOCAL ...; SQL; COMMIT. For low-throughput admin paths this is a non-issue;
// for hot paths it adds latency. The right longer-term answer is to migrate
// repositories onto explicit `uow.WithTx(ctx, dm.RLSFactory, fn)` so a
// single tx covers multiple queries — RLSPool is the safety net that
// makes RLS work correctly without that migration.
//
// **Graceful degradation.** If the context has no RLSScope (background
// jobs, fx OnStart hooks, init paths, Kafka consumers), MustRLSScope
// returns a zero-value `RLSScope{}`, and we fall back to running the
// query without a transaction — preserving the previous behavior. Such
// paths still see zero rows under samavaya_app for tenant-scoped tables;
// this matches what they got before this change and avoids breaking
// startup or background work that happens to run pool queries without
// auth context.
//
// **Quoting.** `SET LOCAL` does not accept parameters, so the tenant /
// company / branch IDs are interpolated as literals. They flow from the
// authenticated request context (via authContextMiddleware) and so are
// trusted, but we still reject quotes and semicolons as defense-in-depth.
type RLSPool struct {
	pool *pgxpool.Pool
}

// NewRLSPool wraps a pool with RLS-aware DBTX semantics.
func NewRLSPool(pool *pgxpool.Pool) *RLSPool {
	return &RLSPool{pool: pool}
}

// Pool returns the underlying pool for code paths that genuinely need
// the bare pool (e.g. for explicit transaction management via uow). Most
// callers should NOT use this — use the DBTX methods.
func (r *RLSPool) Pool() *pgxpool.Pool {
	return r.pool
}

// Exec satisfies sqlc's DBTX. Three paths, fastest first:
//   1. Request-scoped tx attached by rlsConnMiddleware → run on it (1 round-trip)
//   2. RLSScope in ctx but no shared tx → implicit per-call tx with SET LOCAL (4 round-trips)
//   3. No scope at all → plain pool call (1 round-trip, but RLS will return 0 rows)
func (r *RLSPool) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if tx := RequestTxFromCtx(ctx); tx != nil {
		return tx.Exec(ctx, sql, args...)
	}
	scope, hasScope := scopeFromCtx(ctx)
	if !hasScope {
		return r.pool.Exec(ctx, sql, args...)
	}
	var ct pgconn.CommandTag
	err := r.runWithScope(ctx, scope, func(tx pgx.Tx) error {
		var execErr error
		ct, execErr = tx.Exec(ctx, sql, args...)
		return execErr
	})
	return ct, err
}

// Query satisfies sqlc's DBTX. Three-path dispatch like Exec.
//
// Fast path (request-scoped tx attached): just run tx.Query — pgx.Rows
// returned belongs to the shared request tx, the middleware will commit
// it after the handler returns. Caller's `defer rows.Close()` is still
// valid because pgx.Rows.Close() on a tx-bound result set just releases
// the cursor without closing the tx.
//
// Slow path (no shared tx, scope present): open a per-call read-only tx,
// SET LOCAL, run the query, return rows wrapped with txBoundRows that
// commits the tx on Close().
//
// Bypass path (no scope at all): plain pool call.
func (r *RLSPool) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if tx := RequestTxFromCtx(ctx); tx != nil {
		return tx.Query(ctx, sql, args...)
	}
	scope, hasScope := scopeFromCtx(ctx)
	if !hasScope {
		return r.pool.Query(ctx, sql, args...)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("rlspool: begin tx: %w", err)
	}
	if err := setScope(ctx, tx, scope); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	// Wrap rows so that Close() commits the tx. sqlc-generated callers
	// always defer rows.Close().
	return &txBoundRows{Rows: rows, tx: tx, ctx: ctx}, nil
}

// CopyFrom satisfies sqlc's DBTX in packages that opt into bulk-copy
// (e.g. sales/commission). Three-path dispatch like Exec.
func (r *RLSPool) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	if tx := RequestTxFromCtx(ctx); tx != nil {
		return tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
	}
	scope, hasScope := scopeFromCtx(ctx)
	if !hasScope {
		return r.pool.CopyFrom(ctx, tableName, columnNames, rowSrc)
	}
	var n int64
	err := r.runWithScope(ctx, scope, func(tx pgx.Tx) error {
		var copyErr error
		n, copyErr = tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
		return copyErr
	})
	return n, err
}

// QueryRow satisfies sqlc's DBTX. Three-path dispatch like Exec. Fast
// path (request tx attached) returns the row directly; slow path opens
// a per-call tx that commits on Scan.
func (r *RLSPool) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if tx := RequestTxFromCtx(ctx); tx != nil {
		return tx.QueryRow(ctx, sql, args...)
	}
	scope, hasScope := scopeFromCtx(ctx)
	if !hasScope {
		return r.pool.QueryRow(ctx, sql, args...)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errRow{err: fmt.Errorf("rlspool: begin tx: %w", err)}
	}
	if err := setScope(ctx, tx, scope); err != nil {
		_ = tx.Rollback(ctx)
		return errRow{err: err}
	}
	row := tx.QueryRow(ctx, sql, args...)
	return &txBoundRow{Row: row, tx: tx, ctx: ctx}
}

// runWithScope runs fn inside a tx with SET LOCAL applied, then commits.
func (r *RLSPool) runWithScope(ctx context.Context, scope p9context.RLSScope, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("rlspool: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setScope(ctx, tx, scope); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// scopeFromCtx reads RLSScope from ctx and reports whether it carries any
// scope IDs. Returns (zero, false) when missing entirely so the caller can
// fall back to the plain pool.
func scopeFromCtx(ctx context.Context) (p9context.RLSScope, bool) {
	scope := p9context.MustRLSScope(ctx) // returns zero-value if absent
	if scope.TenantID == "" && scope.CompanyID == "" && scope.BranchID == "" {
		return scope, false
	}
	return scope, true
}

// setScope SET LOCALs the three RLS session variables on tx via the
// canonical helper. Wraps errors with the rlspool: prefix so existing
// log filters keep working.
func setScope(ctx context.Context, tx pgx.Tx, scope p9context.RLSScope) error {
	if err := rlssession.SetLocal(ctx, tx, scope); err != nil {
		return fmt.Errorf("rlspool: %w", err)
	}
	return nil
}

// txBoundRows wraps pgx.Rows so Close() commits the underlying tx.
type txBoundRows struct {
	pgx.Rows
	tx     pgx.Tx
	ctx    context.Context
	closed bool
}

func (r *txBoundRows) Close() {
	if r.closed {
		return
	}
	r.closed = true
	r.Rows.Close()
	// Best-effort commit. If the iteration errored, the caller already
	// has the error; we just need to release the tx.
	_ = r.tx.Commit(r.ctx)
}

// txBoundRow wraps a pgx.Row so Scan() commits the underlying tx after
// the read.
type txBoundRow struct {
	pgx.Row
	tx  pgx.Tx
	ctx context.Context
}

func (r *txBoundRow) Scan(dest ...any) error {
	scanErr := r.Row.Scan(dest...)
	// Commit regardless of scanErr — the read transaction is done either
	// way, and the caller has the original error to act on.
	_ = r.tx.Commit(r.ctx)
	return scanErr
}

// errRow is a pgx.Row that always returns the wrapped error on Scan.
// Used when BeginTx or SET LOCAL fails before the actual query runs.
type errRow struct{ err error }

func (e errRow) Scan(_ ...any) error { return e.err }
