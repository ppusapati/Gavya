package p9context

import (
	"context"
)

// RLSScope contains the scope for PostgreSQL RLS (Row-Level Security) policies.
// These values are set as session variables (app.tenant_id, app.company_id, app.branch_id)
// on each database connection/transaction.
type RLSScope struct {
	TenantID  string
	CompanyID string
	BranchID  string
}

type rlsScopeCtx struct{}

// NewRLSScope creates a new context with the RLS scope.
func NewRLSScope(ctx context.Context, scope RLSScope) context.Context {
	return context.WithValue(ctx, rlsScopeCtx{}, &scope)
}

// NewRLSScopeFromIDs creates a new context with RLS scope from individual IDs.
func NewRLSScopeFromIDs(ctx context.Context, tenantID, companyID, branchID string) context.Context {
	return NewRLSScope(ctx, RLSScope{
		TenantID:  tenantID,
		CompanyID: companyID,
		BranchID:  branchID,
	})
}

// FromRLSScope retrieves the RLS scope from context.
// Returns nil if not present.
func FromRLSScope(ctx context.Context) *RLSScope {
	v, ok := ctx.Value(rlsScopeCtx{}).(*RLSScope)
	if ok {
		return v
	}
	return nil
}

// MustRLSScope retrieves the RLS scope from context.
// Falls back to tenant ID from connection info if RLS scope not explicitly set.
// Returns an empty scope if neither is available.
func MustRLSScope(ctx context.Context) RLSScope {
	if scope := FromRLSScope(ctx); scope != nil {
		return *scope
	}

	// Fallback: try to get tenant ID from connection info
	if tenantID := TenantID(ctx); tenantID != "" {
		return RLSScope{TenantID: tenantID}
	}

	// Last resort: try from current tenant
	if tenant, ok := FromCurrentTenant(ctx); ok && tenant.GetId() != "" {
		return RLSScope{TenantID: tenant.GetId()}
	}

	return RLSScope{}
}

// HasRLSScope returns true if the context has RLS scope set.
func HasRLSScope(ctx context.Context) bool {
	return FromRLSScope(ctx) != nil
}

// IsValid returns true if the scope has at least a tenant ID.
func (s RLSScope) IsValid() bool {
	return s.TenantID != ""
}

// IsValidForBranchScope returns true if the scope has tenant, company, and branch IDs.
// Use this for branch-scoped entities (Asset, Transaction, Journal Entry, Stock Movement).
func (s RLSScope) IsValidForBranchScope() bool {
	return s.TenantID != "" && s.CompanyID != "" && s.BranchID != ""
}

// IsValidForCompanyScope returns true if the scope has tenant and company IDs.
// Use this for company-scoped entities (Chart of Accounts, Party, Item, Tax Code, Location).
func (s RLSScope) IsValidForCompanyScope() bool {
	return s.TenantID != "" && s.CompanyID != ""
}

// IsValidForTenantScope returns true if the scope has a tenant ID.
// Use this for tenant-scoped entities (User, Role, Permission, Audit Log).
func (s RLSScope) IsValidForTenantScope() bool {
	return s.TenantID != ""
}

// NewRLSScopeCompanyOnly creates a new context with RLS scope for company-level operations.
// Branch ID is intentionally left empty for company-scoped entities.
func NewRLSScopeCompanyOnly(ctx context.Context, tenantID, companyID string) context.Context {
	return NewRLSScope(ctx, RLSScope{
		TenantID:  tenantID,
		CompanyID: companyID,
		BranchID:  "", // Explicitly empty for company-level
	})
}

// NewRLSScopeTenantOnly creates a new context with RLS scope for tenant-level operations.
// Company and Branch IDs are intentionally left empty for tenant-scoped entities.
func NewRLSScopeTenantOnly(ctx context.Context, tenantID string) context.Context {
	return NewRLSScope(ctx, RLSScope{
		TenantID:  tenantID,
		CompanyID: "", // Explicitly empty for tenant-level
		BranchID:  "",
	})
}

// NewRLSScopeFromUserContext creates RLS scope from the user context.
// Useful for auto-populating RLS scope from authenticated user.
func NewRLSScopeFromUserContext(ctx context.Context) context.Context {
	user, ok := FromUserContext(ctx)
	if !ok || user == nil {
		return ctx // Return unchanged if no user context
	}
	return NewRLSScope(ctx, RLSScope{
		TenantID:  user.TenantID,
		CompanyID: user.CompanyID,
		BranchID:  user.BranchID,
	})
}

// MustRLSScopeBranchLevel retrieves the RLS scope and validates it for branch-scoped entities.
// Panics if scope is invalid or missing required fields.
func MustRLSScopeBranchLevel(ctx context.Context) RLSScope {
	scope := MustRLSScope(ctx)
	if !scope.IsValidForBranchScope() {
		panic("RLS scope missing required fields for branch-level entity (tenant_id, company_id, branch_id)")
	}
	return scope
}

// MustRLSScopeCompanyLevel retrieves the RLS scope and validates it for company-scoped entities.
// Panics if scope is invalid or missing required fields.
func MustRLSScopeCompanyLevel(ctx context.Context) RLSScope {
	scope := MustRLSScope(ctx)
	if !scope.IsValidForCompanyScope() {
		panic("RLS scope missing required fields for company-level entity (tenant_id, company_id)")
	}
	return scope
}

// MustRLSScopeTenantLevel retrieves the RLS scope and validates it for tenant-scoped entities.
// Panics if scope is invalid or missing required fields.
func MustRLSScopeTenantLevel(ctx context.Context) RLSScope {
	scope := MustRLSScope(ctx)
	if !scope.IsValidForTenantScope() {
		panic("RLS scope missing required fields for tenant-level entity (tenant_id)")
	}
	return scope
}

// RLSScopeFromContext is an alias for MustRLSScope for clarity.
// Retrieves RLS scope with fallback chain.
func RLSScopeFromContext(ctx context.Context) RLSScope {
	return MustRLSScope(ctx)
}

// WithBranchID returns a new RLSScope with the specified branch ID.
// Useful when you need to override the branch for a specific operation.
func (s RLSScope) WithBranchID(branchID string) RLSScope {
	return RLSScope{
		TenantID:  s.TenantID,
		CompanyID: s.CompanyID,
		BranchID:  branchID,
	}
}

// WithoutBranch returns a new RLSScope without the branch ID.
// Useful for company-level operations from a branch-scoped context.
func (s RLSScope) WithoutBranch() RLSScope {
	return RLSScope{
		TenantID:  s.TenantID,
		CompanyID: s.CompanyID,
		BranchID:  "",
	}
}
