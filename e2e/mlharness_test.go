//go:build e2e

// A second platform, with the ML tier turned on.
//
// The main harness deliberately runs with no ML tier configured, and that is
// worth keeping: an unreachable ML tier is a supported deployment, every caller
// has a complete deterministic answer without it, and the assertions hold
// anyway. Proving that is not optional.
//
// But it left the other half unproven. Five Rust services, their own tests
// passing and a contract test behind a build tag, and the wired-up path — a Go
// service actually consulting them over the network, during real work, and doing
// something with the answer — had never run. "It compiles and its unit tests
// pass" is not the same claim as "it works when connected", and the difference
// is where wire formats, timeouts, and advisory-degradation logic live.
//
// So both platforms exist and both run in one suite. This one starts the Rust
// binaries and a second copy of the three Go services that call them, against
// the same databases but on their own ports.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// mlService is one Rust binary and the Go environment variables that point at it.
type mlService struct {
	// bin is the cargo package name, which is also the binary name.
	bin string
	// urlEnv are the variables a Go service reads to find it. More than one
	// because a single Rust service can serve several callers.
	urlEnv []string
}

var mlServices = []mlService{
	// Scores a collection against its own history. Advisory: an observation is
	// recorded either way, unscored if this is not reachable.
	{bin: "anomaly-service", urlEnv: []string{"ANOMALY_ML_URL"}},
	// Computes standard and expanded uncertainty. Advisory: the observation is
	// recorded with the estimate marked missing.
	{bin: "uncertainty-service", urlEnv: []string{"UNCERTAINTY_ML_URL"}},
	// Asked only about divergences the deterministic classifier returned as
	// UNEXPLAINED. Its answer is stored beside the authoritative verdict, never
	// in place of it.
	{bin: "divergence-service", urlEnv: []string{"DIVERGENCE_ML_URL"}},
	// Proposes adjustments for an imbalanced window. Advisory: the window is
	// still validated and its imbalance still recorded.
	{bin: "reconciliation-service", urlEnv: []string{"RECONCILIATION_ML_URL"}},
}

// mlCallers are the Go services that consult the tier. They are started a second
// time rather than reconfigured, because the main platform's copies are what
// prove the disabled path and must keep running without ML.
//
// None of the three depends on another, which is why this can be a flat list.
var mlCallers = []string{
	"observation-service",
	"shadow-settlement-service",
	"balance-service",
}

type mlPlatform struct {
	clients map[string]*svcclient.Client
	// mlURLs is where each Rust service ended up, for tests that want to reach
	// one directly.
	mlURLs map[string]string
	tenant string
}

var (
	mlOnce    sync.Once
	mlShared  *mlPlatform
	mlErr     error
	mlSkip    string
	mlProcs   []*exec.Cmd
	mlProcsMu sync.Mutex
)

// startMLPlatform returns the ML-enabled services, scoped to a tenant of this
// test's own.
//
// Skips — loudly, naming what is missing — when the Rust toolchain is not
// available. A skip is honest; quietly running the same disabled-tier tests
// again under an ML-sounding name would not be.
func startMLPlatform(t *testing.T) *mlPlatform {
	t.Helper()
	_ = dsn(t, "postgres")

	mlOnce.Do(func() { mlShared, mlErr, mlSkip = buildAndStartML() })
	if mlSkip != "" {
		t.Skip(mlSkip)
	}
	if mlErr != nil {
		t.Fatalf("start the ML platform: %v", mlErr)
	}
	return &mlPlatform{
		clients: mlShared.clients,
		mlURLs:  mlShared.mlURLs,
		tenant:  newID("tnt"),
	}
}

