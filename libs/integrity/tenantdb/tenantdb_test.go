package tenantdb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAContextWithNoTenantIsAnError(t *testing.T) {
	if _, err := TenantFrom(context.Background()); !errors.Is(err, ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}
	if _, err := TenantFrom(WithTenant(context.Background(), "")); !errors.Is(err, ErrNoTenant) {
		t.Errorf("an empty tenant was accepted as a tenant")
	}
}

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
