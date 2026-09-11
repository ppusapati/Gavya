//go:build e2e

// Milk to money, the whole way.
//
// Every part of this path was unit-tested and none of it had been joined up:
// pooling-service was not in this harness at all, so the step where a collection
// becomes an amount a producer is actually paid had never run end to end.
//
// The claim being tested is narrow and it is the one the business rests on. Two
// producers put milk into a pool. The pool is valued at a component price. Each
// producer's share is allocated. The pool is settled and each share becomes an
// economic event — the representation of what they are owed. Then something is
// found to be wrong, a correction is applied, and the recovery appears as a
// further event rather than by editing the first one.
//
// Two properties are asserted throughout, because they are what makes the number
// defensible rather than merely present:
//
//   - Every amount is exact. The wire carries decimal literals and minor units,
//     never a float, so nothing anywhere in this path can round in a way nobody
//     declared.
//   - The allocations sum to the pool. A distribution that loses a paisa to
//     rounding is a distribution somebody is short, and at scale it is the same
//     paisa every time.
package e2e

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const poolingSvc = "pooling.v1.PoolingService"

type componentPriceProto struct {
	Component string `json:"component"`
	Price     string `json:"price"`
	Scale     int32  `json:"scale"`
}

type createPoolReq struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit,omitempty"`
	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	Actor       string `json:"actor"`
}

type poolProto struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
}

type createPoolResp struct {
	Pool *poolProto `json:"pool"`
}

type addMilkReq struct {
	TenantID    string            `json:"tenant_id"`
	PoolID      string            `json:"pool_id"`
	ProducerRef string            `json:"producer_ref"`
	Quantity    string            `json:"quantity"`
	Components  map[string]string `json:"components,omitempty"`
	OriginKind  string            `json:"origin_kind,omitempty"`
	Actor       string            `json:"actor"`
}

type addMilkResp struct {
	Pool *poolProto `json:"pool"`
}

type allocationProto struct {
	ID          string `json:"id"`
	ProducerRef string `json:"producer_ref"`
	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	Total       string `json:"total"`
	Weight      int64  `json:"weight"`
}

type valuePoolReq struct {
	TenantID        string                `json:"tenant_id"`
	PoolID          string                `json:"pool_id"`
	ComponentPrices []componentPriceProto `json:"component_prices"`
	Actor           string                `json:"actor"`
}

// The valuation as this file needs it. It carries no total_value: the handler
// sends classified_value, component_value and producer_settlement_fund, and a
// field named for something that is never on the wire reads "" forever —
// including in the assertion somebody eventually writes against it.
// pooling_test.go's fullValuationProto names the real ones.
type valuationProto struct {
	ID string `json:"id"`
}

type valuePoolResp struct {
	Pool        *poolProto        `json:"pool"`
	Valuation   *valuationProto   `json:"valuation"`
	Allocations []allocationProto `json:"allocations"`
}

type economicEventProto struct {
	ID                string `json:"id"`
	PoolID            string `json:"pool_id"`
	AllocationID      string `json:"allocation_id"`
	ProducerRef       string `json:"producer_ref"`
	Currency          string `json:"currency"`
	AmountScale       int32  `json:"amount_scale"`
	Amount            string `json:"amount"`
	Kind              string `json:"kind"`
	SupersedesEventID string `json:"supersedes_event_id,omitempty"`
}

type settlePoolReq struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
	Actor    string `json:"actor"`
}

type settlePoolResp struct {
	Pool   *poolProto           `json:"pool"`
	Events []economicEventProto `json:"events"`
}

type listEventsReq struct {
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id,omitempty"`
	ProducerRef string `json:"producer_ref,omitempty"`
	Limit       int32  `json:"limit,omitempty"`
}

type listEventsResp struct {
	Events []economicEventProto `json:"events"`
}

type declareRetroPolicyReq struct {
	TenantID          string `json:"tenant_id"`
	Name              string `json:"name"`
	Mode              string `json:"mode"`
	MaxLookbackDays   int32  `json:"max_lookback_days"`
	Currency          string `json:"currency"`
	AmountScale       int32  `json:"amount_scale"`
	MinimumAdjustment string `json:"minimum_adjustment,omitempty"`
	EffectiveFrom     string `json:"effective_from,omitempty"`
	EffectiveTo       string `json:"effective_to,omitempty"`
	Actor             string `json:"actor"`
}

type declareRetroPolicyResp struct {
	Policy map[string]any `json:"policy"`
}

