package p9context

import (
	"context"

	"p9e.in/samavaya/packages/database/pgxpostgres/validator"
)

type securityContextKey struct{}

// SetSecurityContext stores security context in request context
func SetSecurityContext(ctx context.Context, userID, role string) context.Context {
	secCtx := validator.NewSecurityContext(userID)
	return context.WithValue(ctx, securityContextKey{}, secCtx)
}

// GetSecurityContext retrieves security context from request context
// Returns nil if not found
func GetSecurityContext(ctx context.Context) *validator.SecurityContext {
	if secCtx, ok := ctx.Value(securityContextKey{}).(*validator.SecurityContext); ok {
		return secCtx
	}
	return nil
}

// GetSecurityContextOrDefault returns security context or "system" default
func GetSecurityContextOrDefault(ctx context.Context) *validator.SecurityContext {
	if secCtx := GetSecurityContext(ctx); secCtx != nil {
		return secCtx
	}
	// Default to system user for background tasks or unauthenticated requests
	return validator.NewSecurityContext("system")
}
