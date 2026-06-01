package circuitbreaker

import (
	"context"
	"fmt"

	"p9e.in/samavaya/packages/p9log"
)

// RepositoryCircuitBreaker provides a simple interface for repositories
// to integrate circuit breaker protection.
type RepositoryCircuitBreaker struct {
	cb       *DBCircuitBreaker
	repoName string
}

// NewRepositoryCircuitBreaker creates a circuit breaker for a specific repository.
func NewRepositoryCircuitBreaker(cb *DBCircuitBreaker, repositoryName string) *RepositoryCircuitBreaker {
	return &RepositoryCircuitBreaker{
		cb:       cb,
		repoName: repositoryName,
	}
}

// Wrap wraps an operation with circuit breaker protection.
// It accepts either func() (interface{}, error) or func(context.Context) (interface{}, error).
// Example usage:
//
//	result, err := r.cb.Wrap(ctx, "GetByID", func() (interface{}, error) {
//	    return r.queries.GetAssetByID(ctx, params)
//	})
//
//	result, err := r.cb.Wrap(ctx, "GetByID", func(ctx context.Context) (interface{}, error) {
//	    return r.queries.GetAssetByID(ctx, params)
//	})
func (r *RepositoryCircuitBreaker) Wrap(ctx context.Context, operation string, fn any) (interface{}, error) {
	switch f := fn.(type) {
	case func() (interface{}, error):
		return r.cb.ExecuteWithResult(ctx, r.repoName, operation, f)
	case func(context.Context) (interface{}, error):
		return r.cb.ExecuteWithResult(ctx, r.repoName, operation, func() (interface{}, error) {
			return f(ctx)
		})
	default:
		return nil, fmt.Errorf("circuitbreaker.Wrap: unsupported function type %T", fn)
	}
}

// WrapNoResult wraps an operation that doesn't return a result.
// It accepts either func() error or func(context.Context) error.
// Example usage:
//
//	err := r.cb.WrapNoResult(ctx, "Delete", func() error {
//	    return r.queries.DeleteAsset(ctx, params)
//	})
//
//	err := r.cb.WrapNoResult(ctx, "Delete", func(ctx context.Context) error {
//	    return r.queries.DeleteAsset(ctx, params)
//	})
func (r *RepositoryCircuitBreaker) WrapNoResult(ctx context.Context, operation string, fn any) error {
	switch f := fn.(type) {
	case func() error:
		return r.cb.Execute(ctx, r.repoName, operation, f)
	case func(context.Context) error:
		return r.cb.Execute(ctx, r.repoName, operation, func() error {
			return f(ctx)
		})
	default:
		return fmt.Errorf("circuitbreaker.WrapNoResult: unsupported function type %T", fn)
	}
}

// Check checks if operations are allowed.
func (r *RepositoryCircuitBreaker) Check(ctx context.Context, operation string) (*CheckResult, error) {
	return r.cb.Check(ctx, r.repoName, operation)
}

// RecordSuccess records a successful operation.
func (r *RepositoryCircuitBreaker) RecordSuccess(ctx context.Context, operation string) error {
	return r.cb.RecordSuccess(ctx, r.repoName, operation)
}

// RecordFailure records a failed operation.
func (r *RepositoryCircuitBreaker) RecordFailure(ctx context.Context, operation string) error {
	return r.cb.RecordFailure(ctx, r.repoName, operation)
}

// IsOpen checks if the circuit is open for an operation.
func (r *RepositoryCircuitBreaker) IsOpen(ctx context.Context, operation string) (bool, error) {
	return r.cb.IsCircuitOpen(ctx, r.repoName, operation)
}

// ModuleCircuitBreakers holds circuit breakers for all repositories in a module.
// This is designed to be embedded in the Repositories struct.
type ModuleCircuitBreakers struct {
	db *DBCircuitBreaker
}

// NewModuleCircuitBreakers creates circuit breakers for a module.
func NewModuleCircuitBreakers(logger p9log.Logger, moduleName string) *ModuleCircuitBreakers {
	cfg := DefaultDBCircuitBreakerConfig(moduleName)
	return &ModuleCircuitBreakers{
		db: NewDBCircuitBreaker(logger, cfg),
	}
}

// NewModuleCircuitBreakersWithConfig creates circuit breakers with custom config.
func NewModuleCircuitBreakersWithConfig(logger p9log.Logger, cfg DBCircuitBreakerConfig) *ModuleCircuitBreakers {
	return &ModuleCircuitBreakers{
		db: NewDBCircuitBreaker(logger, cfg),
	}
}

// ForRepository creates a repository-specific circuit breaker.
func (m *ModuleCircuitBreakers) ForRepository(repositoryName string) *RepositoryCircuitBreaker {
	return NewRepositoryCircuitBreaker(m.db, repositoryName)
}

// DB returns the underlying DBCircuitBreaker for advanced usage.
func (m *ModuleCircuitBreakers) DB() *DBCircuitBreaker {
	return m.db
}

// Start starts background workers.
func (m *ModuleCircuitBreakers) Start(ctx context.Context) {
	m.db.StartBackgroundWorkers(ctx)
}

// Stop stops background workers.
func (m *ModuleCircuitBreakers) Stop() {
	m.db.Stop()
}
