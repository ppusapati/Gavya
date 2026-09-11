//go:build e2e

// Nine reads across four services, all of the same shape.
//
// procurement, material, settlement and laboratory each had a Get and a List
// left over — the routes that find a thing again after the tests that made it
// have moved on. Nothing about them is interesting individually, which is
// exactly why they were left: each is two lines in a handler over one query,
// and each of those queries has a filter nobody had ever watched work.
//
// They are together in one file because the risk is the same in all nine. A
// read filtered on the wrong column, or on nothing, still answers 200 and still
// contains the row the caller asked for. Every test below therefore puts a
// second row in the way — another society's cycle, another kind of node,
// another day's sample, another month's rate card — and asks whether the read
// can tell them apart.
//
// The rate card in force is the one with weight behind it. A collection is
// priced against the card that applied on the day the milk was collected, and
// procurement_test proves that end to end for pricing. What it never does is
// ask the platform which card that was — which is the question somebody asks
// when a producer disputes a payment, and it has its own route and its own
// query.
package e2e

import (
	"context"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type getRateCardReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type getRateCardResp struct {
	RateCard *rateCardProto `json:"rate_card"`
}

type rateCardInForceReq struct {
	TenantID string `json:"tenant_id"`
	At       string `json:"at"`
}

type listRateCardsReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit,omitempty"`
	Offset   int32  `json:"offset,omitempty"`
}

type listRateCardsResp struct {
	RateCards []*rateCardProto `json:"rate_cards"`
}

type getNodeReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type listNodesReq struct {
	TenantID string `json:"tenant_id"`
	Kind     string `json:"kind,omitempty"`
}

type listNodesResp struct {
	Nodes []*nodeProto `json:"nodes"`
}

type getCycleReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type listCyclesReq struct {
	TenantID    string `json:"tenant_id"`
	SocietyCode string `json:"society_code,omitempty"`
	Limit       int32  `json:"limit,omitempty"`
	Offset      int32  `json:"offset,omitempty"`
}

type listCyclesResp struct {
	Cycles []*cycleProto `json:"cycles"`
}

type getSampleReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type listSamplesReq struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Limit    int32  `json:"limit,omitempty"`
}

type listSamplesResp struct {
	Samples []*sampleProto `json:"samples"`
}

