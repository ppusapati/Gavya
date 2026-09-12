//go:build e2e

// Explaining a payment.
//
// The question a member asks is "why was I paid this". Before ExplainPayable the
// answer lived in three services: the payable and the lines gathered into it in
// settlement, what each delivery came to and whether it has since been restated
// in procurement, and for imported milk what the member number meant in
// canonical. Getting from one to the others took a database console.
//
// These drive the whole chain over HTTP, because what they are checking is the
// join — and a join is exactly the thing each service's own tests cannot see.
package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type explainReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type explainPaidProto struct {
	CollectedOn  string `json:"collected_on"`
	Shift        string `json:"shift"`
	Amount       string `json:"amount"`
	CollectionID string `json:"collection_id"`
}

type explainVersionProto struct {
	ID               string `json:"id"`
	Amount           string `json:"amount"`
	RateCardID       string `json:"rate_card_id"`
	Explanation      string `json:"explanation"`
	OriginKind       string `json:"origin_kind"`
	SourceSystemID   string `json:"source_system_id"`
	SupersededAt     string `json:"superseded_at"`
	Supersedes       string `json:"supersedes"`
	CorrectionReason string `json:"correction_reason"`
}

type explainedLineProto struct {
	Paid               explainPaidProto      `json:"paid"`
	Current            *explainVersionProto  `json:"current"`
	Versions           []explainVersionProto `json:"versions"`
	PaidMatchesCurrent bool                  `json:"paid_matches_current"`
}

type explainRateCardProto struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	LinesPriced int    `json:"lines_priced"`
	Unreadable  string `json:"unreadable"`
}

type explainMappingProto struct {
	ID           string `json:"id"`
	EntityID     string `json:"entity_id"`
	SupersededAt string `json:"superseded_at"`
	SupersededBy string `json:"superseded_by"`
}

type explainIdentityProto struct {
	SourceSystemID string                `json:"source_system_id"`
	ExternalID     string                `json:"external_id"`
	Lines          int                   `json:"lines"`
	Mappings       []explainMappingProto `json:"mappings"`
}

type explainConsultedProto struct {
	Procurement       bool   `json:"procurement"`
	Canonical         bool   `json:"canonical"`
	ProcurementWhyNot string `json:"procurement_why_not"`
	CanonicalWhyNot   string `json:"canonical_why_not"`
}

type explanationProto struct {
	Payable    *payableProto          `json:"payable"`
	Cycle      *cycleProto            `json:"cycle"`
	Lines      []explainedLineProto   `json:"lines"`
	RateCards  []explainRateCardProto `json:"rate_cards"`
	Identities []explainIdentityProto `json:"identities"`
	Findings   []string               `json:"findings"`
	Consulted  explainConsultedProto  `json:"consulted"`
}

func explain(t *testing.T, p *platform, payableID string) *explanationProto {
	t.Helper()
	out, err := svcclient.Call[explainReq, explanationProto](context.Background(), p.settlement(),
		settlementSvc+"/ExplainPayable", explainReq{TenantID: p.tenant, ID: payableID}, p.opts())
	if err != nil {
		t.Fatalf("ExplainPayable: %v", err)
	}
	return out
}

func findingContaining(findings []string, parts ...string) string {
	for _, f := range findings {
		ok := true
		for _, part := range parts {
			if !strings.Contains(f, part) {
				ok = false
				break
			}
		}
		if ok {
			return f
		}
	}
	return ""
}

