package authz

import (
	"fmt"
	"strings"
)

// Effect represents whether a permission grants or denies access
type Effect int32

const (
	Effect_UNSPECIFIED Effect = 0
	Effect_GRANT       Effect = 1
	Effect_DENY        Effect = 2
)

// Permission represents an authorization permission
type Permission struct {
	Namespace string // e.g., "user", "tenant", "billing"
	Resource  string // e.g., "profile", "settings", "invoke"
	Action    string // e.g., "read", "write", "delete"
	Effect    Effect // GRANT or DENY
}

// String renders the permission in the canonical "namespace:resource:action"
// form used wherever a permission appears as a string (audit logs, JWT
// transport hops where the consumer is []string, error messages, RBAC
// lookup keys). Effect is intentionally omitted from the wire form because
// the canonical permission grant model is allowlist — DENY rules are not
// projected through this surface today, and adding them would silently
// change the meaning of every existing permission string in the system.
//
// This is the single source of truth for the format. Sprintf("%s:%s:%s",
// p.Namespace, p.Resource, p.Action) was previously duplicated across at
// least four call sites; all of them now route through here.
func (p Permission) String() string {
	return p.Namespace + ":" + p.Resource + ":" + p.Action
}

// ParsePermission is the inverse of Permission.String — it accepts a
// canonical "namespace:resource:action" string and returns the structured
// form, defaulting Effect to GRANT (matching the allowlist semantics
// described above). An input that does not split cleanly into exactly
// three non-empty segments is rejected with a descriptive error so silent
// data corruption is impossible.
//
// Used by the auth service when projecting wire-format []string permissions
// (the shape ValidateTokenResponse and Login token transport use) back into
// authz.Permission for storage in CustomClaims, and by any future call site
// that needs the reverse transform.
func ParsePermission(s string) (Permission, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return Permission{}, fmt.Errorf("authz: permission %q must have exactly 3 colon-separated segments (namespace:resource:action)", s)
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Permission{}, fmt.Errorf("authz: permission %q has empty segment(s); all of namespace, resource, action are required", s)
	}
	return Permission{
		Namespace: parts[0],
		Resource:  parts[1],
		Action:    parts[2],
		Effect:    Effect_GRANT,
	}, nil
}

// PermissionsToStrings projects a slice of structured Permissions to their
// canonical wire form. Returns nil for nil input (preserves the JSON
// "permissions" claim's omitempty behavior — a token without permissions
// must not embed an empty array, only a missing key).
func PermissionsToStrings(perms []Permission) []string {
	if len(perms) == 0 {
		return nil
	}
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = p.String()
	}
	return out
}

// PermissionsFromStrings is the inverse — used when the caller has the
// wire-format []string and needs to populate CustomClaims.Permissions
// (which is []Permission). Any malformed entry aborts with the same error
// ParsePermission produces so the caller can decide whether to log + skip
// or hard-fail; we never silently drop entries.
func PermissionsFromStrings(perms []string) ([]Permission, error) {
	if len(perms) == 0 {
		return nil, nil
	}
	out := make([]Permission, 0, len(perms))
	for _, s := range perms {
		p, err := ParsePermission(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// CheckPermissionResponse represents the result of a permission check
type CheckPermissionResponse struct {
	Allowed bool
	Effect  Effect
	Reason  string
}
