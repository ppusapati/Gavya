package circuitbreaker

import (
	"context"
	"fmt"
	"time"
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries     int           // Maximum number of retries
	InitialBackoff time.Duration // Initial backoff duration
	MaxBackoff     time.Duration // Maximum backoff duration
	BackoffFactor  float64       // Multiplier for exponential backoff
	Jitter         bool          // Add random jitter to backoff
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		BackoffFactor:  2.0,
		Jitter:         true,
	}
}

// RetryWithBackoff executes a function with exponential backoff retry logic
func RetryWithBackoff(ctx context.Context, config RetryConfig, fn func(context.Context) error) error {
	var lastErr error
	backoff := config.InitialBackoff

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		if attempt < config.MaxRetries {
			// Wait before retrying
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Calculate next backoff
			backoff = time.Duration(float64(backoff) * config.BackoffFactor)
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// ExecuteWithCircuitBreaker executes a function with circuit breaker and retry logic
func ExecuteWithCircuitBreaker(ctx context.Context, cb *CircuitBreaker, key string, retryConfig RetryConfig, fn func(context.Context) error) error {
	return RetryWithBackoff(ctx, retryConfig, func(ctx context.Context) error {
		return cb.Execute(ctx, key, func() error {
			return fn(ctx)
		})
	})
}
