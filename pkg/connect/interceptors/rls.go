package interceptors

import (
	"context"

	"connectrpc.com/connect"

	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"
)

// ScopeLevel represents the RLS scoping level.
type ScopeLevel int

const (
	// ScopeLevelBranch is branch-level scoping (tenant + company + branch).
	ScopeLevelBranch ScopeLevel = iota
	// ScopeLevelCompany is company-level scoping (tenant + company, no branch).
	ScopeLevelCompany
	// ScopeLevelTenant is tenant-level scoping (tenant only).
	ScopeLevelTenant
)

// BranchIDHeader is the header for branch ID override.
const BranchIDHeader = "X-Branch-ID"

// RLSInterceptorOption configures the RLS interceptor.
type RLSInterceptorOption func(*rlsConfig)

type rlsConfig struct {
	scopeLevel    ScopeLevel
	requireBranch bool
}

// WithScopeLevel sets the RLS scope level.
func WithScopeLevel(level ScopeLevel) RLSInterceptorOption {
	return func(c *rlsConfig) {
		c.scopeLevel = level
	}
}

// WithRequireBranchHeader sets whether the X-Branch-ID header is required.
func WithRequireBranchHeader(required bool) RLSInterceptorOption {
	return func(c *rlsConfig) {
		c.requireBranch = required
	}
}

// RLSInterceptor returns a Connect interceptor that sets the RLS scope.
// It extracts tenant_id, company_id from UserContext (set by auth interceptor)
// and branch_id from X-Branch-ID header or user's default branch.
func RLSInterceptor(opts ...RLSInterceptorOption) connect.UnaryInterceptorFunc {
	cfg := &rlsConfig{
		scopeLevel:    ScopeLevelBranch,
		requireBranch: false,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Get user context (should be set by auth interceptor)
			user, ok := p9context.FromUserContext(ctx)
			if !ok || user == nil {
				p9log.Context(ctx).Debug("rls interceptor: no user context, skipping RLS scope")
				return next(ctx, req)
			}

			// Set RLS scope based on scope level
			switch cfg.scopeLevel {
			case ScopeLevelBranch:
				ctx = setBranchLevelScope(ctx, req, user, cfg.requireBranch)
			case ScopeLevelCompany:
				ctx = setCompanyLevelScope(ctx, user)
			case ScopeLevelTenant:
				ctx = setTenantLevelScope(ctx, user)
			}

			return next(ctx, req)
		}
	}
}

// setBranchLevelScope sets branch-level RLS scope (tenant + company + branch).
func setBranchLevelScope(ctx context.Context, req connect.AnyRequest, user *p9context.UserContext, requireBranch bool) context.Context {
	branchID := user.BranchID

	// Check for branch override from header
	if branchHeader := req.Header().Get(BranchIDHeader); branchHeader != "" {
		branchID = branchHeader
		p9log.Context(ctx).Debugf("rls interceptor: using branch_id from header: %s", branchID)
	}

	// If branch is required but not provided, log warning
	if requireBranch && branchID == "" {
		p9log.Context(ctx).Warn("rls interceptor: branch_id required but not provided")
	}

	ctx = p9context.NewRLSScope(ctx, p9context.RLSScope{
		TenantID:  user.TenantID,
		CompanyID: user.CompanyID,
		BranchID:  branchID,
	})

	p9log.Context(ctx).Debugf("rls interceptor: set branch scope tenant=%s, company=%s, branch=%s",
		user.TenantID, user.CompanyID, branchID)

	return ctx
}

// setCompanyLevelScope sets company-level RLS scope (tenant + company, no branch).
func setCompanyLevelScope(ctx context.Context, user *p9context.UserContext) context.Context {
	ctx = p9context.NewRLSScopeCompanyOnly(ctx, user.TenantID, user.CompanyID)

	p9log.Context(ctx).Debugf("rls interceptor: set company scope tenant=%s, company=%s",
		user.TenantID, user.CompanyID)

	return ctx
}

// setTenantLevelScope sets tenant-level RLS scope (tenant only).
func setTenantLevelScope(ctx context.Context, user *p9context.UserContext) context.Context {
	ctx = p9context.NewRLSScopeTenantOnly(ctx, user.TenantID)

	p9log.Context(ctx).Debugf("rls interceptor: set tenant scope tenant=%s", user.TenantID)

	return ctx
}

// BranchLevelInterceptor returns a Connect interceptor for branch-level RLS scoping.
// This is the most granular level, filtering by tenant + company + branch.
func BranchLevelInterceptor(opts ...RLSInterceptorOption) connect.UnaryInterceptorFunc {
	allOpts := append([]RLSInterceptorOption{WithScopeLevel(ScopeLevelBranch)}, opts...)
	return RLSInterceptor(allOpts...)
}

// CompanyLevelInterceptor returns a Connect interceptor for company-level RLS scoping.
// Use for entities that are scoped to company but not branch (e.g., Chart of Accounts, Party).
func CompanyLevelInterceptor(opts ...RLSInterceptorOption) connect.UnaryInterceptorFunc {
	allOpts := append([]RLSInterceptorOption{WithScopeLevel(ScopeLevelCompany)}, opts...)
	return RLSInterceptor(allOpts...)
}

// TenantLevelInterceptor returns a Connect interceptor for tenant-level RLS scoping.
// Use for entities that are scoped to tenant only (e.g., User, Role, Permission).
func TenantLevelInterceptor(opts ...RLSInterceptorOption) connect.UnaryInterceptorFunc {
	allOpts := append([]RLSInterceptorOption{WithScopeLevel(ScopeLevelTenant)}, opts...)
	return RLSInterceptor(allOpts...)
}

// SetRLSScopeFromUser is a helper function to set RLS scope from user context.
// Can be called directly in handlers if interceptor approach is not suitable.
func SetRLSScopeFromUser(ctx context.Context) context.Context {
	return p9context.NewRLSScopeFromUserContext(ctx)
}

// SetRLSScopeWithBranch sets RLS scope with a specific branch override.
// Useful when you need to switch branch context within a handler.
func SetRLSScopeWithBranch(ctx context.Context, branchID string) context.Context {
	user, ok := p9context.FromUserContext(ctx)
	if !ok || user == nil {
		return ctx
	}

	return p9context.NewRLSScope(ctx, p9context.RLSScope{
		TenantID:  user.TenantID,
		CompanyID: user.CompanyID,
		BranchID:  branchID,
	})
}
