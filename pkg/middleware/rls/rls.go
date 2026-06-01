package rls

import (
	"context"

	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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

// RLSMiddleware provides Row-Level Security scope middleware for gRPC services.
// It sets the RLS scope in the context based on the authenticated user's context.
type RLSMiddleware struct {
	// requireBranch determines if branch_id is required from X-Branch-ID header.
	// If false, uses the user's default branch from JWT.
	requireBranch bool
}

// RLSMiddlewareOption is a functional option for configuring RLSMiddleware.
type RLSMiddlewareOption func(*RLSMiddleware)

// WithRequireBranch sets whether the X-Branch-ID header is required.
func WithRequireBranch(required bool) RLSMiddlewareOption {
	return func(m *RLSMiddleware) {
		m.requireBranch = required
	}
}

// NewRLSMiddleware creates a new RLS middleware.
func NewRLSMiddleware(opts ...RLSMiddlewareOption) *RLSMiddleware {
	m := &RLSMiddleware{
		requireBranch: false, // Default: use user's default branch
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GrpcRLSMiddleware is a gRPC unary interceptor that sets the RLS scope.
// It extracts tenant_id, company_id from UserContext (set by auth middleware)
// and branch_id from X-Branch-ID header or user's default branch.
func (m *RLSMiddleware) GrpcRLSMiddleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Get user context (should be set by auth middleware)
	user, ok := p9context.FromUserContext(ctx)
	if !ok || user == nil {
		p9log.Context(ctx).Debug("rls middleware: no user context, skipping RLS scope")
		return handler(ctx, req)
	}

	// Start with user's default values
	tenantID := user.TenantID
	companyID := user.CompanyID
	branchID := user.BranchID

	// Check for branch override from header
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if branchHeaders := md.Get("x-branch-id"); len(branchHeaders) > 0 && branchHeaders[0] != "" {
			branchID = branchHeaders[0]
			p9log.Context(ctx).Debugf("rls middleware: using branch_id from header: %s", branchID)
		}
	}

	// Set RLS scope in context
	ctx = p9context.NewRLSScope(ctx, p9context.RLSScope{
		TenantID:  tenantID,
		CompanyID: companyID,
		BranchID:  branchID,
	})

	p9log.Context(ctx).Debugf("rls middleware: set scope tenant=%s, company=%s, branch=%s",
		tenantID, companyID, branchID)

	return handler(ctx, req)
}

// GrpcRLSMiddlewareCompanyLevel is a gRPC unary interceptor that sets company-level RLS scope.
// Use this for endpoints that operate at company level (no branch filtering).
func (m *RLSMiddleware) GrpcRLSMiddlewareCompanyLevel(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Get user context (should be set by auth middleware)
	user, ok := p9context.FromUserContext(ctx)
	if !ok || user == nil {
		p9log.Context(ctx).Debug("rls middleware (company): no user context, skipping RLS scope")
		return handler(ctx, req)
	}

	// Set company-level RLS scope (no branch)
	ctx = p9context.NewRLSScopeCompanyOnly(ctx, user.TenantID, user.CompanyID)

	p9log.Context(ctx).Debugf("rls middleware (company): set scope tenant=%s, company=%s",
		user.TenantID, user.CompanyID)

	return handler(ctx, req)
}

// GrpcRLSMiddlewareTenantLevel is a gRPC unary interceptor that sets tenant-level RLS scope.
// Use this for endpoints that operate at tenant level (no company/branch filtering).
func (m *RLSMiddleware) GrpcRLSMiddlewareTenantLevel(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Get user context (should be set by auth middleware)
	user, ok := p9context.FromUserContext(ctx)
	if !ok || user == nil {
		p9log.Context(ctx).Debug("rls middleware (tenant): no user context, skipping RLS scope")
		return handler(ctx, req)
	}

	// Set tenant-level RLS scope (no company/branch)
	ctx = p9context.NewRLSScopeTenantOnly(ctx, user.TenantID)

	p9log.Context(ctx).Debugf("rls middleware (tenant): set scope tenant=%s", user.TenantID)

	return handler(ctx, req)
}

// SetRLSScopeFromUser is a helper function to set RLS scope from user context.
// Can be called directly in handlers if middleware approach is not suitable.
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

// BranchLevelInterceptor returns a gRPC unary interceptor for branch-level RLS scoping.
// This is the most granular level, filtering by tenant + company + branch.
func BranchLevelInterceptor(opts ...RLSMiddlewareOption) grpc.UnaryServerInterceptor {
	m := NewRLSMiddleware(opts...)
	return m.GrpcRLSMiddleware
}

// CompanyLevelInterceptor returns a gRPC unary interceptor for company-level RLS scoping.
// Use for entities that are scoped to company but not branch (e.g., Chart of Accounts, Party).
func CompanyLevelInterceptor(opts ...RLSMiddlewareOption) grpc.UnaryServerInterceptor {
	m := NewRLSMiddleware(opts...)
	return m.GrpcRLSMiddlewareCompanyLevel
}

// TenantLevelInterceptor returns a gRPC unary interceptor for tenant-level RLS scoping.
// Use for entities that are scoped to tenant only (e.g., User, Role, Permission).
func TenantLevelInterceptor(opts ...RLSMiddlewareOption) grpc.UnaryServerInterceptor {
	m := NewRLSMiddleware(opts...)
	return m.GrpcRLSMiddlewareTenantLevel
}
