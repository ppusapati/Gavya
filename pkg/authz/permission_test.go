// Tests for the canonical permission string transform. These pin the
// "namespace:resource:action" wire format and the round-trip contract
// shared by Login (mints []string), ParseJWT (reads []Permission), and
// every middleware that projects []Permission back to []string for
// downstream consumers (UserContext, audit logs).
//
// Regression-safety: until 2026-04-26 four call sites duplicated the
// Sprintf for this transform. Centralizing it via Permission.String()
// is only safe if the format never drifts — these tests are the lock.

package authz

import (
	"testing"
)

func TestPermissionString(t *testing.T) {
	cases := []struct {
		name string
		in   Permission
		want string
	}{
		{
			name: "fully populated",
			in:   Permission{Namespace: "asset", Resource: "asset", Action: "list"},
			want: "asset:asset:list",
		},
		{
			name: "effect is intentionally NOT projected",
			in:   Permission{Namespace: "user", Resource: "profile", Action: "read", Effect: Effect_DENY},
			want: "user:profile:read",
		},
		{
			name: "single-letter segments still produce three colons of structure",
			in:   Permission{Namespace: "a", Resource: "b", Action: "c"},
			want: "a:b:c",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParsePermission(t *testing.T) {
	t.Run("canonical 3-segment string parses with default GRANT effect", func(t *testing.T) {
		p, err := ParsePermission("asset:asset:list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := Permission{Namespace: "asset", Resource: "asset", Action: "list", Effect: Effect_GRANT}
		if p != want {
			t.Fatalf("got %#v, want %#v", p, want)
		}
	})

	t.Run("rejects too-few segments", func(t *testing.T) {
		if _, err := ParsePermission("asset:asset"); err == nil {
			t.Fatal("expected error for 2-segment input")
		}
	})

	t.Run("rejects too-many segments", func(t *testing.T) {
		// Don't silently merge / discard; the caller's audit trail depends
		// on knowing that a permission was malformed.
		if _, err := ParsePermission("asset:asset:list:extra"); err == nil {
			t.Fatal("expected error for 4-segment input")
		}
	})

	t.Run("rejects empty segment in any position", func(t *testing.T) {
		bad := []string{"::action", "ns::action", "ns:res:", ":res:action"}
		for _, s := range bad {
			if _, err := ParsePermission(s); err == nil {
				t.Errorf("ParsePermission(%q) should have failed", s)
			}
		}
	})

	t.Run("rejects empty input", func(t *testing.T) {
		if _, err := ParsePermission(""); err == nil {
			t.Fatal("expected error for empty input")
		}
	})
}

func TestPermissionRoundTrip(t *testing.T) {
	// The composite contract: String -> ParsePermission -> String must
	// be the identity on a GRANT permission. This is what makes it safe
	// to ship a permission across a wire boundary as a string and rebuild
	// it on the other side.
	originals := []Permission{
		{Namespace: "asset", Resource: "asset", Action: "list", Effect: Effect_GRANT},
		{Namespace: "finance", Resource: "journal-entry", Action: "create", Effect: Effect_GRANT},
		{Namespace: "hr", Resource: "employee", Action: "delete", Effect: Effect_GRANT},
	}
	for _, original := range originals {
		s := original.String()
		parsed, err := ParsePermission(s)
		if err != nil {
			t.Errorf("round trip: ParsePermission(%q) failed: %v", s, err)
			continue
		}
		if parsed != original {
			t.Errorf("round trip: got %#v, want %#v", parsed, original)
		}
	}
}

func TestPermissionsToStringsAndBack(t *testing.T) {
	t.Run("nil in -> nil out preserves omitempty semantics", func(t *testing.T) {
		// CRITICAL: a JWT token with `permissions: []` is semantically
		// different from one missing the `permissions` key. The omitempty
		// tag on CustomClaims.Permissions only suppresses the key when
		// the slice is nil — an empty slice serializes to "permissions":[].
		// The transform must preserve nil.
		if got := PermissionsToStrings(nil); got != nil {
			t.Fatalf("nil in should produce nil out, got %v", got)
		}
		if got := PermissionsToStrings([]Permission{}); got != nil {
			t.Fatalf("empty slice should also collapse to nil to preserve omitempty, got %v", got)
		}
	})

	t.Run("non-empty slice round-trips through both transforms", func(t *testing.T) {
		original := []Permission{
			{Namespace: "asset", Resource: "asset", Action: "list", Effect: Effect_GRANT},
			{Namespace: "finance", Resource: "journal-entry", Action: "create", Effect: Effect_GRANT},
		}
		strs := PermissionsToStrings(original)
		want := []string{"asset:asset:list", "finance:journal-entry:create"}
		if len(strs) != len(want) {
			t.Fatalf("got %d strings, want %d", len(strs), len(want))
		}
		for i := range strs {
			if strs[i] != want[i] {
				t.Errorf("strs[%d] = %q, want %q", i, strs[i], want[i])
			}
		}
		back, err := PermissionsFromStrings(strs)
		if err != nil {
			t.Fatalf("PermissionsFromStrings: %v", err)
		}
		if len(back) != len(original) {
			t.Fatalf("round-trip: got %d perms, want %d", len(back), len(original))
		}
		for i := range back {
			if back[i] != original[i] {
				t.Errorf("round-trip[%d] = %#v, want %#v", i, back[i], original[i])
			}
		}
	})

	t.Run("PermissionsFromStrings rejects entire batch on first malformed entry", func(t *testing.T) {
		// All-or-nothing semantics: if any entry is bad, return an error
		// rather than silently filtering. The auth service uses this when
		// building a token; partial silent filtering would mean the user
		// got a token with FEWER permissions than the caller asked for —
		// hard to debug, exactly the wrong failure mode.
		_, err := PermissionsFromStrings([]string{"valid:perm:here", "bad-format-no-colons"})
		if err == nil {
			t.Fatal("expected error when any entry is malformed")
		}
	})

	t.Run("nil/empty input to PermissionsFromStrings is not an error", func(t *testing.T) {
		// Tokens without permissions are legitimate (anonymous user, system
		// user with role-based access, etc.). nil in -> nil out, no error.
		got, err := PermissionsFromStrings(nil)
		if err != nil {
			t.Fatalf("nil in should not error: %v", err)
		}
		if got != nil {
			t.Fatalf("nil in should produce nil out, got %v", got)
		}
		got, err = PermissionsFromStrings([]string{})
		if err != nil {
			t.Fatalf("empty slice should not error: %v", err)
		}
		if got != nil {
			t.Fatalf("empty slice should produce nil out, got %v", got)
		}
	})
}
