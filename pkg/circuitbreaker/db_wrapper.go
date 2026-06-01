package circuitbreaker

import (
	"context"
	"fmt"
	"time"

	"p9e.in/samavaya/packages/p9log"
)

// DBCircuitBreaker wraps database operations with circuit breaker protection.
// This is designed to protect against database connection issues and
// cascading failures in repository layers.
type DBCircuitBreaker struct {
	breaker *CircuitBreaker
	logger  *p9log.Helper
	prefix  string // Key prefix for this module (e.g., "asset", "vehicle")
}

// DBCircuitBreakerConfig holds configuration for database circuit breaker.
type DBCircuitBreakerConfig struct {
	// ModuleName is used as a prefix for circuit breaker keys (e.g., "asset", "vehicle")
	ModuleName string
	// FailureThreshold is the number of failures before opening the circuit
	FailureThreshold int32
	// SuccessThreshold is the number of successes needed to close the circuit
	SuccessThreshold int32
	// RecoveryTimeout is how long to wait before attempting recovery
	RecoveryTimeout time.Duration
}

// DefaultDBCircuitBreakerConfig returns sensible defaults for database operations.
func DefaultDBCircuitBreakerConfig(moduleName string) DBCircuitBreakerConfig {
	return DBCircuitBreakerConfig{
		ModuleName:       moduleName,
		FailureThreshold: 5,
		SuccessThreshold: 3,
		RecoveryTimeout:  30 * time.Second,
	}
}

// NewDBCircuitBreaker creates a new database circuit breaker.
func NewDBCircuitBreaker(logger p9log.Logger, cfg DBCircuitBreakerConfig) *DBCircuitBreaker {
	storage := NewMemoryStorage()

	breaker := New(storage, logger,
		WithDefaultConfig(Config{
			Name:                cfg.ModuleName,
			FailureThreshold:    cfg.FailureThreshold,
			SuccessThreshold:    cfg.SuccessThreshold,
			RecoveryTimeout:     cfg.RecoveryTimeout,
			HalfOpenMaxRequests: 3,
		}),
		WithCleanupInterval(5*time.Minute),
		WithMaxAge(10*time.Minute),
	)

	return &DBCircuitBreaker{
		breaker: breaker,
		logger:  p9log.NewHelper(p9log.With(logger, "component", "db-circuitbreaker")),
		prefix:  cfg.ModuleName,
	}
}

// buildKey creates a circuit breaker key for a repository operation.
func (d *DBCircuitBreaker) buildKey(repository, operation string) string {
	return fmt.Sprintf("db:%s:%s:%s", d.prefix, repository, operation)
}

// Execute wraps a database operation with circuit breaker protection.
// repository: The repository name (e.g., "asset", "category")
// operation: The operation name (e.g., "create", "getById", "list")
// fn: The database operation to execute
func (d *DBCircuitBreaker) Execute(ctx context.Context, repository, operation string, fn func() error) error {
	key := d.buildKey(repository, operation)
	return d.breaker.Execute(ctx, key, fn)
}

// ExecuteWithResult wraps a database operation that returns a result.
func (d *DBCircuitBreaker) ExecuteWithResult(ctx context.Context, repository, operation string, fn func() (interface{}, error)) (interface{}, error) {
	key := d.buildKey(repository, operation)

	result, err := d.breaker.Check(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("circuit breaker check failed: %w", err)
	}

	if !result.Allowed {
		return nil, &CircuitOpenError{
			Key:        key,
			State:      result.State,
			RecoveryAt: result.RecoveryAt,
			WaitTime:   result.WaitTime,
		}
	}

	// Execute the function
	res, fnErr := fn()

	// Record result
	if fnErr != nil {
		if recordErr := d.breaker.RecordFailure(ctx, key); recordErr != nil {
			d.logger.Warn("failed to record failure",
				"key", key,
				"error", recordErr,
			)
		}
		return nil, fnErr
	}

	if recordErr := d.breaker.RecordSuccess(ctx, key); recordErr != nil {
		d.logger.Warn("failed to record success",
			"key", key,
			"error", recordErr,
		)
	}

	return res, nil
}

// Check checks if operations are allowed for a repository.
func (d *DBCircuitBreaker) Check(ctx context.Context, repository, operation string) (*CheckResult, error) {
	key := d.buildKey(repository, operation)
	return d.breaker.Check(ctx, key)
}

// RecordSuccess records a successful operation.
func (d *DBCircuitBreaker) RecordSuccess(ctx context.Context, repository, operation string) error {
	key := d.buildKey(repository, operation)
	return d.breaker.RecordSuccess(ctx, key)
}

// RecordFailure records a failed operation.
func (d *DBCircuitBreaker) RecordFailure(ctx context.Context, repository, operation string) error {
	key := d.buildKey(repository, operation)
	return d.breaker.RecordFailure(ctx, key)
}

// Reset resets the circuit breaker for a specific repository operation.
func (d *DBCircuitBreaker) Reset(ctx context.Context, repository, operation string) error {
	key := d.buildKey(repository, operation)
	return d.breaker.Reset(ctx, key)
}

// ResetRepository resets all circuit breakers for a repository.
// Note: This implementation only resets the "default" operation.
// For full reset, track keys separately.
func (d *DBCircuitBreaker) ResetRepository(ctx context.Context, repository string) error {
	return d.Reset(ctx, repository, "default")
}

// GetStatus returns the status for a repository operation.
func (d *DBCircuitBreaker) GetStatus(ctx context.Context, repository, operation string) (*CircuitState, error) {
	key := d.buildKey(repository, operation)
	return d.breaker.GetStatus(ctx, key)
}

// StartBackgroundWorkers starts background cleanup and recovery workers.
func (d *DBCircuitBreaker) StartBackgroundWorkers(ctx context.Context) {
	d.breaker.StartBackgroundWorkers(ctx)
}

// Stop stops background workers.
func (d *DBCircuitBreaker) Stop() {
	d.breaker.Stop()
}

// IsCircuitOpen checks if a circuit is open for the given repository operation.
func (d *DBCircuitBreaker) IsCircuitOpen(ctx context.Context, repository, operation string) (bool, error) {
	result, err := d.Check(ctx, repository, operation)
	if err != nil {
		return false, err
	}
	return !result.Allowed, nil
}