type applyCorrectionReq struct {
	TenantID        string                `json:"tenant_id"`
	PoolID          string                `json:"pool_id"`
	ComponentPrices []componentPriceProto `json:"component_prices"`
	At              string                `json:"at,omitempty"`
	Actor           string                `json:"actor"`
}

type applyCorrectionResp struct {
	Outcome    string               `json:"outcome"`
	Reason     string               `json:"reason"`
	EventKind  string               `json:"event_kind,omitempty"`
	Adjustment string               `json:"adjustment"`
	Pool       *poolProto           `json:"pool"`
	Events     []economicEventProto `json:"events"`
}

type recordUtilisationReq struct {
	TenantID   string `json:"tenant_id"`
	PoolID     string `json:"pool_id"`
	Class      string `json:"class"`
	Quantity   string `json:"quantity"`
	Price      string `json:"price"`
	PriceScale int32  `json:"price_scale"`
	Actor      string `json:"actor"`
}

type recordUtilisationResp struct {
	Utilisation map[string]any `json:"utilisation"`
}

// valuedPool builds a pool of two producers' milk, records what it was used
// for, and values it.
//
// The arithmetic is chosen so it can be checked by hand, because a test whose
// expected values came out of the code it is testing proves only that the code
// agrees with itself:
//
//	Alpha-1  300.000 L carrying 12.000 kg of fat
//	Alpha-2  200.000 L carrying  8.000 kg of fat
//
//	400 of the 500 litres are classified: Class I at 3.5001  ->  1400.04
//	Fat is paid directly at 20.0000 a kilogram               ->   400.00
//	                                                             (240.00 and 160.00)
//
//	The fund is what the milk earned beyond what was paid for its components:
//	1400.04 - 400.00 = 1000.04, shared by quantity 300:200
//
//	The class price is 3.5001 rather than a round 3.5000 on purpose. At 3.5000
//	the fund is 1000.00 and splits 3:2 exactly, so nothing is left over — and a
//	test for "every paisa reaches a producer" run on figures where no paisa can
//	be lost proves nothing. An earlier version of this used the round price, and
//	replacing the allocation with independent pro-rata truncation still passed.
//
//	At 3.5001 the fund is 100004 paise and the exact shares are 60002.4 and
//	40001.6. Both are floored and the one paisa left over goes to the largest
//	weight, which is the rule money.Allocate documents: deterministic and
//	replayable, so a recomputed settlement lands on the figures that were paid.
//
//	It is worth being clear about what that rule costs. The residual always goes
//	to the largest contributor, so over a year of pools the same producer
//	collects every stray paisa. That is a policy — a defensible one, since the
//	alternative of rotating it makes a settlement irreproducible — and it should
//	be a policy somebody chose rather than an accident of iteration order.
//
//	So Alpha-1 is owed 240.00 + 600.03 = 840.03
//	   Alpha-2 is owed 160.00 + 400.01 = 560.01
//	                                     ------
//	                                     1400.04, the pool as valued
//
// The remaining hundred litres are deliberately left unclassified. That is not a
// contrivance: a tanker whose paperwork arrives a week after the pool was
// settled is the ordinary case the correction machinery exists for, and it is
// what TestACorrectionIsAFurtherEventAndNotAnEdit picks up.
func valuedPool(t *testing.T, p *platform) (poolID string, alloc []allocationProto) {
	t.Helper()
	ctx := context.Background()

	// The period ends a few days ago rather than on a fixed date, because the
	// retroactivity policy measures a correction's lateness from when the pool
	// closed. A hardcoded 2026 date passes today and starts failing once it is
	// more than a lookback old — the kind of test that goes red one morning for
	// reasons nobody can find.
	end := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	start := end.AddDate(0, -1, 0)

	pool, err := svcclient.Call[createPoolReq, createPoolResp](ctx, p.pooling(),
		poolingSvc+"/CreatePool", createPoolReq{
			TenantID: p.tenant, Name: "monthly evening pool", Unit: "L",
			PeriodStart: start.Format(time.RFC3339), PeriodEnd: end.Format(time.RFC3339),
			Currency: "INR", AmountScale: 2, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	for _, m := range []struct{ producer, quantity, fat string }{
		{"producer:alpha-1", "300.000", "12.000"},
		{"producer:alpha-2", "200.000", "8.000"},
	} {
		if _, err := svcclient.Call[addMilkReq, addMilkResp](ctx, p.pooling(),
			poolingSvc+"/AddProducerMilk", addMilkReq{
				TenantID: p.tenant, PoolID: pool.Pool.ID, ProducerRef: m.producer,
				Quantity: m.quantity, Components: map[string]string{"FAT": m.fat},
				OriginKind: "NATIVE", Actor: "e2e",
			}, p.opts()); err != nil {
			t.Fatalf("AddProducerMilk %s: %v", m.producer, err)
		}
	}

	// What the milk was used for. A pool cannot be valued without this: the
	// value of pooled milk is what it was turned into, not a number somebody
	// decided in advance.
	if _, err := svcclient.Call[recordUtilisationReq, recordUtilisationResp](ctx, p.pooling(),
		poolingSvc+"/RecordUtilisation", recordUtilisationReq{
			TenantID: p.tenant, PoolID: pool.Pool.ID, Class: "CLASS_I",
			Quantity: "400.000", Price: "3.5001", PriceScale: 4, Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("RecordUtilisation: %v", err)
	}

	valued, err := svcclient.Call[valuePoolReq, valuePoolResp](ctx, p.pooling(),
		poolingSvc+"/ValuePool", valuePoolReq{
			TenantID: p.tenant, PoolID: pool.Pool.ID,
			ComponentPrices: []componentPriceProto{{Component: "FAT", Price: "20.0000", Scale: 4}},
			Actor:           "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("ValuePool: %v", err)
	}
	return pool.Pool.ID, valued.Allocations
}

// minor parses a decimal literal into minor units at a scale, exactly.
//
// big.Rat rather than a float, for the same reason the wire carries strings: a
// test that checks exact money with floating point cannot distinguish the answer
// it wants from one a paisa away.
func minor(t *testing.T, literal string, scale int) int64 {
	t.Helper()
	r, ok := new(big.Rat).SetString(literal)
	if !ok {
		t.Fatalf("%q is not a decimal literal", literal)
	}
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	r.Mul(r, new(big.Rat).SetInt(pow))
	if !r.IsInt() {
		t.Fatalf("%q does not fit at %d decimal places, so a test comparing it would be "+
			"comparing something already rounded", literal, scale)
	}
	return r.Num().Int64()
}

// The pool is distributed and nothing is lost. A distribution short by a paisa
// is a producer short by a paisa, and at scale it is the same producer every
// time.
func TestEveryPaisaOfThePoolReachesAProducer(t *testing.T) {
	p := startPlatform(t)
	_, allocations := valuedPool(t, p)

	if len(allocations) != 2 {
		t.Fatalf("%d allocations, want one per producer", len(allocations))
	}

	var sum int64
	for _, a := range allocations {
		if a.Currency != "INR" || a.AmountScale != 2 {
			t.Errorf("%s allocated in %s at scale %d", a.ProducerRef, a.Currency, a.AmountScale)
		}
		sum += minor(t, a.Total, 2)
	}
	if want := minor(t, "1400.04", 2); sum != want {
		t.Errorf("the allocations sum to %d minor units and the pool is worth %d; %d has gone "+
			"missing between them", sum, want, want-sum)
	}
}

// And each producer gets their own share, not an average. 300 litres and 200
// litres at the same price are 1050.00 and 700.00, worked out by hand.
func TestEachProducerIsAllocatedTheirOwnMilk(t *testing.T) {
	p := startPlatform(t)
	_, allocations := valuedPool(t, p)

	got := map[string]string{}
	for _, a := range allocations {
		got[a.ProducerRef] = a.Total
	}
	for producer, want := range map[string]string{
		"producer:alpha-1": "840.03",
		"producer:alpha-2": "560.01",
	} {
		if minor(t, got[producer], 2) != minor(t, want, 2) {
			t.Errorf("%s is allocated %s, want %s", producer, got[producer], want)
		}
	}
}

// Settling turns each allocation into the representation of what a producer is
// owed. This is the end of the path: everything upstream exists to make this
// number defensible.
func TestSettlingAPoolProducesWhatEachProducerIsOwed(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()
	poolID, allocations := valuedPool(t, p)

	settled, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{TenantID: p.tenant, PoolID: poolID, Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("SettlePool: %v", err)
	}
	if len(settled.Events) != len(allocations) {
		t.Fatalf("%d events for %d allocations", len(settled.Events), len(allocations))
	}

	var sum int64
	for _, e := range settled.Events {
		if e.Kind != "ORIGINAL" {
			t.Errorf("%s got a %s event on first settlement", e.ProducerRef, e.Kind)
		}
		if e.SupersedesEventID != "" {
			t.Errorf("%s's first event supersedes %s", e.ProducerRef, e.SupersedesEventID)
		}
		if e.Currency != "INR" || e.AmountScale != 2 {
			t.Errorf("%s is owed %s at scale %d", e.ProducerRef, e.Currency, e.AmountScale)
		}
		sum += minor(t, e.Amount, 2)
	}
	if want := minor(t, "1400.04", 2); sum != want {
		t.Errorf("the events total %d minor units, want the pool's %d", sum, want)
	}
}

// The recovery half. A price is found to have been wrong; the correction becomes
// a further event rather than an edit to the first one, so what was paid and
// what is now owed are both readable.
func TestACorrectionIsAFurtherEventAndNotAnEdit(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// A policy has to exist first. Without one the platform refuses to apply a
	// correction at all, which is the right default: whether a late price change
	// reaches producers who have already been paid is a decision somebody makes,
	// not one a system makes for them.
	if _, err := svcclient.Call[declareRetroPolicyReq, declareRetroPolicyResp](ctx, p.pooling(),
		poolingSvc+"/DeclareRetroactivityPolicy", declareRetroPolicyReq{
			TenantID: p.tenant, Name: "february", Mode: "APPLY_INCREMENTAL",
			MaxLookbackDays: 90, Currency: "INR", AmountScale: 2,
			MinimumAdjustment: "1.00",
			// Required, and rightly: a policy with no start has no answer to
			// "did this apply when that pool was settled".
			EffectiveFrom: time.Now().UTC().AddDate(-1, 0, 0).Format(time.RFC3339),
			Actor:         "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("DeclareRetroactivityPolicy: %v", err)
	}

	poolID, _ := valuedPool(t, p)
	settled, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{TenantID: p.tenant, PoolID: poolID, Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("SettlePool: %v", err)
	}
	originals := map[string]string{}
	for _, e := range settled.Events {
		originals[e.ProducerRef] = e.Amount
	}

	// The tanker's paperwork arrives: the last hundred litres went to Class II at
	// 4.0000, which is 400.00 the pool earned and has not yet distributed.
	if _, err := svcclient.Call[recordUtilisationReq, recordUtilisationResp](ctx, p.pooling(),
		poolingSvc+"/RecordUtilisation", recordUtilisationReq{
			TenantID: p.tenant, PoolID: poolID, Class: "CLASS_II",
			Quantity: "100.000", Price: "4.0000", PriceScale: 4, Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("RecordUtilisation (late report): %v", err)
	}

	corrected, err := svcclient.Call[applyCorrectionReq, applyCorrectionResp](ctx, p.pooling(),
		poolingSvc+"/ApplyCorrection", applyCorrectionReq{
			TenantID: p.tenant, PoolID: poolID,
			ComponentPrices: []componentPriceProto{{Component: "FAT", Price: "20.0000", Scale: 4}},
			Actor:           "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	// ADJUST: the difference is raised against what was already settled, rather
	// than the settled amount being rewritten. That is the whole distinction —
	// a producer's statement shows what they were paid and what they are owed
	// on top, not a number that changed behind them.
	if corrected.Outcome != "ADJUST" {
		t.Fatalf("outcome = %q (%s), want ADJUST", corrected.Outcome, corrected.Reason)
	}
	if minor(t, corrected.Adjustment, 2) != minor(t, "400.00", 2) {
		t.Errorf("the adjustment is %s, want 400.00 — a hundred litres to Class II at 4.0000",
			corrected.Adjustment)
	}

	// The originals are still there and still say what was paid.
	all, err := svcclient.Call[listEventsReq, listEventsResp](ctx, p.pooling(),
		poolingSvc+"/ListEconomicEvents", listEventsReq{
			TenantID: p.tenant, PoolID: poolID, Limit: 50,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListEconomicEvents: %v", err)
	}

	var originalCount, correctionCount int
	owed := map[string]int64{}
	for _, e := range all.Events {
		owed[e.ProducerRef] += minor(t, e.Amount, 2)
		switch e.Kind {
		case "ORIGINAL":
			originalCount++
			if got := originals[e.ProducerRef]; got != "" && minor(t, e.Amount, 2) != minor(t, got, 2) {
				t.Errorf("%s's original event now reads %s, was %s — it was edited rather than "+
					"corrected", e.ProducerRef, e.Amount, got)
			}
		default:
			correctionCount++
		}
	}
	if originalCount != 2 {
		t.Errorf("%d original events survive, want both", originalCount)
	}
	if correctionCount == 0 {
		t.Error("the correction produced no event of its own, so the recovery is invisible")
	}

	// And the totals are what a producer would actually be paid once the late
	// report is in: the fund grows by 400.00 and is shared 300:200, so 240.00
	// and 160.00 on top of what was already settled.
	for producer, want := range map[string]string{
		"producer:alpha-1": "1080.03",
		"producer:alpha-2": "720.01",
	} {
		if owed[producer] != minor(t, want, 2) {
			t.Errorf("%s is owed %d minor units in total, want %s", producer, owed[producer], want)
		}
	}
}

// A correction below what the tenant said is worth chasing is not applied. The
// threshold is a declaration rather than a constant, because whether two rupees
// is worth reopening a settlement depends on the society, and a system that
// picks for them has made a decision nobody can point at.
func TestACorrectionBelowTheDeclaredThresholdIsNotApplied(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	if _, err := svcclient.Call[declareRetroPolicyReq, declareRetroPolicyResp](ctx, p.pooling(),
		poolingSvc+"/DeclareRetroactivityPolicy", declareRetroPolicyReq{
			TenantID: p.tenant, Name: "february", Mode: "APPLY_INCREMENTAL",
			MaxLookbackDays: 90, Currency: "INR", AmountScale: 2,
			MinimumAdjustment: "1000.00",
			EffectiveFrom:     time.Now().UTC().AddDate(-1, 0, 0).Format(time.RFC3339),
			Actor:             "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("DeclareRetroactivityPolicy: %v", err)
	}

	poolID, _ := valuedPool(t, p)
	if _, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{TenantID: p.tenant, PoolID: poolID, Actor: "e2e"}, p.opts()); err != nil {
		t.Fatalf("SettlePool: %v", err)
	}

	// The same late report, worth 400.00, against a declared floor of 1000.00.
	if _, err := svcclient.Call[recordUtilisationReq, recordUtilisationResp](ctx, p.pooling(),
		poolingSvc+"/RecordUtilisation", recordUtilisationReq{
			TenantID: p.tenant, PoolID: poolID, Class: "CLASS_II",
			Quantity: "100.000", Price: "4.0000", PriceScale: 4, Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatal(err)
	}

	corrected, err := svcclient.Call[applyCorrectionReq, applyCorrectionResp](ctx, p.pooling(),
		poolingSvc+"/ApplyCorrection", applyCorrectionReq{
			TenantID: p.tenant, PoolID: poolID,
			ComponentPrices: []componentPriceProto{{Component: "FAT", Price: "20.0000", Scale: 4}},
			Actor:           "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	if corrected.Outcome == "ADJUST" || corrected.Outcome == "RESTATE" {
		t.Errorf("a %s adjustment was applied under a 1000.00 threshold (outcome %s)",
			corrected.Adjustment, corrected.Outcome)
	}
	// And it still says what the adjustment would have been, so the decision not
	// to apply it is reviewable rather than invisible.
	if minor(t, corrected.Adjustment, 2) != minor(t, "400.00", 2) {
		t.Errorf("adjustment reported as %s, want the 400.00 that was declined", corrected.Adjustment)
	}
	if corrected.Reason == "" {
		t.Error("no reason was given for not applying it")
	}
}

// Nothing on this path may carry a float. A decimal literal on the wire and
// minor units in the store are what make the arithmetic reproducible; a float
// anywhere in the chain makes the total depend on the order things were added.
func TestNoAmountOnThisPathIsAFloat(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()
	poolID, allocations := valuedPool(t, p)

	settled, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{TenantID: p.tenant, PoolID: poolID, Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatal(err)
	}

	// Every amount that came back has to be exact at the pool's scale. A value
	// that has been through a float shows up here as one that will not parse
	// into minor units without a remainder.
	var checked int
	for _, a := range allocations {
		minor(t, a.Total, int(a.AmountScale))
		checked++
	}
	for _, e := range settled.Events {
		minor(t, e.Amount, int(e.AmountScale))
		if strings.ContainsAny(e.Amount, "eE") {
			t.Errorf("%s's amount is in exponent notation (%s), which is what a float renders as",
				e.ProducerRef, e.Amount)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("nothing was checked, so this test would pass vacuously")
	}
}

// A pool closed longer ago than the tenant said they would reopen is not
// reopened, however large the difference.
//
// This test exists because an earlier version of the one above used a fixed
// period and failed for exactly this reason — the pool was 177 days old against
// a 90 day lookback. That was the policy working, and it was worth keeping
// rather than only working around.
func TestAPoolClosedBeyondTheLookbackIsNotReopened(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// A lookback of one day, so the pool built below — closed three days ago —
	// is already out of reach.
	if _, err := svcclient.Call[declareRetroPolicyReq, declareRetroPolicyResp](ctx, p.pooling(),
		poolingSvc+"/DeclareRetroactivityPolicy", declareRetroPolicyReq{
			TenantID: p.tenant, Name: "tight", Mode: "APPLY_INCREMENTAL",
			MaxLookbackDays: 1, Currency: "INR", AmountScale: 2,
			MinimumAdjustment: "1.00",
			EffectiveFrom:     time.Now().UTC().AddDate(-1, 0, 0).Format(time.RFC3339),
			Actor:             "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("DeclareRetroactivityPolicy: %v", err)
	}

	poolID, _ := valuedPool(t, p)
	if _, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{TenantID: p.tenant, PoolID: poolID, Actor: "e2e"}, p.opts()); err != nil {
		t.Fatal(err)
	}
	if _, err := svcclient.Call[recordUtilisationReq, recordUtilisationResp](ctx, p.pooling(),
		poolingSvc+"/RecordUtilisation", recordUtilisationReq{
			TenantID: p.tenant, PoolID: poolID, Class: "CLASS_II",
			Quantity: "100.000", Price: "4.0000", PriceScale: 4, Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatal(err)
	}

	corrected, err := svcclient.Call[applyCorrectionReq, applyCorrectionResp](ctx, p.pooling(),
		poolingSvc+"/ApplyCorrection", applyCorrectionReq{
			TenantID: p.tenant, PoolID: poolID,
			ComponentPrices: []componentPriceProto{{Component: "FAT", Price: "20.0000", Scale: 4}},
			Actor:           "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	if corrected.Outcome == "ADJUST" || corrected.Outcome == "RESTATE" {
		t.Errorf("a pool closed beyond the lookback was reopened (outcome %s)", corrected.Outcome)
	}
	if corrected.Reason == "" {
		t.Error("no reason was given for leaving it closed")
	}
	// The amount is still reported. A tenant deciding whether their lookback is
	// set correctly needs to know what it is costing them.
	if minor(t, corrected.Adjustment, 2) != minor(t, "400.00", 2) {
		t.Errorf("adjustment reported as %s, want the 400.00 that went unpaid", corrected.Adjustment)
	}
}

// Who gets the stray paisa is a decision, and it must be the same decision every
// time or a recomputed settlement will not match the one that was paid.
//
// money.Allocate documents the rule: the largest weight first, then by index.
// This holds it, because the consequence is systematic — the largest producer in
// every pool collects every residual — and a change to it would move real money
// without any test failing unless one asserts the rule directly.
func TestTheStrayPaisaGoesWhereTheRuleSaysAndDoesSoEveryTime(t *testing.T) {
	p := startPlatform(t)
	_, first := valuedPool(t, p)

	// The fund is 100004 paise split 300000:200000. The exact shares end .4 and
	// .6, so exactly one paisa is left to place.
	got := map[string]string{}
	for _, a := range first {
		got[a.ProducerRef] = a.Total
	}
	if minor(t, got["producer:alpha-1"], 2)+minor(t, got["producer:alpha-2"], 2) != minor(t, "1400.04", 2) {
		t.Fatalf("the shares do not sum to the pool: %v", got)
	}
	// alpha-1 has the larger weight, so it takes the residual.
	if minor(t, got["producer:alpha-1"], 2) != minor(t, "840.03", 2) {
		t.Errorf("the residual paisa went somewhere other than the largest weight: %v", got)
	}

	// The same pool built again lands identically. A settlement that cannot be
	// recomputed to the paisa is a settlement nobody can defend.
	_, second := valuedPool(t, p)
	again := map[string]string{}
	for _, a := range second {
		again[a.ProducerRef] = a.Total
	}
	for producer, want := range got {
		if again[producer] != want {
			t.Errorf("%s got %s the first time and %s the second; the split is not replayable",
				producer, want, again[producer])
		}
	}
}
