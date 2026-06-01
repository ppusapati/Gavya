// Package middleware is the composable request-interceptor framework used
// across gRPC, Connect, and HTTP servers.
//
// The contract is minimal:
//
//	type Handler func(ctx context.Context, req interface{}) (interface{}, error)
//	type Middleware func(Handler) Handler
//	func Chain(m ...Middleware) Middleware
//
// Chain wraps middlewares in REVERSE order — the first Middleware in the
// slice becomes the OUTERMOST wrapper so its pre-processing runs first and
// post-processing runs last, matching the natural "onion" layering.
//
// Registered middlewares (each in its own subpackage):
//
//   - middleware/dbmiddleware — tenant-to-DB-pool resolution (shared vs
//     independent pools). Runs before handler logic.
//   - middleware/tenant       — extracts X-Tenant-ID / X-Tenant-Name from
//     gRPC metadata / HTTP headers and plants it on p9context.
//   - middleware/localize     — i18n locale negotiation (Accept-Language).
//   - middleware/recovery     — panic → error translation.
//   - middleware/cache        — optional response caching for idempotent RPCs.
//
// When adding new middleware, follow the existing pattern: export a
// constructor returning Middleware, document the exact request-lifecycle
// hook point, and register it in the service's server-bootstrap chain.
package middleware
