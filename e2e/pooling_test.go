//go:build e2e

// The seven ways a pool is read back.
//
// payment_test.go drives pooling forward — create, add milk, value, settle,
// correct — and checks the money at every step. What it never does is read any
// of it back. Every route here is a Get or a List, and that is the shape worth
// being careful about: a read that finds nothing returns an empty list and a
// 200, and looks exactly like a read that found nothing because there was
// nothing. cattle-market's ownership history answered that way for every animal
// it was ever asked about.
//
// So the tests below are not "call it and see that it works". Each one puts two
// things where one could be returned — two pools, two tenants, two policy
// windows — and checks that the read distinguishes them. A query that has lost
// its WHERE clause passes the first kind of test and fails this one.
package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type getPoolReq struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}

// fullPoolProto carries the fields a pool is created with, so a read can be
// compared against what was written rather than against itself.
type fullPoolProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit"`
	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	Status      string `json:"status"`
}

type getPoolResp struct {
	Pool *fullPoolProto `json:"pool"`
}

type listPoolsReq struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Limit    int32  `json:"limit,omitempty"`
	Offset   int32  `json:"offset,omitempty"`
}

type listPoolsResp struct {
	Pools []*fullPoolProto `json:"pools"`
}

type listProducerMilkReq struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}

type producerMilkProto struct {
	ID          string            `json:"id"`
	PoolID      string            `json:"pool_id"`
	ProducerRef string            `json:"producer_ref"`
	Quantity    string            `json:"quantity"`
	Components  map[string]string `json:"components,omitempty"`
	OriginKind  string            `json:"origin_kind"`
}

type listProducerMilkResp struct {
	ProducerMilk []*producerMilkProto `json:"producer_milk"`
}

type listUtilisationsReq struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}

type utilisationProto struct {
	ID         string `json:"id"`
	PoolID     string `json:"pool_id"`
	Class      string `json:"class"`
	Quantity   string `json:"quantity"`
	Price      string `json:"price"`
	PriceScale int32  `json:"price_scale"`
}

type listUtilisationsResp struct {
	Utilisations []*utilisationProto `json:"utilisations"`
}

type getValuationReq struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}

// fullValuationProto is the valuation as the handler actually emits it.
//
// The struct payment_test.go uses reads `total_value`, and no such field is
// sent — ValuationProto carries classified_value, component_value and
// producer_settlement_fund instead. Nothing asserts on it today, so it costs
// nothing; the next person to write `if v.TotalValue != "1400.04"` gets a
// comparison of "" against "" and a test that cannot fail. The field is gone
// from there, and this one names what is on the wire.
type fullValuationProto struct {
	ID                     string `json:"id"`
	TenantID               string `json:"tenant_id"`
	PoolID                 string `json:"pool_id"`
	Currency               string `json:"currency"`
	AmountScale            int32  `json:"amount_scale"`
	ClassifiedValue        string `json:"classified_value"`
	ComponentValue         string `json:"component_value"`
	ProducerSettlementFund string `json:"producer_settlement_fund"`
	TotalQuantity          string `json:"total_quantity"`
}

type getValuationResp struct {
	Valuation *fullValuationProto `json:"valuation"`
}

type listAllocationsReq struct {
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id"`
	ValuationID string `json:"valuation_id,omitempty"`
}

type listAllocationsResp struct {
	Allocations []allocationProto `json:"allocations"`
}

type getEffectivePolicyReq struct {
	TenantID string `json:"tenant_id"`
	At       string `json:"at,omitempty"`
}

type retroPolicyProto struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	MaxLookbackDays int32  `json:"max_lookback_days"`
	EffectiveFrom   string `json:"effective_from"`
	EffectiveTo     string `json:"effective_to,omitempty"`
}

type getEffectivePolicyResp struct {
	Policy *retroPolicyProto `json:"policy"`
}

