//go:build e2e

// A fortnight, from milk to the number a farmer is handed.
//
// This is the path the whole platform is for. Every part of it existed before
// this file — a chart that prices milk, exact money, an audit trail — and none
// of it had been joined into the thing a society actually does twice a month:
// total each member's deliveries, take back what they owe, and pay the rest.
//
// The figures are chosen so every total can be checked by hand. A test whose
// expected values came out of the code it is testing proves only that the code
// agrees with itself, and the numbers here are the ones a secretary would arrive
// at with a calculator.
//
// The society's chart, the same one procurement_test.go uses:
//
//	         SNF 8.5   SNF 8.6
//	fat 4.0    40.00     41.00
//	fat 4.1    42.00     43.00
//	fat 4.2    44.00     45.00
package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const settlementSvc = "settlement.v1.SettlementService"

// ---------------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------------

type openCycleReq struct {
	TenantID        string `json:"tenant_id"`
	SocietyCode     string `json:"society_code"`
	Name            string `json:"name"`
	PeriodStart     string `json:"period_start"`
	PeriodEnd       string `json:"period_end"`
	Currency        string `json:"currency"`
	AmountScale     int32  `json:"amount_scale"`
	DeductionPolicy string `json:"deduction_policy"`
	Actor           string `json:"actor"`
}

type cycleProto struct {
	ID              string `json:"id"`
	SocietyCode     string `json:"society_code"`
	Name            string `json:"name"`
	PeriodStart     string `json:"period_start"`
	PeriodEnd       string `json:"period_end"`
	DeductionPolicy string `json:"deduction_policy"`
	Status          string `json:"status"`
	GatheredAt      string `json:"gathered_at,omitempty"`
	ApprovedAt      string `json:"approved_at,omitempty"`
	ApprovedBy      string `json:"approved_by,omitempty"`
}

type cycleResp struct {
	Cycle *cycleProto `json:"cycle"`
}

type cycleActionReq struct {
	TenantID string `json:"tenant_id"`
	CycleID  string `json:"cycle_id"`
	Actor    string `json:"actor"`
}

type openRecoveryReq struct {
	TenantID         string `json:"tenant_id"`
	ProducerRef      string `json:"producer_ref"`
	Kind             string `json:"kind"`
	Reference        string `json:"reference,omitempty"`
	Currency         string `json:"currency"`
	AmountScale      int32  `json:"amount_scale"`
	Principal        string `json:"principal"`
	Instalment       string `json:"instalment,omitempty"`
	AlreadyRecovered string `json:"already_recovered,omitempty"`
	Priority         int32  `json:"priority"`
	OpenedOn         string `json:"opened_on"`
	Actor            string `json:"actor"`
}

type recoveryProto struct {
	ID                    string `json:"id"`
	ProducerRef           string `json:"producer_ref"`
	Kind                  string `json:"kind"`
	Principal             string `json:"principal"`
	Recovered             string `json:"recovered"`
	Outstanding           string `json:"outstanding"`
	OutstandingMinorUnits int64  `json:"outstanding_minor_units"`
	Instalment            string `json:"instalment"`
	Priority              int32  `json:"priority"`
	Status                string `json:"status"`
}

type recoveryResp struct {
	Recovery *recoveryProto `json:"recovery"`
}

type listRecoveriesReq struct {
	TenantID        string `json:"tenant_id"`
	ProducerRef     string `json:"producer_ref,omitempty"`
	OutstandingOnly bool   `json:"outstanding_only,omitempty"`
}

type listRecoveriesResp struct {
	Recoveries []*recoveryProto `json:"recoveries"`
}

type payableProto struct {
	ID               string `json:"id"`
	CycleID          string `json:"cycle_id"`
	ProducerRef      string `json:"producer_ref"`
	Gross            string `json:"gross"`
	Deducted         string `json:"deducted"`
	Net              string `json:"net"`
	CarriedForward   string `json:"carried_forward"`
	NetMinorUnits    int64  `json:"net_minor_units"`
	Kind             string `json:"kind"`
	AdjustsPayableID string `json:"adjusts_payable_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Status           string `json:"status"`
	PaidAt           string `json:"paid_at,omitempty"`
	PaymentReference string `json:"payment_reference,omitempty"`
	HeldReason       string `json:"held_reason,omitempty"`
}

type listPayablesReq struct {
	TenantID string `json:"tenant_id"`
	CycleID  string `json:"cycle_id"`
}

type listPayablesResp struct {
	Payables           []*payableProto `json:"payables"`
	TotalNet           string          `json:"total_net"`
	TotalNetMinorUnits int64           `json:"total_net_minor_units"`
	TotalGross         string          `json:"total_gross"`
}

type markPaidReq struct {
	TenantID         string `json:"tenant_id"`
	ID               string `json:"id"`
	PaymentReference string `json:"payment_reference,omitempty"`
	Actor            string `json:"actor"`
}

type holdPayableReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Reason   string `json:"reason"`
	Actor    string `json:"actor"`
}

type payableResp struct {
	Payable *payableProto `json:"payable"`
}

type statementReq struct {
	TenantID    string `json:"tenant_id"`
	CycleID     string `json:"cycle_id"`
	ProducerRef string `json:"producer_ref"`
}

type statementLineProto struct {
	CollectedOn  string `json:"collected_on"`
	Shift        string `json:"shift"`
	Quantity     string `json:"quantity"`
	QuantityUnit string `json:"quantity_unit"`
	Rate         string `json:"rate,omitempty"`
	Amount       string `json:"amount"`
	CollectionID string `json:"collection_id"`
}

type statementDeductionProto struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference,omitempty"`
	Amount    string `json:"amount"`
}

