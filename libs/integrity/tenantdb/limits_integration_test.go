//go:build dbintegration

// The settings this package makes, read back off a real connection.
//
// The unit tests beside this one assert that applyLimits puts the right values
// into a pgxpool.Config, and that is worth very little on its own: a runtime
// parameter that PostgreSQL rejects, or a spelling it does not recognise, would
// satisfy every one of them. A GUC set to a value the server never applied is
// exactly the shape of defect this repository keeps finding — a control that
// reports success while doing nothing.
//
// So this asks the server.
package tenantdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func openedByTheCode(t *testing.T) (context.Context, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run this")
	}
	// The provisioned database is a local one with no certificate, which is the
	// case the escape hatch exists for. Set here rather than in the harness so
	// this file says out loud that it is connecting in the clear.
	t.Setenv(InsecureEnv, InsecurePhrase)
	return context.Background(), dsn
}

// A query that waits on a lock held a pooled connection for the whole of serve's
// two-minute WriteTimeout, because nothing bounded the statement. This is the
// bound, on a connection the platform's own code opened.
func TestTheStatementTimeoutReachesTheServer(t *testing.T) {
	ctx, dsn := openedByTheCode(t)

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		Forget(pool)
		pool.Close()
	}()

	settings := map[string]time.Duration{
		"statement_timeout":                   StatementTimeout,
		"idle_in_transaction_session_timeout": IdleInTransactionTimeout,
	}
	for name, want := range settings {
		var shown string
		// SHOW renders a GUC the way PostgreSQL itself would, so this compares
		// against what the server holds rather than against what was sent.
		if err := pool.QueryRow(ctx, "SHOW "+name).Scan(&shown); err != nil {
			t.Fatalf("SHOW %s: %v", name, err)
		}
		got, parseErr := time.ParseDuration(strings.Replace(shown, "min", "m", 1))
		if parseErr != nil {
			t.Fatalf("%s reads as %q, which is not a duration this test can compare", name, shown)
		}
		if got != want {
			t.Errorf("%s on the server is %s (%q), want %s", name, got, shown, want)
		}
	}
}

// The pool size, asked of the pool the code opened rather than of a config
// struct. MaxConns is what the arithmetic in gateway-service multiplies by, so a
// pool that quietly held a different number would make that check a check of
// nothing.
func TestThePoolOpensAtTheSizeThePlatformStates(t *testing.T) {
	ctx, dsn := openedByTheCode(t)

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		Forget(pool)
		pool.Close()
	}()

	if got := pool.Stat().MaxConns(); got != MaxConns {
		t.Errorf("the pool holds up to %d connections, and limits.go says %d", got, MaxConns)
	}
}

// The tenant handling and the new settings have to survive each other. Both are
// applied to the same configuration, and a connection that gets the timeouts but
// not the tenant is one that returns nothing while looking perfectly healthy.
func TestTheTenantIsStillAppliedAlongsideTheLimits(t *testing.T) {
	ctx, dsn := openedByTheCode(t)

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		Forget(pool)
		pool.Close()
	}()

	const tenant = "01J000000000000000000TEST"
	var set string
	if err := pool.QueryRow(WithTenant(ctx, tenant),
		"SELECT current_setting($1, true)", SettingName).Scan(&set); err != nil {
		t.Fatalf("read %s: %v", SettingName, err)
	}
	if set != tenant {
		t.Errorf("%s is %q on a connection taken with a tenant, want %q",
			SettingName, set, tenant)
	}
}
