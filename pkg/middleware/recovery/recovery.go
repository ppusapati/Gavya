package recovery

import (
	"context"
	"runtime"

	"p9e.in/samavaya/packages/errors"
	"p9e.in/samavaya/packages/p9log"

	"google.golang.org/grpc"
)

// HandlerFunc is recovery handler func.
type HandlerFunc func(ctx context.Context, req, err interface{}) error

// Recovery returns a gRPC unary server interceptor that recovers from panics.
// When a panic occurs, it logs the error with stack trace and returns an internal error to the client.
func Recovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (reply interface{}, err error) {
		defer func() {
			if rerr := recover(); rerr != nil {
				buf := make([]byte, 64<<10) //nolint:gomnd
				n := runtime.Stack(buf, false)
				buf = buf[:n]
				p9log.Context(ctx).Errorf("panic recovered in %s: %v\nstack: %s", info.FullMethod, rerr, buf)

				// Convert panic to error response - this is critical for proper error handling
				err = errors.InternalServer("PANIC", "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryWithHandler returns a gRPC unary server interceptor with a custom panic handler.
func RecoveryWithHandler(h HandlerFunc) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (reply interface{}, err error) {
		defer func() {
			if rerr := recover(); rerr != nil {
				buf := make([]byte, 64<<10) //nolint:gomnd
				n := runtime.Stack(buf, false)
				buf = buf[:n]
				p9log.Context(ctx).Errorf("panic recovered in %s: %v\nstack: %s", info.FullMethod, rerr, buf)

				// Use custom handler to convert panic to error
				err = h(ctx, req, rerr)
			}
		}()
		return handler(ctx, req)
	}
}