type statementProto struct {
	ProducerRef string `json:"producer_ref"`
	SocietyCode string `json:"society_code"`
	CycleName   string `json:"cycle_name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	CycleStatus string `json:"cycle_status"`

	Lines      []statementLineProto      `json:"lines"`
	Deductions []statementDeductionProto `json:"deductions"`
	Quantities map[string]string         `json:"quantities"`

	Currency       string `json:"currency"`
	Gross          string `json:"gross"`
	Deducted       string `json:"deducted"`
	Net            string `json:"net"`
	CarriedForward string `json:"carried_forward,omitempty"`

	Status           string `json:"status,omitempty"`
	PaidAt           string `json:"paid_at,omitempty"`
	PaymentReference string `json:"payment_reference,omitempty"`
	HeldReason       string `json:"held_reason,omitempty"`

	Adjustments []statementAdjustmentProto `json:"adjustments,omitempty"`
}

type statementAdjustmentProto struct {
	ID     string `json:"id"`
	Amount string `json:"amount"`
	Reason string `json:"reason"`
	Status string `json:"status"`
	PaidAt string `json:"paid_at,omitempty"`
}

type statementResp struct {
	Statement *statementProto `json:"statement"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const society = "SOC_KOTHAPALLI"

func openCycle(t *testing.T, p *platform, name, from, to, policy string) (*cycleProto, error) {
	t.Helper()
	resp, err := svcclient.Call[openCycleReq, cycleResp](
		context.Background(), p.settlement(), settlementSvc+"/OpenCycle",
		openCycleReq{
			TenantID: p.tenant, SocietyCode: society, Name: name,
			PeriodStart: from, PeriodEnd: to,
			Currency: "INR", AmountScale: 2,
			DeductionPolicy: policy, Actor: "e2e",
		}, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Cycle, nil
}

func mustOpenCycle(t *testing.T, p *platform, name, from, to, policy string) *cycleProto {
	t.Helper()
	c, err := openCycle(t, p, name, from, to, policy)
	if err != nil {
		t.Fatalf("OpenCycle %s: %v", name, err)
	}
	return c
}

func openRecovery(t *testing.T, p *platform, in openRecoveryReq) (*recoveryProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	in.Currency, in.AmountScale = "INR", 2
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	resp, err := svcclient.Call[openRecoveryReq, recoveryResp](
		context.Background(), p.settlement(), settlementSvc+"/OpenRecovery", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Recovery, nil
}

func gather(t *testing.T, p *platform, cycleID string) (*cycleProto, error) {
	t.Helper()
	resp, err := svcclient.Call[cycleActionReq, cycleResp](
		context.Background(), p.settlement(), settlementSvc+"/GatherCycle",
		cycleActionReq{TenantID: p.tenant, CycleID: cycleID, Actor: "e2e"}, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Cycle, nil
}

func approve(t *testing.T, p *platform, cycleID string) (*cycleProto, error) {
	t.Helper()
	resp, err := svcclient.Call[cycleActionReq, cycleResp](
		context.Background(), p.settlement(), settlementSvc+"/ApproveCycle",
		cycleActionReq{TenantID: p.tenant, CycleID: cycleID, Actor: "e2e_secretary"}, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Cycle, nil
}

func payables(t *testing.T, p *platform, cycleID string) *listPayablesResp {
	t.Helper()
	resp, err := svcclient.Call[listPayablesReq, listPayablesResp](
		context.Background(), p.settlement(), settlementSvc+"/ListPayables",
		listPayablesReq{TenantID: p.tenant, CycleID: cycleID}, p.opts())
	if err != nil {
		t.Fatalf("ListPayables: %v", err)
	}
	return resp
}

// payableFor returns a producer's SETTLEMENT payable for a cycle.
//
// The kind matters. A producer may have several payables in one cycle once
// corrections exist — the fortnight and any adjustments to it — and a helper
// that took the first match by producer would silently return whichever the
// database yielded. It did, and a test asserting the fortnight was approved
// started reading an adjustment's status instead.
func payableFor(t *testing.T, p *platform, cycleID, producer string) *payableProto {
	t.Helper()
	var found *payableProto
	for _, pp := range payables(t, p, cycleID).Payables {
		if pp.ProducerRef != producer || pp.Kind != "SETTLEMENT" {
			continue
		}
		if found != nil {
			t.Fatalf("%s has two settlement payables in cycle %s, which is one producer paid "+
				"twice for one fortnight", producer, cycleID)
		}
		found = pp
	}
	if found == nil {
		t.Fatalf("no settlement payable for %s in cycle %s", producer, cycleID)
	}
	return found
}

func statement(t *testing.T, p *platform, cycleID, producer string) *statementProto {
	t.Helper()
	resp, err := svcclient.Call[statementReq, statementResp](
		context.Background(), p.settlement(), settlementSvc+"/GetProducerStatement",
		statementReq{TenantID: p.tenant, CycleID: cycleID, ProducerRef: producer}, p.opts())
	if err != nil {
		t.Fatalf("GetProducerStatement %s: %v", producer, err)
	}
	return resp.Statement
}

func recoveriesFor(t *testing.T, p *platform, producer string) []*recoveryProto {
	t.Helper()
	resp, err := svcclient.Call[listRecoveriesReq, listRecoveriesResp](
		context.Background(), p.settlement(), settlementSvc+"/ListRecoveries",
		listRecoveriesReq{TenantID: p.tenant, ProducerRef: producer}, p.opts())
	if err != nil {
		t.Fatalf("ListRecoveries %s: %v", producer, err)
	}
	return resp.Recoveries
}

// deliverFortnight puts one producer's milk in for a run of mornings.
//
// Every morning is 10.000 litres at fat 4.1 / SNF 8.6, which the chart prices at
// 43.0000 a litre: 430.00 a day. Ten days is 4300.00, which is the figure most
// of these tests are built on.
func deliverFortnight(t *testing.T, p *platform, producer string, days []string) {
	t.Helper()
	for _, d := range days {
		in := morning(producer, d, "10.000", "4.1", "8.6")
		in.SocietyCode = society
		if _, err := collect(t, p, in); err != nil {
			t.Fatalf("collect %s on %s: %v", producer, d, err)
		}
	}
}

// marchFirstTen is the first ten days of a March fortnight.
var marchFirstTen = []string{
	"2026-03-01", "2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05",
	"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10",
}

// ---------------------------------------------------------------------------
// The whole path
// ---------------------------------------------------------------------------

// A producer with no debts is paid what their milk came to, and the statement
// they are handed says so.
func TestAFortnightOfMilkBecomesAPaymentAProducerCanRead(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if cycle.Status != "OPEN" {
		t.Fatalf("a new cycle is %s, want OPEN", cycle.Status)
	}

	gathered, err := gather(t, p, cycle.ID)
	if err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	if gathered.Status != "GATHERED" {
		t.Errorf("after gathering the cycle is %s, want GATHERED", gathered.Status)
	}

	// Ten mornings of 10 litres at 43.00 is 4300.00.
	pay := payableFor(t, p, cycle.ID, producer)
	if pay.Gross != "4300.00" {
		t.Errorf("gross %s, want 4300.00", pay.Gross)
	}
	if pay.Deducted != "0.00" {
		t.Errorf("deducted %s from a producer who owes nothing", pay.Deducted)
	}
	if pay.Net != "4300.00" {
		t.Errorf("net %s, want 4300.00", pay.Net)
	}

	s := statement(t, p, cycle.ID, producer)
	if len(s.Lines) != 10 {
		t.Errorf("the statement shows %d deliveries, want 10", len(s.Lines))
	}
	if s.Quantities["PER_LITRE"] != "100.000" {
		t.Errorf("the statement totals %q litres, want 100.000", s.Quantities["PER_LITRE"])
	}
	if s.Net != "4300.00" || s.Gross != "4300.00" {
		t.Errorf("the statement reads gross %s net %s", s.Gross, s.Net)
	}
	if s.SocietyCode != society {
		t.Errorf("the statement names society %q", s.SocietyCode)
	}
	// A producer holding this should be able to find one day on it and check it.
	var found bool
	for _, l := range s.Lines {
		if l.CollectedOn == "2026-03-03" {
			found = true
			if l.Amount != "430.00" {
				t.Errorf("3 March reads %s, want 430.00", l.Amount)
			}
			if l.Quantity != "10.000" {
				t.Errorf("3 March shows %s litres", l.Quantity)
			}
		}
	}
	if !found {
		t.Error("the statement does not show the milk delivered on 3 March")
	}
}

// The ordinary case a society actually runs: an advance being recovered by
// instalments, and the fortnight's dues.
//
//	gross                    4300.00
//	advance instalment      -1000.00
//	society dues              -50.00
//	                        --------
//	net                      3250.00
func TestRecoveriesComeOutOfTheMilkInOrder(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)

	advance, err := openRecovery(t, p, openRecoveryReq{
		ProducerRef: producer, Kind: "ADVANCE", Reference: "Advance for a shed",
		Principal: "5000.00", Instalment: "1000.00",
		Priority: 1, OpenedOn: "2026-01-10",
	})
	if err != nil {
		t.Fatalf("OpenRecovery advance: %v", err)
	}
	if _, err := openRecovery(t, p, openRecoveryReq{
		ProducerRef: producer, Kind: "SOCIETY_DUES",
		Principal: "50.00", Priority: 2, OpenedOn: "2026-03-01",
	}); err != nil {
		t.Fatalf("OpenRecovery dues: %v", err)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	pay := payableFor(t, p, cycle.ID, producer)
	if pay.Gross != "4300.00" {
		t.Errorf("gross %s, want 4300.00", pay.Gross)
	}
	if pay.Deducted != "1050.00" {
		t.Errorf("deducted %s, want 1050.00", pay.Deducted)
	}
	if pay.Net != "3250.00" {
		t.Errorf("net %s, want 3250.00", pay.Net)
	}

	// The statement itemises them, in the order they were served.
	s := statement(t, p, cycle.ID, producer)
	if len(s.Deductions) != 2 {
		t.Fatalf("the statement shows %d deductions, want 2", len(s.Deductions))
	}

	// And the debts have gone down by exactly what was taken.
	for _, r := range recoveriesFor(t, p, producer) {
		switch r.Kind {
		case "ADVANCE":
			if r.ID != advance.ID {
				continue
			}
			if r.Recovered != "1000.00" || r.Outstanding != "4000.00" {
				t.Errorf("the advance reads %s recovered, %s outstanding; want 1000.00 and 4000.00",
					r.Recovered, r.Outstanding)
			}
			if r.Status != "OUTSTANDING" {
				t.Errorf("the advance is %s after one of five instalments", r.Status)
			}
		case "SOCIETY_DUES":
			if r.Outstanding != "0.00" {
				t.Errorf("the dues read %s outstanding, want 0.00", r.Outstanding)
			}
			// A debt taken to zero is settled in the same breath, not left
			// outstanding for a producer to see next fortnight.
			if r.Status != "SETTLED" {
				t.Errorf("the dues are %s having been recovered in full, want SETTLED", r.Status)
			}
		}
	}
}

// The defect this whole design exists to prevent.
//
// A fortnight is gathered. Somebody re-runs it — a retry, a second operator, a
// script that ran twice. Without the constraint on the collection, every
// producer is paid a second time for the same milk, every debt is recovered
// twice, and the totals look like a good fortnight.
func TestTheSameMilkCannotBePaidForTwice(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)

	first := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, first.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	// Gathering the same cycle again is refused on its status.
	if _, err := gather(t, p, first.ID); err == nil {
		t.Error("the same cycle was gathered twice")
	} else if !strings.Contains(err.Error(), "GATHERED") {
		t.Errorf("the refusal does not say the cycle has already been gathered: %v", err)
	}

	// And a second cycle over the same period is refused before it can even try,
	// which is the constraint that does not depend on anybody checking a status.
	if _, err := openCycle(t, p, "March 1-15 again", "2026-03-05", "2026-03-20", "CAP_AT_EARNINGS"); err == nil {
		t.Error("a second cycle was opened over a period already being settled")
	}

	// The producer was paid once.
	if got := payableFor(t, p, first.ID, producer).Net; got != "4300.00" {
		t.Errorf("net %s after one gather, want 4300.00", got)
	}
}

// A cycle gathered in error is abandoned, and everything it held goes back:
// the collections become gatherable again and the debts return to what they were.
//
// The failure this guards is subtle. A society re-gathers a corrected fortnight
// and the producer's advance has gone down twice for one payment, so they owe
// less than they borrowed and nobody can say why.
func TestAbandoningACycleGivesBackTheMilkAndTheDebt(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	if _, err := openRecovery(t, p, openRecoveryReq{
		ProducerRef: producer, Kind: "ADVANCE", Principal: "5000.00",
		Instalment: "1000.00", Priority: 1, OpenedOn: "2026-01-10",
	}); err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}

	wrong := mustOpenCycle(t, p, "March 1-20 (wrong period)", "2026-03-01", "2026-03-20", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, wrong.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	if got := recoveriesFor(t, p, producer)[0].Recovered; got != "1000.00" {
		t.Fatalf("the advance reads %s recovered before abandoning", got)
	}

	if _, err := svcclient.Call[cycleActionReq, struct {
		Abandoned bool `json:"abandoned"`
	}](context.Background(), p.settlement(), settlementSvc+"/AbandonCycle",
		cycleActionReq{TenantID: p.tenant, CycleID: wrong.ID, Actor: "e2e"}, p.opts()); err != nil {
		t.Fatalf("AbandonCycle: %v", err)
	}

	// The debt is back where it was.
	back := recoveriesFor(t, p, producer)[0]
	if back.Recovered != "0.00" || back.Outstanding != "5000.00" {
		t.Errorf("after abandoning, the advance reads %s recovered and %s outstanding; "+
			"want 0.00 and 5000.00", back.Recovered, back.Outstanding)
	}

	// And the right cycle can now be gathered over the same milk.
	right := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, right.ID); err != nil {
		t.Fatalf("the corrected cycle could not gather the released collections: %v", err)
	}
	if got := payableFor(t, p, right.ID, producer).Net; got != "3300.00" {
		t.Errorf("net %s after re-gathering, want 3300.00", got)
	}
	// Recovered once, not twice — which is the whole point.
	if got := recoveriesFor(t, p, producer)[0].Recovered; got != "1000.00" {
		t.Errorf("the advance reads %s recovered after one payment through two cycles", got)
	}
}

// The two policies, over the same fortnight and the same debt, so the difference
// between them is the only thing that varies.
//
// This is why deduction_policy has no default: both of these are somebody's
// correct answer, and a platform that picked one would be picking for them.
func TestTheDeductionPolicyDecidesWhetherAProducerIsHandedABill(t *testing.T) {
	debt := func(t *testing.T, p *platform, producer string) {
		t.Helper()
		if _, err := openRecovery(t, p, openRecoveryReq{
			ProducerRef: producer, Kind: "LOAN", Principal: "6000.00",
			Instalment: "6000.00", Priority: 1, OpenedOn: "2026-01-10",
		}); err != nil {
			t.Fatalf("OpenRecovery: %v", err)
		}
	}

	t.Run("capping at earnings pays out nothing and carries the rest", func(t *testing.T) {
		p := startPlatform(t)
		declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")
		producer := newID("prod")
		deliverFortnight(t, p, producer, marchFirstTen)
		debt(t, p, producer)

		c := mustOpenCycle(t, p, "March capped", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
		if _, err := gather(t, p, c.ID); err != nil {
			t.Fatalf("GatherCycle: %v", err)
		}
		pay := payableFor(t, p, c.ID, producer)
		if pay.Net != "0.00" {
			t.Errorf("net %s, want 0.00", pay.Net)
		}
		if pay.Deducted != "4300.00" {
			t.Errorf("deducted %s, want the whole 4300.00 of earnings", pay.Deducted)
		}
		// 6000 wanted, 4300 taken.
		if pay.CarriedForward != "1700.00" {
			t.Errorf("carried forward %s, want 1700.00", pay.CarriedForward)
		}
		// The statement says why the debt did not go down as far as expected.
		if got := statement(t, p, c.ID, producer).CarriedForward; got != "1700.00" {
			t.Errorf("the statement carries forward %q, want 1700.00", got)
		}
	})

	t.Run("allowing negative leaves the producer owing", func(t *testing.T) {
		p := startPlatform(t)
		declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")
		producer := newID("prod")
		deliverFortnight(t, p, producer, marchFirstTen)
		debt(t, p, producer)

		c := mustOpenCycle(t, p, "March uncapped", "2026-03-01", "2026-03-15", "ALLOW_NEGATIVE")
		if _, err := gather(t, p, c.ID); err != nil {
			t.Fatalf("GatherCycle: %v", err)
		}
		pay := payableFor(t, p, c.ID, producer)
		if pay.Net != "-1700.00" {
			t.Errorf("net %s, want -1700.00", pay.Net)
		}
		if pay.Deducted != "6000.00" {
			t.Errorf("deducted %s, want the full 6000.00", pay.Deducted)
		}
	})
}

// A cycle opened without saying which policy it settles under is refused rather
// than filled in.
func TestACycleWithNoDeductionPolicyIsRefused(t *testing.T) {
	p := startPlatform(t)
	_, err := openCycle(t, p, "March", "2026-03-01", "2026-03-15", "")
	if err == nil {
		t.Fatal("a cycle was opened without saying what it does when a producer owes more than they earned")
	}
	if !strings.Contains(err.Error(), "CAP_AT_EARNINGS") {
		t.Errorf("the refusal does not say what the choices are: %v", err)
	}
}

// The payable lifecycle: a figure is computed, a person approves it, and money
// leaves. Each step refuses to be skipped.
func TestAProducerIsNotPaidUntilSomebodyApprovesTheFigures(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	pay := payableFor(t, p, cycle.ID, producer)
	if pay.Status != "PAYABLE" {
		t.Errorf("a freshly gathered payable is %s, want PAYABLE", pay.Status)
	}

	// Paying before approval is refused.
	//
	// The refusal has to explain itself. A CHECK constraint on the table also
	// stops this — paid_at cannot be set while approved_at is null — and that
	// backstop is worth having, but on its own it reports a constraint name to
	// somebody looking at a screen that says "pay". The service's own check is
	// what turns that into a sentence, so this asserts the sentence rather than
	// only that something went wrong.
	_, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: pay.ID, Actor: "e2e_cashier"}, p.opts())
	if err == nil {
		t.Fatal("a producer was paid a figure nobody had approved")
	}
	if !strings.Contains(err.Error(), "APPROVED") || !strings.Contains(err.Error(), "PAYABLE") {
		t.Errorf("the refusal does not say what state the payable is in or what it needs to be: %v", err)
	}

	approved, err := approve(t, p, cycle.ID)
	if err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}
	if approved.Status != "APPROVED" || approved.ApprovedBy != "e2e_secretary" {
		t.Errorf("the cycle is %s, approved by %q", approved.Status, approved.ApprovedBy)
	}

	paidResp, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: pay.ID, PaymentReference: "CHQ 448120", Actor: "e2e_cashier"},
		p.opts())
	if err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if paidResp.Payable.Status != "PAID" {
		t.Errorf("after paying, the payable is %s", paidResp.Payable.Status)
	}

	// Paying it a second time is refused. This is real money: the row after a
	// second payment looks exactly like the row after the first.
	if _, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: pay.ID, Actor: "e2e_cashier"}, p.opts()); err == nil {
		t.Error("the same producer was paid twice for one fortnight")
	}

	// And the statement now says it was paid, with the cheque number on it.
	s := statement(t, p, cycle.ID, producer)
	if s.Status != "PAID" || s.PaymentReference != "CHQ 448120" {
		t.Errorf("the statement reads %s / %q", s.Status, s.PaymentReference)
	}
	if s.PaidAt == "" {
		t.Error("the statement does not say when the producer was paid")
	}
}

