package p9context

import (
	"context"
)

// UserContext contains authenticated user information extracted from JWT claims.
// This is set by the auth middleware after validating the JWT token.
type UserContext struct {
	UserID      string
	TenantID    string
	CompanyID   string
	BranchID    string   // May be empty for company-scoped entities
	Role        string
	Permissions []string
}

type userContextKey struct{}

// NewUserContext creates a new context with the user context.
func NewUserContext(ctx context.Context, user UserContext) context.Context {
	return context.WithValue(ctx, userContextKey{}, &user)
}

// FromUserContext retrieves the user context from context.
// Returns nil and false if not present.
func FromUserContext(ctx context.Context) (*UserContext, bool) {
	v, ok := ctx.Value(userContextKey{}).(*UserContext)
	if ok && v != nil {
		return v, true
	}
	return nil, false
}

// MustUserContext retrieves the user context from context.
// Panics if not present.
func MustUserContext(ctx context.Context) UserContext {
	v, ok := FromUserContext(ctx)
	if !ok || v == nil {
		panic("user context not found in context")
	}
	return *v
}

// MustUserID retrieves the user ID from context, panics if not present.
func MustUserID(ctx context.Context) string {
	return MustUserContext(ctx).UserID
}

// UserID retrieves the user ID from context.
// Returns empty string if not present.
func UserID(ctx context.Context) string {
	if user, ok := FromUserContext(ctx); ok {
		return user.UserID
	}
	return ""
}

// UserTenantID retrieves the tenant ID from user context.
// Returns empty string if not present.
// Note: Use this for user's tenant, TenantID() from saas_context for connection info.
func UserTenantID(ctx context.Context) string {
	if user, ok := FromUserContext(ctx); ok {
		return user.TenantID
	}
	return ""
}

// UserCompanyID retrieves the company ID from user context.
// Returns empty string if not present.
func UserCompanyID(ctx context.Context) string {
	if user, ok := FromUserContext(ctx); ok {
		return user.CompanyID
	}
	return ""
}

// UserBranchID retrieves the branch ID from user context.
// Returns empty string if not present (company-scoped entities may not have branch).
func UserBranchID(ctx context.Context) string {
	if user, ok := FromUserContext(ctx); ok {
		return user.BranchID
	}
	return ""
}

// UserRole retrieves the user's role from context.
// Returns empty string if not present.
func UserRole(ctx context.Context) string {
	if user, ok := FromUserContext(ctx); ok {
		return user.Role
	}
	return ""
}

// UserPermissions retrieves the user's permissions from context.
// Returns nil if not present.
func UserPermissions(ctx context.Context) []string {
	if user, ok := FromUserContext(ctx); ok {
		return user.Permissions
	}
	return nil
}

// HasPermission checks if the user has a specific permission.
// Returns false if user context is not present or permission is not found.
func HasPermission(ctx context.Context, permission string) bool {
	permissions := UserPermissions(ctx)
	if permissions == nil {
		return false
	}
	for _, p := range permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// HasAnyPermission checks if the user has any of the specified permissions.
// Returns false if user context is not present or none of the permissions are found.
func HasAnyPermission(ctx context.Context, permissions ...string) bool {
	for _, p := range permissions {
		if HasPermission(ctx, p) {
			return true
		}
	}
	return false
}

// HasAllPermissions checks if the user has all of the specified permissions.
// Returns false if user context is not present or any permission is missing.
func HasAllPermissions(ctx context.Context, permissions ...string) bool {
	if len(permissions) == 0 {
		return true
	}
	for _, p := range permissions {
		if !HasPermission(ctx, p) {
			return false
		}
	}
	return true
}

// HasUserContext returns true if the context has user context set.
func HasUserContext(ctx context.Context) bool {
	_, ok := FromUserContext(ctx)
	return ok
}

// IsAuthenticated returns true if the context has a valid user context with a user ID.
func IsAuthenticated(ctx context.Context) bool {
	return UserID(ctx) != ""
}
