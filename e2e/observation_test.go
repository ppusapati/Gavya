//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A deployment that has not said which measurement-control law it operates
// under would otherwise be given India's, and every eligibility verdict it
// issued would cite an Act that does not apply where it runs — looking, to
// every reader, exactly like one that had been configured deliberately.
//
// The service refuses to start instead. That refusal is the whole safeguard, so
// it is exercised here rather than assumed: the binary is run for real, without
// the setting, and must exit.
func TestObservationRefusesToStartWithoutARegime(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "observation-service")

	build := exec.Command("go", "build", "-o", bin, "./cmd/server")
	build.Dir = filepath.Join(root, "services", "observation-service")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"SERVER_ADDR=127.0.0.1:0",
		"DATABASE_URL="+dsn(t, "e2e_observation"),
		"MEASUREMENT_REGIME=", // the thing under test
		"ANOMALY_ML_URL=",
		"UNCERTAINTY_ML_URL=",
	)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the service started and exited cleanly with no regime configured")
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the service kept running with no regime configured, so every verdict it issues cites a law nobody chose")
	}

	// The operator has to learn what to put in the variable, or they will guess —
	// and the guess that works is the wrong one.
	logged := out.String()
	if !strings.Contains(logged, "MEASUREMENT_REGIME") {
		t.Errorf("the failure does not name the setting: %s", logged)
	}
	for _, id := range []string{"IN_LEGAL_METROLOGY", "NONE"} {
		if !strings.Contains(logged, id) {
			t.Errorf("the failure does not offer %s as an option: %s", id, logged)
		}
	}
}

// And with a regime it starts, which is what the harness relies on.
func TestObservationServesHealthUnderADeclaredRegime(t *testing.T) {
	p := startPlatform(t)
	if p.observation() == nil {
		t.Fatal("observation-service is not in the platform")
	}
}
