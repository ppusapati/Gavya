package main

import (
	"bytes"
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

// A defence a service wraps around its own server is also wrapped around this
// one.
//
// The modulith does not run any service's main. Anything a main does beyond
// assembling the module — every `srv.Handler = something(srv.Handler)` — is
// therefore absent here unless somebody wired it a second time, and absent
// silently: the module still answers every procedure it has, correctly, with the
// defence gone.
//
// That is worse than never having built the defence. The sign-in rate limit was
// written, unit-tested, end-to-end tested against identity-service's own stack,
// and missing from the one deployment shape this platform ships — a control
// reporting success while doing nothing, which is the failure this repository
// keeps finding. So: two lists of the same thing, compared.
//
// RUN WITH -count=1. It reads Go files in other modules, which the test cache
// does not track.
func TestEveryServiceLevelDefenceIsWrappedHereToo(t *testing.T) {
	root := repoRoot(t)
	mains, err := filepath.Glob(filepath.Join(root, "services", "*-service", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mains) < 20 {
		t.Fatalf("found only %d service main functions; the glob has probably stopped "+
			"matching, and a check that finds nothing passes", len(mains))
	}

	// srv.Handler = pkg.Wrapper(srv.Handler) — the name of the wrapper is what
	// has to appear in this binary as well.
	wrap := regexp.MustCompile(`srv\.Handler\s*=\s*(?:[A-Za-z0-9_]+\.)?([A-Za-z0-9_]+)\(`)

	self, err := os.ReadFile(filepath.Join(root, "services", "modulith", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}

	var missing []string
	found := 0
	for _, path := range mains {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		service := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		for _, m := range wrap.FindAllSubmatch(src, -1) {
			name := string(m[1])
			found++
			if !bytes.Contains(self, []byte(name+"(")) {
				missing = append(missing, service+" wraps its server in "+name+
					", and this binary does not")
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d defences exist only in the separate-process deployment:\n  %s\n"+
			"The modulith is the shape that ships. A defence it does not carry is not "+
			"a weaker deployment, it is no deployment with that defence at all.",
			len(missing), strings.Join(missing, "\n  "))
	}
	// A regexp that has stopped matching would report nothing missing forever.
	if found == 0 {
		t.Error("no service wraps its own server, so this check compared two empty lists; " +
			"either the pattern changed or the regexp no longer matches it")
	}
	t.Logf("%d service-level wrappers, %d of them missing here", found, len(missing))
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
