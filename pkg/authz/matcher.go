package authz

// PermissionRequirement defines the minimal permission needed for a gRPC method
// This is used to map full gRPC method names to required permissions

var methodPermissionMap = map[string]PermissionRequirement{
	"/formbuilder.api.v1.FormService/CreateForm": {
		Namespace: "form",
		Resource:  "form",
		Action:    "create",
	},
	"/identity.auth.api.v1.role.RoleService/DeleteRole": {
		Namespace: "user.role",
		Resource:  "id",
		Action:    "delete",
	},
	// Add more mappings here as needed
}

// matchMethodToPermission returns the PermissionRequirement for a gRPC method
func matchMethodToPermission(method string) *PermissionRequirement {
	if perm, ok := methodPermissionMap[method]; ok {
		return &perm
	}
	return nil // unprotected or public method
}

// matchMethodToPermission defines method-to-permission mapping
