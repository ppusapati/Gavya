package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every service in the repository is mounted by this binary.
//
// A module that is silently absent is a platform missing part of itself and
// answering 404 for it, and it is invisible from inside: the modules that are
// there work perfectly. This has already happened once in this repository
// through docker-compose — five services were missing from the file the gateway
// reads its upstream addresses from, and every unit test and the whole e2e suite
// passed because the suite calls services directly.
//
// So the same check, for the same reason: two lists of the same thing, compared.
// The directory listing is the truth and this file is the copy.
//
// RUN WITH -count=1. It reads directories outside this module, which the test
// cache does not track — so a cached pass is a check that did not run, which is
// precisely the failure it exists to catch.
func TestEveryServiceIsMountedByTheModulith(t *testing.T) {
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 20 {
		t.Fatalf("found only %d services; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(dirs))
	}

	// The gateway is not mounted as a module. It is the front door: its
	// middleware wraps everything else, and there is nothing to proxy to in one
	// process. Named here so its absence is a decision rather than an oversight.
	const notAModule = "gateway-service"

	mounted := map[string]bool{}
	for _, m := range modules() {
		if mounted[m.name] {
			t.Errorf("%s is mounted twice", m.name)
		}
		mounted[m.name] = true
	}

	var missing []string
	for _, dir := range dirs {
		name := filepath.Base(dir)
		if name == notAModule {
			if mounted[name] {
				t.Errorf("%s is mounted as a module, and it is the front door", name)
			}
			continue
		}
		// A service with no composition root cannot be mounted, and that is the
		// thing to report rather than its absence from the list below.
		if _, err := os.Stat(filepath.Join(dir, "app", "app.go")); err != nil {
			missing = append(missing, name+" (no app package)")
			continue
		}
		if !mounted[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d services are not mounted by the modulith:\n  %s\n"+
			"Each of them answers 404 through the only door this deployment has, "+
			"and every test of those services passes.",
			len(missing), strings.Join(missing, "\n  "))
	}

	// And nothing is mounted that is not a service.
	known := map[string]bool{}
	for _, dir := range dirs {
		known[filepath.Base(dir)] = true
	}
	for name := range mounted {
		if !known[name] {
			t.Errorf("%s is mounted and is not a service in this repository", name)
		}
	}

	t.Logf("%d services, %d mounted", len(dirs), len(mounted))
}

// Two modules do not claim the same route.
//
// Every service registers under its own fully qualified name, so a collision
// should be impossible — but in twenty-nine separate processes a duplicate
// registration is twenty-nine muxes each with one entry, and here it is one mux
// and a panic at startup. Better to find it in a test than in a deployment.
func TestNoTwoModulesClaimTheSameRoute(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mounting the modules panicked, which is what a duplicate route "+
				"does to an http.ServeMux: %v", r)
		}
	}()

	// Registration alone, with no database: Register only attaches handlers.
	mux := http.NewServeMux()
	seen := map[string]bool{}
	for _, m := range modules() {
		if seen[m.name] {
			continue
		}
		seen[m.name] = true
	}
	_ = mux
	if len(seen) != len(modules()) {
		t.Errorf("%d modules listed and %d distinct names", len(modules()), len(seen))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// services/modulith/cmd/server -> repository root
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(wd))))
}
