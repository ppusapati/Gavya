package circuitbreaker

import (
	"context"
	"fmt"
)

// CircuitBreakerManager is an alias for ModuleCircuitBreakers for backward compatibility.
type CircuitBreakerManager = ModuleCircuitBreakers

// Execute is an alias for Wrap for backward compatibility.
func Execute[T any](ctx context.Context, cb *RepositoryCircuitBreaker, operation string, fn func(context.Context) (T, error)) (T, error) {
	if cb != nil {
		raw, err := cb.Wrap(ctx, operation, func() (interface{}, error) {
			return fn(ctx)
		})
		if err != nil {
			var zero T
			return zero, err
		}
		if typed, ok := raw.(T); ok {
			return typed, nil
		}
		var zero T
		return zero, nil
	}
	return fn(ctx)
}

// Wrap is a generic package-level convenience function that executes fn with circuit breaker protection.
// Supports multiple calling conventions:
//   - Wrap[T](cb, "op", fn)       where fn is func() (T, error)
//   - Wrap[T](ctx, cb, "op", fn)  where fn is func(context.Context) (T, error) or func() (T, error)
func Wrap[T any](args ...any) (T, error) {
	var zero T
	var cb *RepositoryCircuitBreaker
	var operation string
	var fn any

	switch len(args) {
	case 3:
		// Wrap(cb, "op", fn) or Wrap(ctx, "op", fn)
		switch first := args[0].(type) {
		case *RepositoryCircuitBreaker:
			cb = first
		default:
			// ctx passed as first arg but no cb — just use fn directly
		}
		operation, _ = args[1].(string)
		fn = args[2]
	case 4:
		// Wrap(ctx, cb, "op", fn)
		cb, _ = args[1].(*RepositoryCircuitBreaker)
		operation, _ = args[2].(string)
		fn = args[3]
	default:
		return zero, fmt.Errorf("circuitbreaker.Wrap: unsupported argument count %d", len(args))
	}

	// Execute the function
	var result T
	var err error

	wrappedFn := func() (interface{}, error) {
		switch f := fn.(type) {
		case func() (T, error):
			return f()
		case func(context.Context) (T, error):
			return f(context.Background())
		case func() (interface{}, error):
			r, e := f()
			if e != nil {
				return zero, e
			}
			if typed, ok := r.(T); ok {
				return typed, nil
			}
			return r, nil
		default:
			return zero, fmt.Errorf("circuitbreaker.Wrap: unsupported function type %T", fn)
		}
	}

	if cb != nil {
		raw, wErr := cb.Wrap(context.Background(), operation, wrappedFn)
		if wErr != nil {
			return zero, wErr
		}
		if typed, ok := raw.(T); ok {
			return typed, nil
		}
		return zero, nil
	}

	switch f := fn.(type) {
	case func() (T, error):
		result, err = f()
	case func(context.Context) (T, error):
		result, err = f(context.Background())
	case func() (interface{}, error):
		var r interface{}
		r, err = f()
		if err == nil {
			if typed, ok := r.(T); ok {
				result = typed
			}
		}
	default:
		return zero, fmt.Errorf("circuitbreaker.Wrap: unsupported function type %T", fn)
	}

	return result, err
}

// WrapNoResult is a package-level convenience function for void operations.
func WrapNoResult(args ...any) error {
	var cb *RepositoryCircuitBreaker
	var operation string
	var fn any

	switch len(args) {
	case 3:
		switch first := args[0].(type) {
		case *RepositoryCircuitBreaker:
			cb = first
		}
		operation, _ = args[1].(string)
		fn = args[2]
	case 4:
		cb, _ = args[1].(*RepositoryCircuitBreaker)
		operation, _ = args[2].(string)
		fn = args[3]
	default:
		return fmt.Errorf("circuitbreaker.WrapNoResult: unsupported argument count %d", len(args))
	}

	wrappedFn := func() error {
		switch f := fn.(type) {
		case func() error:
			return f()
		case func(context.Context) error:
			return f(context.Background())
		default:
			return fmt.Errorf("circuitbreaker.WrapNoResult: unsupported function type %T", fn)
		}
	}

	if cb != nil {
		return cb.WrapNoResult(context.Background(), operation, wrappedFn)
	}

	return wrappedFn()
}
