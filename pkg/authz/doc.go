// Package authz provides JWT + RBAC plumbing for the gRPC / Connect
// interceptor layer.
//
// The package exposes:
//   - JWTConfig + InitJWTFromConfig — wires signing keys + expiry from config
//   - CustomClaims — the token payload shape used across all services
//   - UnaryInterceptor — a gRPC server interceptor that extracts the JWT,
//     validates it, and calls the supplied checkPermission callback before
//     letting the request through
//
// Callers typically compose authz.UnaryInterceptor into the server's
// interceptor chain alongside request-id / tenant / recovery middleware.
// The checkPermission callback is the service's authorization hook — it
// receives the resolved PermissionRequirement and returns either the
// CheckPermissionResponse (allow / deny + reason) or an error.
//
// See core/identity/pdp for the reference checkPermission implementation.
package authz
