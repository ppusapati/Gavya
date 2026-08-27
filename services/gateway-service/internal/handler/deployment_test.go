package handler

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The gateway can only reach an upstream it was given an address for, and under
// compose that address comes from an environment variable in docker-compose.yaml.
// A service missing from that file starts, answers, and is unreachable through
// the only address a person outside the platform has.
//
// This had happened to five of them. procurement-service was in compose and the
// gateway had no URL for it; settlement, material, laboratory and production were
// not in compose at all. Every unit test passed, the e2e suite passed — it calls
// services directly — and nothing anywhere said the deployed platform was missing
// a fifth of itself.
//
// The check that would have caught it is this one: the code and the deployment
// descriptor are two lists of the same thing, and they are compared.
//
// RUN THESE WITH -count=1.
//
// docker-compose.yaml sits at the repository root, outside this module, and Go's
// test cache does not track it. Change compose without touching a Go file and
// `go test ./...` reports a cached pass — the check appears to run and does
// nothing, which is precisely the failure it was written to catch, one level up.
// Verified: removing SETTLEMENT_SERVICE_URL from compose passes cached and fails
// under -count=1.
//
// scripts/check-all.sh runs the whole suite that way.
func TestEveryUpstreamTheGatewayNeedsIsInDockerCompose(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yaml")
	configSrc := readRepoFile(t, filepath.Join(
		"services", "gateway-service", "internal", "config", "config.go"))

	// Every *_SERVICE_URL the gateway reads. Deliberately derived from the source
	// rather than listed here: a list maintained by hand is one that goes stale
	// in exactly the way this test exists to prevent.
	wantRe := regexp.MustCompile(`getEnv\("([A-Z_]+_SERVICE_URL)"`)
	var want []string
	for _, m := range wantRe.FindAllStringSubmatch(configSrc, -1) {
		want = append(want, m[1])
	}
	if len(want) < 20 {
		t.Fatalf("found only %d service URLs in the gateway config; the pattern this test "+
			"scrapes with has probably stopped matching, and a check that finds nothing "+
			"passes", len(want))
	}

	for _, env := range want {
		// Anchored to the start of a line and to the colon. Substring matching
		// reports SETTLEMENT_SERVICE_URL as present because
		// SHADOW_SETTLEMENT_SERVICE_URL contains it — which is how the first
		// version of this check missed one of the six it was written to find.
		if !regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(env) + `:`).MatchString(compose) {
			t.Errorf("the gateway reads %s and docker-compose.yaml never sets it, so under "+
				"compose that upstream is unreachable through the gateway", env)
		}
	}
}

// A service in the tree that compose does not build never runs.
func TestEveryServiceIsInDockerCompose(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yaml")
	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(e.Name()) + `:`).MatchString(compose) {
			missing = append(missing, e.Name())
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these services exist and compose does not build them, so they never run: %s",
			strings.Join(missing, ", "))
	}
}

// A service compose runs on a port the gateway does not expect is a service the
// gateway cannot reach, which looks exactly like one that is down.
func TestComposeAddressesMatchTheDeclaredPorts(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yaml")

	// SERVER_ADDR: ":8103" beside a "8103:8103" mapping. A service listening on
	// one port and published on another still works from outside; one whose
	// gateway URL names a third does not.
	urlRe := regexp.MustCompile(`(?m)^\s+([A-Z_]+_SERVICE_URL): "http://([a-z-]+):(\d+)"`)
	for _, m := range urlRe.FindAllStringSubmatch(compose, -1) {
		env, host, port := m[1], m[2], m[3]

		block := serviceBlock(compose, host)
		if block == "" {
			t.Errorf("%s points at %s and compose has no such service", env, host)
			continue
		}
		addr := regexp.MustCompile(`SERVER_ADDR: ":(\d+)"`).FindStringSubmatch(block)
		if addr == nil {
			t.Errorf("%s has no SERVER_ADDR, so what it listens on is whatever its binary "+
				"defaults to and nothing here says what that is", host)
			continue
		}
		if addr[1] != port {
			t.Errorf("%s says %s is on port %s and %s listens on %s; the gateway would be "+
				"connecting to a closed port, which reads as the service being down",
				env, host, port, host, addr[1])
		}
	}
}

