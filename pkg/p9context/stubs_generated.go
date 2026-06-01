package p9context

import "context"

// TenantContext is a simple struct for tenant information used by event publishers.
type TenantContext struct {
	ID   string
	Name string
}

// GetTenantID retrieves the tenant ID from context.
// Convenience alias for TenantID - checks connection info first, then user context.
func GetTenantID(ctx context.Context) string {
	if id := TenantID(ctx); id != "" {
		return id
	}
	return UserTenantID(ctx)
}

// GetTenant retrieves tenant info from context as a TenantContext.
// Checks FromCurrentTenant first, then falls back to user context.
func GetTenant(ctx context.Context) TenantContext {
	if info, ok := FromCurrentTenant(ctx); ok {
		return TenantContext{
			ID:   info.GetId(),
			Name: info.GetName(),
		}
	}
	return TenantContext{
		ID: UserTenantID(ctx),
	}
}

// GetUserID retrieves the user ID from context.
// Convenience alias for UserID.
func GetUserID(ctx context.Context) string {
	return UserID(ctx)
}

// GetCompanyID retrieves the company ID from context.
// Convenience alias for UserCompanyID.
func GetCompanyID(ctx context.Context) string {
	return UserCompanyID(ctx)
}

// GetBranchID retrieves the branch ID from context.
// Convenience alias for UserBranchID.
func GetBranchID(ctx context.Context) string {
	return UserBranchID(ctx)
}

// GetUserContext retrieves the user context from context.
// Convenience alias for FromUserContext.
func GetUserContext(ctx context.Context) (*UserContext, bool) {
	return FromUserContext(ctx)
}
