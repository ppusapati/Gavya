//go:build dbintegration

// End-to-end reconciliation against the real Rust reconciler and a real
// PostgreSQL. The unit tests prove the arithmetic; this proves the wire
// contract, and that the same window still closes the books locally when no
// reconciler is there to answer.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags dbintegration ./internal/service/...
package service

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/balance-service/internal/domain"
	"github.com/ppusapati/gavya/services/balance-service/internal/repository"
)

func TestASeriesNodeReconcilesAgainstTheRealReconcilerAndWithoutIt(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	baseURL := startReconciler(t)
	repo := repository.New(pool, sys.IDs{})
	log := p9log.NewHelper(p9log.DefaultLogger)
	tenantID := fmt.Sprintf("tnt%d", time.Now().UnixNano())

	withModel := New(repo, log, mlclient.NewReconciliationClient(mlclient.Config{
		BaseURL: baseURL,
		Timeout: 5 * time.Second,
	}))
	window := seriesWindow(t, withModel, tenantID, "route-a", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))

	run, err := withModel.Reconcile(context.Background(), ReconcileInput{
		TenantID: tenantID,
		WindowID: window.ID,
		Actor:    "tester",
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if !run.Converged {
		t.Fatalf("converged = false, want true; reason %q", run.Reason)
	}
	if run.ResidualBefore != "2.000" {
		t.Errorf("residual before = %q, want 2.000", run.ResidualBefore)
	}
	if run.ResidualAfter == nil {
		t.Fatal("residual after is null after a converged run")
	}
	after, err := strconv.ParseFloat(*run.ResidualAfter, 64)
	if err != nil {
		t.Fatalf("residual after %q: %v", *run.ResidualAfter, err)
	}
	if math.Abs(after) > 0.001 {
		t.Errorf("residual after = %s, want the node to close", *run.ResidualAfter)
	}
	if run.ModelVersion == "" {
		t.Error("a reconciled state names no model version and could not be reproduced")
	}

	// Two equally trusted streams two litres apart meet in the middle.
	byFlow := map[string]domain.ReconciledFlow{}
	for _, f := range run.Flows {
		byFlow[f.FlowID] = f
	}
	if len(byFlow) != 2 {
		t.Fatalf("got %d reconciled flows, want 2", len(byFlow))
	}
	for _, id := range []string{"in", "out"} {
		if byFlow[id].Reconciled != "99.000" {
			t.Errorf("flow %s reconciled to %q, want 99.000", id, byFlow[id].Reconciled)
		}
		if byFlow[id].GrossError {
			t.Errorf("flow %s was nominated as a gross error at one standard uncertainty", id)
		}
	}
	if byFlow["in"].Adjustment != "-1.000" {
		t.Errorf("in adjustment = %q, want -1.000", byFlow["in"].Adjustment)
	}
	if byFlow["out"].Adjustment != "1.000" {
		t.Errorf("out adjustment = %q, want 1.000", byFlow["out"].Adjustment)
	}

	// The run survives a restart of the process that made it.
	stored, err := withModel.GetRun(context.Background(), tenantID, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if len(stored.Flows) != 2 || stored.Flows[0].Reconciled != "99.000" {
		t.Errorf("stored run = %+v, want both flows at 99.000", stored.Flows)
	}

	// The same window with no reconciler configured: the imbalance is still
	// computed, recorded and attributable, which is the whole point of computing
	// it here rather than taking it from the model.
	offline := New(repo, log, nil)
	offlineWindow := seriesWindow(t, offline, tenantID, "route-b", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	degraded, err := offline.Reconcile(context.Background(), ReconcileInput{
		TenantID: tenantID,
		WindowID: offlineWindow.ID,
		Actor:    "tester",
	})
	if err != nil {
		t.Fatalf("reconcile with no model: %v", err)
	}
	if degraded.Converged {
		t.Error("converged = true with no model to converge")
	}
	if degraded.ResidualBefore != "2.000" {
		t.Errorf("residual before = %q, want the locally computed 2.000", degraded.ResidualBefore)
	}
	if degraded.ResidualAfter != nil {
		t.Errorf("residual after = %q, want null: nothing reconciled it", *degraded.ResidualAfter)
	}
	if len(degraded.Flows) != 0 {
		t.Errorf("got %d adjustments with no model", len(degraded.Flows))
	}
	if degraded.Reason == "" {
		t.Error("a run with no model answer states no reason")
	}
}

// seriesWindow is one inflow of 100.000 +/- 1.000 and one outflow of 98.000 +/-
// 1.000 across a single chilling unit.
func seriesWindow(t *testing.T, svc *Service, tenantID, routeRef string, start time.Time) *domain.BalanceWindow {
	t.Helper()
	ctx := context.Background()

	window, err := svc.CreateWindow(ctx, CreateWindowInput{
		TenantID:    tenantID,
		RouteRef:    routeRef,
		PeriodStart: start,
		PeriodEnd:   start.Add(24 * time.Hour),
		Unit:        domain.UnitLitres,
		Actor:       "tester",
	})
	if err != nil {
		t.Fatalf("create window: %v", err)
	}

	flows := []AddFlowInput{
		{
			FlowID:              "in",
			To:                  domain.BalanceNode{ID: "cc-1", Kind: domain.NodeChillingUnit},
			Measured:            "100.000",
			StandardUncertainty: "1.000",
		},
		{
			FlowID:              "out",
			From:                domain.BalanceNode{ID: "cc-1", Kind: domain.NodeChillingUnit},
			Measured:            "98.000",
			StandardUncertainty: "1.000",
		},
	}
	for _, f := range flows {
		f.TenantID, f.WindowID, f.Actor = tenantID, window.ID, "tester"
		if _, err := svc.AddFlow(ctx, f); err != nil {
			t.Fatalf("add flow %s: %v", f.FlowID, err)
		}
	}
	return window
}

// startReconciler runs the real Rust binary on a free port and waits for it to
// report healthy.
func startReconciler(t *testing.T) string {
	t.Helper()

	binary := os.Getenv("RECONCILIATION_ML_BINARY")
	if binary == "" {
		binary = filepath.Join("..", "..", "..", "..", "ml", "target", "debug", "reconciliation-service")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("reconciliation-service binary not built at %s: %v", binary, err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "LISTEN_ADDR="+addr, "LOG_LEVEL=warn")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	baseURL := "http://" + addr
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return baseURL
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("reconciler at %s never became healthy", addr)
	return ""
}
