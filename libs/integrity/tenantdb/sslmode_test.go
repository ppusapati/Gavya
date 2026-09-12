package tenantdb

import (
	"strings"
	"testing"
)

// A DSN that can end up in plaintext is refused.
//
// Including the one with no sslmode at all, which is the case that matters most:
// it looks like nobody made a bad choice, and PostgreSQL reads it as prefer —
// encrypt if offered, fall back silently if not, never verify. A default that
// looks like a decision.
func TestAConnectionThatCanBePlaintextIsRefused(t *testing.T) {
	for _, c := range []struct {
		name    string
		dsn     string
		refused bool
	}{
		{"disable", "postgres://app@db:5432/dairy?sslmode=disable", true},
		{"allow", "postgres://app@db:5432/dairy?sslmode=allow", true},
		{"prefer", "postgres://app@db:5432/dairy?sslmode=prefer", true},
		{"absent", "postgres://app@db:5432/dairy", true},
		{"absent with other settings", "postgres://app@db:5432/dairy?pool_max_conns=10", true},

		{"require", "postgres://app@db:5432/dairy?sslmode=require", false},
		{"verify-ca", "postgres://app@db:5432/dairy?sslmode=verify-ca", false},
		{"verify-full", "postgres://app@db:5432/dairy?sslmode=verify-full", false},

		// The keyword form. A check that understands only the URL shape passes
		// everything a deployment writes in the other one.
		{"keyword disable", "host=db user=app dbname=dairy sslmode=disable", true},
		{"keyword absent", "host=db user=app dbname=dairy", true},
		{"keyword verify-full", "host=db user=app dbname=dairy sslmode=verify-full", false},

		// Case and spacing are the deployment's, not ours.
		{"uppercase", "postgres://app@db:5432/dairy?sslmode=VERIFY-FULL", false},
		{"uppercase disable", "postgres://app@db:5432/dairy?sslmode=DISABLE", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkSSLMode(c.dsn)
			if c.refused && err == nil {
				t.Errorf("%s was accepted, and it can carry every payment in this "+
					"platform in the clear", c.dsn)
			}
			if !c.refused && err != nil {
				t.Errorf("%s was refused: %v", c.dsn, err)
			}
		})
	}
}

// The refusal says what to do about it.
//
// A service that will not start and does not say why is an outage somebody
// resolves by reverting whatever they last changed, which here would be the
// encryption.
func TestTheRefusalSaysHowToFixIt(t *testing.T) {
	err := checkSSLMode("postgres://app@db:5432/dairy?sslmode=disable")
	if err == nil {
		t.Fatal("sslmode=disable was accepted")
	}
	for _, want := range []string{"verify-full", InsecureEnv, InsecurePhrase} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%v", want, err)
		}
	}
}

// A deployment that really has a plaintext database can say so.
//
// And has to say it in words. The thing being prevented is not somebody who does
// not know — it is a setting inherited from a template and carried into
// production by nobody in particular.
func TestTheWayOutHasToBeTypedOut(t *testing.T) {
	const dsn = "postgres://app@db:5432/dairy?sslmode=disable"

	for _, value := range []string{"", "1", "true", "yes", "y", "TRUE",
		"plaintext-to-the-database-is-acceptable", " "} {
		t.Setenv(InsecureEnv, value)
		if err := checkSSLMode(dsn); err == nil {
			t.Errorf("%s=%q was enough to connect in the clear", InsecureEnv, value)
		}
	}

	t.Setenv(InsecureEnv, InsecurePhrase)
	if err := checkSSLMode(dsn); err != nil {
		t.Errorf("the phrase did not permit a plaintext connection: %v", err)
	}
	// Surrounding whitespace is a compose file, not a different answer.
	t.Setenv(InsecureEnv, "  "+InsecurePhrase+"  ")
	if err := checkSSLMode(dsn); err != nil {
		t.Errorf("the phrase with whitespace around it was refused: %v", err)
	}
}

// An empty DSN is its own answer rather than an accidental pass.
//
// It would otherwise read as "no sslmode", be refused for the wrong reason, and
// send somebody to look at their certificates when what they have is an unset
// variable.
func TestAnEmptyURLSaysItIsEmpty(t *testing.T) {
	t.Setenv(InsecureEnv, InsecurePhrase)
	err := checkSSLMode("")
	if err == nil {
		t.Fatal("an empty database URL was accepted")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("the refusal does not say the URL is empty: %v", err)
	}
}
