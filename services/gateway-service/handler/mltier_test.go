package handler

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The ML tier is offered by both deployment shapes, or by neither.
//
// docker-compose.yaml ran all four Rust services and wired all four addresses.
// docker-compose.modulith.yaml — the shape meant to be deployed — defined none
// of them and left every address empty, so the advisory tier existed only in the
// deployment nobody intends to run.
//
// That is the asymmetry this repository already wrote a rule about, when the
// monitoring turned out to be in the same state: a thing present only in the
// other shape is not a weaker version of it, it is none. The rule had been
// applied to the defences and to the metrics and not to this.
//
// The tier stays opt-in — advisory means advisory — so what is compared here is
// availability, not whether it is on. docker-compose.modulith.ml.yaml is the
// overlay that runs it, and the two halves of turning it on have to arrive
// together: the services, and the addresses that reach them.
//
// RUN THESE WITH -count=1: the compose files sit outside this module and Go's
// test cache does not track them.

// mlServices is the four, by the name each compose file gives them.
//
// Written out rather than discovered, because the point of the comparison is
// that one file might be missing one of them — and a list read from the file
// being checked cannot notice that.
var mlServices = []string{
	"anomaly-service",
	"uncertainty-service",
	"reconciliation-service",
	"divergence-service",
}

// mlURLVars is what each service is reached by. Every one of these is read with
// a default of "", and empty means the estimate is skipped.
var mlURLVars = []string{
	"ANOMALY_ML_URL",
	"UNCERTAINTY_ML_URL",
	"RECONCILIATION_ML_URL",
	"DIVERGENCE_ML_URL",
}

type composeFile struct {
	Services map[string]struct {
		Environment map[string]string `yaml:"environment"`
		Build       struct {
			Context    string            `yaml:"context"`
			Dockerfile string            `yaml:"dockerfile"`
			Args       map[string]string `yaml:"args"`
		} `yaml:"build"`
	} `yaml:"services"`
}

func readCompose(t *testing.T, root, name string) composeFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var f composeFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s is not valid YAML, so it does not deploy at all: %v", name, err)
	}
	return f
}

// Both shapes can run the tier.
func TestBothShapesCanRunTheMLTier(t *testing.T) {
	root := repoRoot(t)

	separate := readCompose(t, root, "docker-compose.yaml")
	overlay := readCompose(t, root, "docker-compose.modulith.ml.yaml")

	for _, name := range mlServices {
		if _, ok := separate.Services[name]; !ok {
			t.Errorf("docker-compose.yaml does not run %s", name)
		}
		svc, ok := overlay.Services[name]
		if !ok {
			t.Errorf("docker-compose.modulith.ml.yaml does not run %s, so the shape "+
				"that ships cannot run the tier the other shape runs", name)
			continue
		}
		// Built from the same Dockerfile with the same argument, because the
		// one image takes SERVICE to choose which binary it carries. A wrong
		// argument here is a container that runs somebody else's model.
		if got := svc.Build.Args["SERVICE"]; got != name {
			t.Errorf("%s in the overlay builds with SERVICE=%q; the ml image selects "+
				"the binary from that argument, so it would run the wrong model",
				name, got)
		}
		if svc.Build.Context != "./ml" {
			t.Errorf("%s builds from %q rather than ./ml", name, svc.Build.Context)
		}
	}
}

// The overlay sets every address, and the base file sets none of them.
//
// The two halves of turning the tier on are the services running and the
// modulith knowing where they are. Apart, they make a state worth avoiding: an
// address that is set and unreachable costs a two-second dial and three attempts
// on every observation, where an unset one costs nothing because each service
// checks for empty and skips. So this fails if the base file starts naming an
// address, and if the overlay stops naming one.
func TestTurningTheMLTierOnIsOneStep(t *testing.T) {
	root := repoRoot(t)

	base := readCompose(t, root, "docker-compose.modulith.yaml")
	overlay := readCompose(t, root, "docker-compose.modulith.ml.yaml")

	baseEnv := base.Services["gavya"].Environment
	overlayEnv := overlay.Services["gavya"].Environment
	if len(overlayEnv) == 0 {
		t.Fatal("the overlay sets no environment on gavya, so the four services it " +
			"runs are never called")
	}

	for _, v := range mlURLVars {
		// The base file may mention the variable; what it must not do is give it
		// an address. `${VAR:-}` is empty unless somebody sets VAR, which is the
		// deliberate off state.
		if got := baseEnv[v]; got != "" && !strings.HasSuffix(got, ":-}") {
			t.Errorf("docker-compose.modulith.yaml sets %s to %q. The base file is the "+
				"tier switched off; an address here without the services beside it is "+
				"the half-on state that costs six seconds an observation.", v, got)
		}
		got, ok := overlayEnv[v]
		if !ok || got == "" {
			t.Errorf("docker-compose.modulith.ml.yaml runs the ML services and does not "+
				"set %s, so nothing calls them", v)
			continue
		}
		if !strings.HasPrefix(got, "http://") {
			t.Errorf("%s is %q, which is not an address the client can dial", v, got)
		}
	}
}

// Every address the overlay sets names a service the overlay runs.
//
// The failure this catches is a rename: the service becomes anomaly-svc, the URL
// still says anomaly-service, and the tier is on and unreachable — which is the
// one state worse than off, because it is off and slow.
func TestEveryMLAddressNamesAServiceThatIsThere(t *testing.T) {
	root := repoRoot(t)
	overlay := readCompose(t, root, "docker-compose.modulith.ml.yaml")

	running := map[string]bool{}
	for name := range overlay.Services {
		running[name] = true
	}

	for _, v := range mlURLVars {
		addr := overlay.Services["gavya"].Environment[v]
		host := hostOf(addr)
		if host == "" {
			t.Errorf("%s is %q, which names no host", v, addr)
			continue
		}
		if !running[host] {
			var have []string
			for name := range running {
				if name != "gavya" {
					have = append(have, name)
				}
			}
			sort.Strings(have)
			t.Errorf("%s points at %q and the overlay runs %v. An address that resolves "+
				"to nothing leaves the tier on and unreachable, which is off and slow.",
				v, host, have)
		}
	}
}

// hostOf is the host in http://host:port, empty when there is not one.
func hostOf(addr string) string {
	rest, ok := strings.CutPrefix(addr, "http://")
	if !ok {
		return ""
	}
	host, _, _ := strings.Cut(rest, ":")
	if host == "" || strings.Contains(host, "/") {
		return ""
	}
	return host
}
