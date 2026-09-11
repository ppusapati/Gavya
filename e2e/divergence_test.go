//go:build e2e

// Reading the divergences back, and deciding one.
//
// shadow_pipeline_test walks a settlement all the way from an incumbent's
// export to an adjudicated divergence, and stops there. The four routes left
// are what happens next: somebody opens the queue, filters it down to what is
// worth their morning, looks at one, and records what they decided.
//
// ListDivergences filters on four things at once — status, classification,
// producer and a minimum size — and every one of them is optional. Each is
// written as "this parameter is empty, or the column equals it", and a filter
// shaped that way fails open: when it breaks it returns more than it should,
// never less, so the divergence the caller was looking for is still in the
// answer and nothing looks wrong.
package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type getDivergenceReq struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

// fullDivergenceProto reads the fields that say what was decided and by whom,
// which shadow_pipeline_test's divergenceProto does not carry.
type fullDivergenceProto struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id"`
	ProducerRef     string `json:"producer_ref"`
	Currency        string `json:"currency"`
	AmountScale     int32  `json:"amount_scale"`
	Delta           string `json:"delta"`
	DeltaMinorUnits int64  `json:"delta_minor_units"`
	Classification  string `json:"classification"`
	Rationale       string `json:"rationale"`
	Status          string `json:"status"`
	Resolution      string `json:"resolution,omitempty"`
	ResolvedAt      string `json:"resolved_at,omitempty"`
	ResolvedBy      string `json:"resolved_by,omitempty"`
	NeedsReview     bool   `json:"needs_review"`
}

type getDivergenceResp struct {
	Divergence *fullDivergenceProto `json:"divergence"`
}

