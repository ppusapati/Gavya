package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every setting a module reads is in the modulith's compose file.
//
// This is the gateway's own deployment check, one shape over. There, five
// services were in the code and not in docker-compose.yaml: they started,
// answered, and were unreachable through the only address anybody outside had.
// Every unit test passed and so did the whole e2e suite, because the suite calls
// services directly.
//
// The same thing happens here differently. A module whose DATABASE_URL is not in
// the file fails at startup, loudly, which is fine. A module whose optional
// setting is missing does not fail at all — it runs with the feature off, and
// nothing says so. PROCUREMENT_URL is the one that matters: unset, settlement
// starts, serves, and refuses every gather, which looks like a broken
// settlement rather than a missing line in a file.
//
// RUN WITH -count=1. This reads files outside the module and the test cache does
// not track them, so a cached pass is a check that did not run.
func TestEverySettingAModuleReadsIsInTheModulithCompose(t *testing.T) {
	root := repoRoot(t)
	compose := read(t, filepath.Join(root, "docker-compose.modulith.yaml"))

	// Every getEnv("NAME") across the service configs, which is where a setting
	// is actually read. Scraped rather than listed, for the same reason the
	// gateway's check scrapes its own config: a list kept by hand goes stale in
	// exactly the way the check exists to prevent.
	wanted := map[string][]string{}
	configs, err := filepath.Glob(filepath.Join(root, "services", "*-service", "*", "config", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	more, _ := filepath.Glob(filepath.Join(root, "services", "*-service", "config", "config.go"))
	configs = append(configs, more...)
	if len(configs) < 20 {
		t.Fatalf("found only %d service configs; the glob has stopped matching and a "+
			"check that finds nothing passes", len(configs))
	}

	envRe := regexp.MustCompile(`getEnv\w*\("([A-Z][A-Z0-9_]+)"`)
	for _, path := range configs {
		svc := serviceOf(path)
		for _, m := range envRe.FindAllStringSubmatch(read(t, path), -1) {
			wanted[m[1]] = append(wanted[m[1]], svc)
		}
	}

	// Settings that are deliberately absent, each for a stated reason.
	skip := map[string]string{
		// Set per-process, not per-module: the modulith has one address and one
		// name, and carries both already.
		"SERVER_ADDR":  "one process, one address",
		"SERVICE_NAME": "one process, one name",
		"PORT":         "one process, one port",
		"LOG_LEVEL":    "carried once for the process",
	}

	var missing []string
	for name, readers := range wanted {
		if _, ok := skip[name]; ok {
			continue
		}
		if strings.Contains(compose, name+":") {
			continue
		}
		sort.Strings(readers)
		missing = append(missing, name+"  (read by "+strings.Join(unique(readers), ", ")+")")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d settings are read by a module and absent from "+
			"docker-compose.modulith.yaml:\n  %s\n"+
			"A required one fails at startup, which is fine. An optional one runs "+
			"with the feature quietly off, which is not.",
			len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("%d settings read across %d configs", len(wanted), len(configs))
}

// Nothing but the platform's own port is published.
//
// The whole reason this file exists: docker-compose.yaml publishes a host port
// for every service, and the services believe the headers the gateway sets. One
// published port is the property, and a check is the only thing that keeps it
// true after somebody adds a service here for convenience.
func TestTheModulithComposePublishesOneApplicationPort(t *testing.T) {
	compose := read(t, filepath.Join(repoRoot(t), "docker-compose.modulith.yaml"))

	published := regexp.MustCompile(`(?m)^\s*-\s*"([^"]+)"`).FindAllStringSubmatch(compose, -1)
	var offHost []string
	for _, p := range published {
		spec := p[1]
		if !strings.Contains(spec, ":") || strings.Contains(spec, "/") {
			continue // not a port mapping
		}
		// "127.0.0.1:5432:5432" is bound to the loopback address and is not
		// reachable from off the host.
		if strings.HasPrefix(spec, "127.0.0.1:") {
			continue
		}
		offHost = append(offHost, spec)
	}

	if len(offHost) != 1 {
		sort.Strings(offHost)
		t.Errorf("%d ports are published to every interface, want exactly one:\n  %s\n"+
			"Services take the tenant and the permissions they act on from headers "+
			"the gateway sets. That is sound while the gateway is the only way in, "+
			"and nothing at all once it is not.",
			len(offHost), strings.Join(offHost, "\n  "))
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// serviceOf names the service a config belongs to.
func serviceOf(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if strings.HasSuffix(p, "-service") {
			return parts[i]
		}
	}
	return path
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
