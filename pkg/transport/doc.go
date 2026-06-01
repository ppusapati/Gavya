// Package transport defines the transport-agnostic abstractions that every
// concrete server (gRPC / HTTP / ConnectRPC) implements.
//
// Core types:
//
//	type Server interface {
//	    Start(ctx context.Context) error
//	    Stop(ctx context.Context) error
//	}
//	type Endpointer interface {
//	    Endpoint() (*url.URL, error)
//	}
//	type Header interface {      // minimal request header reader
//	    Get(key string) string
//	    Set(key, value string)
//	    Values(key string) []string
//	}
//	type Transporter interface {
//	    Kind() Kind
//	    Endpoint() string
//	    Operation() string
//	    RequestHeader() Header
//	    ReplyHeader() Header
//	}
//
// Kind enumerates the transport variants (KindGRPC, KindHTTP, KindConnect).
// Transporter is planted on ctx via NewServerContext / FromServerContext so
// middleware + handlers can read the currently-active transport without
// taking a dependency on the concrete server package.
//
// See packages/server for the concrete Server implementations built on
// top of this abstraction.
package transport
