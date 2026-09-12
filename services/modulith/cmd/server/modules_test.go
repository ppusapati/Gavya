package main

import (
	"os"
	"path/filepath"
	"regexp"
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
// In twenty-nine separate processes a duplicate registration is twenty-nine
// muxes each with one entry. Here it is one mux, and an http.ServeMux panics on
// a duplicate pattern — at startup, before anything serves.
//
// The first version of this check counted module names and registered nothing,
// so it passed while every service was registering its own /healthz and this
// binary would have panicked on the second module. A check that does not do the
// thing it is checking is the failure this repository keeps finding, and it
// found it in its own test.
//
// It now reads every pattern every service registers, out of the source, and
// looks for one claimed twice. Scraped rather than executed because registering
// for real needs twenty-eight databases — and the panic it is looking for
// happens before any of them are touched.
func TestNoTwoModulesClaimTheSameRoute(t *testing.T) {
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}

	// mux.HandleFunc("literal", ...) — the routes registered by a path rather
	// than through the `route` helper, which composes a per-service name and
	// cannot collide.
	literal := regexp.MustCompile(`mux\.HandleFunc\("(/[^"]*)"`)

	claimedBy := map[string][]string{}
	for _, dir := range dirs {
		svc := filepath.Base(dir)
		for _, pattern := range []string{"internal/handler/*.go", "handler/*.go"} {
			files, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, f := range files {
				if strings.HasSuffix(f, "_test.go") {
					continue
				}
				src, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				for _, m := range literal.FindAllSubmatch(src, -1) {
					path := string(m[1])
					if !contains(claimedBy[path], svc) {
						claimedBy[path] = append(claimedBy[path], svc)
					}
				}
			}
		}
	}

	var clashes []string
	for path, services := range claimedBy {
		if len(services) > 1 {
			sort.Strings(services)
			clashes = append(clashes, path+" is registered by "+strings.Join(services, ", "))
		}
	}
	sort.Strings(clashes)
	if len(clashes) > 0 {
		t.Errorf("%d paths are registered by more than one service:\n  %s\n"+
			"Mounted on one mux this panics at startup, before anything serves. "+
			"A path every service needs belongs in libs/integrity/serve, where it "+
			"is registered once.",
			len(clashes), strings.Join(clashes, "\n  "))
	}
	t.Logf("%d literal paths registered across %d services", len(claimedBy), len(dirs))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
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
