package tenantctx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestATenantSurvivesTheContext(t *testing.T) {
	ctx := With(context.Background(), "T_01HZ")
	got, err := From(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "T_01HZ" {
		t.Errorf("tenant = %q", got)
	}
}

// A context with no tenant must say so rather than yield the empty string. The
// empty string reaches the database as a cleared parameter, which the policies
// refuse — but only because they were written to; a caller checking for "" would
// be one refactor away from treating it as a tenant.

// A context with no tenant must say so rather than yield the empty string. The
// empty string reaches the database as a cleared parameter, which the policies
// refuse — but only because they were written to; a caller checking for "" would
// be one refactor away from treating it as a tenant.
func TestAContextWithNoTenantIsAnError(t *testing.T) {
	if _, err := From(context.Background()); !errors.Is(err, ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}
	if _, err := From(With(context.Background(), "")); !errors.Is(err, ErrNoTenant) {
		t.Errorf("an empty tenant was accepted as a tenant")
	}
}

// The context key is a private type, so a value stored under a plain string —
// which is what any other package would use — cannot be mistaken for a tenant.

// The context key is a private type, so a value stored under a plain string —
// which is what any other package would use — cannot be mistaken for a tenant.
func TestAStringKeyCannotImpersonateATenant(t *testing.T) {
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "T_SOMEBODY_ELSE")
	//nolint:staticcheck // deliberately the wrong key shape
	ctx = context.WithValue(ctx, "app.tenant_id", "T_SOMEBODY_ELSE")

	if _, err := From(ctx); !errors.Is(err, ErrNoTenant) {
		t.Error("a value stored under a different key was read as the tenant")
	}
}

func TestATenantThatCannotBeOneIsRejected(t *testing.T) {
	for _, tc := range []struct{ name, value, wantReason string }{
		{"empty", "", "empty"},
		{"newline", "T_01\nHZ", "control character"},
		{"carriage return", "T_01\rHZ", "control character"},
		{"null byte", "T_01\x00HZ", "control character"},
		{"padded", " T_01HZ ", "whitespace"},
		{"absurdly long", strings.Repeat("T", 65), "longer than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(tc.value)
			if err == nil {
				t.Fatalf("%q was accepted as a tenant identifier", tc.value)
			}
			var bad *ErrBadTenant
			if !errors.As(err, &bad) {
				t.Fatalf("err = %v, want ErrBadTenant", err)
			}
			if !strings.Contains(bad.Reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", bad.Reason, tc.wantReason)
			}
		})
	}
}

func TestARealIdentifierIsAccepted(t *testing.T) {
	for _, v := range []string{"T_01HZ8Q9W2K3M4N5P6R7S8T9V", "tenant-1", "01HZ8Q9W2K3M4N5P6R7S8T9V"} {
		if err := Check(v); err != nil {
			t.Errorf("%q was rejected: %v", v, err)
		}
	}
}

// Apply must bind the tenant rather than build a statement around it. PostgreSQL
// refuses parameters on SET, which is why the older code in this tree
// interpolates and then validates the value character by character; set_config
// is a function and takes them, so the value never reaches the parser at all.
