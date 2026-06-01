// Package circuitbreaker provides a generic circuit breaker pattern implementation
// for building resilient distributed systems.
//
// Circuit breakers help prevent cascading failures in distributed systems by
// temporarily blocking requests to failing services, allowing them time to recover.
//
// # State Machine
//
// The circuit breaker has three states:
//
//   - CLOSED: Normal operation, requests are allowed
//   - OPEN: Requests are blocked, service is considered unhealthy
//   - HALF_OPEN: Testing state, limited requests are allowed to test recovery
//
// # State Transitions
//
//   - CLOSED → OPEN: When failure count reaches the threshold
//   - OPEN → HALF_OPEN: When recovery timeout elapses
//   - HALF_OPEN → CLOSED: When success count reaches the threshold
//   - HALF_OPEN → OPEN: On any failure
//
// # Usage
//
//	// Create storage and circuit breaker
//	storage := circuitbreaker.NewMemoryStorage()
//	cb := circuitbreaker.New(storage, logger)
//
//	// Simple usage with Execute
//	err := cb.Execute(ctx, "my-service", func() error {
//	    return callExternalService()
//	})
//
//	// Manual check and record
//	result, err := cb.Check(ctx, "my-service")
//	if err != nil {
//	    return err
//	}
//	if !result.Allowed {
//	    return errors.New("circuit open")
//	}
//
//	err = callExternalService()
//	if err != nil {
//	    cb.RecordFailure(ctx, "my-service")
//	    return err
//	}
//	cb.RecordSuccess(ctx, "my-service")
//
// # Custom Configuration
//
//	cfg := circuitbreaker.Config{
//	    Name:                "payment-gateway",
//	    FailureThreshold:    3,
//	    SuccessThreshold:    2,
//	    RecoveryTimeout:     time.Minute,
//	    HalfOpenMaxRequests: 5,
//	}
//	result, err := cb.CheckWithConfig(ctx, "payment:charge", cfg)
//
// # Storage Backends
//
// The package includes a MemoryStorage implementation. For distributed systems,
// implement the Storage interface with Redis or a database backend.
//
//	type Storage interface {
//	    Get(ctx context.Context, key string) (*CircuitState, error)
//	    Save(ctx context.Context, key string, state *CircuitState) error
//	    Delete(ctx context.Context, key string) error
//	    GetAll(ctx context.Context) (map[string]*CircuitState, error)
//	    GetByState(ctx context.Context, state State) (map[string]*CircuitState, error)
//	}
package circuitbreaker
