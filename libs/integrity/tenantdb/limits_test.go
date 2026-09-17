package tenantdb

import (
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// parse is what NewPool does up to the point applyLimits is called.
func parse(t *testing.T, dsn string) *pgxpool.Config {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}
	return cfg
}

// The defect this closes is arithmetic rather than a bug in anything: twenty
// eight pools, each sized by a default nobody chose, against a server limit
// nobody set.
func TestAPoolIsSizedRatherThanLeftToTheDefault(t *testing.T) {
	dsn := "postgres://app@db:5432/dairy?sslmode=require"
	cfg := parse(t, dsn)

	// What pgxpool decided on its own, before this package has a say.
	def := cfg.MaxConns

	applyLimits(cfg, dsn)

	if cfg.MaxConns != MaxConns {
		t.Fatalf("pool size = %d, want the platform's %d", cfg.MaxConns, MaxConns)
	}
	if def == MaxConns {
		t.Logf("note: pgxpool's default on this machine is also %d, so this test "+
			"would pass with applyLimits doing nothing; the mutation check for it "+
			"is TestADeploymentThatSizesItsOwnPoolIsObeyed below", def)
	}
}

// A deployment that knows its own database is entitled to overrule the number in
// limits.go. The whole distinction being drawn is between a value somebody chose
// and a value that arrived by default, so obeying an explicit one is the other
// half of the same property.
func TestADeploymentThatSizesItsOwnPoolIsObeyed(t *testing.T) {
	dsn := "postgres://app@db:5432/dairy?sslmode=require&pool_max_conns=3"
	cfg := parse(t, dsn)
	applyLimits(cfg, dsn)

	if cfg.MaxConns != 3 {
		t.Fatalf("pool size = %d, want the 3 the DSN asked for", cfg.MaxConns)
	}
}

// Two timeouts, and the second is the one with no other bound on it. A statement
// waiting on a lock is at least bounded by serve's WriteTimeout at two minutes;
// a transaction left open by a handler that returned without committing holds
// its connection and its locks until the process dies.
func TestAQueryAndAStrandedTransactionAreBothBounded(t *testing.T) {
	dsn := "postgres://app@db:5432/dairy?sslmode=require"
	cfg := parse(t, dsn)
	applyLimits(cfg, dsn)

	want := map[string]string{
		"statement_timeout":                   strconv.FormatInt(StatementTimeout.Milliseconds(), 10),
		"idle_in_transaction_session_timeout": strconv.FormatInt(IdleInTransactionTimeout.Milliseconds(), 10),
	}
	for param, value := range want {
		if got := cfg.ConnConfig.RuntimeParams[param]; got != value {
			t.Errorf("%s = %q, want %q", param, got, value)
		}
	}
}

// Same reason as the pool size: a DSN that names one of these has said something
// deliberate, and overwriting it would make the setting unusable.
func TestATimeoutTheDSNNamesIsLeftAlone(t *testing.T) {
	dsn := "postgres://app@db:5432/dairy?sslmode=require&statement_timeout=5000"
	cfg := parse(t, dsn)
	applyLimits(cfg, dsn)

	if got := cfg.ConnConfig.RuntimeParams["statement_timeout"]; got != "5000" {
		t.Errorf("statement_timeout = %q, want the 5000 the DSN asked for", got)
	}
	// The one it did not name is still set, so naming one does not opt out of
	// the other.
	if cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] == "" {
		t.Error("naming statement_timeout on the DSN also dropped the " +
			"idle-in-transaction bound, which is the one nothing else limits")
	}
}

// settingOf is what tells a chosen value from a default, and it reads the DSN
// rather than the parsed configuration because the parse is where that
// distinction is lost. It has to understand both shapes pgx accepts: this
// repository writes the URL form, and a deployment may well hand it the keyword
// form.
func TestASettingIsReadFromEitherDSNShape(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		key  string
		want string
	}{
		{"url form", "postgres://app@db/dairy?sslmode=require", "sslmode", "require"},
		{"url form, absent", "postgres://app@db/dairy", "sslmode", ""},
		{"keyword form", "host=db user=app sslmode=verify-full", "sslmode", "verify-full"},
		{"keyword form, last wins", "sslmode=require host=db sslmode=disable", "sslmode", "disable"},
		{"keyword form, absent", "host=db user=app", "sslmode", ""},
		{"pool size in url form", "postgres://app@db/dairy?pool_max_conns=12", "pool_max_conns", "12"},
		{"pool size in keyword form", "host=db pool_max_conns=12", "pool_max_conns", "12"},
		{"empty", "", "sslmode", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := settingOf(c.dsn, c.key); got != c.want {
				t.Errorf("settingOf(%q, %q) = %q, want %q", c.dsn, c.key, got, c.want)
			}
		})
	}
}
