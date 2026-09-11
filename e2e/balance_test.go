//go:build e2e

// The five ways a balance window is read back.
//
// material_test drives a window forward — create it, add the flows, reconcile —
// and decisions_test accepts a run. Between them nothing ever reads a window, a
// flow or a run back by any route but the one that produced it. What is missing
// is exactly what somebody investigating a loss actually does: find the windows
// that are still open, look at what was measured on one, and compare this
// morning's reconciliation with last night's.
//
// Three of the five filter on a value the caller supplies — a status, a window,
// a run id — and a filter that has quietly stopped filtering looks like a
// working endpoint. So each test puts a second window, a second flow or a second
// run in the way.
package e2e

import (
	"context"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type getWindowReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

// fullWindowProto names every field the handler sends. material_test's
// windowProto reads three of them, which is all it needs; a window read back by
// its own route should be checked against what it was opened with.
type fullWindowProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	RouteRef    string `json:"route_ref"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit"`
	Status      string `json:"status"`
}

type getWindowResp struct {
	Window *fullWindowProto `json:"window"`
}

type listWindowsReq struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status,omitempty"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type listWindowsResp struct {
	Windows []*fullWindowProto `json:"windows"`
}

type listFlowsReq struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
}

type measuredFlowProto struct {
	ID                  string `json:"id"`
	WindowID            string `json:"window_id"`
	FlowID              string `json:"flow_id"`
	FromNode            string `json:"from_node"`
	FromNodeKind        string `json:"from_node_kind,omitempty"`
	ToNode              string `json:"to_node"`
	ToNodeKind          string `json:"to_node_kind,omitempty"`
	Measured            string `json:"measured"`
	StandardUncertainty string `json:"standard_uncertainty,omitempty"`
	Unmeasured          bool   `json:"unmeasured"`
	ObservationRef      string `json:"observation_ref,omitempty"`
}

type listFlowsResp struct {
	Flows []*measuredFlowProto `json:"flows"`
}

type getRunReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type fullRunProto struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	WindowID       string `json:"window_id"`
	Converged      bool   `json:"converged"`
	ResidualBefore string `json:"residual_before"`
	ResidualAfter  string `json:"residual_after,omitempty"`
	Reason         string `json:"reason,omitempty"`
	AcceptedAt     string `json:"accepted_at,omitempty"`
	AcceptedBy     string `json:"accepted_by,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type getRunResp struct {
	Run *fullRunProto `json:"run"`
}

type listRunsReq struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id,omitempty"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type listRunsResp struct {
	Runs []*fullRunProto `json:"runs"`
}

// balancedWindow opens a window on a route and puts one tanker's day into it:
// 5000 litres loaded at the boundary, 4990 delivered back to it.
//
// The ten litres are the point. A window that balanced exactly would let a
// reconciler that returns zero for everything pass, and the residual is the
// figure the whole service exists to produce.
func balancedWindow(t *testing.T, p *platform, route string, out string) string {
	t.Helper()
	ctx := context.Background()

	window, err := svcclient.Call[createWindowReq, createWindowResp](ctx, p.balance(),
		balanceSvc+"/CreateWindow", createWindowReq{
			TenantID: p.tenant, RouteRef: route,
			PeriodStart: "2026-04-01T00:00:00Z", PeriodEnd: "2026-04-02T00:00:00Z",
			Unit: "LITRES", Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("CreateWindow %s: %v", route, err)
	}
	id := window.Window.ID

	node := "TANK-" + route
	for _, f := range []addFlowReq{
		{
			FlowID: "loaded", FromNode: "", ToNode: node, ToNodeKind: "TANKER",
			Measured: "5000.000", StandardUncertainty: "9.980",
		},
		{
			FlowID: "delivered", FromNode: node, FromNodeKind: "TANKER", ToNode: "",
			Measured: out, StandardUncertainty: "9.960",
		},
	} {
		f.TenantID, f.WindowID, f.Actor = p.tenant, id, "e2e"
		if _, err := svcclient.Call[addFlowReq, addFlowResp](ctx, p.balance(),
			balanceSvc+"/AddFlow", f, p.opts()); err != nil {
			t.Fatalf("AddFlow %s on %s: %v", f.FlowID, route, err)
		}
	}
	return id
}

// A window reads back as it was opened, and its status follows the work done
// on it.
//
// The status filter on ListWindows is what an operator's queue is built from:
// "which windows are still open" is the first question of the morning. A filter
// that has stopped filtering answers it with every window the tenant has ever
// had, including the ones settled months ago.
func TestAWindowReadsBackAndItsStatusFollowsTheWork(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	route := newID("rt")
	id := balancedWindow(t, p, route, "4990.000")
	stillOpen := balancedWindow(t, p, newID("rt"), "4990.000")

	got, err := svcclient.Call[getWindowReq, getWindowResp](ctx, p.balance(),
		balanceSvc+"/GetWindow", getWindowReq{TenantID: p.tenant, ID: id}, p.opts())
	if err != nil {
		t.Fatalf("GetWindow: %v", err)
	}
	w := got.Window
	if w == nil {
		t.Fatal("GetWindow answered with no window at all")
	}
	for _, c := range []struct{ field, got, want string }{
		{"id", w.ID, id},
		{"tenant_id", w.TenantID, p.tenant},
		{"route_ref", w.RouteRef, route},
		{"unit", w.Unit, "LITRES"},
		{"status", w.Status, "OPEN"},
	} {
		if c.got != c.want {
			t.Errorf("window %s is %q, want %q", c.field, c.got, c.want)
		}
	}

	// Reconciling moves this window on and leaves the other where it was.
	if _, err := svcclient.Call[reconcileReq, reconcileResp](ctx, p.balance(),
		balanceSvc+"/Reconcile", reconcileReq{
			TenantID: p.tenant, WindowID: id, Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	after, err := svcclient.Call[getWindowReq, getWindowResp](ctx, p.balance(),
		balanceSvc+"/GetWindow", getWindowReq{TenantID: p.tenant, ID: id}, p.opts())
	if err != nil {
		t.Fatalf("GetWindow after reconciling: %v", err)
	}
	if after.Window.Status != "RECONCILED" {
		t.Errorf("the window is %q after being reconciled, want RECONCILED",
			after.Window.Status)
	}

	open, err := svcclient.Call[listWindowsReq, listWindowsResp](ctx, p.balance(),
		balanceSvc+"/ListWindows", listWindowsReq{
			TenantID: p.tenant, Status: "OPEN", Limit: 100,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListWindows OPEN: %v", err)
	}
	inOpen := map[string]bool{}
	for _, x := range open.Windows {
		inOpen[x.ID] = true
		if x.Status != "OPEN" {
			t.Errorf("a window with status %q came back from a listing asked for OPEN", x.Status)
		}
	}
	if !inOpen[stillOpen] {
		t.Error("a window nobody has reconciled is missing from the open listing")
	}
	if inOpen[id] {
		t.Error("a reconciled window is listed as open\n" +
			"This listing is the queue somebody works through; a status filter " +
			"that does not filter buries the windows still needing attention " +
			"among every window the tenant has ever had.")
	}

	reconciled, err := svcclient.Call[listWindowsReq, listWindowsResp](ctx, p.balance(),
		balanceSvc+"/ListWindows", listWindowsReq{
			TenantID: p.tenant, Status: "RECONCILED", Limit: 100,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListWindows RECONCILED: %v", err)
	}
	found := false
	for _, x := range reconciled.Windows {
		if x.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("the reconciled window is in neither listing, so nothing can find it")
	}
}

// The flows on a window are that window's flows.
//
// Two windows are filled in the same moment with different measurements. This
// is the listing an investigator reads to see what was actually weighed, so a
// query keyed on the wrong column shows them another tanker's day and nothing
// in the answer says so.
func TestAWindowsFlowsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	mine := balancedWindow(t, p, newID("rt"), "4990.000")
	_ = balancedWindow(t, p, newID("rt"), "4980.000")

	got, err := svcclient.Call[listFlowsReq, listFlowsResp](ctx, p.balance(),
		balanceSvc+"/ListFlows", listFlowsReq{TenantID: p.tenant, WindowID: mine}, p.opts())
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(got.Flows) != 2 {
		t.Fatalf("this window holds %d flows, want 2 — another window was filled in "+
			"the same moment and its flows are not this window's", len(got.Flows))
	}

	byID := map[string]*measuredFlowProto{}
	for _, f := range got.Flows {
		byID[f.FlowID] = f
		if f.WindowID != mine {
			t.Errorf("flow %s says it belongs to window %s, and %s was asked",
				f.FlowID, f.WindowID, mine)
		}
	}
	for _, c := range []struct{ flow, measured, uncertainty string }{
		{"loaded", "5000.000", "9.980"},
		{"delivered", "4990.000", "9.960"},
	} {
		f, ok := byID[c.flow]
		if !ok {
			t.Errorf("flow %s is missing from the window it was added to", c.flow)
			continue
		}
		if f.Measured != c.measured {
			t.Errorf("flow %s measured %q, want %q — this is the figure the "+
				"residual is computed from", c.flow, f.Measured, c.measured)
		}
		if f.StandardUncertainty != c.uncertainty {
			t.Errorf("flow %s carries uncertainty %q, want %q — a measurement "+
				"without its uncertainty cannot be reconciled against another, "+
				"only compared", c.flow, f.StandardUncertainty, c.uncertainty)
		}
		if f.Unmeasured {
			t.Errorf("flow %s is marked unmeasured and it carries a measurement", c.flow)
		}
	}
}

// A reconciliation run is found by its id, and under the window it ran on.
//
// A window is reconciled more than once — that is the ordinary case, because
// somebody corrects a reading and runs it again. Which run produced a figure is
// then the whole question, and both routes here answer it: one by id, one by
// window. The listing filters on the window and must not carry another
// window's runs, or the history of one tanker's day is interleaved with
// another's.
func TestAReconciliationRunIsFoundByIdAndUnderItsWindow(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	mine := balancedWindow(t, p, newID("rt"), "4990.000")
	theirs := balancedWindow(t, p, newID("rt"), "4980.000")

	reconcile := func(window string) *fullRunProto {
		t.Helper()
		out, err := svcclient.Call[reconcileReq, getRunResp](ctx, p.balance(),
			balanceSvc+"/Reconcile", reconcileReq{
				TenantID: p.tenant, WindowID: window, Actor: "e2e",
			}, p.opts())
		if err != nil {
			t.Fatalf("Reconcile %s: %v", window, err)
		}
		return out.Run
	}

	first := reconcile(mine)
	second := reconcile(mine)
	other := reconcile(theirs)

	if first.ID == second.ID {
		t.Fatal("reconciling the same window twice produced one run; a second " +
			"reconciliation is a new run, not an edit of the first")
	}

	// Ten litres out of five thousand, reached by subtraction rather than by
	// the reconciler agreeing with itself.
	if first.ResidualBefore != "10.000" {
		t.Errorf("the run puts the window's imbalance at %q, want 10.000 — "+
			"5000.000 loaded against 4990.000 delivered", first.ResidualBefore)
	}
	if other.ResidualBefore != "20.000" {
		t.Errorf("the other window's imbalance is %q, want 20.000", other.ResidualBefore)
	}

	// By id. The run that comes back must be the one asked for, carrying its
	// own window and its own residual.
	got, err := svcclient.Call[getRunReq, getRunResp](ctx, p.balance(),
		balanceSvc+"/GetRun", getRunReq{TenantID: p.tenant, ID: other.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Run.ID != other.ID {
		t.Errorf("GetRun asked for %s and answered with %s", other.ID, got.Run.ID)
	}
	if got.Run.WindowID != theirs {
		t.Errorf("the run reads back against window %s and it ran on %s",
			got.Run.WindowID, theirs)
	}
	if got.Run.ResidualBefore != "20.000" {
		t.Errorf("the run reads back with residual %q and was computed as 20.000",
			got.Run.ResidualBefore)
	}

	// By window. Both of this window's runs, and neither of the other's.
	listed, err := svcclient.Call[listRunsReq, listRunsResp](ctx, p.balance(),
		balanceSvc+"/ListRuns", listRunsReq{
			TenantID: p.tenant, WindowID: mine, Limit: 100,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range listed.Runs {
		seen[r.ID] = true
		if r.WindowID != mine {
			t.Errorf("a run on window %s came back from a listing asked for %s",
				r.WindowID, mine)
		}
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Errorf("the window was reconciled twice and %d of its runs are listed",
			len(listed.Runs))
	}
	if seen[other.ID] {
		t.Error("another window's run is in this window's history")
	}

	// Newest first, which is what the route promises and what makes "the
	// current answer" the first row rather than a row somebody has to search
	// for.
	if len(listed.Runs) >= 2 && listed.Runs[0].ID != second.ID {
		t.Errorf("the most recent run is listed at position %d, and a history "+
			"read newest-first is how somebody finds the figure in force",
			indexOfRun(listed.Runs, second.ID))
	}
}

func indexOfRun(runs []*fullRunProto, id string) int {
	for i, r := range runs {
		if r.ID == id {
			return i
		}
	}
	return -1
}