func buildAndStartML() (*mlPlatform, error, string) {
	if _, err := exec.LookPath("cargo"); err != nil {
		return nil, nil, "cargo is not on PATH, so the Rust ML tier cannot be built; " +
			"the ML-enabled tests are skipped and the disabled-tier tests still ran"
	}

	root, err := os.Getwd()
	if err != nil {
		return nil, err, ""
	}
	root = filepath.Dir(root)
	mlRoot := filepath.Join(root, "ml")

	// One build for the whole workspace. --offline because a test suite that
	// reaches the network to run is a test suite that fails for reasons that
	// have nothing to do with the code.
	build := exec.Command("cargo", "build", "--workspace", "--offline")
	build.Dir = mlRoot
	if out, err := build.CombinedOutput(); err != nil {
		return nil, nil, fmt.Sprintf(
			"the Rust workspace did not build, so the ML-enabled tests are skipped:\n%s", out)
	}

	p := &mlPlatform{clients: map[string]*svcclient.Client{}, mlURLs: map[string]string{}}

	// The environment the Go callers get. Built up as each Rust service starts.
	mlEnv := []string{}

	for _, svc := range mlServices {
		bin := filepath.Join(mlRoot, "target", "debug", svc.bin)
		if _, err := os.Stat(bin); err != nil {
			return nil, fmt.Errorf("%s built and produced no binary at %s: %w",
				svc.bin, bin, err), ""
		}

		port, err := freePortErr()
		if err != nil {
			return nil, err, ""
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)

		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(), "LISTEN_ADDR="+addr, "LOG_LEVEL=warn")
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start %s: %w", svc.bin, err), ""
		}
		mlProcsMu.Lock()
		mlProcs = append(mlProcs, cmd)
		mlProcsMu.Unlock()

		url := "http://" + addr
		client := svcclient.New(svcclient.Config{BaseURL: url, Timeout: 10 * time.Second})
		if err := waitReadyErr(client); err != nil {
			return nil, fmt.Errorf("%s did not become ready on %s: %w", svc.bin, addr, err), ""
		}
		p.mlURLs[svc.bin] = url
		for _, env := range svc.urlEnv {
			mlEnv = append(mlEnv, env+"="+url)
		}
	}

	// A second copy of each Go caller, this time told where the tier is.
	binDir, err := os.MkdirTemp("", "gavya-e2e-ml")
	if err != nil {
		return nil, err, ""
	}
	// Deleted by TestMain along with the main platform's, for the same reason:
	// a directory of service binaries per run adds up faster than it looks.
	sharedBinDirs = append(sharedBinDirs, binDir)
	for _, name := range mlCallers {
		svc, found := serviceByName(name)
		if !found {
			return nil, fmt.Errorf("%s is not in the harness's services list, so its database "+
				"and schema are unknown here", name), ""
		}

		bin := filepath.Join(binDir, name)
		b := exec.Command("go", "build", "-o", bin, "./cmd/server")
		b.Dir = filepath.Join(root, "services", name)
		if out, err := b.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build %s: %v\n%s", name, err, out), ""
		}

		port, err := freePortErr()
		if err != nil {
			return nil, err, ""
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)

		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(),
			"SERVER_ADDR="+addr,
			"DATABASE_URL="+dsnFor(svc.database),
		)
		cmd.Env = append(cmd.Env, svc.env...)
		cmd.Env = append(cmd.Env, mlEnv...)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start %s with ML: %w", name, err), ""
		}
		mlProcsMu.Lock()
		mlProcs = append(mlProcs, cmd)
		mlProcsMu.Unlock()

		client := svcclient.New(svcclient.Config{
			BaseURL: "http://" + addr,
			Timeout: 15 * time.Second,
		})
		if err := waitReadyErr(client); err != nil {
			return nil, fmt.Errorf("%s did not become ready with ML configured: %w",
				name, err), ""
		}
		p.clients[name] = client
	}
	return p, nil, ""
}

func serviceByName(name string) (service, bool) {
	for _, s := range services {
		if s.name == name {
			return s, true
		}
	}
	return service{}, false
}

func (p *mlPlatform) observation() *svcclient.Client { return p.clients["observation-service"] }
func (p *mlPlatform) shadow() *svcclient.Client      { return p.clients["shadow-settlement-service"] }
func (p *mlPlatform) balance() *svcclient.Client     { return p.clients["balance-service"] }

func (p *mlPlatform) opts() svcclient.CallOptions {
	return (&platform{tenant: p.tenant}).opts()
}

// stopMLPlatform is called from TestMain alongside the main platform's cleanup.
func stopMLPlatform() {
	mlProcsMu.Lock()
	defer mlProcsMu.Unlock()
	for _, cmd := range mlProcs {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}
}