// A payment is explained by its deliveries and the card that priced them, and a
// delivery restated after the payment is called out with the reason.
//
// The restatement is the case the whole feature exists for. A fortnight is
// gathered and paid; then a reading is found wrong and the delivery is
// corrected. The cycle line keeps the paid figure — correctly, a paid figure
// must not move — so the two now differ, and from inside settlement there is no
// way to tell a line that has stopped matching its delivery from one that has
// not. The explanation is where that difference becomes visible, together with
// procurement's own reason for it.
func TestAPaymentIsExplainedByItsDeliveriesAndACorrectionAfterwardsIsCalledOut(t *testing.T) {
	p := startPlatform(t)
	chart := declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	var collected []*pricedCollectionProto
	for _, day := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		in := morning(producer, day, "10.000", "4.1", "8.6")
		in.SocietyCode = society
		c, err := collect(t, p, in)
		if err != nil {
			t.Fatalf("collect on %s: %v", day, err)
		}
		collected = append(collected, c)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	pay := payableFor(t, p, cycle.ID, producer)

	// After the gather, the second morning is found to have been misread. 4.2
	// rather than something further off: the test chart runs 4.0 to 4.2 and
	// refuses a reading outside it, which is right and is not what this test is
	// about.
	restated := collected[1]
	if _, err := correctCollection(t, p, correctCollectionReq{
		ID:       restated.ID,
		Quantity: pointProto{Value: "10.000", Scale: 3}, QuantityUnit: "PER_LITRE",
		Fat: pointProto{Value: "4.2", Scale: 1}, SNF: pointProto{Value: "8.6", Scale: 1},
		Reason: "analyser slip read 4.2",
	}); err != nil {
		t.Fatalf("CorrectCollection: %v", err)
	}

	ex := explain(t, p, pay.ID)

	if ex.Payable == nil || ex.Payable.ID != pay.ID {
		t.Fatalf("the explanation is of a different payable: %+v", ex.Payable)
	}
	if ex.Cycle == nil || ex.Cycle.ID != cycle.ID {
		t.Errorf("the explanation names a different cycle: %+v", ex.Cycle)
	}
	if !ex.Consulted.Procurement {
		t.Fatalf("procurement was not consulted: %q", ex.Consulted.ProcurementWhyNot)
	}
	if len(ex.Lines) != 3 {
		t.Fatalf("%d lines explained for three deliveries", len(ex.Lines))
	}

	// The two untouched deliveries still match; the restated one does not, and
	// the explanation says so with the reason procurement recorded.
	var mismatched int
	for _, l := range ex.Lines {
		if l.Current == nil {
			t.Errorf("the delivery on %s has no current version from procurement", l.Paid.CollectedOn)
			continue
		}
		switch l.Paid.CollectedOn {
		case "2026-03-02":
			mismatched++
			if l.PaidMatchesCurrent {
				t.Error("the restated delivery is reported as still matching what was paid")
			}
			if l.Current.Amount == l.Paid.Amount {
				t.Errorf("the restated delivery is priced at %s now and was paid at %s; they "+
					"should differ, or the correction changed nothing", l.Current.Amount, l.Paid.Amount)
			}
			if len(l.Versions) != 2 {
				t.Errorf("the restated delivery has %d versions, want the original and the correction",
					len(l.Versions))
			}
			if l.Current.CorrectionReason != "analyser slip read 4.2" {
				t.Errorf("the current version carries reason %q", l.Current.CorrectionReason)
			}
		default:
			if !l.PaidMatchesCurrent {
				t.Errorf("the untouched delivery on %s is reported as no longer matching", l.Paid.CollectedOn)
			}
		}
		if l.Current.Explanation == "" {
			t.Errorf("the delivery on %s carries no pricing explanation from procurement", l.Paid.CollectedOn)
		}
	}
	if mismatched != 1 {
		t.Errorf("%d lines were the restated one, want exactly 1", mismatched)
	}

	f := findingContaining(ex.Findings, "2026-03-02", "analyser slip read 4.2")
	if f == "" {
		t.Errorf("no finding names the restated delivery and the reason:\n  %s",
			strings.Join(ex.Findings, "\n  "))
	} else if !strings.Contains(f, "adjustment") {
		t.Errorf("the finding does not say how a paid figure is corrected: %s", f)
	}
	if f := findingContaining(ex.Findings, "should not be possible"); f != "" {
		t.Errorf("the explanation reports its own figures as inconsistent: %s", f)
	}

	// One rate card priced all three, and it is the one declared above.
	if len(ex.RateCards) != 1 {
		t.Fatalf("%d rate cards for deliveries all priced against one: %+v", len(ex.RateCards), ex.RateCards)
	}
	if rc := ex.RateCards[0]; rc.ID != chart.ID || rc.Name != "March" || rc.LinesPriced != 3 || rc.Unreadable != "" {
		t.Errorf("rate card = %+v, want %s named March pricing 3 lines", rc, chart.ID)
	}

	// Native milk: nothing to look up, and it says so rather than saying nothing.
	if len(ex.Identities) != 0 {
		t.Errorf("identity traces for milk the society recorded itself: %+v", ex.Identities)
	}
	if ex.Consulted.Canonical {
		t.Error("canonical is reported as consulted for milk with no imported deliveries")
	}
	if !strings.Contains(ex.Consulted.CanonicalWhyNot, "no imported deliveries") {
		t.Errorf("canonical_why_not = %q", ex.Consulted.CanonicalWhyNot)
	}
	if findingContaining(ex.Findings, "recorded by the society itself", producer) == "" {
		t.Errorf("no finding says the deliveries were native under %s:\n  %s",
			producer, strings.Join(ex.Findings, "\n  "))
	}

	// And another tenant cannot explain this society's payment.
	stranger := actingAs(newID("ten"), "e2e")
	if _, err := svcclient.Call[explainReq, explanationProto](context.Background(), p.settlement(),
		settlementSvc+"/ExplainPayable", explainReq{TenantID: stranger.Tenant, ID: pay.ID}, stranger); err == nil {
		t.Error("another tenant explained this society's payment")
	}
}