// The rate card in force on a day is the one that applied that day.
//
// Two cards, back to back: the society's rates changed in April. Somebody
// disputing a February payment needs February's card, and answering with
// today's is how a dispute is settled against a document that did not exist
// when the milk was collected.
//
// The listing is the other half. It carries both cards, newest first, because
// it is the history rather than the answer to "what applies now".
func TestTheRateCardInForceIsTheOneThatAppliedThatDay(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Its own tenant: a tenant holds one card in force at a time, so declaring
	// these into the shared tenant would collide with every pricing test.
	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	declare := func(name, from, to string) *rateCardProto {
		t.Helper()
		req := declareCardReq{
			TenantID: tenant, Name: name, Kind: "CHART",
			Currency: "INR", AmountScale: 2,
			Basis: "PER_LITRE", BetweenPoints: "BAND", OutsideChart: "REFUSE",
			Rounding: "HALF_UP", Cells: societyChart(),
			ValidFrom: from, ValidTo: to, Actor: "e2e",
		}
		out, err := svcclient.Call[declareCardReq, declareCardResp](ctx, p.procurement(),
			procurementSvc+"/DeclareRateCard", req, opts)
		if err != nil {
			t.Fatalf("DeclareRateCard %s: %v", name, err)
		}
		return out.RateCard
	}

	winter := declare("winter rates", "2026-01-01T00:00:00Z", "2026-04-01T00:00:00Z")
	spring := declare("spring rates", "2026-04-01T00:00:00Z", "")

	for _, c := range []struct{ when, wantID, name string }{
		{"2026-02-14T00:00:00Z", winter.ID, "winter rates"},
		{"2026-06-14T00:00:00Z", spring.ID, "spring rates"},
	} {
		got, err := svcclient.Call[rateCardInForceReq, getRateCardResp](ctx, p.procurement(),
			procurementSvc+"/GetRateCardInForce", rateCardInForceReq{
				TenantID: tenant, At: c.when,
			}, opts)
		if err != nil {
			t.Fatalf("GetRateCardInForce at %s: %v", c.when[:10], err)
		}
		if got.RateCard.ID != c.wantID {
			t.Errorf("the card in force on %s is %q, want %q — a disputed payment "+
				"is settled against the card that applied when the milk was "+
				"collected", c.when[:10], got.RateCard.Name, c.name)
		}
	}

	// Before either was declared there is no card, and that is a refusal rather
	// than the nearest one: pricing milk against a card that did not exist is
	// worse than saying there was none.
	if _, err := svcclient.Call[rateCardInForceReq, getRateCardResp](ctx, p.procurement(),
		procurementSvc+"/GetRateCardInForce", rateCardInForceReq{
			TenantID: tenant, At: "2025-06-01T00:00:00Z",
		}, opts); err == nil {
		t.Error("a rate card is in force six months before any was declared")
	}

	// By id, and the chart comes back with it — a card without its cells prices
	// nothing.
	byID, err := svcclient.Call[getRateCardReq, getRateCardResp](ctx, p.procurement(),
		procurementSvc+"/GetRateCard", getRateCardReq{TenantID: tenant, ID: winter.ID}, opts)
	if err != nil {
		t.Fatalf("GetRateCard: %v", err)
	}
	if byID.RateCard.ID != winter.ID {
		t.Errorf("GetRateCard asked for %s and answered with %s", winter.ID, byID.RateCard.ID)
	}
	if len(byID.RateCard.Cells) != len(societyChart()) {
		t.Errorf("the card reads back with %d cells and was declared with %d; "+
			"a chart without its cells prices nothing",
			len(byID.RateCard.Cells), len(societyChart()))
	}
	if byID.RateCard.Basis != "PER_LITRE" {
		t.Errorf("the card's basis reads back as %q, want PER_LITRE — the basis "+
			"decides whether a rate is per litre or per kilogram, and the two "+
			"differ by about three percent on the same milk", byID.RateCard.Basis)
	}
	if byID.RateCard.OutsideChart != "REFUSE" {
		t.Errorf("the card's outside-chart rule reads back as %q, want REFUSE",
			byID.RateCard.OutsideChart)
	}

	listed, err := svcclient.Call[listRateCardsReq, listRateCardsResp](ctx, p.procurement(),
		procurementSvc+"/ListRateCards", listRateCardsReq{TenantID: tenant, Limit: 50}, opts)
	if err != nil {
		t.Fatalf("ListRateCards: %v", err)
	}
	if len(listed.RateCards) != 2 {
		t.Fatalf("this tenant declared 2 cards and %d are listed", len(listed.RateCards))
	}
	if listed.RateCards[0].ID != spring.ID {
		t.Errorf("the card history opens with %q, want the newest window first",
			listed.RateCards[0].Name)
	}
}

