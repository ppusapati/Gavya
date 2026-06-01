package interceptors

import (
	"context"
	"fmt"
	"runtime/debug"

	"connectrpc.com/connect"

	"p9e.in/samavaya/packages/p9log"
)

// RecoveryInterceptorOption configures the Recovery interceptor.
type RecoveryInterceptorOption func(*recoveryConfig)

type recoveryConfig struct {
	logStackTrace bool
}

// WithLogStackTrace enables/disables logging of stack traces on panic.
func WithLogStackTrace(enabled bool) RecoveryInterceptorOption {
	return func(c *recoveryConfig) {
		c.logStackTrace = enabled
	}
}

// RecoveryInterceptor returns a Connect interceptor that recovers from panics.
// It logs the panic and returns an internal error to the client.
func RecoveryInterceptor(opts ...RecoveryInterceptorOption) connect.UnaryInterceptorFunc {
	cfg := &recoveryConfig{
		logStackTrace: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) {
			defer func() {
				if r := recover(); r != nil {
					procedure := req.Spec().Procedure

					if cfg.logStackTrace {
						p9log.Context(ctx).Errorf("panic recovered in %s: %v\n%s",
							procedure, r, string(debug.Stack()))
					} else {
						p9log.Context(ctx).Errorf("panic recovered in %s: %v", procedure, r)
					}

					err = connect.NewError(connect.CodeInternal, fmt.Errorf("internal server error"))
				}
			}()

			return next(ctx, req)
		}
	}
}