// makePool creates one pool over a stated period and returns its id.
func makePool(t *testing.T, p *platform, name string, start, end time.Time) string {
	t.Helper()
	out, err := svcclient.Call[createPoolReq, createPoolResp](context.Background(), p.pooling(),
		poolingSvc+"/CreatePool", createPoolReq{
			TenantID: p.tenant, Name: name, Unit: "L",
			PeriodStart: start.Format(time.RFC3339), PeriodEnd: end.Format(time.RFC3339),
			Currency: "INR", AmountScale: 2, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("CreatePool %s: %v", name, err)
	}
	return out.Pool.ID
}

// A pool reads back carrying what it was created with.
//
// Every field is compared, not just the id. A read that returned the right row
// with the period silently swapped, or the scale defaulted to 0, would satisfy
// a test that only checked the id came back — and an amount_scale of 0 turns
// 840.03 into 84003 rupees.
func TestAPoolReadsBackTheWayItWasCreated(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	end := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	start := end.AddDate(0, -1, 0)
	name := "pool " + newID("nm")
	id := makePool(t, p, name, start, end)

	got, err := svcclient.Call[getPoolReq, getPoolResp](ctx, p.pooling(),
		poolingSvc+"/GetPool", getPoolReq{TenantID: p.tenant, PoolID: id}, p.opts())
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	pool := got.Pool
	if pool == nil {
		t.Fatal("GetPool answered with no pool at all")
	}

	for _, c := range []struct{ field, got, want string }{
		{"id", pool.ID, id},
		{"tenant_id", pool.TenantID, p.tenant},
		{"name", pool.Name, name},
		{"unit", pool.Unit, "L"},
		{"currency", pool.Currency, "INR"},
		{"status", pool.Status, "OPEN"},
	} {
		if c.got != c.want {
			t.Errorf("pool %s is %q, want %q", c.field, c.got, c.want)
		}
	}
	if pool.AmountScale != 2 {
		t.Errorf("pool amount_scale is %d, want 2 — every amount in this pool is "+
			"read at this scale, so a wrong one moves the decimal point on all of them",
			pool.AmountScale)
	}
	if !sameInstant(t, pool.PeriodStart, start) {
		t.Errorf("pool period_start is %q, want %s", pool.PeriodStart, start.Format(time.RFC3339))
	}
	if !sameInstant(t, pool.PeriodEnd, end) {
		t.Errorf("pool period_end is %q, want %s", pool.PeriodEnd, end.Format(time.RFC3339))
	}
}

// A pool belongs to the tenant that created it.
//
// Both the id and the tenant are in the request body, and the tenant is on the
// transport too. A query that trusted the body's pool id alone would hand one
// tenant another's settlement figures.
func TestAPoolIsNotReadableByAnotherTenant(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	end := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	id := makePool(t, p, "private pool", end.AddDate(0, -1, 0), end)

	stranger := newID("ten")
	_, err := svcclient.Call[getPoolReq, getPoolResp](ctx, p.pooling(),
		poolingSvc+"/GetPool", getPoolReq{TenantID: stranger, PoolID: id},
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e", RequestID: newID("req")})
	if err == nil {
		t.Fatal("another tenant read this pool; the pool id alone was enough")
	}
	var ce *connect.Error
	if errors.As(err, &ce) && ce.Code() != connect.CodeNotFound {
		t.Errorf("refused with %s, want not_found — a pool belonging to somebody "+
			"else should be indistinguishable from one that does not exist",
			ce.Code())
	}
}

// ListPools answers for the window it was asked about.
//
// Three pools over three separate months, and a window covering only the middle
// one. A query whose period bounds had been dropped, or reversed, returns all
// three — and still looks like it is working, because the pool the caller
// wanted is in the answer.
func TestListPoolsAnswersForTheWindowItWasAsked(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Far enough back that no other test's pool lands in these windows.
	base := time.Now().UTC().AddDate(-3, 0, 0).Truncate(24 * time.Hour)
	var ids []string
	for i := range 3 {
		start := base.AddDate(0, i*2, 0)
		ids = append(ids, makePool(t, p, "window pool", start, start.AddDate(0, 1, 0)))
	}

	// The middle pool's own month, and nothing either side of it.
	from := base.AddDate(0, 2, 0)
	to := from.AddDate(0, 1, 0)

	got, err := svcclient.Call[listPoolsReq, listPoolsResp](ctx, p.pooling(),
		poolingSvc+"/ListPools", listPoolsReq{
			TenantID: p.tenant,
			From:     from.Format(time.RFC3339), To: to.Format(time.RFC3339),
			Limit: 50,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}

	found := map[string]bool{}
	for _, pool := range got.Pools {
		found[pool.ID] = true
	}
	if !found[ids[1]] {
		t.Errorf("the pool whose period is exactly this window is not in the answer")
	}
	if found[ids[0]] || found[ids[2]] {
		t.Errorf("a pool outside the window came back: before=%v after=%v\n"+
			"The window bounds are not being applied, so a caller asking for one "+
			"month is handed every month it has.",
			found[ids[0]], found[ids[2]])
	}
}

// What went into a pool, and what it was used for, belongs to that pool.
//
// Two pools are filled in the same moment with different milk and different
// utilisations. Asking one for its contents must not return the other's. This
// is the shape that caught cattle-market: a list keyed on the wrong column
// answers 200 with somebody else's rows, or with none, and nothing in the
// response says which.
func TestAPoolsMilkAndUtilisationsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	end := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	start := end.AddDate(0, -1, 0)
	mine := makePool(t, p, "mine", start, end)
	theirs := makePool(t, p, "theirs", start, end)

	for _, f := range []struct {
		pool, producer, quantity, class, price string
	}{
		{mine, "producer:mine-1", "300.000", "CLASS_I", "3.5001"},
		{theirs, "producer:theirs-1", "111.000", "CLASS_II", "2.2500"},
	} {
		if _, err := svcclient.Call[addMilkReq, addMilkResp](ctx, p.pooling(),
			poolingSvc+"/AddProducerMilk", addMilkReq{
				TenantID: p.tenant, PoolID: f.pool, ProducerRef: f.producer,
				Quantity: f.quantity, Components: map[string]string{"FAT": "12.000"},
				OriginKind: "NATIVE", Actor: "e2e",
			}, p.opts()); err != nil {
			t.Fatalf("AddProducerMilk %s: %v", f.producer, err)
		}
		if _, err := svcclient.Call[recordUtilisationReq, recordUtilisationResp](ctx, p.pooling(),
			poolingSvc+"/RecordUtilisation", recordUtilisationReq{
				TenantID: p.tenant, PoolID: f.pool, Class: f.class,
				Quantity: "100.000", Price: f.price, PriceScale: 4, Actor: "e2e",
			}, p.opts()); err != nil {
			t.Fatalf("RecordUtilisation %s: %v", f.class, err)
		}
	}

	milk, err := svcclient.Call[listProducerMilkReq, listProducerMilkResp](ctx, p.pooling(),
		poolingSvc+"/ListProducerMilk", listProducerMilkReq{TenantID: p.tenant, PoolID: mine}, p.opts())
	if err != nil {
		t.Fatalf("ListProducerMilk: %v", err)
	}
	if len(milk.ProducerMilk) != 1 {
		t.Fatalf("this pool holds %d producers' milk, want 1 — the other pool was "+
			"filled in the same moment and its milk is not this pool's",
			len(milk.ProducerMilk))
	}
	m := milk.ProducerMilk[0]
	if m.ProducerRef != "producer:mine-1" {
		t.Errorf("producer_ref is %q, want producer:mine-1", m.ProducerRef)
	}
	if m.Quantity != "300.000" {
		t.Errorf("quantity is %q, want 300.000 — the figure this pool is paid on", m.Quantity)
	}
	if m.PoolID != mine {
		t.Errorf("the milk says it belongs to pool %s, and it was asked of %s", m.PoolID, mine)
	}
	if m.OriginKind != "NATIVE" {
		t.Errorf("origin_kind is %q, want NATIVE — where a figure came from is not "+
			"decoration, it decides whether it can be settled on", m.OriginKind)
	}

	used, err := svcclient.Call[listUtilisationsReq, listUtilisationsResp](ctx, p.pooling(),
		poolingSvc+"/ListUtilisations", listUtilisationsReq{TenantID: p.tenant, PoolID: mine}, p.opts())
	if err != nil {
		t.Fatalf("ListUtilisations: %v", err)
	}
	if len(used.Utilisations) != 1 {
		t.Fatalf("this pool holds %d utilisations, want 1", len(used.Utilisations))
	}
	u := used.Utilisations[0]
	if u.Class != "CLASS_I" {
		t.Errorf("class is %q, want CLASS_I — the other pool's was CLASS_II", u.Class)
	}
	if u.Price != "3.5001" || u.PriceScale != 4 {
		t.Errorf("price reads back %q at scale %d, want 3.5001 at 4\n"+
			"The fourth decimal is the point: at 3.5000 the pool divides evenly "+
			"and a rounding mistake cannot show itself.",
			u.Price, u.PriceScale)
	}
}

// A valuation reads back the figures it was computed with, and its allocations
// still add up to it.
//
// The expected values are the ones worked out by hand in valuedPool's comment,
// not values taken from the response: 400 litres of Class I at 3.5001 is
// 1400.04, the fat is 400.00, and the fund is the difference.
func TestAValuationReadsBackTheFiguresItWasComputedWith(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	poolID, allocations := valuedPool(t, p)

	got, err := svcclient.Call[getValuationReq, getValuationResp](ctx, p.pooling(),
		poolingSvc+"/GetValuation", getValuationReq{TenantID: p.tenant, PoolID: poolID}, p.opts())
	if err != nil {
		t.Fatalf("GetValuation: %v", err)
	}
	v := got.Valuation
	if v == nil {
		t.Fatal("GetValuation answered with no valuation for a pool that has been valued")
	}

	for _, c := range []struct{ field, got, want string }{
		{"classified_value", v.ClassifiedValue, "1400.04"},
		{"component_value", v.ComponentValue, "400.00"},
		{"producer_settlement_fund", v.ProducerSettlementFund, "1000.04"},
		{"pool_id", v.PoolID, poolID},
		{"currency", v.Currency, "INR"},
	} {
		if c.got != c.want {
			t.Errorf("valuation %s reads back %q, want %q", c.field, c.got, c.want)
		}
	}
	if v.AmountScale != 2 {
		t.Errorf("valuation amount_scale is %d, want 2 — the decimal strings above "+
			"have no unambiguous reading without it", v.AmountScale)
	}

	// The allocations, read back rather than returned by the call that made
	// them. Each must match what ValuePool said, to the paisa.
	listed, err := svcclient.Call[listAllocationsReq, listAllocationsResp](ctx, p.pooling(),
		poolingSvc+"/ListAllocations", listAllocationsReq{TenantID: p.tenant, PoolID: poolID}, p.opts())
	if err != nil {
		t.Fatalf("ListAllocations: %v", err)
	}
	if len(listed.Allocations) != len(allocations) {
		t.Fatalf("the pool was valued into %d allocations and reads back %d",
			len(allocations), len(listed.Allocations))
	}
	want := map[string]string{}
	for _, a := range allocations {
		want[a.ProducerRef] = a.Total
	}
	total := int64(0)
	for _, a := range listed.Allocations {
		w, ok := want[a.ProducerRef]
		if !ok {
			t.Errorf("an allocation came back for %q, which this pool was not valued for", a.ProducerRef)
			continue
		}
		if a.Total != w {
			t.Errorf("%s is allocated %q on reading back and was allocated %q when valued",
				a.ProducerRef, a.Total, w)
		}
		total += minor(t, a.Total, 2)
	}
	// 840.03 + 560.01. If the read dropped a producer this is short, and the
	// count check above would already have caught it; if it returned somebody
	// else's allocation as well, this is long.
	if wantTotal := minor(t, "1400.04", 2); total != wantTotal {
		t.Errorf("the allocations read back total %d paise, want %d — what is "+
			"handed out must be what the pool was valued at, or somebody is paid "+
			"money the pool did not have", total, wantTotal)
	}

	// Filtering by the valuation is a filter, not an ornament: a pool that has
	// been re-valued has allocations under more than one valuation id, and
	// paying out both would pay twice.
	none, err := svcclient.Call[listAllocationsReq, listAllocationsResp](ctx, p.pooling(),
		poolingSvc+"/ListAllocations", listAllocationsReq{
			TenantID: p.tenant, PoolID: poolID, ValuationID: newID("val"),
		}, p.opts())
	if err != nil {
		t.Fatalf("ListAllocations by valuation: %v", err)
	}
	if len(none.Allocations) != 0 {
		t.Errorf("asking for a valuation this pool does not have returned %d "+
			"allocations; the valuation_id is not filtering", len(none.Allocations))
	}
}

// The policy in force is the one whose window covers the moment asked about.
//
// Two policies, back to back, differing in the figure that matters — how far
// back a correction may reach. Asking at a moment in the first window must
// return the first, not merely the most recently declared. The repository query
// has no ORDER BY and no LIMIT, so it is relying on there being exactly one
// answer; the last part of this test is what that reliance rests on.
func TestThePolicyInForceIsTheOneCoveringTheMoment(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Its own tenant: the policy table holds at most one policy in force per
	// tenant at a time, so a test that declared one into the shared tenant
	// would collide with every other pooling test.
	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	firstFrom := time.Now().UTC().AddDate(-2, 0, 0).Truncate(24 * time.Hour)
	secondFrom := firstFrom.AddDate(1, 0, 0)

	// The two policies differ in mode as well as lookback, so picking the wrong
	// one is not an off-by-a-number: RECALCULATE re-values a settled pool from
	// scratch, APPLY_INCREMENTAL only pays the difference.
	for _, d := range []struct {
		name     string
		mode     string
		lookback int32
		from     time.Time
		to       time.Time
	}{
		{"first", "RECALCULATE", 30, firstFrom, secondFrom},
		{"second", "APPLY_INCREMENTAL", 90, secondFrom, time.Time{}},
	} {
		req := declareRetroPolicyReq{
			TenantID: tenant, Name: d.name, Mode: d.mode,
			MaxLookbackDays: d.lookback, Currency: "INR", AmountScale: 2,
			EffectiveFrom: d.from.Format(time.RFC3339), Actor: "e2e",
		}
		if !d.to.IsZero() {
			req.EffectiveTo = d.to.Format(time.RFC3339)
		}
		if _, err := svcclient.Call[declareRetroPolicyReq, declareRetroPolicyResp](ctx, p.pooling(),
			poolingSvc+"/DeclareRetroactivityPolicy", req, opts); err != nil {
			t.Fatalf("DeclareRetroactivityPolicy %s: %v", d.name, err)
		}
	}

	for _, c := range []struct {
		when         time.Time
		wantName     string
		wantMode     string
		wantLookback int32
	}{
		{firstFrom.AddDate(0, 1, 0), "first", "RECALCULATE", 30},
		{secondFrom.AddDate(0, 1, 0), "second", "APPLY_INCREMENTAL", 90},
	} {
		got, err := svcclient.Call[getEffectivePolicyReq, getEffectivePolicyResp](ctx, p.pooling(),
			poolingSvc+"/GetEffectiveRetroactivityPolicy", getEffectivePolicyReq{
				TenantID: tenant, At: c.when.Format(time.RFC3339),
			}, opts)
		if err != nil {
			t.Fatalf("GetEffectiveRetroactivityPolicy at %s: %v", c.when.Format(time.DateOnly), err)
		}
		if got.Policy == nil {
			t.Fatalf("no policy in force at %s, and one was declared covering it",
				c.when.Format(time.DateOnly))
		}
		if got.Policy.Name != c.wantName {
			t.Errorf("the policy in force at %s is %q, want %q — the answer is the "+
				"window covering the moment, not the newest declaration",
				c.when.Format(time.DateOnly), got.Policy.Name, c.wantName)
		}
		if got.Policy.Mode != c.wantMode {
			t.Errorf("the mode in force at %s is %q, want %q — RECALCULATE re-values "+
				"a settled pool and APPLY_INCREMENTAL pays only the difference, so "+
				"this is not a label",
				c.when.Format(time.DateOnly), got.Policy.Mode, c.wantMode)
		}
		if got.Policy.MaxLookbackDays != c.wantLookback {
			t.Errorf("max_lookback_days at %s is %d, want %d — this decides whether "+
				"a late correction is applied or escalated",
				c.when.Format(time.DateOnly), got.Policy.MaxLookbackDays, c.wantLookback)
		}
	}

	// Nothing was in force before the first policy began.
	early, err := svcclient.Call[getEffectivePolicyReq, getEffectivePolicyResp](ctx, p.pooling(),
		poolingSvc+"/GetEffectiveRetroactivityPolicy", getEffectivePolicyReq{
			TenantID: tenant, At: firstFrom.AddDate(0, -1, 0).Format(time.RFC3339),
		}, opts)
	if err == nil && early.Policy != nil && early.Policy.Name != "" {
		t.Errorf("a policy (%q) is in force a month before any was declared",
			early.Policy.Name)
	}

	// And the query above is allowed to have no ORDER BY only because two
	// policies cannot be in force at once. That is a database constraint, not a
	// convention, so it is checked here rather than assumed.
	overlapping := declareRetroPolicyReq{
		TenantID: tenant, Name: "overlapping", Mode: "RECALCULATE",
		MaxLookbackDays: 5, Currency: "INR", AmountScale: 2,
		EffectiveFrom: secondFrom.AddDate(0, 6, 0).Format(time.RFC3339), Actor: "e2e",
	}
	if _, err := svcclient.Call[declareRetroPolicyReq, declareRetroPolicyResp](ctx, p.pooling(),
		poolingSvc+"/DeclareRetroactivityPolicy", overlapping, opts); err == nil {
		t.Error("a second policy was accepted over a window already covered\n" +
			"GetEffectiveRetroactivityPolicy selects without an ORDER BY or a " +
			"LIMIT, so with two rows in force it returns whichever the planner " +
			"reaches first — and that decides how far back money may be moved.")
	}
}

// sameInstant compares a timestamp from the wire against the time it was sent
// as, rather than comparing formatted strings: the service is free to answer in
// UTC with a different but equivalent spelling.
func sameInstant(t *testing.T, got string, want time.Time) bool {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Errorf("timestamp %q is not RFC 3339: %v", got, err)
		return false
	}
	return parsed.Equal(want)
}
