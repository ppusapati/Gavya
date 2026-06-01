// Package interceptors provides ConnectRPC interceptors for the unified p9context architecture.
// These interceptors mirror the gRPC middleware functionality but are adapted for ConnectRPC.
package interceptors

import (
	"connectrpc.com/connect"
)

// ChainInterceptors creates a connect.Option that chains multiple interceptors.
// Interceptors are executed in the order they are provided.
func ChainInterceptors(interceptors ...connect.Interceptor) connect.Option {
	return connect.WithInterceptors(interceptors...)
}
