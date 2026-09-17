package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ppusapati/gavya/libs/integrity/observe"
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
// modulith is not a service. It is the whole platform as one process, built
// from the other twenty-nine and deployed from its own compose file, so the
// checks below that ask "is this service deployable on its own" do not apply to
// it. Named here rather than special-cased in three places, so a second
// exemption is something somebody has to write down.
const modulith = "modulith"

func TestEveryUpstreamTheGatewayNeedsIsInDockerCompose(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yaml")
	configSrc := readRepoFile(t, filepath.Join(
		"services", "gateway-service", "config", "config.go"))

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
		if !e.IsDir() || e.Name() == modulith {
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
// This check has now been wrong twice, in the same way, and the second time is
// the more interesting.
//
// Six services were missing when it was written, and the first version found
// one: it tested that the directory existed rather than that it held anything,
// and several existed empty. Fixed to count files.
//
// Counting files was still wrong. Every service had a deployment.yaml and
// fifteen had no service.yaml, so every one of them passed — and a Deployment
// with no Service receives no traffic at all. The pods run, the probes pass, and
// nothing can reach them. "Has some manifest" is not the property; "has the ones
// that make it reachable" is.
//
// Both versions reported success while doing nothing, which is the thing this
// file is about.
func TestEveryServiceHasKubernetesManifests(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatal(err)
	}

	var missing, empty, unreachable []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == modulith {
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

		// The three that decide whether anything can reach it, and who.
		for _, kind := range []struct{ file, why string }{
			{"deployment.yaml", "nothing runs"},
			{"service.yaml", "the pods run and nothing can reach them"},
			{"networkpolicy.yaml", "every pod in the cluster can reach it, and " +
				"this service believes the tenant and permission headers it is sent"},
			{"egresspolicy.yaml", "a compromise of it can reach every other " +
				"service in the cluster and anywhere outside it"},
		} {
			if !dirExists(dir) {
				break
			}
			if _, err := os.Stat(filepath.Join(dir, kind.file)); err != nil {
				unreachable = append(unreachable,
					e.Name()+" has no "+kind.file+", so "+kind.why)
			}
		}
	}
	sort.Strings(unreachable)
	if len(unreachable) > 0 {
		t.Errorf("%d services are not deployable:\n  %s",
			len(unreachable), strings.Join(unreachable, "\n  "))
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
		if !e.IsDir() || e.Name() == modulith {
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
	configs, err := configGlob(root)
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

// configGlob finds every service's config, wherever it sits.
//
// Most are under internal/config. The gateway's is not: the modulith imports it,
// and an internal package cannot be imported across module boundaries. Both
// shapes are looked for rather than one, because a glob that quietly matches
// fewer files than it used to is a check that quietly does less.
func configGlob(root string) ([]string, error) {
	inner, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "config", "config.go"))
	if err != nil {
		return nil, err
	}
	outer, err := filepath.Glob(filepath.Join(root, "services", "*", "config", "config.go"))
	if err != nil {
		return nil, err
	}
	return append(inner, outer...), nil
}

// Every service is probed, and the two probes ask different questions.
//
// Readiness must ask /readyz and liveness must ask /healthz, and getting them
// the wrong way round is worse than having neither.
//
// All twenty-five probes that existed pointed readiness at /healthz, which at
// the time returned 200 unconditionally — so readiness could not fail, and a pod
// whose database had gone stayed in the Service's endpoints being sent traffic
// it could not serve. Four services had no probes at all, so nothing would ever
// have restarted them or taken them out.
//
// The other direction is the reason liveness must NOT ask /readyz: a failing
// liveness probe kills the pod, so tying it to the database would turn a
// recoverable database blip into a crash loop across the whole platform.
//
// RUN WITH -count=1.
func TestReadinessAsksReadyzAndLivenessAsksHealthz(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "services", "*", "deployments", "k8s", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 20 {
		t.Fatalf("found only %d deployments; the glob has stopped matching and a "+
			"check that finds nothing passes", len(files))
	}

	probe := regexp.MustCompile(`(?s)(readinessProbe|livenessProbe):.*?path:\s*(\S+)`)

	var problems []string
	for _, path := range files {
		svc := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]string{}
		// Each probe block, matched from its keyword to the first path under it.
		for _, kind := range []string{"readinessProbe", "livenessProbe"} {
			idx := strings.Index(string(src), kind+":")
			if idx < 0 {
				problems = append(problems, svc+" has no "+kind)
				continue
			}
			m := probe.FindStringSubmatch(string(src[idx:]))
			if m == nil {
				problems = append(problems, svc+"'s "+kind+" has no path")
				continue
			}
			found[kind] = m[2]
		}
		if p, ok := found["readinessProbe"]; ok && p != "/readyz" {
			problems = append(problems, svc+"'s readinessProbe asks "+p+
				", which cannot report a dependency failure")
		}
		if p, ok := found["livenessProbe"]; ok && p != "/healthz" {
			problems = append(problems, svc+"'s livenessProbe asks "+p+
				"; a liveness probe that depends on the database turns an outage "+
				"into a crash loop")
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Errorf("%d probe problems:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	t.Logf("checked %d deployments", len(files))
}

// Every compose file in the repository is valid YAML.
//
// services/cattle-service/docker-compose.yaml was not, and had not been for as
// long as it has existed: three healthcheck settings on one line separated by
// semicolons, which is a shell habit and not YAML. `docker compose up` in that
// directory fails at parse. Nothing said so, because nothing in this repository
// had ever read the per-service compose files — they are deployment descriptors,
// and deployment descriptors are the part that is never exercised until the day
// somebody runs them.
//
// RUN WITH -count=1. These files are outside this module.
func TestEveryComposeFileParses(t *testing.T) {
	root := repoRoot(t)
	var files []string
	for _, pattern := range []string{
		filepath.Join(root, "docker-compose*.yaml"),
		filepath.Join(root, "docker-compose*.yml"),
		filepath.Join(root, "services", "*", "docker-compose*.yaml"),
		filepath.Join(root, "services", "*", "docker-compose*.yml"),
	} {
		found, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	if len(files) < 10 {
		t.Fatalf("found only %d compose files; the globs have probably stopped "+
			"matching, and a check that finds nothing passes", len(files))
	}

	var broken []string
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(src, &doc); err != nil {
			rel, _ := filepath.Rel(root, path)
			broken = append(broken, rel+": "+err.Error())
			continue
		}
		if _, ok := doc["services"]; !ok {
			rel, _ := filepath.Rel(root, path)
			broken = append(broken, rel+": no services key")
		}
	}
	sort.Strings(broken)
	if len(broken) > 0 {
		t.Errorf("%d compose files do not parse:\n  %s",
			len(broken), strings.Join(broken, "\n  "))
	}
	t.Logf("checked %d compose files", len(files))
}

// A compose file that connects in the clear says so, in the file.
//
// Every service in this repository connected with sslmode=disable, and none of
// it was a decision — it was what the first service was written with and what
// the next twenty-seven copied. libs/integrity/tenantdb now refuses such a
// connection unless the deployment states the case in words, and this is the
// other half: a compose file cannot both keep sslmode=disable and stay silent
// about it, because a service started from it would not come up.
//
// The point is not the variable. It is that the two lines sit together, so
// somebody pointing DATABASE_URL at a database across a network has the
// sentence about what that means directly under their cursor.
//
// RUN WITH -count=1.
func TestAComposeFileThatConnectsInTheClearSaysSo(t *testing.T) {
	root := repoRoot(t)
	var files []string
	for _, pattern := range []string{
		filepath.Join(root, "docker-compose*.yaml"),
		filepath.Join(root, "services", "*", "docker-compose*.yaml"),
	} {
		found, _ := filepath.Glob(pattern)
		files = append(files, found...)
	}
	if len(files) < 10 {
		t.Fatalf("found only %d compose files; the globs have probably stopped matching", len(files))
	}

	var silent []string
	checked := 0
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)

		var doc struct {
			Services map[string]struct {
				Environment yaml.Node `yaml:"environment"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(src, &doc); err != nil {
			t.Errorf("%s does not parse: %v", rel, err)
			continue
		}

		for name, svc := range doc.Services {
			env := environmentOf(svc.Environment)
			dsn, connects := env["DATABASE_URL"]
			if !connects {
				continue
			}
			checked++
			if !strings.Contains(dsn, "sslmode=disable") &&
				!strings.Contains(dsn, "sslmode=allow") &&
				!strings.Contains(dsn, "sslmode=prefer") {
				continue
			}
			if env["GAVYA_INSECURE_DATABASE"] == "" {
				silent = append(silent, rel+": "+name)
			}
		}
	}
	sort.Strings(silent)
	if len(silent) > 0 {
		t.Errorf("%d services connect in the clear without saying so:\n  %s\n"+
			"Each would refuse to start: libs/integrity/tenantdb declines an "+
			"unencrypted connection unless the deployment states the case.",
			len(silent), strings.Join(silent, "\n  "))
	}
	if checked == 0 {
		t.Error("no service in any compose file has a DATABASE_URL, so this check " +
			"compared two empty lists")
	}
	t.Logf("checked %d services across %d compose files", checked, len(files))
}

// environmentOf reads compose's two spellings of the environment block.
//
// A map (KEY: value) and a list (- KEY=value) mean the same thing to compose,
// and both are used in this repository. A check that understands one of them
// passes everything written in the other, silently.
func environmentOf(node yaml.Node) map[string]string {
	env := map[string]string{}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			env[node.Content[i].Value] = node.Content[i+1].Value
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if key, value, found := strings.Cut(item.Value, "="); found {
				env[key] = value
			}
		}
	}
	return env
}

// Every service's NetworkPolicy allows exactly the callers it has.
//
// This is the check that makes the policies worth having. Two lists of the same
// thing, compared: the callers a service actually has, read out of what every
// other service's config.go looks up, against the podSelectors in its policy.
//
// Both directions matter and they fail differently.
//
// A caller present in code and missing from the policy fails at runtime, as a
// timeout, in the one deployment shape where it is hard to reproduce — and
// settlement-service reading procurement is exactly that edge: a settlement that
// gathers nothing reports a fortnight in which every producer earned zero.
//
// A caller present in the policy and not in code is the more interesting one. It
// is a hole nothing will ever close, because nothing fails: the platform works
// perfectly with a pod allowed to reach a service it has no reason to, and that
// pod can send any tenant and any permissions it likes.
//
// RUN WITH -count=1. These files are outside this module.
func TestEveryNetworkPolicyAllowsExactlyTheCallersThatExist(t *testing.T) {
	root := repoRoot(t)
	services, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	if len(services) < 20 {
		t.Fatalf("found only %d services; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(services))
	}

	// Who calls whom, inverted: this check asks who may reach a service, and the
	// derivation says who each service reaches.
	//
	// Inverted from the same function the egress check uses rather than derived
	// again here. Two copies of one derivation is two answers about the same edge
	// the first time somebody changes one of them, and the two would then disagree
	// about whether a call is allowed in and allowed out.
	callersOf := map[string][]string{}
	for caller, peers := range callGraph(t, services) {
		for _, peer := range peers {
			if !contains(callersOf[peer], caller) {
				callersOf[peer] = append(callersOf[peer], caller)
			}
		}
	}
	if len(callersOf) < 20 {
		t.Fatalf("derived callers for only %d services; the pattern has probably "+
			"stopped matching config.go, and a check with nothing to compare passes",
			len(callersOf))
	}

	for _, dir := range services {
		svc := filepath.Base(dir)
		path := filepath.Join(dir, "deployments", "k8s", "networkpolicy.yaml")
		src, err := os.ReadFile(path)
		if err != nil {
			// Reported by TestEveryServiceHasKubernetesManifests; not repeated.
			continue
		}

		var policy struct {
			Spec struct {
				PodSelector struct {
					MatchLabels map[string]string `yaml:"matchLabels"`
				} `yaml:"podSelector"`
				Ingress []struct {
					From []struct {
						PodSelector *struct {
							MatchLabels map[string]string `yaml:"matchLabels"`
						} `yaml:"podSelector"`
						NamespaceSelector *struct {
							MatchLabels map[string]string `yaml:"matchLabels"`
						} `yaml:"namespaceSelector"`
						IPBlock *struct {
							CIDR string `yaml:"cidr"`
						} `yaml:"ipBlock"`
					} `yaml:"from"`
				} `yaml:"ingress"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal(src, &policy); err != nil {
			t.Errorf("%s does not parse: %v", svc, err)
			continue
		}

		// The policy has to select the pods it claims to protect. One that
		// selects nothing applies cleanly and guards nothing.
		if got := policy.Spec.PodSelector.MatchLabels["app"]; got != svc {
			t.Errorf("%s's policy selects app=%q, so it protects something else "+
				"or nothing at all", svc, got)
		}

		allowed := map[string]bool{}
		for _, rule := range policy.Spec.Ingress {
			if len(rule.From) == 0 {
				t.Errorf("%s has an ingress rule with no source, which allows every "+
					"pod in the cluster and every address outside it", svc)
				continue
			}
			for _, from := range rule.From {
				switch {
				case from.IPBlock != nil:
					// Ingress rules are OR-ed, so one broad ipBlock undoes every
					// podSelector beside it. This has been written here once
					// already, for the health probes, and it made the policy
					// decorative while leaving it looking correct.
					t.Errorf("%s allows the address range %s. Ingress rules are OR-ed, "+
						"so this permits every source in that range regardless of the "+
						"podSelectors beside it", svc, from.IPBlock.CIDR)
				case from.PodSelector != nil:
					allowed[from.PodSelector.MatchLabels["app"]] = true
				case from.NamespaceSelector != nil:
					// The gateway's ingress controller, which is not a service.
					allowed[gatewayIngress] = true
				}
			}
		}

		if svc == "gateway-service" {
			// The front door is reached from outside, not from a service.
			if !allowed[gatewayIngress] {
				t.Error("gateway-service's policy names no ingress controller, so " +
					"nothing outside the cluster can reach the only door there is")
			}
			continue
		}

		want := append([]string{}, callersOf[svc]...)
		sort.Strings(want)
		var missing, extra []string
		for _, caller := range want {
			if !allowed[caller] {
				missing = append(missing, caller)
			}
		}
		for caller := range allowed {
			if caller != gatewayIngress && !contains(want, caller) {
				extra = append(extra, caller)
			}
		}
		sort.Strings(extra)

		if len(missing) > 0 {
			t.Errorf("%s is called by %s and its policy does not allow them, so those "+
				"calls time out in the cluster and nowhere else",
				svc, strings.Join(missing, ", "))
		}
		if len(extra) > 0 {
			t.Errorf("%s's policy allows %s, and nothing in this repository calls it "+
				"from there. Nothing will ever fail because of this, and that pod can "+
				"send %s any tenant and any permissions it likes",
				svc, strings.Join(extra, ", "), svc)
		}
	}
}

// gatewayIngress stands for "something outside the cluster", which is a
// namespaceSelector rather than a service and so is not compared against code.
const gatewayIngress = "<ingress controller>"

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// No service compiles in a credential, and none defaults its database.
//
// Twenty-two of them defaulted DATABASE_URL to
// postgres://postgres:secret@localhost:5432/dairy?sslmode=disable and six to the
// same thing without the password. Two separate problems in one line: a password
// in every binary this repository builds, and a service that starts without
// being told where its database is and connects to whatever is on localhost —
// which on a developer's machine is their own database and in a container is
// nothing, so the failure mode differs by where it runs.
//
// It is now unset by default and libs/integrity/tenantdb refuses an empty URL,
// naming the setting. This is the check that it stays that way: a default is one
// line, it is convenient, and it is exactly what twenty-eight services already
// copied from each other once.
//
// RUN WITH -count=1. These files are in other modules.
func TestNoServiceCompilesInACredentialOrADatabase(t *testing.T) {
	root := repoRoot(t)
	var configs []string
	for _, pattern := range []string{
		filepath.Join(root, "services", "*", "internal", "config", "*.go"),
		filepath.Join(root, "services", "*", "config", "*.go"),
	} {
		found, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		configs = append(configs, found...)
	}
	if len(configs) < 20 {
		t.Fatalf("found only %d config files; the globs have probably stopped "+
			"matching, and a check that finds nothing passes", len(configs))
	}

	// A URL literal carrying user:password@ — the shape of a credential,
	// whatever the setting is called.
	credential := regexp.MustCompile(`"[a-z][a-z0-9+.-]*://[^"/@\s]+:[^"/@\s]+@`)
	// And any default at all for the database, with or without one.
	defaulted := regexp.MustCompile(`getEnv\(\s*"DATABASE_URL"\s*,\s*"[^"]`)

	var withCredential, withDefault []string
	for _, path := range configs {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		if m := credential.Find(src); m != nil {
			withCredential = append(withCredential, rel+": "+string(m)+"...")
		}
		if defaulted.Match(src) {
			withDefault = append(withDefault, rel)
		}
	}
	sort.Strings(withCredential)
	sort.Strings(withDefault)

	if len(withCredential) > 0 {
		t.Errorf("%d config files carry a credential in a literal:\n  %s\n"+
			"It ends up in every binary built from this repository, and in the "+
			"source history whatever is done to it later.",
			len(withCredential), strings.Join(withCredential, "\n  "))
	}
	if len(withDefault) > 0 {
		t.Errorf("%d services default DATABASE_URL:\n  %s\n"+
			"A service started without it then connects somewhere rather than "+
			"saying it was not configured, and which somewhere depends on the machine.",
			len(withDefault), strings.Join(withDefault, "\n  "))
	}
	t.Logf("checked %d config files", len(configs))
}

// Every service's egress policy allows exactly the calls it makes.
//
// The ingress policies bound who may start a conversation with a service. This
// bounds where a service can send things once something inside it has been taken
// over — which an ingress policy says nothing about at all, and which is the
// half that matters after the first mistake rather than before it.
//
// Compared against the same call graph as the ingress check and failing the same
// two ways, with the directions swapped. A call in code and missing from the
// policy is a timeout in the cluster and nowhere else. A peer in the policy and
// not in code is a route out that nothing will ever close, because nothing fails.
//
// Two things every one of them must have, and both are quiet when absent:
//
//   - DNS. Every other rule names a Service, and a Service name is resolved
//     before it is dialled, so a policy without this denies everything it was
//     written to allow — as a timeout, not as anything a reader can see.
//   - The database. A service that cannot reach it answers nothing, and the
//     rule that allows it is the one this repository cannot verify: it assumes
//     a Service named postgres in the namespace, which is what every
//     DATABASE_URL here says and may not be what a given deployment runs.
//
// RUN WITH -count=1. These files are outside this module.
func TestEveryEgressPolicyAllowsExactlyTheCallsThatExist(t *testing.T) {
	root := repoRoot(t)
	services, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	if len(services) < 20 {
		t.Fatalf("found only %d services; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(services))
	}

	callsOf := callGraph(t, services)
	if len(callsOf) < 2 {
		t.Fatalf("derived calls for only %d services; the pattern has probably "+
			"stopped matching config.go", len(callsOf))
	}

	for _, dir := range services {
		svc := filepath.Base(dir)
		src, err := os.ReadFile(filepath.Join(dir, "deployments", "k8s", "egresspolicy.yaml"))
		if err != nil {
			// Reported by TestEveryServiceHasKubernetesManifests; not repeated.
			continue
		}

		var policy struct {
			Spec struct {
				PodSelector struct {
					MatchLabels map[string]string `yaml:"matchLabels"`
				} `yaml:"podSelector"`
				PolicyTypes []string `yaml:"policyTypes"`
				Egress      []struct {
					To []struct {
						PodSelector *struct {
							MatchLabels map[string]string `yaml:"matchLabels"`
						} `yaml:"podSelector"`
						NamespaceSelector *struct {
							MatchLabels map[string]string `yaml:"matchLabels"`
						} `yaml:"namespaceSelector"`
						IPBlock *struct {
							CIDR string `yaml:"cidr"`
						} `yaml:"ipBlock"`
					} `yaml:"to"`
					Ports []struct {
						Port any `yaml:"port"`
					} `yaml:"ports"`
				} `yaml:"egress"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal(src, &policy); err != nil {
			t.Errorf("%s's egress policy does not parse: %v", svc, err)
			continue
		}

		if got := policy.Spec.PodSelector.MatchLabels["app"]; got != svc {
			t.Errorf("%s's egress policy selects app=%q, so it bounds something else "+
				"or nothing at all", svc, got)
		}
		if !contains(policy.Spec.PolicyTypes, "Egress") {
			t.Errorf("%s's egress policy does not list Egress in policyTypes, so it "+
				"restricts nothing", svc)
		}

		allowed := map[string]bool{}
		dns, database := false, false
		for _, rule := range policy.Spec.Egress {
			if len(rule.To) == 0 {
				t.Errorf("%s has an egress rule with no destination, which permits "+
					"every address there is and undoes every rule beside it", svc)
				continue
			}
			for _, to := range rule.To {
				switch {
				case to.IPBlock != nil:
					// Egress rules are OR-ed like ingress ones. A broad ipBlock
					// here is a route to anywhere, which is the thing being
					// prevented.
					if to.IPBlock.CIDR == "0.0.0.0/0" {
						t.Errorf("%s allows egress to 0.0.0.0/0, which is every address "+
							"there is — the policy applies cleanly and bounds nothing", svc)
					}
				case to.NamespaceSelector != nil:
					if to.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "kube-system" {
						dns = true
					}
				case to.PodSelector != nil:
					app := to.PodSelector.MatchLabels["app"]
					if app == "postgres" {
						database = true
						continue
					}
					allowed[app] = true
				}
			}
		}

		if !dns {
			t.Errorf("%s's egress policy does not allow DNS. Every other rule in it "+
				"names a Service, and a Service name is resolved before it is dialled, "+
				"so this denies everything it was written to allow — as a timeout", svc)
		}
		if !database {
			t.Errorf("%s's egress policy does not allow the database, so the service "+
				"starts, passes its liveness probe and answers nothing", svc)
		}

		want := append([]string{}, callsOf[svc]...)
		sort.Strings(want)
		var missing, extra []string
		for _, peer := range want {
			if !allowed[peer] {
				missing = append(missing, peer)
			}
		}
		for peer := range allowed {
			if !contains(want, peer) {
				extra = append(extra, peer)
			}
		}
		sort.Strings(extra)

		if len(missing) > 0 {
			t.Errorf("%s calls %s and its egress policy does not allow it, so those "+
				"calls time out in the cluster and nowhere else",
				svc, strings.Join(missing, ", "))
		}
		if len(extra) > 0 {
			t.Errorf("%s's egress policy allows it to reach %s, and nothing in this "+
				"repository calls them from there. Nothing will ever fail because of "+
				"this, and a compromise of %s reaches them",
				svc, strings.Join(extra, ", "), svc)
		}
	}
}

// callGraph reads who calls whom out of the *_URL settings each config looks up.
//
// The one source both policy checks compare against, so ingress and egress
// cannot disagree about the same edge — which they would, eventually, as two
// copies of the same derivation.
func callGraph(t *testing.T, services []string) map[string][]string {
	t.Helper()
	reads := regexp.MustCompile(`"([A-Z][A-Z0-9_]*?)(?:_SERVICE)?_URL"`)
	known := map[string]bool{}
	for _, dir := range services {
		known[filepath.Base(dir)] = true
	}

	calls := map[string][]string{}
	for _, dir := range services {
		caller := filepath.Base(dir)
		var src []byte
		for _, pattern := range []string{"internal/config/*.go", "config/*.go"} {
			files, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, f := range files {
				if strings.HasSuffix(f, "_test.go") {
					continue
				}
				if b, err := os.ReadFile(f); err == nil {
					src = append(src, b...)
				}
			}
		}
		for _, m := range reads.FindAllSubmatch(src, -1) {
			name := strings.ToLower(strings.ReplaceAll(string(m[1]), "_", "-")) + "-service"
			// DATABASE_URL is not a service; the Rust tier has no manifests here;
			// the modulith points every URL at itself.
			if !known[name] || name == caller {
				continue
			}
			if !contains(calls[caller], name) {
				calls[caller] = append(calls[caller], name)
			}
		}
	}
	return calls
}

// Every secret a deployment references is one something produces.
//
// Twenty-eight deployments name a secret in envFrom:
//
//	envFrom:
//	- secretRef:
//	    name: settlement-service-secret
//
// and nothing in this repository created one. Applying the manifests produced
// twenty-eight pods in CreateContainerConfigError, which is the good outcome.
// The bad one is a cluster where somebody made the secrets by hand on the first
// afternoon, and the service added six months later has none — the same failure
// this file already catches for compose entries and Kubernetes manifests, in the
// one place it was still possible.
//
// scripts/make-secrets.sh produces them, and reads which services need one from
// the deployments themselves rather than from a list. This runs it and checks
// that what comes out covers what is asked for, because a generator that reads a
// glob can stop matching, and then it silently produces nothing.
func TestEverySecretADeploymentReferencesIsProduced(t *testing.T) {
	root := repoRoot(t)

	referenced := map[string]string{} // secret name -> service that wants it
	deployments, err := filepath.Glob(filepath.Join(root, "services", "*", "deployments", "k8s", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) < 20 {
		t.Fatalf("found only %d deployments; the glob has stopped matching and a check "+
			"that finds nothing passes", len(deployments))
	}
	secretRef := regexp.MustCompile(`secretRef:\s*\n\s*name:\s*(\S+)`)
	for _, path := range deployments {
		svc := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range secretRef.FindAllStringSubmatch(string(b), -1) {
			referenced[m[1]] = svc
		}
	}
	if len(referenced) == 0 {
		t.Fatal("no deployment references a secret, which cannot be right")
	}

	out, err := exec.Command(filepath.Join(root, "scripts", "make-secrets.sh"),
		"--password", "not-a-real-password", "--stdout").Output()
	if err != nil {
		t.Fatalf("run scripts/make-secrets.sh: %v", err)
	}

	produced := map[string]map[string]string{}
	for _, doc := range strings.Split(string(out), "\n---") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var secret struct {
			Kind       string                `yaml:"kind"`
			Metadata   struct{ Name string } `yaml:"metadata"`
			StringData map[string]string     `yaml:"stringData"`
		}
		if err := yaml.Unmarshal([]byte(doc), &secret); err != nil {
			t.Fatalf("the generator produced something that is not YAML: %v\n%s", err, doc)
		}
		if secret.Kind != "Secret" {
			continue
		}
		produced[secret.Metadata.Name] = secret.StringData
	}

	for name, svc := range referenced {
		data, ok := produced[name]
		if !ok {
			t.Errorf("%s references the secret %q and scripts/make-secrets.sh does not "+
				"produce it, so the deployment cannot start: the pod sits in "+
				"CreateContainerConfigError", svc, name)
			continue
		}
		// The one credential every service reads. A secret that exists and is
		// empty starts the pod and fails at the first query, which is worse:
		// the deployment looks healthy until something asks it for data.
		if data["DATABASE_URL"] == "" {
			t.Errorf("the secret %q carries no DATABASE_URL, so %s starts and fails at its "+
				"first query rather than at boot", name, svc)
		}
	}

	// And nothing spare. A secret produced for a service that does not exist is
	// a credential distributed for no reason.
	for name := range produced {
		if _, ok := referenced[name]; !ok {
			t.Errorf("scripts/make-secrets.sh produces %q and no deployment references it", name)
		}
	}

	t.Logf("%d secrets referenced, %d produced", len(referenced), len(produced))
}

// Nothing a service reads as a credential is left for somebody to remember.
//
// The check above says the secret exists. This one says it is the right shape:
// every environment variable a service reads whose name says it carries a
// credential is supplied by its ConfigMap or its Secret, and not by neither.
//
// The failure it guards is a service that starts reading a new one — an API key
// for something, a signing secret — and a deployment that supplies it on the
// developer's machine through a shell and nowhere else.
func TestEveryCredentialAServiceReadsIsSuppliedByItsDeployment(t *testing.T) {
	root := repoRoot(t)

	// Names that say "this is a credential". Deliberately a small list: a
	// heuristic that matches everything reports noise and gets ignored.
	credential := regexp.MustCompile(`PASSWORD|SECRET|TOKEN|CREDENTIAL|DATABASE_URL|PRIVATE_KEY`)
	reads := regexp.MustCompile(`(?:os\.Getenv|getEnv)\("([A-Z][A-Z0-9_]*)"`)

	services, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range services {
		svc := filepath.Base(dir)
		wants := map[string]bool{}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, m := range reads.FindAllStringSubmatch(string(b), -1) {
				if credential.MatchString(m[1]) {
					wants[m[1]] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(wants) == 0 {
			continue
		}
		checked++

		supplied := map[string]bool{}
		manifest := filepath.Join(dir, "deployments", "k8s", "service.yaml")
		if b, err := os.ReadFile(manifest); err == nil {
			for name := range wants {
				if strings.Contains(string(b), name+":") {
					supplied[name] = true
				}
			}
		}
		deployment := filepath.Join(dir, "deployments", "k8s", "deployment.yaml")
		b, err := os.ReadFile(deployment)
		if err != nil {
			t.Errorf("%s reads %v and has no deployment manifest", svc, keysOf(wants))
			continue
		}
		hasSecret := strings.Contains(string(b), "secretRef")

		for name := range wants {
			if supplied[name] {
				continue
			}
			// DATABASE_URL is what the generated secret carries; anything else
			// has to be named somewhere a deployer can see it.
			if name == "DATABASE_URL" && hasSecret {
				continue
			}
			t.Errorf("%s reads %s and neither its ConfigMap nor its Secret supplies it. "+
				"A credential that only exists in somebody's shell is one the deployment "+
				"does not have.", svc, name)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d services were found to read a credential, and there are more "+
			"than that; the patterns have stopped matching", checked)
	}
	t.Logf("%d services read a credential, and each one's deployment supplies it", checked)
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Continuous integration runs the gate, and not a copy of it.
//
// scripts/check-all.sh is the whole of this repository's quality control and
// nothing ran it: it ran when somebody remembered. The workflow added to run it
// has the failure mode every pipeline has, which is to grow its own list of
// steps — `go test ./...` here, a vet there — until "it passes in CI" and "it
// passes locally" are two claims about two different things, and the one CI
// makes is the weaker.
//
// So the workflow calls the script, and this says so. It also checks the two
// pieces of setup the gate silently degrades without: a full clone, because the
// upgrade test applies every committed version of each schema and a shallow one
// makes it compare a version against itself, and a DSN with a %s in it, because
// without one every service shares a database and the suite passes anyway.
func TestContinuousIntegrationRunsTheGate(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, ".github", "workflows", "check.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the workflow: %v\nNothing runs scripts/check-all.sh without it, "+
			"and a gate that depends on being remembered stops happening the week "+
			"everyone is busy.", err)
	}

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]any    `yaml:"with"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("the workflow is not valid YAML, so it does not run at all: %v", err)
	}
	if len(wf.Jobs) == 0 {
		t.Fatal("the workflow defines no jobs")
	}

	var runsGate, fullClone, installsLinter bool
	var dsn string
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "check-all.sh") {
				runsGate = true
				if v, ok := step.Env["TEST_DATABASE_DSN"]; ok {
					dsn = v
				}
			}
			if strings.Contains(step.Run, "golangci-lint") {
				installsLinter = true
			}
			if strings.HasPrefix(step.Uses, "actions/checkout") {
				if depth, ok := step.With["fetch-depth"]; ok {
					fullClone = fullClone || depth == 0
				}
			}
		}
	}

	if !runsGate {
		t.Error("no job in the workflow runs scripts/check-all.sh. A pipeline that lists " +
			"its own steps drifts from the script, and then what CI checks is not what " +
			"a person checks.")
	}
	if !fullClone {
		t.Error("no checkout asks for fetch-depth: 0. e2e/upgrade_test.go applies every " +
			"committed version of each schema in order, and a shallow clone leaves it " +
			"comparing one version against itself — which passes.")
	}
	if !strings.Contains(dsn, "%s") {
		t.Errorf("the gate runs with TEST_DATABASE_DSN=%q, which has no %%s in it. "+
			"Every service would share one database and the suite would pass anyway.", dsn)
	}
	if !installsLinter {
		t.Error("no job installs golangci-lint. The gate skips linting by name when it " +
			"is absent, which is right on a laptop and wrong here: CI would run every " +
			"other check, print one line about the one it did not, and pass.")
	}
}

// Every service is scraped, in both deployment shapes.
//
// Every service has served /metrics since readiness was added and nothing read
// them, so the platform's observability was a page that existed. Adding a reader
// creates the next failure, which is quieter: a service added later and not
// added to the scrape list. It runs, it serves its metrics, and nothing asks —
// and the first anybody knows is that the dashboard has a gap where the outage
// was.
//
// Both shapes, because a service scraped under compose and not under Kubernetes
// is watched in testing and unwatched in production, which is the worse half of
// the two.
func TestEveryServiceIsScrapedInBothShapes(t *testing.T) {
	root := repoRoot(t)

	// compose: the static scrape list.
	var prom struct {
		ScrapeConfigs []struct {
			JobName       string `yaml:"job_name"`
			StaticConfigs []struct {
				Targets []string `yaml:"targets"`
			} `yaml:"static_configs"`
		} `yaml:"scrape_configs"`
	}
	b, err := os.ReadFile(filepath.Join(root, "deploy", "monitoring", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read the scrape configuration: %v", err)
	}
	if err := yaml.Unmarshal(b, &prom); err != nil {
		t.Fatalf("the scrape configuration is not valid YAML, so Prometheus would not "+
			"start and nothing would be watched at all: %v", err)
	}
	scraped := map[string]string{} // service -> target
	for _, job := range prom.ScrapeConfigs {
		name := strings.TrimPrefix(job.JobName, "gavya-")
		for _, sc := range job.StaticConfigs {
			for _, target := range sc.Targets {
				scraped[name] = target
			}
		}
	}

	// Compared against compose rather than against libs/integrity/ports, and
	// that is the whole substance of this check.
	//
	// The registry is what a service listens on when run directly on a
	// developer's machine, where everything shares a host. Under compose each
	// service is alone in its container and listens on 8080, and the registry
	// number is what is published to the host — and for several services not even
	// that: audit is 8100 in the registry and published on 8094. Prometheus runs
	// inside the compose network and reaches a container by name and container
	// port.
	//
	// The first version of this file compared against the registry, so it passed
	// while the scrape configuration pointed at a port nothing listens on, for
	// every service. Monitoring switched on and blind, with a green test over it.
	listening := composeListeningPorts(t, root)
	if len(listening) < 20 {
		t.Fatalf("read only %d services out of docker-compose.yaml; the parsing has "+
			"stopped matching and this comparison would pass against anything", len(listening))
	}
	for container, port := range listening {
		name := strings.TrimSuffix(container, "-service")
		target, ok := scraped[name]
		if !ok {
			t.Errorf("%s is not in deploy/monitoring/prometheus.yml, so nothing reads its "+
				"metrics and an outage in it is invisible", container)
			continue
		}
		want := fmt.Sprintf("%s:%d", container, port)
		if target != want {
			t.Errorf("%s is scraped at %s and listens on %s inside the compose network; a "+
				"scrape of the wrong port fails quietly and the service reads as down",
				container, target, want)
		}
	}
	for name := range scraped {
		if _, ok := listening[name+"-service"]; !ok {
			t.Errorf("prometheus.yml scrapes %q, which is not a service in compose", name)
		}
	}

	// Kubernetes: the pod annotations a cluster Prometheus discovers.
	deployments, err := filepath.Glob(filepath.Join(root, "services", "*", "deployments", "k8s", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) < 20 {
		t.Fatalf("found only %d deployments; the glob has stopped matching", len(deployments))
	}
	for _, path := range deployments {
		svc := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc struct {
				Kind string `yaml:"kind"`
				Spec struct {
					Template struct {
						Metadata struct {
							Annotations map[string]string `yaml:"annotations"`
						} `yaml:"metadata"`
					} `yaml:"template"`
				} `yaml:"spec"`
			}
			if err := dec.Decode(&doc); err != nil {
				break
			}
			if doc.Kind != "Deployment" {
				continue
			}
			found = true
			a := doc.Spec.Template.Metadata.Annotations
			if a["prometheus.io/scrape"] != "true" {
				t.Errorf("%s's pods are not annotated for scraping, so a cluster Prometheus "+
					"never finds them", svc)
			}
			if a["prometheus.io/path"] != "/metrics" {
				t.Errorf("%s's pods point the scraper at %q rather than /metrics",
					svc, a["prometheus.io/path"])
			}
			if a["prometheus.io/port"] == "" {
				t.Errorf("%s's pods say to scrape and not on which port", svc)
			}
		}
		if !found {
			t.Errorf("%s has no Deployment in its manifests", svc)
		}
	}

	t.Logf("%d services scraped under compose, %d deployments annotated for Kubernetes",
		len(scraped), len(deployments))
}

// Every alert names a metric this platform actually emits.
//
// The easiest way to have monitoring that does nothing is an alert on a metric
// with a plausible name that nothing publishes. It scrapes clean, it evaluates
// to no data, it never fires, and a rule that never fires is indistinguishable
// from a system that never breaks. Nobody notices for months, and what they
// notice then is the outage it did not catch.
//
// So the metric names are not read out of a list somebody maintains: a live
// metrics handler is exercised and scraped, and the names in the alerts are
// checked against what actually came back.
func TestEveryAlertNamesAMetricThatExists(t *testing.T) {
	root := repoRoot(t)

	// What the platform really publishes, from a handler that has served a
	// request and a failure so that every family appears.
	m := observe.NewMetrics()
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "Boom") {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	for _, path := range []string{"/milk.v1.MilkService/RecordMilk", "/milk.v1.MilkService/Boom"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", path, nil))
	}
	rec := httptest.NewRecorder()
	m.Handler()(rec, httptest.NewRequest("GET", "/metrics", nil))

	emitted := map[string]bool{}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, "{")
		name, _, _ = strings.Cut(name, " ")
		emitted[strings.TrimSpace(name)] = true
	}
	if len(emitted) < 4 {
		t.Fatalf("only %d metric names came out of a live handler, which cannot be right; "+
			"the parsing has stopped matching and this check would pass against anything",
			len(emitted))
	}

	// And the gauges a service publishes about itself, which a live handler in
	// this test cannot show: they are registered by settlement and audit at boot,
	// against their own databases.
	//
	// Found in the code that publishes them rather than listed here, so a gauge
	// that is renamed or deleted takes its alert with it. A list would be a
	// second copy, and the second copy is the one that goes stale — which is the
	// whole failure this test exists to catch, one level up.
	for name := range gaugesServicesPublish(t, root) {
		emitted[name] = true
	}

	// Names Prometheus itself supplies rather than the platform.
	fromPrometheus := map[string]bool{"up": true}

	b, err := os.ReadFile(filepath.Join(root, "deploy", "monitoring", "alerts.yml"))
	if err != nil {
		t.Fatalf("read the alert rules: %v", err)
	}
	var rules struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert       string            `yaml:"alert"`
				Expr        string            `yaml:"expr"`
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(b, &rules); err != nil {
		t.Fatalf("the alert rules are not valid YAML, so Prometheus would refuse to load "+
			"them and nothing would alert: %v", err)
	}

	// A metric name in a PromQL expression: an identifier not immediately
	// followed by an opening bracket, which is how a function reads.
	ident := regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\b`)
	promFunctions := map[string]bool{
		"rate": true, "sum": true, "by": true, "histogram_quantile": true,
		"resets": true, "increase": true, "avg": true, "max": true, "min": true,
		"count": true, "le": true, "job": true, "procedure": true, "code": true,
		"and": true, "or": true, "unless": true, "without": true, "on": true,
		"group_left": true, "group_right": true, "irate": true, "delta": true,
	}

	alerts := 0
	for _, group := range rules.Groups {
		for _, rule := range group.Rules {
			alerts++
			if rule.Annotations["summary"] == "" || rule.Annotations["description"] == "" {
				t.Errorf("%s has no summary or no description. An alert nobody knows how to "+
					"act on gets silenced, and the silence outlives the reason for it",
					rule.Alert)
			}
			for _, m := range ident.FindAllStringSubmatch(rule.Expr, -1) {
				name := m[1]
				if promFunctions[name] || fromPrometheus[name] {
					continue
				}
				// A bare word that is not a metric: a label value, a number's
				// suffix, a matcher. Only names shaped like this platform's
				// metrics are checked, and every one of ours starts with gavya_.
				if !strings.HasPrefix(name, "gavya_") {
					continue
				}
				if !emitted[name] {
					t.Errorf("%s alerts on %q and no service emits that metric. "+
						"It will evaluate to no data forever, which reads exactly like a "+
						"platform that never breaks.\nemitted: %v",
						rule.Alert, name, sortedKeys(emitted))
				}
			}
		}
	}
	if alerts < 4 {
		t.Fatalf("only %d alerts were read out of the rules file, and it has more; the "+
			"parsing has stopped matching", alerts)
	}
	t.Logf("%d alerts, every metric they name is emitted", alerts)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// composeListeningPorts is what each service listens on inside the compose
// network, read from the SERVER_ADDR it is given.
func composeListeningPorts(t *testing.T, root string) map[string]int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "docker-compose.yaml"))
	if err != nil {
		t.Fatalf("read docker-compose.yaml: %v", err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &compose); err != nil {
		t.Fatalf("docker-compose.yaml is not valid YAML: %v", err)
	}
	out := map[string]int{}
	for name, svc := range compose.Services {
		if !strings.HasSuffix(name, "-service") {
			continue
		}
		addr := strings.TrimPrefix(svc.Environment["SERVER_ADDR"], ":")
		port, err := strconv.Atoi(addr)
		if err != nil {
			continue
		}
		out[name] = port
	}
	return out
}

// gaugesServicesPublish is every metric name the platform registers with
// observe.Publish or observe.PublishCounter, read out of the source.
//
// It walks libs as well as services, and that was not always true. The first
// version looked only under services/, because the only two things publishing a
// number were settlement's outbox reader and audit's chain walk. Then tenantdb
// began publishing the pool statistics — from libs/integrity, where every
// service's database connection is made — and the scanner could not see any of
// them. An alert naming one would have read as an alert on a metric nothing
// emits, which is the exact failure this whole check exists to catch, arriving
// through the check itself.
func gaugesServicesPublish(t *testing.T, root string) map[string]bool {
	t.Helper()
	// observe.Publish(observe.Gauge{ Name: "..." — the name is the first field
	// and the call is always written this way, which the check below enforces by
	// failing when it finds none. Counters are registered the same way and count
	// the same: what matters here is whether a scrape would contain the name.
	published := regexp.MustCompile(
		`observe\.Publish(?:Counter)?\(observe\.(?:Gauge|Counter)\{\s*\n?\s*Name:\s*"([a-zA-Z_][a-zA-Z0-9_]*)"`)

	out := map[string]bool{}
	for _, tree := range []string{"services", "libs"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, m := range published.FindAllStringSubmatch(string(b), -1) {
				out[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) == 0 {
		t.Fatal("nothing in the platform publishes a gauge, and several things do. " +
			"The pattern has stopped matching, and an alert on a gauge that no " +
			"longer exists would pass here")
	}
	return out
}
