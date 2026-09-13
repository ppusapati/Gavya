//go:build dbintegration

// Repository tests against a real PostgreSQL.
//
// The balance rules are unit tested as pure functions elsewhere. What can only
// be tested here is whether the schema keeps what those rules assume: that a
// quantity survives to its third decimal, that a flow id is unique within its
// window, that a run and its per-flow verdicts land together or not at all, and
// that one tenant's window is invisible to another.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags dbintegration ./internal/repository/...
package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
	"github.com/ppusapati/gavya/services/balance-service/internal/domain"
)

var (
	poolOnce sync.Once
	testPool *pgxpool.Pool

	runNonce  = time.Now().UnixNano()
	idCounter atomic.Int64
)

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	poolOnce.Do(func() {
		p, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		testPool = p
	})
	return testPool
}

func newID(prefix string) string {
	body := fmt.Sprintf("%s%d", prefix, runNonce+idCounter.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + strings.Repeat("0", 26-len(body))
}

var (
	periodStart = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	periodEnd   = time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
)

// acting returns a context carrying what a real request carries.
//
// These tests called the repository with a bare context.Background(), which
// worked for as long as nothing on these paths wrote an audit entry — and an
// audit entry is the one thing that refuses to be written without a tenant and
// an actor to attribute it to. Nothing ran this suite between the day the
// entries were added and the day it was wired into check-all, which is why
// every audited call here failed at once.
func (f *fixture) acting() context.Context {
	return tenantctx.WithActor(
		tenantdb.WithTenant(context.Background(), f.tenantID),
		tenantctx.Actor{ID: "integration-test"})
}

type fixture struct {
	repo     Repository
	tenantID string
	routeRef string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return &fixture{repo: New(pool(t), sys.IDs{}), tenantID: newID("tnt"), routeRef: newID("rte")}
}

func (f *fixture) window(t *testing.T) *domain.BalanceWindow {
	t.Helper()
	w, err := f.repo.CreateWindow(f.acting(), &domain.BalanceWindow{
		ID:          newID("win"),
		TenantID:    f.tenantID,
		RouteRef:    f.routeRef,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Unit:        domain.UnitLitres,
		CreatedBy:   "tester",
	})
	if err != nil {
		t.Fatalf("create window: %v", err)
	}
	return w
}

func (f *fixture) flow(windowID, flowID, from, to, measured, uncertainty string) *domain.FlowMeasurement {
	flow := &domain.FlowMeasurement{
		ID:                  newID("flw"),
		TenantID:            f.tenantID,
		WindowID:            windowID,
		FlowID:              flowID,
		Measured:            measured,
		StandardUncertainty: uncertainty,
		CreatedBy:           "tester",
	}
	if from != domain.Boundary {
		flow.From = domain.BalanceNode{ID: from, Kind: domain.NodeChillingUnit}
	}
	if to != domain.Boundary {
		flow.To = domain.BalanceNode{ID: to, Kind: domain.NodeChillingUnit}
	}
	return flow
}

func (f *fixture) addFlow(t *testing.T, windowID, flowID, from, to, measured, uncertainty string) *domain.FlowMeasurement {
	t.Helper()
	out, err := f.repo.AddFlow(f.acting(), f.flow(windowID, flowID, from, to, measured, uncertainty))
	if err != nil {
		t.Fatalf("add flow %s: %v", flowID, err)
	}
	return out
}

func (f *fixture) run(windowID string, converged bool, before string, after *string, flows []domain.ReconciledFlow) *domain.ReconciliationRun {
	run := &domain.ReconciliationRun{
		ID:             newID("run"),
		TenantID:       f.tenantID,
		WindowID:       windowID,
		Converged:      converged,
		ResidualBefore: before,
		ResidualAfter:  after,
		Flows:          flows,
		CreatedBy:      "tester",
	}
	if after != nil {
		run.ModelVersion = "reconciliation-wls-1.0.0"
	} else {
		run.Reason = "the reconciler did not answer"
	}
	return run
}

func TestWindowAndFlowsRoundTripToTheThirdDecimal(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	w := f.window(t)
	if w.Status != domain.WindowOpen {
		t.Errorf("new window status = %s, want OPEN", w.Status)
	}

	f.addFlow(t, w.ID, "in", domain.Boundary, "cc-1", "1234.567", "1.250")
	f.addFlow(t, w.ID, "out", "cc-1", domain.Boundary, "1234.566", "1.250")

	got, err := f.repo.ListFlows(ctx, f.tenantID, w.ID)
	if err != nil {
		t.Fatalf("list flows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d flows, want 2", len(got))
	}

	byID := map[string]domain.FlowMeasurement{}
	for _, fl := range got {
		byID[fl.FlowID] = fl
	}
	if byID["in"].Measured != "1234.567" {
		t.Errorf("measured = %q, want 1234.567; the third decimal was lost", byID["in"].Measured)
	}
	if byID["in"].StandardUncertainty != "1.250" {
		t.Errorf("standard uncertainty = %q, want 1.250", byID["in"].StandardUncertainty)
	}
	if byID["in"].To.ID != "cc-1" || byID["in"].To.Kind != domain.NodeChillingUnit {
		t.Errorf("to node = %+v, want cc-1/CHILLING_UNIT", byID["in"].To)
	}
	// The boundary is stored as the empty node with no kind.
	if byID["in"].From.ID != domain.Boundary || byID["in"].From.Kind != "" {
		t.Errorf("from node = %+v, want the boundary", byID["in"].From)
	}

	residuals, err := domain.ResidualByNode(got)
	if err != nil {
		t.Fatalf("residual: %v", err)
	}
	total, err := domain.TotalResidual(residuals)
	if err != nil {
		t.Fatalf("total residual: %v", err)
	}
	if total.String() != "0.001" {
		t.Errorf("imbalance = %s, want 0.001 from the stored values", total.String())
	}
}

// The flow id is what a reconciler's verdict is keyed by, so two streams
// sharing one within a window would share a single verdict.
func TestDuplicateFlowIDInAWindowIsRejected(t *testing.T) {
	f := setup(t)
	w := f.window(t)

	f.addFlow(t, w.ID, "in", domain.Boundary, "cc-1", "100.000", "1.000")

	_, err := f.repo.AddFlow(f.acting(), f.flow(w.ID, "in", "cc-1", domain.Boundary, "98.000", "1.000"))
	if !errors.Is(err, ErrDuplicateFlow) {
		t.Fatalf("got %v, want ErrDuplicateFlow", err)
	}

	// The same id in another window is a different stream and is allowed.
	second, err := f.repo.CreateWindow(f.acting(), &domain.BalanceWindow{
		ID:          newID("win"),
		TenantID:    f.tenantID,
		RouteRef:    f.routeRef,
		PeriodStart: periodEnd,
		PeriodEnd:   periodEnd.Add(24 * time.Hour),
		Unit:        domain.UnitLitres,
		CreatedBy:   "tester",
	})
	if err != nil {
		t.Fatalf("create second window: %v", err)
	}
	f.addFlow(t, second.ID, "in", domain.Boundary, "cc-1", "100.000", "1.000")
}

func TestRunAndItsFlowsAreWrittenTogether(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	w := f.window(t)
	f.addFlow(t, w.ID, "in", domain.Boundary, "cc-1", "100.000", "1.000")
	f.addFlow(t, w.ID, "out", "cc-1", domain.Boundary, "98.000", "1.000")

	after := "0.000"
	stored, err := f.repo.CreateRun(ctx, f.run(w.ID, true, "2.000", &after, []domain.ReconciledFlow{
		{FlowID: "in", Measured: "100.000", Reconciled: "99.000", Adjustment: "-1.000", TestStatistic: 1},
		{FlowID: "out", Measured: "98.000", Reconciled: "99.000", Adjustment: "1.000", TestStatistic: 1},
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	got, err := f.repo.GetRun(ctx, f.tenantID, stored.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if !got.Converged {
		t.Error("converged = false, want true")
	}
	if got.ResidualBefore != "2.000" {
		t.Errorf("residual before = %q, want 2.000", got.ResidualBefore)
	}
	if got.ResidualAfter == nil || *got.ResidualAfter != "0.000" {
		t.Errorf("residual after = %v, want 0.000", got.ResidualAfter)
	}
	if len(got.Flows) != 2 {
		t.Fatalf("got %d reconciled flows, want 2", len(got.Flows))
	}
	if got.Flows[0].FlowID != "in" || got.Flows[0].Reconciled != "99.000" {
		t.Errorf("first flow = %+v, want in reconciled to 99.000", got.Flows[0])
	}
	if got.Flows[1].Adjustment != "1.000" {
		t.Errorf("out adjustment = %q, want 1.000", got.Flows[1].Adjustment)
	}

	window, err := f.repo.GetWindow(ctx, f.tenantID, w.ID)
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	if window.Status != domain.WindowReconciled {
		t.Errorf("window status = %s, want RECONCILED", window.Status)
	}
}

// A run whose per-flow rows only partly landed would nominate a culprit out of
// a network nobody could reconstruct, so a failure part way through must leave
// no run at all.
func TestAFailedFlowRowLeavesNoRunBehind(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	w := f.window(t)
	f.addFlow(t, w.ID, "in", domain.Boundary, "cc-1", "100.000", "1.000")

	after := "0.000"
	run := f.run(w.ID, true, "2.000", &after, []domain.ReconciledFlow{
		{FlowID: "in", Measured: "100.000", Reconciled: "99.000", Adjustment: "-1.000"},
		// The same flow twice: the second row collides with the first on the
		// run's primary key, part way through the transaction.
		{FlowID: "in", Measured: "100.000", Reconciled: "99.000", Adjustment: "-1.000"},
	})

	if _, err := f.repo.CreateRun(ctx, run); err == nil {
		t.Fatal("a run with a colliding flow row was accepted")
	}

	if _, err := f.repo.GetRun(ctx, f.tenantID, run.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the run survived a failed flow row: %v", err)
	}
	window, err := f.repo.GetWindow(ctx, f.tenantID, w.ID)
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	if window.Status != domain.WindowOpen {
		t.Errorf("window status = %s, want OPEN; the rolled back run advanced it", window.Status)
	}
}

// An under-determined network is a real operational state: the run is kept, its
// adjustments are indicative, and it accuses nobody.
func TestANonConvergedRunIsKeptAndAccusesNobody(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	w := f.window(t)
	f.addFlow(t, w.ID, "out", "cc-1", "plant-1", "100.000", "1.000")
	f.addFlow(t, w.ID, "back", "plant-1", "cc-1", "98.000", "1.000")

	run := f.run(w.ID, false, "2.000", nil, []domain.ReconciledFlow{
		{FlowID: "back", Measured: "98.000", Reconciled: "98.000", Adjustment: "0.000"},
		{FlowID: "out", Measured: "100.000", Reconciled: "100.000", Adjustment: "0.000"},
	})
	stored, err := f.repo.CreateRun(ctx, run)
	if err != nil {
		t.Fatalf("a run that did not converge was refused: %v", err)
	}

	got, err := f.repo.GetRun(ctx, f.tenantID, stored.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Converged {
		t.Error("converged = true, want false")
	}
	if got.ResidualAfter != nil {
		t.Errorf("residual after = %q, want null: nothing was reconciled", *got.ResidualAfter)
	}
	if got.ResidualBefore != "2.000" {
		t.Errorf("residual before = %q, want 2.000", got.ResidualBefore)
	}
	if got.Reason == "" {
		t.Error("a run with no model answer states no reason")
	}
	if len(got.Flows) != 2 {
		t.Fatalf("got %d reconciled flows, want 2", len(got.Flows))
	}
	for _, fl := range got.Flows {
		if fl.GrossError {
			t.Errorf("flow %s carries a gross error on a run that never converged", fl.FlowID)
		}
	}

	// The same run with an accusation attached is refused outright.
	accusing := f.run(w.ID, false, "2.000", nil, []domain.ReconciledFlow{
		{FlowID: "out", Measured: "100.000", Reconciled: "100.000", Adjustment: "0.000", GrossError: true},
	})
	if _, err := f.repo.CreateRun(ctx, accusing); !errors.Is(err, domain.ErrGrossErrorWithoutConvergence) {
		t.Fatalf("got %v, want ErrGrossErrorWithoutConvergence", err)
	}
}

func TestAcceptedRunClosesTheWindowOnlyOnce(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	w := f.window(t)
	f.addFlow(t, w.ID, "in", domain.Boundary, "cc-1", "100.000", "1.000")

	after := "0.000"
	first, err := f.repo.CreateRun(ctx, f.run(w.ID, true, "2.000", &after, []domain.ReconciledFlow{
		{FlowID: "in", Measured: "100.000", Reconciled: "99.000", Adjustment: "-1.000"},
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	second, err := f.repo.CreateRun(ctx, f.run(w.ID, true, "2.000", &after, []domain.ReconciledFlow{
		{FlowID: "in", Measured: "100.000", Reconciled: "99.000", Adjustment: "-1.000"},
	}))
	if err != nil {
		t.Fatalf("create second run: %v", err)
	}

	accepted, err := f.repo.AcceptRun(ctx, f.tenantID, first.ID, "auditor")
	if err != nil {
		t.Fatalf("accept run: %v", err)
	}
	if accepted.AcceptedAt == nil || accepted.AcceptedBy != "auditor" {
		t.Errorf("acceptance is unattributed: %+v", accepted)
	}

	window, err := f.repo.GetWindow(ctx, f.tenantID, w.ID)
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	if window.Status != domain.WindowAccepted {
		t.Errorf("window status = %s, want ACCEPTED", window.Status)
	}

	if _, err := f.repo.AcceptRun(ctx, f.tenantID, second.ID, "auditor"); !errors.Is(err, ErrAlreadyAccepted) {
		t.Fatalf("got %v, want ErrAlreadyAccepted; a period must not close twice", err)
	}
}

func TestOneRoutePeriodHasOneWindow(t *testing.T) {
	f := setup(t)
	f.window(t)

	_, err := f.repo.CreateWindow(f.acting(), &domain.BalanceWindow{
		ID:          newID("win"),
		TenantID:    f.tenantID,
		RouteRef:    f.routeRef,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Unit:        domain.UnitLitres,
		CreatedBy:   "tester",
	})
	if !errors.Is(err, ErrDuplicateWindow) {
		t.Fatalf("got %v, want ErrDuplicateWindow", err)
	}
}

func TestTenantsCannotSeeEachOthersWindows(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	other := newID("tnt")

	w := f.window(t)
	f.addFlow(t, w.ID, "in", domain.Boundary, "cc-1", "100.000", "1.000")
	after := "0.000"
	run, err := f.repo.CreateRun(ctx, f.run(w.ID, true, "2.000", &after, []domain.ReconciledFlow{
		{FlowID: "in", Measured: "100.000", Reconciled: "99.000", Adjustment: "-1.000"},
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if _, err := f.repo.GetWindow(ctx, other, w.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another tenant read the window: %v", err)
	}
	if _, err := f.repo.GetRun(ctx, other, run.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another tenant read the run: %v", err)
	}
	flows, err := f.repo.ListFlows(ctx, other, w.ID)
	if err != nil {
		t.Fatalf("list flows: %v", err)
	}
	if len(flows) != 0 {
		t.Errorf("another tenant listed %d flows", len(flows))
	}
	runs, err := f.repo.ListRuns(ctx, other, w.ID, 10, 0)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("another tenant listed %d runs", len(runs))
	}
	if _, err := f.repo.AcceptRun(ctx, other, run.ID, "intruder"); err == nil {
		t.Error("another tenant accepted the run")
	}

	windows, err := f.repo.ListWindows(ctx, other, "", 10, 0)
	if err != nil {
		t.Fatalf("list windows: %v", err)
	}
	for _, got := range windows {
		if got.ID == w.ID {
			t.Fatalf("another tenant listed window %s", w.ID)
		}
	}
}

// The window's own tenant sees it under its status filter.
func TestListWindowsFiltersByStatus(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	w := f.window(t)

	open, err := f.repo.ListWindows(ctx, f.tenantID, domain.WindowOpen, 10, 0)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(open) != 1 || open[0].ID != w.ID {
		t.Fatalf("open windows = %d, want just %s", len(open), w.ID)
	}

	reconciled, err := f.repo.ListWindows(ctx, f.tenantID, domain.WindowReconciled, 10, 0)
	if err != nil {
		t.Fatalf("list reconciled: %v", err)
	}
	if len(reconciled) != 0 {
		t.Errorf("reconciled windows = %d, want 0", len(reconciled))
	}
}