// A payment held for a reason stays held when the cycle is approved in bulk.
//
// Approving a cycle approves the figures; a hold is a decision about one person
// and a bulk action must not quietly undo it.
func TestAHeldPaymentSurvivesABulkApproval(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	held, ordinary := newID("prod"), newID("prod")
	deliverFortnight(t, p, held, marchFirstTen)
	deliverFortnight(t, p, ordinary, marchFirstTen)

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	target := payableFor(t, p, cycle.ID, held)
	if _, err := svcclient.Call[holdPayableReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/HoldPayable",
		holdPayableReq{
			TenantID: p.tenant, ID: target.ID,
			Reason: "membership under question after a death in the family",
			Actor:  "e2e_secretary",
		}, p.opts()); err != nil {
		t.Fatalf("HoldPayable: %v", err)
	}

	if _, err := approve(t, p, cycle.ID); err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}

	stillHeld := payableFor(t, p, cycle.ID, held)
	if stillHeld.Status != "HELD" {
		t.Errorf("the held payment is %s after a bulk approval, want HELD", stillHeld.Status)
	}
	if stillHeld.HeldReason == "" {
		t.Error("the hold does not say why, which is a thing done to a person with no reason given")
	}
	if got := payableFor(t, p, cycle.ID, ordinary).Status; got != "APPROVED" {
		t.Errorf("the ordinary payable is %s after approval, want APPROVED", got)
	}

	// And a held payment cannot be paid.
	if _, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: stillHeld.ID, Actor: "e2e_cashier"}, p.opts()); err == nil {
		t.Error("a payment that was being withheld was made anyway")
	}
}