// An imported delivery is explained by what its member number meant — including
// a mapping that has since been retired, and who retired it.
//
// This is the mapping every other read filters out, and the one that answers
// the question: the usual reason anybody asks why a payment went to somebody is
// that a mapping has since been found wrong. The payment stands as computed; the
// explanation says under what, and that it was withdrawn.
func TestAnImportedDeliveryIsExplainedByWhatItsMemberNumberMeant(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")
	ctx := context.Background()

	source := newID("src")
	member := newID("mem") // the member number as the source system knew it
	entity := newID("ent")

	mapped, err := svcclient.Call[mapIdentityReq, mapIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/MapIdentity", mapIdentityReq{
			TenantID: p.tenant, SourceSystemID: source, EntityKind: "PRODUCER",
			ExternalID: member, EntityID: entity, Method: "MANUAL",
			ValidFrom: "2020-01-01T00:00:00Z", Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("MapIdentity: %v", err)
	}

	in := morning(member, "2026-03-01", "10.000", "4.1", "8.6")
	in.SocietyCode = society
	in.OriginKind = "IMPORTED"
	in.SourceSystemID = source
	in.ImportBatchID = newID("batch")
	in.SourceRecordID = newID("rec")
	if _, err := collect(t, p, in); err != nil {
		t.Fatalf("collect imported: %v", err)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	pay := payableFor(t, p, cycle.ID, member)

	// After the payment, the mapping is found to have been wrong and withdrawn.
	if _, err := svcclient.Call[retireIdentityReq, struct{}](ctx, p.canonical(),
		canonicalSvc+"/RetireIdentity", retireIdentityReq{
			TenantID: p.tenant, ID: mapped.Identity.ID, Actor: "US_SECRETARY",
		}, p.opts()); err != nil {
		t.Fatalf("RetireIdentity: %v", err)
	}

	ex := explain(t, p, pay.ID)

	if !ex.Consulted.Canonical {
		t.Fatalf("canonical was not consulted for an imported delivery: %q", ex.Consulted.CanonicalWhyNot)
	}
	if len(ex.Lines) != 1 || ex.Lines[0].Current == nil {
		t.Fatalf("expected one explained line with a current version, got %+v", ex.Lines)
	}
	if got := ex.Lines[0].Current; got.OriginKind != "IMPORTED" || got.SourceSystemID != source {
		t.Errorf("the current version is %s from %q, want IMPORTED from %s",
			got.OriginKind, got.SourceSystemID, source)
	}

	if len(ex.Identities) != 1 {
		t.Fatalf("%d identity traces for one member number: %+v", len(ex.Identities), ex.Identities)
	}
	tr := ex.Identities[0]
	if tr.SourceSystemID != source || tr.ExternalID != member || tr.Lines != 1 {
		t.Errorf("trace = %+v, want %s in %s carried by 1 line", tr, member, source)
	}
	if len(tr.Mappings) != 1 {
		t.Fatalf("%d mappings in the history, want the one asserted above", len(tr.Mappings))
	}
	m := tr.Mappings[0]
	if m.ID != mapped.Identity.ID || m.EntityID != entity {
		t.Errorf("mapping = %+v, want %s → %s", m, mapped.Identity.ID, entity)
	}
	if m.SupersededAt == "" || m.SupersededBy != "US_SECRETARY" {
		t.Errorf("the retired mapping is reported as retired at %q by %q; a payment explained "+
			"by a mapping that does not say it was withdrawn is explained by a lie of omission",
			m.SupersededAt, m.SupersededBy)
	}

	f := findingContaining(ex.Findings, member, "retired", "US_SECRETARY")
	if f == "" {
		t.Errorf("no finding says the mapping was retired and by whom:\n  %s",
			strings.Join(ex.Findings, "\n  "))
	} else if !strings.Contains(f, "stands as computed") {
		t.Errorf("the finding does not say what happens to the payment: %s", f)
	}
}

// An adjustment is explained by its reason and the payment it corrects.
//
// It gathered no deliveries — it is money raised against a fortnight after the
// fortnight was settled — so there are no lines to trace. What explains it is
// the reason it was raised with, which the schema already requires, and the
// payable it corrects, which the explanation names so a reader can follow the
// chain back to the milk.
func TestAnAdjustmentIsExplainedByItsReasonAndWhatItCorrects(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen[:3])
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	if _, err := approve(t, p, cycle.ID); err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}
	settled := payableFor(t, p, cycle.ID, producer)

	raised, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer,
		Currency: "INR", AmountScale: 2, Amount: "25.00",
		AdjustsPayableID: settled.ID,
		Reason:           "one morning's fat was read low and the fortnight is already paid",
	})
	if err != nil {
		t.Fatalf("RaiseAdjustment: %v", err)
	}

	ex := explain(t, p, raised.Payable.ID)

	if ex.Payable == nil || ex.Payable.ID != raised.Payable.ID {
		t.Fatalf("the explanation is of a different payable: %+v", ex.Payable)
	}
	if len(ex.Lines) != 0 {
		t.Errorf("an adjustment gathered no deliveries and %d lines are explained", len(ex.Lines))
	}
	if f := findingContaining(ex.Findings, "adjustment", "one morning's fat was read low"); f == "" {
		t.Errorf("no finding gives the adjustment's reason:\n  %s", strings.Join(ex.Findings, "\n  "))
	}
	if f := findingContaining(ex.Findings, settled.ID); f == "" {
		t.Errorf("no finding names the payable this adjustment corrects, so a reader cannot "+
			"follow it back to the deliveries:\n  %s", strings.Join(ex.Findings, "\n  "))
	}
}
