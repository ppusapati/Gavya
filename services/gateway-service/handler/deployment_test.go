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

	// Who calls whom, from the settings each service reads. A service that looks
	// up FOO_SERVICE_URL or FOO_URL calls foo-service.
	reads := regexp.MustCompile(`"([A-Z][A-Z0-9_]*?)(?:_SERVICE)?_URL"`)
	callersOf := map[string][]string{}
	for _, dir := range services {
		caller := filepath.Base(dir)
		var src []byte
		for _, pattern := range []string{"internal/config/*.go", "config/*.go"} {
			files, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, f := range files {
				if strings.HasSuffix(f, "_test.go") {
					continue
				}
				b, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				src = append(src, b...)
			}
		}
		for _, m := range reads.FindAllSubmatch(src, -1) {
			name := strings.ToLower(strings.ReplaceAll(string(m[1]), "_", "-")) + "-service"
			switch {
			case name == "database-service":
				// DATABASE_URL. Not a service in this platform.
			case strings.HasSuffix(name, "-ml-service"):
				// The Rust tier, which has no manifests in this repository.
			case name == caller:
				// The modulith points every URL at itself.
			case !contains(callersOf[name], caller):
				callersOf[name] = append(callersOf[name], caller)
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