// A cycle with money already out of the door cannot be abandoned. The status
// check is one guard; the trigger on the paid row is the one that holds when
// something other than this service is doing the deleting.
func TestACycleWithPaymentsAlreadyMadeCannotBeUnwound(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	if _, err := approve(t, p, cycle.ID); err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}
	pay := payableFor(t, p, cycle.ID, producer)
	if _, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: pay.ID, Actor: "e2e_cashier"}, p.opts()); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	_, err := svcclient.Call[cycleActionReq, struct {
		Abandoned bool `json:"abandoned"`
	}](context.Background(), p.settlement(), settlementSvc+"/AbandonCycle",
		cycleActionReq{TenantID: p.tenant, CycleID: cycle.ID, Actor: "e2e"}, p.opts())
	if err == nil {
		t.Fatal("a cycle whose payments had already been made was abandoned")
	}
	if !strings.Contains(err.Error(), "APPROVED") && !strings.Contains(err.Error(), "paid") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// A debt already partly repaid before the platform arrived does not get
// recovered from the start again.
//
// This is the migration case and it is where a society loses trust fastest: a
// member who has repaid four of five instalments elsewhere, whose ledger starts
// at zero here, pays the whole loan back twice.
func TestADebtCarriedInPartlyRepaidIsNotRecoveredFromTheStart(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)

	rec, err := openRecovery(t, p, openRecoveryReq{
		ProducerRef: producer, Kind: "LOAN", Reference: "migrated from the paper ledger",
		Principal: "5000.00", AlreadyRecovered: "4800.00", Instalment: "1000.00",
		Priority: 1, OpenedOn: "2025-08-01",
	})
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	if rec.Outstanding != "200.00" {
		t.Fatalf("the migrated loan reads %s outstanding, want 200.00", rec.Outstanding)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	pay := payableFor(t, p, cycle.ID, producer)
	if pay.Deducted != "200.00" {
		t.Errorf("deducted %s against a loan with 200.00 left on it, not the 1000.00 instalment",
			pay.Deducted)
	}
	if pay.Net != "4100.00" {
		t.Errorf("net %s, want 4100.00", pay.Net)
	}
}

