// Package server hosts the generic transport-server framework that the
// platform's gRPC, HTTP, and ConnectRPC servers share.
//
// The minimal contract:
//
//	type Server interface {
//	    Run(ctx context.Context) error
//	}
//
// Subpackages implement Server for each transport:
//
//   - server/grpc   — gRPC with interceptor chains + tenant middleware
//   - server/http   — gRPC-Gateway-backed REST with CORS
//   - server/connect — ConnectRPC handler mount
//
// MultiServer composes several Server implementations and runs them in
// parallel until any one fails or ctx is cancelled. On shutdown it calls
// each server's Stop (if implemented) giving the graceful-shutdown grace
// window from config:
//
//	mux := server.NewMultiServer(grpcSrv, httpSrv, connectSrv)
//	return mux.Run(ctx)
//
// Graceful-shutdown ordering is stable across restarts — servers added
// first are stopped first so streaming connections drain in the same
// sequence they were established.
package server