type listDivergencesReq struct {
	TenantID       string `json:"tenant_id"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
	ProducerRef    string `json:"producer_ref"`
	MinAbsDelta    int64  `json:"min_abs_delta"`
	Limit          int32  `json:"limit"`
	Offset         int32  `json:"offset"`
}

type listDivergencesResp struct {
	Divergences []*fullDivergenceProto `json:"divergences"`
}

type resolveDivergenceReq struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	Status     string `json:"status"`
	Resolution string `json:"resolution"`
	Actor      string `json:"actor"`
}

type resolveDivergenceResp struct {
	Divergence *fullDivergenceProto `json:"divergence"`
}

type summariseReq struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type classSummaryProto struct {
	Classification     string `json:"classification"`
	Currency           string `json:"currency"`
	AmountScale        int32  `json:"amount_scale"`
	Count              int64  `json:"count"`
	TotalAbsMinorUnits int64  `json:"total_abs_minor_units"`
}

type summariseResp struct {
	Summaries []classSummaryProto `json:"summaries"`
}

// adjudicated builds one divergence: an incumbent's figure, the platform's own
// recomputation of the same period, and the verdict between them.
//
// The two totals are given rather than derived, so each caller states the delta
// it is arranging and a reader can check the arithmetic without running
// anything.
func adjudicated(t *testing.T, p *platform, tenant, producer, external, shadow, extQty, shadowQty, extRate, shadowRate string) *divergenceProto {
	t.Helper()
	ctx := context.Background()
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	assertion, err := svcclient.Call[ingestAssertionReq, ingestAssertionResp](ctx, p.shadow(),
		shadowSvc+"/IngestAssertion", ingestAssertionReq{
			TenantID: tenant, SourceSystemID: newID("src"),
			ExternalSettlementID: newID("ext"), ProducerRef: producer,
			PeriodStart: "2026-02-01", PeriodEnd: "2026-02-28",
			Currency: "INR", AmountScale: 2, Total: external,
			Components: []componentProto{
				{Kind: "BASE_PRICE", Amount: external, Quantity: extQty, Rate: extRate},
			},
			AssertedAt: "2026-03-01T00:00:00Z", ImportBatchID: newID("bat"),
			SourceRecordID: newID("rec"),
			RawPayload:     json.RawMessage(`{"net":"` + external + `"}`),
			CreatedBy:      "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("IngestAssertion for %s: %v", producer, err)
	}

	computation, err := svcclient.Call[recordComputationReq, recordComputationResp](ctx, p.shadow(),
		shadowSvc+"/RecordComputation", recordComputationReq{
			TenantID: tenant, AssertionID: assertion.Assertion.ID, ProducerRef: producer,
			PeriodStart: "2026-02-01", PeriodEnd: "2026-02-28",
			Currency: "INR", AmountScale: 2, Total: shadow,
			Components: []componentProto{
				{Kind: "BASE_PRICE", Amount: shadow, Quantity: shadowQty, Rate: shadowRate},
			},
			PolicyVersion: "policy-2026.02", RateCardID: newID("rct"),
			InputDigest: "sha256:e2e", AsOf: "2026-03-02T00:00:00Z", CreatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("RecordComputation for %s: %v", producer, err)
	}

	out, err := svcclient.Call[adjudicateReq, adjudicateResp](ctx, p.shadow(),
		shadowSvc+"/Adjudicate", adjudicateReq{
			TenantID: tenant, AssertionID: assertion.Assertion.ID,
			ComputationID: computation.Computation.ID, Actor: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("Adjudicate for %s: %v", producer, err)
	}
	return out.Divergence
}

// A divergence is found by its id, and the queue narrows by each thing it can
// be narrowed by.
//
// Three divergences in one tenant, differing in producer, in classification and
// in size, so every filter has something it must exclude. All four fail open —
// each returns everything when it breaks, never nothing — so a test that only
// checked the wanted row was present would pass against all four of them broken
// at once.
func TestADivergenceIsFoundByIdAndTheQueueNarrowsByEachFilter(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Its own tenant: this test counts what a filter returns, and the shared
	// tenant accumulates divergences from every other test in the file.
	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}
	alpha, beta := "producer:"+newID("a"), "producer:"+newID("b")

	// Same milk, a different rate: a policy difference of 50.00.
	big := adjudicated(t, p, tenant, alpha, "1050.00", "1000.00", "300.000", "300.000", "3.5000", "3.3333")
	// The same disagreement, smaller: 10.00.
	small := adjudicated(t, p, tenant, beta, "1010.00", "1000.00", "300.000", "300.000", "3.3667", "3.3333")
	// Different milk: the two sides disagree about how much was collected.
	input := adjudicated(t, p, tenant, alpha, "1000.00", "900.00", "300.000", "270.000", "3.3333", "3.3333")

	if big.Classification != "POLICY_DIFFERENCE" {
		t.Fatalf("the rate disagreement classified as %s: %s", big.Classification, big.Rationale)
	}
	if input.Classification != "INPUT_DIFFERENCE" {
		t.Fatalf("the quantity disagreement classified as %s: %s", input.Classification, input.Rationale)
	}

	// By id.
	got, err := svcclient.Call[getDivergenceReq, getDivergenceResp](ctx, p.shadow(),
		shadowSvc+"/GetDivergence", getDivergenceReq{ID: big.ID, TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("GetDivergence: %v", err)
	}
	d := got.Divergence
	if d == nil {
		t.Fatal("GetDivergence answered with no divergence at all")
	}
	if d.ID != big.ID {
		t.Errorf("GetDivergence asked for %s and answered with %s", big.ID, d.ID)
	}
	if d.DeltaMinorUnits != 5000 {
		t.Errorf("the delta is %d minor units, want 5000 — 1050.00 asserted "+
			"against 1000.00 computed", d.DeltaMinorUnits)
	}
	if d.ProducerRef != alpha {
		t.Errorf("the divergence is against producer %q, want %q", d.ProducerRef, alpha)
	}
	if d.Status != "OPEN" {
		t.Errorf("a freshly adjudicated divergence is %q, want OPEN", d.Status)
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[getDivergenceReq, getDivergenceResp](ctx, p.shadow(),
		shadowSvc+"/GetDivergence", getDivergenceReq{ID: big.ID, TenantID: stranger},
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e", RequestID: newID("req")}); err == nil {
		t.Error("another tenant read this divergence, which names what one society " +
			"paid a producer and what the platform thinks it should have")
	}

	list := func(what string, req listDivergencesReq) map[string]bool {
		t.Helper()
		req.TenantID, req.Limit = tenant, 100
		out, err := svcclient.Call[listDivergencesReq, listDivergencesResp](ctx, p.shadow(),
			shadowSvc+"/ListDivergences", req, opts)
		if err != nil {
			t.Fatalf("ListDivergences %s: %v", what, err)
		}
		seen := map[string]bool{}
		for _, x := range out.Divergences {
			seen[x.ID] = true
		}
		return seen
	}

	for _, c := range []struct {
		what    string
		req     listDivergencesReq
		want    []string
		exclude []string
	}{
		{
			"by producer", listDivergencesReq{ProducerRef: alpha},
			[]string{big.ID, input.ID}, []string{small.ID},
		},
		{
			"by classification", listDivergencesReq{Classification: "POLICY_DIFFERENCE"},
			[]string{big.ID, small.ID}, []string{input.ID},
		},
		{
			// 50.00 is in, 10.00 is out, and the 100.00 input difference is in.
			"by minimum size", listDivergencesReq{MinAbsDelta: 2000},
			[]string{big.ID, input.ID}, []string{small.ID},
		},
		{
			"by producer and classification together",
			listDivergencesReq{ProducerRef: alpha, Classification: "POLICY_DIFFERENCE"},
			[]string{big.ID}, []string{small.ID, input.ID},
		},
	} {
		seen := list(c.what, c.req)
		for _, id := range c.want {
			if !seen[id] {
				t.Errorf("%s: a divergence the filter should match is missing", c.what)
			}
		}
		for _, id := range c.exclude {
			if seen[id] {
				t.Errorf("%s: a divergence the filter should exclude came back\n"+
					"These filters fail open — when one breaks it returns more "+
					"than it should, never less — so the row somebody wanted is "+
					"still in the answer and nothing looks wrong.", c.what)
			}
		}
	}

	// Biggest first. The queue is worked through by hand and the order is what
	// decides which disagreement somebody sees on a Monday morning.
	ordered, err := svcclient.Call[listDivergencesReq, listDivergencesResp](ctx, p.shadow(),
		shadowSvc+"/ListDivergences", listDivergencesReq{TenantID: tenant, Limit: 100}, opts)
	if err != nil {
		t.Fatalf("ListDivergences unfiltered: %v", err)
	}
	if len(ordered.Divergences) != 3 {
		t.Fatalf("the tenant has three divergences and %d are listed", len(ordered.Divergences))
	}
	// 100.00, then 50.00, then 10.00.
	for i, want := range []int64{10000, 5000, 1000} {
		if ordered.Divergences[i].DeltaMinorUnits != want {
			t.Errorf("position %d holds a delta of %d minor units, want %d — "+
				"the queue is ordered largest first", i,
				ordered.Divergences[i].DeltaMinorUnits, want)
		}
	}
}

// Deciding a divergence records what was decided, who decided it, and why.
//
// This is the endpoint somebody reaches for when the platform and the incumbent
// disagree about what a producer is owed. A status change with no reason
// attached is indistinguishable afterwards from a mistake, so the service
// refuses one — and it refuses a status that is not a resolution at all, which
// would otherwise let a divergence be quietly reopened.
func TestDecidingADivergenceRecordsWhoDecidedAndWhy(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}
	producer := "producer:" + newID("a")
	d := adjudicated(t, p, tenant, producer, "1050.00", "1000.00", "300.000", "300.000", "3.5000", "3.3333")

	// A decision with no reason is refused.
	if _, err := svcclient.Call[resolveDivergenceReq, resolveDivergenceResp](ctx, p.shadow(),
		shadowSvc+"/ResolveDivergence", resolveDivergenceReq{
			ID: d.ID, TenantID: tenant, Status: "EXTERNAL_CONFIRMED",
			Resolution: "", Actor: "supervisor",
		}, opts); err == nil {
		t.Error("a divergence was decided with no stated reason; the record would " +
			"be indistinguishable from a mistake")
	}

	// OPEN is a state a divergence arrives in, not a decision somebody makes.
	if _, err := svcclient.Call[resolveDivergenceReq, resolveDivergenceResp](ctx, p.shadow(),
		shadowSvc+"/ResolveDivergence", resolveDivergenceReq{
			ID: d.ID, TenantID: tenant, Status: "OPEN",
			Resolution: "leaving it be", Actor: "supervisor",
		}, opts); err == nil {
		t.Error("a divergence was set back to OPEN through the resolution route")
	}

	decided, err := svcclient.Call[resolveDivergenceReq, resolveDivergenceResp](ctx, p.shadow(),
		shadowSvc+"/ResolveDivergence", resolveDivergenceReq{
			ID: d.ID, TenantID: tenant, Status: "EXTERNAL_CONFIRMED",
			Resolution: "the society's rate card was the one in force that month",
			Actor:      "supervisor",
		}, opts)
	if err != nil {
		t.Fatalf("ResolveDivergence: %v", err)
	}
	if decided.Divergence.Status != "EXTERNAL_CONFIRMED" {
		t.Errorf("the divergence is %q after being decided", decided.Divergence.Status)
	}

	// And it reads back that way from the other route, not only from the
	// response to the call that changed it.
	got, err := svcclient.Call[getDivergenceReq, getDivergenceResp](ctx, p.shadow(),
		shadowSvc+"/GetDivergence", getDivergenceReq{ID: d.ID, TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("GetDivergence after deciding: %v", err)
	}
	if got.Divergence.ResolvedBy != "supervisor" {
		t.Errorf("the divergence says it was decided by %q, want supervisor",
			got.Divergence.ResolvedBy)
	}
	if got.Divergence.Resolution == "" {
		t.Error("the divergence carries no reason for its decision")
	}
	if got.Divergence.ResolvedAt == "" {
		t.Error("the divergence carries no time of decision")
	}
	// The delta is untouched. A decision says what to do about a disagreement;
	// it does not change what the disagreement was.
	if got.Divergence.DeltaMinorUnits != 5000 {
		t.Errorf("the delta reads %d minor units after the decision and was 5000",
			got.Divergence.DeltaMinorUnits)
	}

	// It leaves the open queue.
	open, err := svcclient.Call[listDivergencesReq, listDivergencesResp](ctx, p.shadow(),
		shadowSvc+"/ListDivergences", listDivergencesReq{
			TenantID: tenant, Status: "OPEN", Limit: 100,
		}, opts)
	if err != nil {
		t.Fatalf("ListDivergences OPEN: %v", err)
	}
	for _, x := range open.Divergences {
		if x.ID == d.ID {
			t.Error("a decided divergence is still in the open queue")
		}
	}
}

// The summary counts and totals by classification, over the window asked for.
//
// This is the figure a board sees: how much money the platform and the
// incumbent disagreed about last month, and under which heading. Each row is
// grouped by currency and scale as well as classification, because minor units
// at two different scales cannot be added.
func TestTheDivergenceSummaryGroupsByClassificationOverItsWindow(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}
	alpha, beta := "producer:"+newID("a"), "producer:"+newID("b")

	// Two policy differences and one input difference: 50.00 + 10.00 under one
	// heading, 100.00 under another.
	adjudicated(t, p, tenant, alpha, "1050.00", "1000.00", "300.000", "300.000", "3.5000", "3.3333")
	adjudicated(t, p, tenant, beta, "1010.00", "1000.00", "300.000", "300.000", "3.3667", "3.3333")
	adjudicated(t, p, tenant, alpha, "1000.00", "900.00", "300.000", "270.000", "3.3333", "3.3333")

	// The window is over when the divergences were adjudicated, which is now —
	// not over the settlement period they concern.
	now := time.Now().UTC()
	got, err := svcclient.Call[summariseReq, summariseResp](ctx, p.shadow(),
		shadowSvc+"/Summarise", summariseReq{
			TenantID: tenant,
			From:     now.Add(-time.Hour).Format(time.RFC3339),
			To:       now.Add(time.Hour).Format(time.RFC3339),
		}, opts)
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}

	by := map[string]classSummaryProto{}
	for _, s := range got.Summaries {
		by[s.Classification] = s
		if s.Currency != "INR" || s.AmountScale != 2 {
			t.Errorf("a %s row is in %s at scale %d; rows are grouped by currency "+
				"and scale because minor units at two scales cannot be added",
				s.Classification, s.Currency, s.AmountScale)
		}
	}

	policy, ok := by["POLICY_DIFFERENCE"]
	if !ok {
		t.Fatal("no POLICY_DIFFERENCE row in the summary, and two were adjudicated")
	}
	if policy.Count != 2 {
		t.Errorf("the summary counts %d policy differences, want 2", policy.Count)
	}
	if policy.TotalAbsMinorUnits != 6000 {
		t.Errorf("the policy differences total %d minor units, want 6000 — "+
			"50.00 and 10.00", policy.TotalAbsMinorUnits)
	}

	input, ok := by["INPUT_DIFFERENCE"]
	if !ok {
		t.Fatal("no INPUT_DIFFERENCE row in the summary, and one was adjudicated")
	}
	if input.Count != 1 || input.TotalAbsMinorUnits != 10000 {
		t.Errorf("the input differences are %d worth %d minor units, want 1 worth 10000",
			input.Count, input.TotalAbsMinorUnits)
	}

	// A window that does not reach them summarises nothing. Without this the
	// test above would pass against a Summarise that ignored its window
	// entirely and totalled everything the tenant has.
	empty, err := svcclient.Call[summariseReq, summariseResp](ctx, p.shadow(),
		shadowSvc+"/Summarise", summariseReq{
			TenantID: tenant,
			From:     now.AddDate(-2, 0, 0).Format(time.RFC3339),
			To:       now.AddDate(-1, 0, 0).Format(time.RFC3339),
		}, opts)
	if err != nil {
		t.Fatalf("Summarise over an earlier window: %v", err)
	}
	if len(empty.Summaries) != 0 {
		t.Errorf("a window ending a year ago summarised %d classifications of "+
			"divergences adjudicated today", len(empty.Summaries))
	}
}