// A society settles many members at once, and the total paid out has to be the
// total earned less the total recovered — to the paisa, over amounts that do not
// divide evenly.
func TestTheCycleTotalIsTheSumOfWhatEveryProducerTakesHome(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	// Three producers with awkward quantities, so nothing divides evenly.
	quantities := []string{"7.015", "3.333", "11.117"}
	producers := make([]string, len(quantities))
	for i, q := range quantities {
		producers[i] = newID("prod")
		for _, d := range marchFirstTen[:3] {
			in := morning(producers[i], d, q, "4.1", "8.6")
			in.SocietyCode = society
			if _, err := collect(t, p, in); err != nil {
				t.Fatalf("collect: %v", err)
			}
		}
	}
	// One of them owes something that will not be covered evenly either.
	if _, err := openRecovery(t, p, openRecoveryReq{
		ProducerRef: producers[1], Kind: "FEED_CREDIT", Principal: "137.77",
		Priority: 1, OpenedOn: "2026-02-01",
	}); err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	list := payables(t, p, cycle.ID)
	var gross, deducted, net int64
	for _, pp := range list.Payables {
		gross += mustMinorUnits(t, pp.Gross)
		deducted += mustMinorUnits(t, pp.Deducted)
		net += pp.NetMinorUnits
	}
	if gross-deducted != net {
		t.Errorf("the cycle earned %d and recovered %d, which is %d, but pays out %d",
			gross, deducted, gross-deducted, net)
	}
	if list.TotalNetMinorUnits != net {
		t.Errorf("the cycle reports a total of %d against %d in its payables",
			list.TotalNetMinorUnits, net)
	}

	// And each producer's statement reconciles on its own: the lines add up to
	// the gross, and the deductions to what was taken.
	for _, ref := range producers {
		s := statement(t, p, cycle.ID, ref)
		var lines int64
		for _, l := range s.Lines {
			lines += mustMinorUnits(t, l.Amount)
		}
		if lines != mustMinorUnits(t, s.Gross) {
			t.Errorf("%s: the statement's deliveries come to %d but it reads a gross of %s",
				ref, lines, s.Gross)
		}
		var ded int64
		for _, d := range s.Deductions {
			ded += mustMinorUnits(t, d.Amount)
		}
		if ded != mustMinorUnits(t, s.Deducted) {
			t.Errorf("%s: the statement's deductions come to %d but it reads %s taken",
				ref, ded, s.Deducted)
		}
	}
}

// mustMinorUnits reads a two-decimal amount as an integer of paise.
//
// Written out rather than reached for via a float, because this file exists to
// check that no paisa is lost and parsing the amounts as floats to check that
// would be checking it with the tool that loses them.
func mustMinorUnits(t *testing.T, s string) int64 {
	t.Helper()
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, ok := strings.Cut(s, ".")
	if !ok || len(frac) != 2 {
		t.Fatalf("%q is not an amount written to two decimal places", s)
	}
	var v int64
	for _, digit := range whole + frac {
		if digit < '0' || digit > '9' {
			t.Fatalf("%q is not a number", s)
		}
		v = v*10 + int64(digit-'0')
	}
	if neg {
		return -v
	}
	return v
}

// A gather over a period with no milk in it is refused rather than producing a
// cycle in which everybody earned nothing.
//
// The two are indistinguishable in the data and completely different in the
// world: one is a misconfigured service or a wrong period, the other is a
// village that stopped delivering.
func TestGatheringAPeriodWithNoMilkIsRefused(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	empty := mustOpenCycle(t, p, "A fortnight nobody delivered in",
		"2027-06-01", "2027-06-15", "CAP_AT_EARNINGS")
	_, err := gather(t, p, empty.ID)
	if err == nil {
		t.Fatal("a period with no collections settled into a cycle where everybody earned zero")
	}
	if !strings.Contains(err.Error(), "no collections") {
		t.Errorf("the refusal does not say the period held no milk: %v", err)
	}
}

