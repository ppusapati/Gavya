package tenantdb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestATenantSurvivesTheContext(t *testing.T) {
	ctx := WithTenant(context.Background(), "T_01HZ")
	got, err := TenantFrom(ctx)
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
func TestAContextWithNoTenantIsAnError(t *testing.T) {
	if _, err := TenantFrom(context.Background()); !errors.Is(err, ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}
	if _, err := TenantFrom(WithTenant(context.Background(), "")); !errors.Is(err, ErrNoTenant) {
		t.Errorf("an empty tenant was accepted as a tenant")
	}
}

// The context key is a private type, so a value stored under a plain string —
// which is what any other package would use — cannot be mistaken for a tenant.
func TestAStringKeyCannotImpersonateATenant(t *testing.T) {
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "T_SOMEBODY_ELSE")
	//nolint:staticcheck // deliberately the wrong key shape
	ctx = context.WithValue(ctx, "app.tenant_id", "T_SOMEBODY_ELSE")

	if _, err := TenantFrom(ctx); !errors.Is(err, ErrNoTenant) {
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
			err := checkTenant(tc.value)
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
		if err := checkTenant(v); err != nil {
			t.Errorf("%q was rejected: %v", v, err)
		}
	}
}

// Apply must bind the tenant rather than build a statement around it. PostgreSQL
// refuses parameters on SET, which is why the older code in this tree
// interpolates and then validates the value character by character; set_config
// is a function and takes them, so the value never reaches the parser at all.
func TestTheTenantIsBoundAsAParameterAndNotWrittenIntoTheStatement(t *testing.T) {
	rec := &recordingExecer{}
	if err := Apply(context.Background(), rec, "T_01HZ"); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("%d statements run, want 1", len(rec.calls))
	}
	c := rec.calls[0]
	if strings.Contains(c.sql, "T_01HZ") {
		t.Errorf("the tenant was written into the statement: %s", c.sql)
	}
	if len(c.args) != 2 || c.args[0] != SettingName || c.args[1] != "T_01HZ" {
		t.Errorf("args = %v, want the setting name and the tenant bound", c.args)
	}
	if !strings.Contains(c.sql, "false") {
		t.Errorf("statement = %q; the setting must be session-scoped, or the "+
			"transaction the caller opens next will roll it back", c.sql)
	}
}

// A bad tenant must be refused before it reaches the database, so the failure
// names the caller's mistake rather than arriving as a driver error.
func TestABadTenantNeverReachesTheDatabase(t *testing.T) {
	rec := &recordingExecer{}
	if err := Apply(context.Background(), rec, "T_01\nHZ"); err == nil {
		t.Fatal("a tenant containing a newline was sent to the database")
	}
	if len(rec.calls) != 0 {
		t.Errorf("%d statements were run for an invalid tenant", len(rec.calls))
	}
}

func TestClearSetsTheParameterToEmptyRatherThanLeavingIt(t *testing.T) {
	rec := &recordingExecer{}
	if err := Clear(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("%d statements run, want 1", len(rec.calls))
	}
	if got := rec.calls[0].args; len(got) != 1 || got[0] != SettingName {
		t.Errorf("args = %v, want just the setting name", got)
	}
}
