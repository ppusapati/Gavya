package interceptors

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"
)

// LoggingInterceptorOption configures the Logging interceptor.
type LoggingInterceptorOption func(*loggingConfig)

type loggingConfig struct {
	// logger stores a pointer so nil-check (no-logger path) works. Helper is
	// a struct, not a pointer alias — 2026-04-19 B.1 sweep changed this from
	// `p9log.Helper` (value) to `*p9log.Helper` to match p9log's API.
	logger            *p9log.Helper
	logRequestStart   bool
	logRequestEnd     bool
	slowRequestThresh time.Duration
}

// WithLogger sets the logger for the interceptor.
func WithLogger(logger *p9log.Helper) LoggingInterceptorOption {
	return func(c *loggingConfig) {
		c.logger = logger
	}
}

// WithLogRequestStart enables/disables logging at request start.
func WithLogRequestStart(enabled bool) LoggingInterceptorOption {
	return func(c *loggingConfig) {
		c.logRequestStart = enabled
	}
}

// WithLogRequestEnd enables/disables logging at request end.
func WithLogRequestEnd(enabled bool) LoggingInterceptorOption {
	return func(c *loggingConfig) {
		c.logRequestEnd = enabled
	}
}

// WithSlowRequestThreshold sets the threshold for logging slow requests.
// Requests taking longer than this will be logged as warnings.
func WithSlowRequestThreshold(threshold time.Duration) LoggingInterceptorOption {
	return func(c *loggingConfig) {
		c.slowRequestThresh = threshold
	}
}

// LoggingInterceptor returns a Connect interceptor that logs requests.
// It logs request start, end, duration, and errors.
func LoggingInterceptor(opts ...LoggingInterceptorOption) connect.UnaryInterceptorFunc {
	cfg := &loggingConfig{
		logRequestStart:   true,
		logRequestEnd:     true,
		slowRequestThresh: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			procedure := req.Spec().Procedure

			// Get request ID for correlation
			requestID := p9context.RequestID(ctx)

			// Get logger from config or create from context. p9log.Context
			// already returns *p9log.Helper — no wrapping needed.
			logger := cfg.logger
			if logger == nil {
				logger = p9log.Context(ctx)
			}

			// Log request start
			if cfg.logRequestStart {
				logger.Infof("[%s] Request started: %s", requestID, procedure)
			}

			// Call next handler
			resp, err := next(ctx, req)

			// Calculate duration
			duration := time.Since(start)

			// Log request end
			if cfg.logRequestEnd {
				if err != nil {
					logger.Errorf("[%s] Request failed: %s (duration: %v, error: %v)",
						requestID, procedure, duration, err)
				} else if duration > cfg.slowRequestThresh {
					logger.Warnf("[%s] Slow request: %s (duration: %v)",
						requestID, procedure, duration)
				} else {
					logger.Infof("[%s] Request completed: %s (duration: %v)",
						requestID, procedure, duration)
				}
			}

			return resp, err
		}
	}
}
