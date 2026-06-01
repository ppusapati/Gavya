package uow

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"p9e.in/samavaya/packages/errors"
)

// UnitOfWork manages a database transaction
type UnitOfWork interface {
	// Commit commits the transaction
	Commit(ctx context.Context) error
	// Rollback rolls back the transaction
	Rollback(ctx context.Context) error
	// Tx returns the underlying transaction for executing queries
	Tx() pgx.Tx
	// GetTx is an alias for Tx for backward compatibility
	GetTx() pgx.Tx
}

// Factory creates new UnitOfWork instances
type Factory interface {
	// Begin starts a new transaction
	Begin(ctx context.Context) (UnitOfWork, error)
	// New is an alias for Begin
	New(ctx context.Context) (UnitOfWork, error)
}

// pgxUnitOfWork implements UnitOfWork using pgx.Tx
type pgxUnitOfWork struct {
	tx     pgx.Tx
	closed bool
}

// NewUnitOfWork creates a new unit of work from a pgx transaction
func NewUnitOfWork(tx pgx.Tx) UnitOfWork {
	return &pgxUnitOfWork{
		tx:     tx,
		closed: false,
	}
}

// Tx returns the underlying transaction
func (uow *pgxUnitOfWork) Tx() pgx.Tx {
	return uow.tx
}

// GetTx is an alias for Tx for backward compatibility
func (uow *pgxUnitOfWork) GetTx() pgx.Tx {
	return uow.tx
}

// Commit commits the transaction
func (uow *pgxUnitOfWork) Commit(ctx context.Context) error {
	if uow.closed {
		return errors.BadRequest(
			"TRANSACTION_ALREADY_CLOSED",
			"Transaction has already been committed or rolled back",
		)
	}

	err := uow.tx.Commit(ctx)
	if err != nil {
		return errors.InternalServer("COMMIT_FAILED", fmt.Sprintf("Failed to commit transaction: %v", err))
	}

	uow.closed = true
	return nil
}

// Rollback rolls back the transaction
func (uow *pgxUnitOfWork) Rollback(ctx context.Context) error {
	if uow.closed {
		// Rollback is idempotent - already closed is not an error
		return nil
	}

	err := uow.tx.Rollback(ctx)
	if err != nil && err != pgx.ErrTxClosed {
		return errors.InternalServer("ROLLBACK_FAILED", fmt.Sprintf("Failed to rollback transaction: %v", err))
	}

	uow.closed = true
	return nil
}

// UnitOfWorkFactory is an alias for Factory for backward compatibility.
type UnitOfWorkFactory = Factory

// pgxFactory implements Factory using pgxpool.Pool
type pgxFactory struct {
	pool *pgxpool.Pool
}

// NewFactory creates a new Factory from a connection pool
func NewFactory(pool *pgxpool.Pool) Factory {
	return &pgxFactory{pool: pool}
}

// Begin starts a new transaction
func (f *pgxFactory) Begin(ctx context.Context) (UnitOfWork, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return nil, errors.InternalServer(
			"TRANSACTION_BEGIN_FAILED",
			fmt.Sprintf("Failed to begin transaction: %v", err),
		)
	}

	return NewUnitOfWork(tx), nil
}

// New is an alias for Begin for backward compatibility.
func (f *pgxFactory) New(ctx context.Context) (UnitOfWork, error) {
	return f.Begin(ctx)
}

// WithTransaction executes a function within a transaction
// Automatically commits on success or rolls back on error
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(uow UnitOfWork) error) error {
	factory := NewFactory(pool)
	return WithTx(ctx, factory, fn)
}
