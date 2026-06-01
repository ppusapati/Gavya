package uow

import (
	"context"
)

// UnitOfWorkManager provides a convenient interface for transactional operations.
// It wraps a Factory and exposes WithTx/WithRead as methods.
type UnitOfWorkManager interface {
	WithTx(ctx context.Context, fn func(UnitOfWork) error) error
	WithRead(ctx context.Context, fn func(UnitOfWork) error) error
}

// unitOfWorkManager implements UnitOfWorkManager using a Factory.
type unitOfWorkManager struct {
	factory Factory
}

// NewManager creates a new UnitOfWorkManager from a Factory.
func NewManager(factory Factory) UnitOfWorkManager {
	return &unitOfWorkManager{factory: factory}
}

func (m *unitOfWorkManager) WithTx(ctx context.Context, fn func(UnitOfWork) error) error {
	return WithTx(ctx, m.factory, fn)
}

func (m *unitOfWorkManager) WithRead(ctx context.Context, fn func(UnitOfWork) error) error {
	return WithRead(ctx, m.factory, fn)
}

func WithTx(ctx context.Context, factory Factory, fn func(UnitOfWork) error) error {
	tx, err := factory.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func WithRead(ctx context.Context, factory Factory, fn func(UnitOfWork) error) error {
	tx, err := factory.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return fn(tx)
}