// Milk recorded without a society is refused rather than quietly left out.
//
// The producer in this test delivered ten times and three of those rows carry no
// society code. A gather that filtered them away would pay for seven and hand
// over a statement with nothing wrong on it — only three deliveries absent,
// which is the kind of error a farmer notices and cannot prove.
func TestMilkThatBelongsToNoSocietyStopsTheSettlement(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	for i, d := range marchFirstTen {
		in := morning(producer, d, "10.000", "4.1", "8.6")
		// Three of the ten were entered without a society.
		if i >= 7 {
			in.SocietyCode = ""
		} else {
			in.SocietyCode = society
		}
		if _, err := collect(t, p, in); err != nil {
			t.Fatalf("collect on %s: %v", d, err)
		}
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	_, err := gather(t, p, cycle.ID)
	if err == nil {
		t.Fatal("a fortnight settled while three of the producer's ten deliveries " +
			"belonged to no society and were left out of it")
	}
	if !strings.Contains(err.Error(), "no society") {
		t.Errorf("the refusal does not say what is wrong: %v", err)
	}
	// And it says how much milk is affected, so somebody can go and fix it
	// rather than hunting.
	if !strings.Contains(err.Error(), "3 collections") {
		t.Errorf("the refusal does not say how many collections are affected: %v", err)
	}
}

// The constraint that stops one collection being paid for twice, tested where it
// is actually reachable.
//
// It is not reachable through the service: a second cycle over the same period
// is refused by the exclusion constraint, and re-gathering the same cycle is
// refused by its status. Both of those fire first, which is the right order —
// but it means an end-to-end test of the API cannot demonstrate this index does
// anything, and a test that appeared to would be a test proving something else.
//
// The index exists for the writer that is not this service: an import script, a
// repair somebody ran at eleven at night. So it is exercised the way that writer
// would reach it, straight at the table.
func TestTheDatabaseRefusesOneCollectionInTwoCycles(t *testing.T) {
	p := startPlatform(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn(t, "e2e_settlement"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Two cycles that do not overlap, so both are legitimate.
	first := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	second := mustOpenCycle(t, p, "March 16-31", "2026-03-16", "2026-03-31", "CAP_AT_EARNINGS")

	collection := newID("col")
	line := func(cycleID string) error {
		_, err := conn.Exec(ctx, `
			INSERT INTO settlement_service.cycle_lines
				(id,tenant_id,cycle_id,producer_ref,collection_id,collected_on,shift,
				 quantity,quantity_unit,currency,amount_scale,amount_minor_units)
			VALUES ($1,$2,$3,$4,$5,'2026-03-02','MORNING','10.000','PER_LITRE','INR',2,43000)`,
			newID("line"), p.tenant, cycleID, newID("prod"), collection)
		return err
	}

	if err := line(first.ID); err != nil {
		t.Fatalf("the first cycle could not take the collection: %v", err)
	}
	if err := line(second.ID); err == nil {
		t.Fatal("one collection was gathered into two cycles, so a producer would be " +
			"paid twice for the same milk")
	} else if !strings.Contains(err.Error(), "cycle_lines_one_cycle_per_collection") {
		t.Errorf("the refusal came from somewhere unexpected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The printed statement
// ---------------------------------------------------------------------------

type printStatementReq struct {
	TenantID    string            `json:"tenant_id"`
	CycleID     string            `json:"cycle_id"`
	ProducerRef string            `json:"producer_ref"`
	Width       int32             `json:"width,omitempty"`
	SocietyName string            `json:"society_name,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type printStatementResp struct {
	ProducerRef string `json:"producer_ref"`
	Page        string `json:"page"`
	Width       int32  `json:"width"`
}

type printCycleReq struct {
	TenantID    string `json:"tenant_id"`
	CycleID     string `json:"cycle_id"`
	Width       int32  `json:"width,omitempty"`
	SocietyName string `json:"society_name,omitempty"`
}

type printCycleResp struct {
	Statements []printStatementResp `json:"statements"`
	Width      int32                `json:"width"`
}

func printStatement(t *testing.T, p *platform, in printStatementReq) (*printStatementResp, error) {
	t.Helper()
	in.TenantID = p.tenant
	return svcclient.Call[printStatementReq, printStatementResp](
		context.Background(), p.settlement(), settlementSvc+"/PrintProducerStatement", in, p.opts())
}

// The page a member is handed, printed from a fortnight that actually went
// through the platform.
//
// The renderer has its own tests against a statement built in memory. This one
// exists because those cannot show that the figures reaching the page are the
// figures the settlement computed — a renderer that laid out beautiful pages of
// somebody else's numbers would pass every one of them.
func TestTheFortnightComesOutOfThePrinterAsAPageAMemberCanCheck(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	if _, err := openRecovery(t, p, openRecoveryReq{
		ProducerRef: producer, Kind: "ADVANCE", Reference: "shed",
		Principal: "5000.00", Instalment: "1000.00", Priority: 1, OpenedOn: "2026-01-10",
	}); err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	out, err := printStatement(t, p, printStatementReq{
		CycleID: cycle.ID, ProducerRef: producer, Width: 80,
		SocietyName: "Kothapalli Milk Producers Co-operative Society",
	})
	if err != nil {
		t.Fatalf("PrintProducerStatement: %v", err)
	}
	page := out.Page
	t.Logf("the page a member is handed:\n%s", page)

	// The figures on the page are the figures the settlement reached.
	pay := payableFor(t, p, cycle.ID, producer)
	for _, want := range []string{pay.Gross, pay.Net, "-" + pay.Deducted} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not carry %s:\n%s", want, page)
		}
	}
	// Ten deliveries, each on the page.
	if n := strings.Count(page, "430.00"); n < 10 {
		t.Errorf("the page shows %d deliveries of 430.00, want at least 10:\n%s", n, page)
	}
	if !strings.Contains(page, "Total litres") || !strings.Contains(page, "100.000") {
		t.Errorf("the page does not total the milk:\n%s", page)
	}
	// And the advance is shown as money taken back, not as a mystery.
	if !strings.Contains(page, "Advance (shed)") {
		t.Errorf("the page does not say what the deduction was for:\n%s", page)
	}

	// Every line fits the printer.
	for i, line := range strings.Split(strings.TrimRight(page, "\n"), "\n") {
		if n := len([]rune(line)); n > 80 {
			t.Errorf("line %d is %d columns and would wrap: %q", i+1, n, line)
		}
	}

	// The deliveries on the page add up to the gross on the page. This is the
	// arithmetic a member does with their own slips, done here.
	var lines int64
	for _, line := range strings.Split(page, "\n") {
		if !strings.Contains(line, "Mar 2026") || !strings.Contains(line, "Morning") {
			continue
		}
		fields := strings.Fields(line)
		lines += mustMinorUnits(t, fields[len(fields)-1])
	}
	if lines != mustMinorUnits(t, pay.Gross) {
		t.Errorf("the deliveries printed on the page come to %d, and the page reads a gross of %s",
			lines, pay.Gross)
	}
}

// A printer too narrow for the figures is refused, and the refusal says how
// wide the page has to be.
//
// The alternative is a statement with a digit missing, which is not a printing
// fault a clerk notices — it is a plausible number on the one piece of paper a
// member keeps.
func TestAPrinterTooNarrowForTheFiguresIsRefused(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	_, err := printStatement(t, p, printStatementReq{
		CycleID: cycle.ID, ProducerRef: producer, Width: 34,
	})
	if err == nil {
		t.Fatal("a statement was laid out on a 34-column page")
	}
	if !strings.Contains(err.Error(), "columns") {
		t.Errorf("the refusal does not say the page is too narrow: %v", err)
	}
}

// A society's own words reach the page, and the English defaults do not sit
// beside them.
func TestASocietyPrintsItsOwnWords(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	out, err := printStatement(t, p, printStatementReq{
		CycleID: cycle.ID, ProducerRef: producer, Width: 80,
		Labels: map[string]string{"net": "CHELLINCHAVALASINA MOTTAM", "member": "Sabhyudu"},
	})
	if err != nil {
		t.Fatalf("PrintProducerStatement: %v", err)
	}
	if !strings.Contains(out.Page, "CHELLINCHAVALASINA MOTTAM") || !strings.Contains(out.Page, "Sabhyudu") {
		t.Errorf("the society's own words are not on the page:\n%s", out.Page)
	}
	if strings.Contains(out.Page, "NET PAYABLE") {
		t.Errorf("the English default was printed as well:\n%s", out.Page)
	}
	// The words it did not override keep the defaults rather than going blank.
	if !strings.Contains(out.Page, "Society") || !strings.Contains(out.Page, "Gross") {
		t.Errorf("overriding two labels blanked the others:\n%s", out.Page)
	}
}

// The whole fortnight printed in one run, which is what a society does.
//
// The stack has to be all or nothing. A run that produced pages for the first
// hundred and ninety-nine members and failed on the two hundredth leaves those
// hundred and ninety-nine believing the settlement is done, and the last one
// with nothing to compare against.
func TestACycleIsPrintedAsOneStackOrNotAtAll(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producers := []string{newID("prod"), newID("prod"), newID("prod")}
	for _, ref := range producers {
		deliverFortnight(t, p, ref, marchFirstTen[:4])
	}
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	stack, err := svcclient.Call[printCycleReq, printCycleResp](
		context.Background(), p.settlement(), settlementSvc+"/PrintCycleStatements",
		printCycleReq{TenantID: p.tenant, CycleID: cycle.ID, Width: 80,
			SocietyName: "Kothapalli Milk Producers"}, p.opts())
	if err != nil {
		t.Fatalf("PrintCycleStatements: %v", err)
	}
	if len(stack.Statements) != len(producers) {
		t.Fatalf("the stack has %d pages for %d members", len(stack.Statements), len(producers))
	}
	seen := map[string]bool{}
	for _, s := range stack.Statements {
		seen[s.ProducerRef] = true
		if !strings.Contains(s.Page, "MILK PAYMENT STATEMENT") {
			t.Errorf("%s got a page that is not a statement:\n%s", s.ProducerRef, s.Page)
		}
		// Four mornings of ten litres at 43.00.
		if !strings.Contains(s.Page, "1720.00") {
			t.Errorf("%s: the page does not show the fortnight's 1720.00:\n%s", s.ProducerRef, s.Page)
		}
		// One member's page must not carry another member's reference.
		for _, other := range producers {
			if other != s.ProducerRef && strings.Contains(s.Page, other) {
				t.Errorf("%s's page carries %s's reference", s.ProducerRef, other)
			}
		}
	}
	for _, ref := range producers {
		if !seen[ref] {
			t.Errorf("no page was printed for %s", ref)
		}
	}

	// And a narrow printer fails the whole run rather than half of it.
	_, err = svcclient.Call[printCycleReq, printCycleResp](
		context.Background(), p.settlement(), settlementSvc+"/PrintCycleStatements",
		printCycleReq{TenantID: p.tenant, CycleID: cycle.ID, Width: 34}, p.opts())
	if err == nil {
		t.Error("a stack was printed on a page too narrow for the figures")
	}
}

// A cycle nobody has gathered has nothing to print, and says so rather than
// producing a stack of empty pages.
func TestACycleThatHasNotBeenGatheredPrintsNothing(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")
	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := printStatement(t, p, printStatementReq{
		CycleID: cycle.ID, ProducerRef: producer, Width: 80,
	}); err == nil {
		t.Error("a statement was printed for a cycle that has not been gathered")
	}

	if _, err := svcclient.Call[printCycleReq, printCycleResp](
		context.Background(), p.settlement(), settlementSvc+"/PrintCycleStatements",
		printCycleReq{TenantID: p.tenant, CycleID: cycle.ID, Width: 80}, p.opts()); err == nil {
		t.Error("a stack was printed for a cycle that has not been gathered")
	}
}

// ---------------------------------------------------------------------------
// Corrections
// ---------------------------------------------------------------------------

type correctCollectionReq struct {
	TenantID     string     `json:"tenant_id"`
	ID           string     `json:"id"`
	Quantity     pointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Fat          pointProto `json:"fat,omitempty"`
	SNF          pointProto `json:"snf,omitempty"`
	Reason       string     `json:"reason"`
	Actor        string     `json:"actor"`
}

type correctCollectionResp struct {
	Collection *pricedCollectionProto `json:"collection"`
	Supersedes string                 `json:"supersedes"`
}

type versionsReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type versionsResp struct {
	Versions []*pricedCollectionProto `json:"versions"`
}

type raiseAdjustmentReq struct {
	TenantID         string `json:"tenant_id"`
	CycleID          string `json:"cycle_id"`
	ProducerRef      string `json:"producer_ref"`
	Currency         string `json:"currency"`
	AmountScale      int32  `json:"amount_scale"`
	Amount           string `json:"amount"`
	AdjustsPayableID string `json:"adjusts_payable_id,omitempty"`
	Reason           string `json:"reason"`
	Actor            string `json:"actor"`
}

type getPayableReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type approvePayableReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Actor    string `json:"actor"`
}

func correctCollection(t *testing.T, p *platform, in correctCollectionReq) (*correctCollectionResp, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	return svcclient.Call[correctCollectionReq, correctCollectionResp](
		context.Background(), p.procurement(), procurementSvc+"/CorrectCollection", in, p.opts())
}

func raiseAdjustment(t *testing.T, p *platform, in raiseAdjustmentReq) (*payableResp, error) {
	t.Helper()
	in.TenantID = p.tenant
	in.Currency, in.AmountScale = "INR", 2
	if in.Actor == "" {
		in.Actor = "e2e_secretary"
	}
	return svcclient.Call[raiseAdjustmentReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/RaiseAdjustment", in, p.opts())
}

// A mis-entered reading is corrected, and both versions stay readable.
//
// This is the commonest data-entry error in a dairy: the analyser said 4.2 and
// the operator typed 4.1. Before there was a correction path the unique index
// made that permanent — the row could not be replaced and a second one could
// not be added — so a society would have kept its real ledger on paper, which
// is the failure this platform exists to end.
func TestAMisEnteredReadingIsCorrectedAndBothVersionsRemain(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	in := morning(producer, "2026-03-01", "10.000", "4.1", "8.6")
	in.SocietyCode = society
	wrong, err := collect(t, p, in)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	// fat 4.1 / SNF 8.6 reads 43.0000, so 10 litres is 430.00.
	if wrong.Amount != "430.00" {
		t.Fatalf("the original reads %s, want 430.00", wrong.Amount)
	}

	// Re-recording the same delivery is still refused: a correction is not a
	// second collection.
	if _, err := collect(t, p, in); err == nil {
		t.Error("the same producer, day and shift was recorded a second time")
	}

	corrected, err := correctCollection(t, p, correctCollectionReq{
		ID:       wrong.ID,
		Quantity: pointProto{Value: "10.000", Scale: 3}, QuantityUnit: "PER_LITRE",
		Fat: pointProto{Value: "4.2", Scale: 1}, SNF: pointProto{Value: "8.6", Scale: 1},
		Reason: "analyser slip read 4.2; entered as 4.1",
	})
	if err != nil {
		t.Fatalf("CorrectCollection: %v", err)
	}
	// fat 4.2 / SNF 8.6 reads 45.0000, so 450.00. Re-priced from the readings
	// rather than carrying the old rate forward.
	if corrected.Collection.Amount != "450.00" {
		t.Errorf("the correction reads %s, want 450.00", corrected.Collection.Amount)
	}
	if corrected.Supersedes != wrong.ID {
		t.Errorf("the correction supersedes %q, want %q", corrected.Supersedes, wrong.ID)
	}

	// Both versions are readable, and the history says why it changed.
	versions, err := svcclient.Call[versionsReq, versionsResp](
		context.Background(), p.procurement(), procurementSvc+"/GetCollectionVersions",
		versionsReq{TenantID: p.tenant, ID: wrong.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetCollectionVersions: %v", err)
	}
	if len(versions.Versions) != 2 {
		t.Fatalf("%d versions, want 2", len(versions.Versions))
	}
	if versions.Versions[0].Amount != "430.00" || versions.Versions[1].Amount != "450.00" {
		t.Errorf("the versions read %s then %s, want 430.00 then 450.00",
			versions.Versions[0].Amount, versions.Versions[1].Amount)
	}
	if versions.Versions[0].SupersededAt == "" {
		t.Error("the original does not record when it stopped being believed")
	}
	if !strings.Contains(versions.Versions[1].CorrectionReason, "analyser slip") {
		t.Errorf("the correction does not carry its reason: %q",
			versions.Versions[1].CorrectionReason)
	}
}

// A correction with no reason is refused, and so is one that changes nothing.
func TestACorrectionMustSayWhyAndMustChangeSomething(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	in := morning(producer, "2026-03-01", "10.000", "4.1", "8.6")
	in.SocietyCode = society
	original, err := collect(t, p, in)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	base := correctCollectionReq{
		ID:       original.ID,
		Quantity: pointProto{Value: "10.000", Scale: 3}, QuantityUnit: "PER_LITRE",
		Fat: pointProto{Value: "4.2", Scale: 1}, SNF: pointProto{Value: "8.6", Scale: 1},
	}

	noReason := base
	noReason.Reason = ""
	if _, err := correctCollection(t, p, noReason); err == nil {
		t.Error("a figure was restated with no reason recorded")
	}

	// The same readings back again restates nothing.
	same := base
	same.Fat = pointProto{Value: "4.1", Scale: 1}
	same.Reason = "no change"
	if _, err := correctCollection(t, p, same); err == nil {
		t.Error("a correction that changes no reading was accepted")
	} else if !strings.Contains(err.Error(), "does not change") {
		t.Errorf("the refusal does not say the correction changes nothing: %v", err)
	}

	// The real one lands, and correcting the superseded version afterwards is
	// refused: a chain is corrected at its head.
	good := base
	good.Reason = "analyser slip read 4.2"
	if _, err := correctCollection(t, p, good); err != nil {
		t.Fatalf("CorrectCollection: %v", err)
	}
	// Correcting the superseded version is refused, and the refusal has to say
	// so. Three things stop this: the service checks before pricing, the
	// repository checks again under a row lock, and the live-version index
	// refuses the insert because the correction already there is live.
	//
	// The index alone is enough to keep the data right, and asserting only that
	// something failed would pass with both checks removed — it did. What the
	// checks buy is a refusal that names the cause instead of a duplicate-key
	// error naming an index, so that is what is asserted.
	_, err = correctCollection(t, p, correctCollectionReq{
		ID:       original.ID,
		Quantity: pointProto{Value: "11.000", Scale: 3}, QuantityUnit: "PER_LITRE",
		Fat: pointProto{Value: "4.0", Scale: 1}, SNF: pointProto{Value: "8.5", Scale: 1},
		Reason: "correcting the old version",
	})
	if err == nil {
		t.Fatal("a superseded version was corrected, leaving two live corrections of one delivery")
	}
	if !strings.Contains(err.Error(), "already been corrected") {
		t.Errorf("the refusal does not say the version has already been corrected: %v", err)
	}
}

// A settlement gathers the corrected version, once.
//
// The hazard is the corrected collection arriving alongside the version it
// corrected: the producer would be paid for the same milk twice, at the wrong
// figure and then at the right one, and both lines would look entirely ordinary
// on the statement.
func TestASettlementGathersTheCorrectedVersionAndOnlyThat(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	in := morning(producer, "2026-03-01", "10.000", "4.1", "8.6")
	in.SocietyCode = society
	original, err := collect(t, p, in)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if _, err := correctCollection(t, p, correctCollectionReq{
		ID:       original.ID,
		Quantity: pointProto{Value: "10.000", Scale: 3}, QuantityUnit: "PER_LITRE",
		Fat: pointProto{Value: "4.2", Scale: 1}, SNF: pointProto{Value: "8.6", Scale: 1},
		Reason: "analyser slip read 4.2",
	}); err != nil {
		t.Fatalf("CorrectCollection: %v", err)
	}

	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	pay := payableFor(t, p, cycle.ID, producer)
	if pay.Gross != "450.00" {
		t.Errorf("gross %s, want the corrected 450.00 — 880.00 would mean both versions "+
			"were gathered and the producer paid twice for one delivery", pay.Gross)
	}
	s := statement(t, p, cycle.ID, producer)
	if len(s.Lines) != 1 {
		t.Errorf("the statement shows %d deliveries for one morning's milk", len(s.Lines))
	}
}

// The remedy the paid-is-final trigger names.
//
// A fortnight is settled and paid. Then a reading is found to be wrong. The
// payment cannot be edited — the trigger refuses, and it says a payment that
// turned out to be wrong is corrected by a further payment. This is that
// further payment, and until it existed the refusal named a remedy the platform
// did not have.
func TestAWrongPaymentIsCorrectedByAFurtherPaymentRatherThanAnEdit(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen)
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	if _, err := approve(t, p, cycle.ID); err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}
	pay := payableFor(t, p, cycle.ID, producer)
	if _, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: pay.ID, Actor: "e2e_cashier"}, p.opts()); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if pay.Net != "4300.00" {
		t.Fatalf("the fortnight paid %s, want 4300.00", pay.Net)
	}

	adj, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer, Amount: "20.00",
		AdjustsPayableID: pay.ID,
		Reason:           "fat on 3 March restated from 4.1 to 4.2 after the payment",
	})
	if err != nil {
		t.Fatalf("RaiseAdjustment: %v", err)
	}
	if adj.Payable.Kind != "ADJUSTMENT" || adj.Payable.Status != "PAYABLE" {
		t.Errorf("the adjustment is %s / %s", adj.Payable.Kind, adj.Payable.Status)
	}

	// It goes through the same approval and payment as anything else that moves
	// money to a producer.
	if _, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: adj.Payable.ID, Actor: "e2e_cashier"}, p.opts()); err == nil {
		t.Error("an adjustment nobody had approved was paid")
	}
	if _, err := svcclient.Call[approvePayableReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/ApprovePayable",
		approvePayableReq{TenantID: p.tenant, ID: adj.Payable.ID, Actor: "e2e_secretary"},
		p.opts()); err != nil {
		t.Fatalf("ApprovePayable: %v", err)
	}
	paid, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: adj.Payable.ID, PaymentReference: "CASH", Actor: "e2e_cashier"},
		p.opts())
	if err != nil {
		t.Fatalf("MarkPaid on the adjustment: %v", err)
	}
	if paid.Payable.Status != "PAID" {
		t.Errorf("the adjustment is %s after payment", paid.Payable.Status)
	}

	// The original payment is untouched, which is the whole point.
	after := payableFor(t, p, cycle.ID, producer)
	if after.Net != "4300.00" || after.Status != "PAID" {
		t.Errorf("the original payment changed: %s / %s", after.Net, after.Status)
	}

	// And the member's statement shows both events, not their sum.
	s := statement(t, p, cycle.ID, producer)
	if s.Net != "4300.00" {
		t.Errorf("the statement's net is %s; the adjustment was folded into what the member "+
			"was handed at the window", s.Net)
	}
	page, err := printStatement(t, p, printStatementReq{
		CycleID: cycle.ID, ProducerRef: producer, Width: 80,
	})
	if err != nil {
		t.Fatalf("PrintProducerStatement: %v", err)
	}
	t.Logf("the corrected statement:\n%s", page.Page)
	if !strings.Contains(page.Page, "20.00") || !strings.Contains(page.Page, "restated from 4.1") {
		t.Errorf("the printed statement does not show the correction:\n%s", page.Page)
	}
	if !strings.Contains(page.Page, "4300.00") {
		t.Errorf("the printed statement lost the fortnight's own net:\n%s", page.Page)
	}
}

// An overpayment is recovered by a negative adjustment.
//
// Corrections run both ways: a reading restated downwards means the producer
// was paid too much. A schema that refused a negative amount here — which is
// what "a gross is never below zero" looked like at first — makes half of what
// adjustments are for impossible.
func TestAnOverpaymentIsRecoveredByANegativeAdjustment(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen[:2])
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	adj, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer, Amount: "-40.00",
		Reason: "fat restated 4.2 to 4.1 on both mornings; overpaid",
	})
	if err != nil {
		t.Fatalf("RaiseAdjustment: %v", err)
	}
	if adj.Payable.Net != "-40.00" {
		t.Errorf("the adjustment reads %s, want -40.00", adj.Payable.Net)
	}

	// It appears on the statement as a subtraction with its reason.
	page, err := printStatement(t, p, printStatementReq{
		CycleID: cycle.ID, ProducerRef: producer, Width: 80,
	})
	if err != nil {
		t.Fatalf("PrintProducerStatement: %v", err)
	}
	if !strings.Contains(page.Page, "-40.00") || !strings.Contains(page.Page, "overpaid") {
		t.Errorf("the page does not show the recovery:\n%s", page.Page)
	}
}

// An adjustment must say why, must move some money, and must belong to the
// producer whose payment it names.
func TestAnAdjustmentIsRefusedWithoutAReasonAnAmountOrTheRightProducer(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer, other := newID("prod"), newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen[:2])
	deliverFortnight(t, p, other, marchFirstTen[:2])
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	theirs := payableFor(t, p, cycle.ID, other)

	if _, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer, Amount: "10.00", Reason: "",
	}); err == nil {
		t.Error("money moved to a producer with no reason recorded")
	}
	if _, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer, Amount: "0.00", Reason: "nothing",
	}); err == nil {
		t.Error("an adjustment of zero was raised")
	}
	// Naming another member's payment would be moving money between members
	// with a field marked "reason".
	if _, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer, Amount: "10.00",
		AdjustsPayableID: theirs.ID, Reason: "correcting somebody else's payment",
	}); err == nil {
		t.Error("an adjustment for one member named another member's payment")
	}
}

// Approving a fortnight approves the fortnight, not the corrections raised
// beside it.
//
// A correction is a separate decision about a separate movement of money, often
// made by a different person for a different reason. Sweeping it up in the bulk
// approval of the period it corrects means nobody ever looked at it — the
// signature on the cycle was for the figures the gathering produced, and the
// adjustment was not among them.
func TestApprovingACycleDoesNotApproveTheCorrectionsBesideIt(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen[:3])
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}

	// Raised while the cycle is still GATHERED, so the bulk approval that
	// follows would reach it.
	adj, err := raiseAdjustment(t, p, raiseAdjustmentReq{
		CycleID: cycle.ID, ProducerRef: producer, Amount: "15.00",
		Reason: "a reading queried by the member and not yet resolved",
	})
	if err != nil {
		t.Fatalf("RaiseAdjustment: %v", err)
	}

	if _, err := approve(t, p, cycle.ID); err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}

	// The fortnight is approved.
	if got := payableFor(t, p, cycle.ID, producer).Status; got != "APPROVED" {
		t.Errorf("the fortnight's payable is %s after approval, want APPROVED", got)
	}

	// The correction is not.
	after, err := svcclient.Call[getPayableReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/GetPayable",
		getPayableReq{TenantID: p.tenant, ID: adj.Payable.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetPayable: %v", err)
	}
	if after.Payable.Status != "PAYABLE" {
		t.Errorf("the correction is %s after a bulk approval of the period it corrects; "+
			"nobody approved it on its own", after.Payable.Status)
	}
	// And it therefore cannot be paid until somebody does.
	if _, err := svcclient.Call[markPaidReq, payableResp](
		context.Background(), p.settlement(), settlementSvc+"/MarkPaid",
		markPaidReq{TenantID: p.tenant, ID: adj.Payable.ID, Actor: "e2e_cashier"}, p.opts()); err == nil {
		t.Error("a correction nobody approved was paid")
	}
}
