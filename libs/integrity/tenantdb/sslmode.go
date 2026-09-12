package tenantdb

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Every service connected with sslmode=disable.
//
// Twenty-eight compiled-in defaults, every docker-compose file and every
// Kubernetes ConfigMap in this repository, and none of it was a decision — it
// was what the first service was written with and what the next twenty-seven
// copied. The connection that carries every producer's name, every payment and
// every password hash went over the wire as plaintext.
//
// # WHAT IS REFUSED
//
// Anything that can end up in plaintext without saying so:
//
//   - disable — never encrypts.
//   - allow — plaintext unless the server insists.
//   - prefer — encrypts if offered, silently falls back if not, and never
//     verifies. This is also what an absent sslmode means, which makes it the
//     most dangerous of the three: a DSN with no opinion gets it.
//
// require, verify-ca and verify-full are allowed. require encrypts without
// verifying who it is talking to, which is weaker than verify-full and is a
// legitimate choice over a private link or a unix socket; the repository does
// not have enough information to insist otherwise, and a check that refuses
// reasonable configurations is a check somebody removes.
//
// # THE WAY OUT
//
// A deployment that genuinely has a plaintext database — a sidecar PostgreSQL on
// a private network, the test harness against a server with no certificate —
// sets GAVYA_INSECURE_DATABASE to the phrase below. A phrase rather than a flag
// because the thing being prevented is not ignorance, it is a value inherited
// from a template: somebody has to type a sentence saying what they are doing
// before this connects in the clear.
const (
	InsecureEnv    = "GAVYA_INSECURE_DATABASE"
	InsecurePhrase = "plaintext-to-the-database-is-acceptable-here"
)

// insecureModes are the settings under which a connection may be unencrypted.
var insecureModes = map[string]string{
	"":        "unset, which PostgreSQL reads as prefer: encrypted if the server offers it, plaintext if not, and never verified",
	"disable": "never encrypted",
	"allow":   "plaintext unless the server refuses to accept it",
	"prefer":  "encrypted if the server offers it, plaintext if not, and never verified",
}

// checkSSLMode refuses a DSN that can carry the platform's data in the clear.
//
// Called from NewPool, which is the one way twenty-eight services connect — so
// this is a property of the platform rather than of each service's config.go,
// which is where the twenty-eight copies of the wrong answer came from.
func checkSSLMode(dsn string) error {
	mode, err := sslModeOf(dsn)
	if err != nil {
		return err
	}
	why, insecure := insecureModes[mode]
	if !insecure {
		return nil
	}
	if strings.TrimSpace(os.Getenv(InsecureEnv)) == InsecurePhrase {
		return nil
	}

	shown := mode
	if shown == "" {
		shown = "absent"
	}
	return fmt.Errorf("tenantdb: refusing to connect with sslmode=%s (%s).\n"+
		"This connection carries every producer's name, every payment and every "+
		"password hash.\n"+
		"Set sslmode=verify-full on DATABASE_URL, with the server's CA in "+
		"sslrootcert.\n"+
		"If this database really is reachable only over a private network and has "+
		"no certificate, say so: %s=%s",
		shown, why, InsecureEnv, InsecurePhrase)
}

// sslModeOf reads the setting out of either DSN shape pgx accepts.
//
// Both, because this repository uses the URL form and a deployment may well hand
// it the keyword form — and a check that understands one of the two formats
// passes everything written in the other.
func sslModeOf(dsn string) (string, error) {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return "", fmt.Errorf("tenantdb: the database URL is empty")
	}

	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("tenantdb: %w", err)
		}
		return strings.ToLower(strings.TrimSpace(u.Query().Get("sslmode"))), nil
	}

	// Keyword form: sslmode=require host=... — last occurrence wins, as libpq
	// reads it.
	mode := ""
	for _, field := range strings.Fields(trimmed) {
		key, value, found := strings.Cut(field, "=")
		if found && strings.EqualFold(strings.TrimSpace(key), "sslmode") {
			mode = strings.ToLower(strings.TrimSpace(value))
		}
	}
	return mode, nil
}