// A node reads back, and a listing by kind holds only that kind.
//
// The kind is what says whether a thing is a dock, a silo or a tanker, and a
// listing by kind is how somebody picks a vessel to dispatch into. Handing them
// a silo when they asked for tankers is not a display problem: the next call
// dispatches milk into it.
func TestANodeReadsBackAndAListingByKindHoldsOnlyThatKind(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	register := func(code, kind string) *nodeProto {
		t.Helper()
		out, err := svcclient.Call[registerNodeReq, nodeResp](ctx, p.material(),
			materialSvc+"/RegisterNode", registerNodeReq{
				TenantID: tenant, Code: code, Name: code + " " + kind, Kind: kind,
				Actor: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("RegisterNode %s: %v", code, err)
		}
		return out.Node
	}

	tanker := register(newID("tk"), "TANKER")
	plant := register(newID("pl"), "PLANT")

	got, err := svcclient.Call[getNodeReq, nodeResp](ctx, p.material(),
		materialSvc+"/GetNode", getNodeReq{TenantID: tenant, ID: tanker.ID}, opts)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Node.ID != tanker.ID || got.Node.Kind != "TANKER" || got.Node.Code != tanker.Code {
		t.Errorf("the node reads back as %+v, want the tanker just registered", got.Node)
	}
	if !got.Node.Active {
		t.Error("a node registered a moment ago reads back inactive")
	}

	tankers, err := svcclient.Call[listNodesReq, listNodesResp](ctx, p.material(),
		materialSvc+"/ListNodes", listNodesReq{TenantID: tenant, Kind: "TANKER"}, opts)
	if err != nil {
		t.Fatalf("ListNodes TANKER: %v", err)
	}
	seen := map[string]bool{}
	for _, n := range tankers.Nodes {
		seen[n.ID] = true
		if n.Kind != "TANKER" {
			t.Errorf("a %s came back from a listing asked for tankers", n.Kind)
		}
	}
	if !seen[tanker.ID] {
		t.Error("the tanker is missing from a listing of tankers")
	}
	if seen[plant.ID] {
		t.Error("a plant is in a listing of tankers\n" +
			"This is how somebody picks a vessel to dispatch into; the next " +
			"call puts milk in it.")
	}

	// Naming no kind asks for all of them, which is the deliberate other case:
	// the filter is `$2='' OR kind=$2`.
	all, err := svcclient.Call[listNodesReq, listNodesResp](ctx, p.material(),
		materialSvc+"/ListNodes", listNodesReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListNodes unfiltered: %v", err)
	}
	both := map[string]bool{}
	for _, n := range all.Nodes {
		both[n.ID] = true
	}
	if !both[tanker.ID] || !both[plant.ID] {
		t.Errorf("asking for no particular kind returned %d nodes and should have "+
			"held both", len(all.Nodes))
	}
}

// A payment cycle reads back, and a listing for one society is that society's.
//
// A cycle is a month's payments for one society. Listing another society's
// cycles beside it is how somebody approves the wrong month's payments for the
// wrong village.
func TestACycleReadsBackAndAListingForOneSocietyIsThatSocietys(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}
	// Whole ids, not truncated: newID pads with zeros, so the first twelve
	// characters of two ids made in the same millisecond are identical and both
	// societies would be the same society. VARCHAR(64) has room.
	mine, theirs := newID("socA"), newID("socB")

	open := func(society, name, start, end string) *cycleProto {
		t.Helper()
		out, err := svcclient.Call[openCycleReq, cycleResp](ctx, p.settlement(),
			settlementSvc+"/OpenCycle", openCycleReq{
				TenantID: tenant, SocietyCode: society, Name: name,
				PeriodStart: start, PeriodEnd: end,
				Currency: "INR", AmountScale: 2,
				DeductionPolicy: "CAP_AT_EARNINGS", Actor: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("OpenCycle %s: %v", name, err)
		}
		return out.Cycle
	}

	february := open(mine, "february", "2026-02-01", "2026-02-28")
	march := open(mine, "march", "2026-03-01", "2026-03-31")
	other := open(theirs, "february elsewhere", "2026-02-01", "2026-02-28")

	got, err := svcclient.Call[getCycleReq, cycleResp](ctx, p.settlement(),
		settlementSvc+"/GetCycle", getCycleReq{TenantID: tenant, ID: february.ID}, opts)
	if err != nil {
		t.Fatalf("GetCycle: %v", err)
	}
	if got.Cycle.ID != february.ID {
		t.Errorf("GetCycle asked for %s and answered with %s", february.ID, got.Cycle.ID)
	}
	if got.Cycle.SocietyCode != mine {
		t.Errorf("the cycle belongs to society %q, want %q", got.Cycle.SocietyCode, mine)
	}
	if got.Cycle.DeductionPolicy != "CAP_AT_EARNINGS" {
		t.Errorf("the cycle's deduction policy reads back as %q, want CAP_AT_EARNINGS — "+
			"this decides whether a producer who owes more than their milk earned "+
			"ends the month at zero or in debt, which is not a detail",
			got.Cycle.DeductionPolicy)
	}

	listed, err := svcclient.Call[listCyclesReq, listCyclesResp](ctx, p.settlement(),
		settlementSvc+"/ListCycles", listCyclesReq{
			TenantID: tenant, SocietyCode: mine, Limit: 50,
		}, opts)
	if err != nil {
		t.Fatalf("ListCycles: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range listed.Cycles {
		seen[c.ID] = true
		if c.SocietyCode != mine {
			t.Errorf("society %q's cycle came back from a listing asked for %q",
				c.SocietyCode, mine)
		}
	}
	if !seen[february.ID] || !seen[march.ID] {
		t.Errorf("this society opened two cycles and %d are listed", len(listed.Cycles))
	}
	if seen[other.ID] {
		t.Error("another society's cycle is in this society's listing\n" +
			"This is the screen somebody approves a month's payments from.")
	}
	// Newest period first.
	if len(listed.Cycles) >= 2 && listed.Cycles[0].ID != march.ID {
		t.Errorf("the cycle listing opens with %q, want the most recent period",
			listed.Cycles[0].Name)
	}
}

// A sample reads back with its seal, and a listing covers the day asked for.
//
// Whether a sample is still sealed is the whole question a laboratory result
// rests on, so it is checked on the way back rather than only when the seal was
// broken. The listing is by the day the sample was drawn, which is how somebody
// reconciles a day's samples against a day's collections.
func TestASampleReadsBackWithItsSealAndAListingCoversTheDayAsked(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	draw := func(code, drawnAt string) *sampleProto {
		t.Helper()
		out, err := svcclient.Call[drawSampleReq, sampleResp](ctx, p.laboratory(),
			laboratorySvc+"/DrawSample", drawSampleReq{
				TenantID: tenant, Code: code, SourceKind: "MOVEMENT", SourceRef: "MV-1",
				DrawnAt: drawnAt, DrawnBy: "collector", SealNumber: "SEAL-" + code,
				Purpose: "PAYMENT", Actor: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("DrawSample %s: %v", code, err)
		}
		return out.Sample
	}

	// Whole ids for the same reason: a truncated one collides with the next.
	monday := draw(newID("smon"), "2026-05-04T06:00:00Z")
	tuesday := draw(newID("stue"), "2026-05-05T06:00:00Z")

	got, err := svcclient.Call[getSampleReq, sampleResp](ctx, p.laboratory(),
		laboratorySvc+"/GetSample", getSampleReq{TenantID: tenant, ID: tuesday.ID}, opts)
	if err != nil {
		t.Fatalf("GetSample: %v", err)
	}
	if got.Sample.ID != tuesday.ID {
		t.Errorf("GetSample asked for %s and answered with %s", tuesday.ID, got.Sample.ID)
	}
	if !got.Sample.Sealed {
		t.Error("a sample nobody has opened reads back unsealed\n" +
			"Whether the seal is intact is what a result taken from this sample " +
			"rests on; a sample that reads unsealed when it is not makes every " +
			"result from it arguable.")
	}
	if got.Sample.SealNumber == "" {
		t.Error("the sample reads back with no seal number")
	}
	if got.Sample.SealBrokenAt != "" {
		t.Errorf("an unopened sample carries a seal-broken time of %q", got.Sample.SealBrokenAt)
	}

	listed, err := svcclient.Call[listSamplesReq, listSamplesResp](ctx, p.laboratory(),
		laboratorySvc+"/ListSamples", listSamplesReq{
			TenantID: tenant,
			From:     "2026-05-05T00:00:00Z", To: "2026-05-05T23:59:59Z",
			Limit: 50,
		}, opts)
	if err != nil {
		t.Fatalf("ListSamples: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range listed.Samples {
		seen[s.ID] = true
	}
	if !seen[tuesday.ID] {
		t.Error("the sample drawn inside the window is not in the answer")
	}
	if seen[monday.ID] {
		t.Error("a sample drawn the day before is in a listing for Tuesday\n" +
			"This is what a day's samples are reconciled against a day's " +
			"collections with.")
	}
}
