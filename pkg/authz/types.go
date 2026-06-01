package authz

// InjectedUserInfo holds parsed user claims injected into the gRPC context
// after token verification.
type InjectedUserInfo struct {
	UserID      string
	TenantID    string
	CompanyID   string       // User's company within the tenant
	BranchID    string       // User's default branch, may be empty for company-scoped entities
	Role        string
	Permissions []Permission // Optional: if JWT bundling is enabled
	SessionID   string       // Optional: links to database session for revocation support
}

// PermissionRequirement defines the required namespace/resource/action
// for a given RPC method or protected resource.
type PermissionRequirement struct {
	Namespace string
	Resource  string
	Action    string
}
