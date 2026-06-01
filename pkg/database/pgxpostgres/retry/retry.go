package retry

import (
	"context"
	"errors"
	"strings"
	"time"

	"p9e.in/samavaya/packages/p9log"

	"github.com/cenkalti/backoff/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RetryableOperation represents a database operation that can be retried
type RetryableOperation func(ctx context.Context) error

// IsRetryableError checks if the error is retryable
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Common PostgreSQL error codes that are safe to retry
		switch pgErr.Code {
		case "40001": // serialization_failure
		case "40P01": // deadlock_detected
		case "55P03": // lock_not_available
		case "08006": // connection_failure
		case "08001": // sqlclient_unable_to_establish_sqlconnection
		case "08004": // sqlserver_rejected_establishment_of_sqlconnection
			return true
		}
	}

	// Check for connection/timeout related errors
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, pgx.ErrTxClosed) {
		return true
	}

	return false
}

// WithRetry executes an operation with exponential backoff retry
func WithRetry(ctx context.Context, operation RetryableOperation, maxRetries uint64) error {
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxRetries)

	return backoff.RetryNotify(func() error {
		err := operation(ctx)
		if err != nil && !IsRetryableError(err) {
			return backoff.Permanent(err)
		}
		return err
	}, b, func(err error, duration time.Duration) {
		p9log.With(
			p9log.WithContext(ctx, p9log.DefaultLogger),
			"error", err,
			"retry_after", duration,
		).Log(p9log.LevelWarn, "Retrying database operation")
	})
}

// WithRetry retries a database operation with exponential backoff
func CustomRetry(ctx context.Context, operation func(context.Context) error) error {
	var lastErr error
	for i := 0; i < 3; i++ {
		if err := operation(ctx); err == nil {
			return nil
		} else {
			lastErr = err
			if !shouldRetry(err) {
				return err
			}
			time.Sleep(time.Duration(1<<uint(i)) * time.Second)
		}
	}
	return lastErr
}

// shouldRetry determines if an error should trigger a retry
func shouldRetry(err error) bool {
	// Add specific error types that should trigger a retry
	return errors.Is(err, pgx.ErrNoRows) ||
		strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connection reset")
}
