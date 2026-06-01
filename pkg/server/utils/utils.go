package utils

import (
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// GetHeader defines a custom header matcher for gRPC Gateway
func GetHeader(key string) (string, bool) {
	if key == "X-Tenant-Id" {
		return "X-Tenant-id", true
	}
	if key == "X-Tenant-Name" {
		return "X-Tenant-Name", true
	}
	return runtime.DefaultHeaderMatcher(key)
}

// // Registers a Service with GRPC endpoint
// func RegisterService(
// 	ctx context.Context,
// 	mux *runtime.ServeMux,
// 	endpoint string,
// 	opts []grpc.DialOption,
// 	registerFunc func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) error,
// ) {
// 	p9log.Infow("Connecting to gRPC Server with endpoint", endpoint)
// 	err := registerFunc(ctx, mux, endpoint, opts)
// 	if err != nil {
// 		p9log.Fatal("Failed to serve: ", err)
// 	}
// }
