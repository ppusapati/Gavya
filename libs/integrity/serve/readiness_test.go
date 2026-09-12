package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every service's readiness check asks its own database.
//
// This exists because of a mutation that did not kill. The e2e suite calls
// /readyz on all twenty-nine running services and requires a 200 that names
// what it checked — and a check written as `Ping: func(context.Context) error
// { return nil }` satisfies every one of those assertions. It answers 200, it
// names "database", and it cannot fail. That is the exact shape being replaced:
// a probe that reports success while doing nothing.
//
// The e2e test proves the endpoint is mounted and answers. The unit tests in
// libs/integrity/observe prove Ready goes red when a Ping fails. Neither proves
// the Ping is the real pool, and only reading the source does.
//
// RUN WITH -count=1: this reads files in other modules.
func TestEveryServiceChecksItsOwnPoolForReadiness(t *testing.T) {
	root := repoRoot(t)
	mains, err := filepath.Glob(filepath.Join(root, "services", "*-service", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mains) < 20 {
		t.Fatalf("found only %d service main functions; the glob has stopped matching "+
			"and a check that finds nothing passes", len(mains))
	}

	// The gateway has no database. Its readiness is whether it can verify a
	// session, which is the identity service's answer — named here so the
	// exception is written down rather than inferred from its absence.
	const noDatabase = "gateway-service"

	// Ping: pool.Ping — a method value on the real pool, not a literal.
	realPing := regexp.MustCompile(`Ping:\s*pool\.Ping\b`)
	anyCheck := regexp.MustCompile(`observe\.Check\{`)

	var missing, fake []string
	for _, path := range mains {
		service := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		if service == noDatabase {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		switch {
		case !anyCheck.Match(src):
			missing = append(missing, service)
		case !realPing.Match(src):
			fake = append(fake, service)
		}
	}
	sort.Strings(missing)
	sort.Strings(fake)

	if len(missing) > 0 {
		t.Errorf("%d services pass no readiness check, so /readyz answers ready "+
			"having checked nothing:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	if len(fake) > 0 {
		t.Errorf("%d services pass a readiness check that is not their pool's Ping:\n  %s\n"+
			"A check that cannot fail answers 200 for a service whose database has "+
			"gone, which keeps the pod in rotation being sent traffic it cannot "+
			"serve — the thing /readyz exists to prevent.",
			len(fake), strings.Join(fake, "\n  "))
	}
	t.Logf("checked %d service main functions", len(mains))
}