// serviceBlock returns the compose entry for one service: from its key to the
// next top-level key at the same indentation.
func serviceBlock(compose, name string) string {
	start := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:$`).FindStringIndex(compose)
	if start == nil {
		return ""
	}
	rest := compose[start[1]:]
	if end := regexp.MustCompile(`(?m)^  [a-z]`).FindStringIndex(rest); end != nil {
		return rest[:end[0]]
	}
	return rest
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("cannot find the repository root from here")
	return ""
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// A service with no Kubernetes manifests is one that cannot be deployed at all,
// and nothing else in the repository says so.
//
// Six were missing when this was written, and the check that first looked for
// them found one — because it tested that the directory existed rather than that
// it held anything, and several existed empty. A check that looks in the wrong
// place reports success while doing nothing, which is the thing this file is
// about.
func TestEveryServiceHasKubernetesManifests(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatal(err)
	}

	var missing, empty []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, "services", e.Name(), "deployments", "k8s")
		files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		switch {
		case err != nil:
			t.Fatal(err)
		case len(files) == 0 && !dirExists(dir):
			missing = append(missing, e.Name())
		case len(files) == 0:
			empty = append(empty, e.Name())
		}
	}
	sort.Strings(missing)
	sort.Strings(empty)
	if len(missing) > 0 {
		t.Errorf("no deployments/k8s at all, so these cannot be deployed: %s",
			strings.Join(missing, ", "))
	}
	if len(empty) > 0 {
		t.Errorf("deployments/k8s exists and holds no manifests, which looks deployable from "+
			"a directory listing and is not: %s", strings.Join(empty, ", "))
	}
}

// A manifest set has to agree with itself: the port the binary is told to listen
// on must be the one the probes hit and the one the Service targets.
//
// A readiness probe against the wrong port never succeeds, so the pod never
// becomes ready and the rollout hangs — which reads as the service being broken
// rather than misconfigured, and sends somebody to debug the wrong thing.
//
// Parsed rather than pattern-matched. The first version scraped `port: N` with a
// regex and had to skip bare `port` so a Service's own port could legitimately
// differ from its target — and that skip silently swallowed `httpGet.port` too,
// so probe ports, the very case above, went unchecked. A mutation moving a probe
// to a closed port survived it. The difference between a Service's port and a
// probe's port is structural, so the structure is what gets read.
//
// Deliberately NOT checked against ports.All. That table says so itself: it is a
// development convenience for running binaries directly, and under Kubernetes
// each container is free to listen wherever it likes. An earlier version
// compared the two and reported cattle-service and milk-service as broken when
// both are internally consistent on 8080 — forcing manifests to follow a table
// that explicitly disclaims authority over them.
func TestKubernetesManifestsAgreeWithThemselves(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(
			root, "services", e.Name(), "deployments", "k8s", "*.yaml"))
		if len(files) == 0 {
			continue
		}

		var docs []map[string]any
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			dec := yaml.NewDecoder(bytes.NewReader(b))
			for {
				var d map[string]any
				if err := dec.Decode(&d); err != nil {
					break
				}
				if d != nil {
					docs = append(docs, d)
				}
			}
		}

		listen := listenPort(docs)
		if listen == "" {
			t.Errorf("%s has manifests and none of them set SERVER_ADDR, so what the container "+
				"listens on is whatever its binary defaults to and nothing here says what that "+
				"is", e.Name())
			continue
		}
		checked++

		for _, got := range containerPorts(docs) {
			if got.port != listen {
				t.Errorf("%s tells its container to listen on :%s and its %s says %s; a probe "+
					"or a Service pointed at a closed port never becomes ready, and the "+
					"rollout hangs looking like a broken service",
					e.Name(), listen, got.what, got.port)
			}
		}
	}

	if checked < 20 {
		t.Fatalf("only %d services were checked; if the manifests moved or the field names "+
			"changed this test would find nothing and pass", checked)
	}
}

type portRef struct{ what, port string }

// listenPort is what SERVER_ADDR tells the binary, from whichever ConfigMap
// carries it.
func listenPort(docs []map[string]any) string {
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		addr, _ := data["SERVER_ADDR"].(string)
		if strings.HasPrefix(addr, ":") {
			return addr[1:]
		}
	}
	return ""
}

// containerPorts is every port in the set that must equal the listen port: a
// container's declared port, a Service's targetPort, and every probe's port.
//
// A Service's own `port` is what other pods dial and is free to differ, so it is
// the one thing here that is not collected.
func containerPorts(docs []map[string]any) []portRef {
	var out []portRef
	num := func(v any) string {
		switch t := v.(type) {
		case int:
			return strconv.Itoa(t)
		case string:
			return t
		}
		return ""
	}

	for _, d := range docs {
		switch d["kind"] {
		case "Deployment":
			spec, _ := d["spec"].(map[string]any)
			tmpl, _ := spec["template"].(map[string]any)
			tspec, _ := tmpl["spec"].(map[string]any)
			containers, _ := tspec["containers"].([]any)
			for _, c := range containers {
				cm, _ := c.(map[string]any)
				cports, _ := cm["ports"].([]any)
				for _, p := range cports {
					pm, _ := p.(map[string]any)
					if v := num(pm["containerPort"]); v != "" {
						out = append(out, portRef{"containerPort", v})
					}
				}
				for _, probe := range []string{"readinessProbe", "livenessProbe", "startupProbe"} {
					pr, _ := cm[probe].(map[string]any)
					hg, _ := pr["httpGet"].(map[string]any)
					if v := num(hg["port"]); v != "" {
						out = append(out, portRef{probe, v})
					}
				}
			}
		case "Service":
			spec, _ := d["spec"].(map[string]any)
			sports, _ := spec["ports"].([]any)
			for _, p := range sports {
				pm, _ := p.(map[string]any)
				if v := num(pm["targetPort"]); v != "" {
					out = append(out, portRef{"targetPort", v})
				}
			}
		}
	}
	return out
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// The two deployment descriptors must agree about the ML tier.
//
// Every call through these is advisory, so emptying one is a supported way to
// turn a tier off. What is not supported is the two files disagreeing: compose
// wired all four ML URLs, Kubernetes wired one of four and left three empty, and
// nothing anywhere said whether that was a decision. A deployment where the
// anomaly tier runs under compose and silently does not under Kubernetes is one
// where an observation is scored in testing and unscored in production.
//
// This does not require them to be set. It requires them to say the same thing.
func TestComposeAndKubernetesAgreeAboutTheMLTier(t *testing.T) {
	root := repoRoot(t)
	compose := readRepoFile(t, "docker-compose.yaml")

	// Every ML URL any service reads, taken from the configs rather than listed.
	var vars []string
	configs, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "config", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`getEnv\("([A-Z_]+_ML_URL)"`)
	seen := map[string]bool{}
	for _, f := range configs {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				vars = append(vars, m[1])
			}
		}
	}
	sort.Strings(vars)
	if len(vars) == 0 {
		t.Fatal("no ML URL variables found in any service config; this check would pass " +
			"against a platform that had stopped calling the tier entirely")
	}

	k8s := map[string]string{}
	manifests, _ := filepath.Glob(filepath.Join(root, "services", "*", "deployments", "k8s", "*.yaml"))
	for _, f := range manifests {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		for {
			var d map[string]any
			if err := dec.Decode(&d); err != nil {
				break
			}
			data, _ := d["data"].(map[string]any)
			for _, v := range vars {
				if raw, ok := data[v]; ok {
					s, _ := raw.(string)
					k8s[v] = s
				}
			}
		}
	}

	composeSet := regexp.MustCompile(`(?m)^\s+([A-Z_]+_ML_URL): "([^"]*)"`)
	inCompose := map[string]string{}
	for _, m := range composeSet.FindAllStringSubmatch(compose, -1) {
		inCompose[m[1]] = m[2]
	}

	for _, v := range vars {
		ck, inK := k8s[v]
		cc, inC := inCompose[v]
		switch {
		case !inK && !inC:
			// Neither names it. Both deployments then get the binary's default,
			// which is empty, and they agree.
		case !inK:
			t.Errorf("compose sets %s to %q and no Kubernetes ConfigMap mentions it, so that "+
				"tier runs under compose and silently does not under Kubernetes", v, cc)
		case !inC:
			t.Errorf("a Kubernetes ConfigMap sets %s to %q and compose never mentions it", v, ck)
		case (ck == "") != (cc == ""):
			t.Errorf("%s is %q under Kubernetes and %q under compose; one deployment consults "+
				"that tier and the other does not, and nothing says which was intended",
				v, ck, cc)
		}
	}
}
