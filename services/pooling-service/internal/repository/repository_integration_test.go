//go:build dbintegration

// Repository tests against a real PostgreSQL.
//
// The pool arithmetic is unit tested as a pure function elsewhere. What can
// only be tested here is whether the schema and the repository preserve what
// that arithmetic produced: that a producer's money survives the round trip
// without losing a minor unit, that a valuation and its allocations are written
// together or not at all, and that the constraints refuse the records which
// would let a producer be paid twice or paid an amount nothing explains.
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

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
	"github.com/ppusapati/gavya/services/pooling-service/internal/domain"
)

var (
	poolOnce sync.Once
	testPool *pgxpool.Pool

	runNonce  = time.Now().UnixNano()
	idCounter atomic.Int64
)

func pgpool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	// The same search path this service's pool runs with.
	//
	// TEST_DATABASE_URL names a database holding every service's schema, each in
	// its own — which is what both deployments build. The queries below are this
	// service's, written unqualified, so a connection without its search path
	// resolves none of them. See libs/integrity/tenantdb/namespace.go.
	dsn, err := tenantdb.NamespacedDSN(dsn, "pooling-service")
	if err != nil {
		t.Fatal(err)
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
	periodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd   = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	y2020       = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	y2024       = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	y2026       = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
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
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return &fixture{repo: New(pgpool(t), sys.IDs{}), tenantID: newID("tnt")}
}

func rate(t *testing.T, s string, scale int32) money.Rate {
	t.Helper()
	r, err := money.ParseRate(s, scale)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

func (f *fixture) pool(t *testing.T) *domain.Pool {
	t.Helper()
	p, err := f.repo.CreatePool(f.acting(), &domain.Pool{
		ID:          newID("pol"),
		TenantID:    f.tenantID,
		Name:        "january-2026",
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Unit:        "LITRE",
		Currency:    "INR",
		Scale:       2,
		Status:      domain.PoolOpen,
		CreatedBy:   "tester",
	})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	return p
}

func (f *fixture) addMilk(t *testing.T, poolID, ref, quantity, fat, snf string) *domain.ProducerMilk {
	t.Helper()
	m, err := f.repo.AddProducerMilk(f.acting(), &domain.ProducerMilk{
		ID:          newID("mlk"),
		TenantID:    f.tenantID,
		PoolID:      poolID,
		ProducerRef: ref,
		Quantity:    quantity,
		Components: map[domain.ComponentKind]string{
			domain.ComponentFat: fat,
			domain.ComponentSNF: snf,
		},
		SlotRefs:  []string{"slot-" + ref},
		Origin:    origin.NewNative(),
		CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("add producer milk for %s: %v", ref, err)
	}
	return m
}

func (f *fixture) addUtilisation(t *testing.T, poolID string, class domain.UtilisationClass, quantity, price string) {
	t.Helper()
	if _, err := f.repo.AddUtilisation(f.acting(), &domain.ClassifiedUtilisation{
		ID:        newID("utl"),
		TenantID:  f.tenantID,
		PoolID:    poolID,
		Class:     class,
		Quantity:  quantity,
		Price:     rate(t, price, 4),
		CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("add utilisation %s: %v", class, err)
	}
}

// loaded builds a pool holding the same milk and utilisations the domain tests
// value by hand, so the numbers here can be checked against them.
func (f *fixture) loaded(t *testing.T) *domain.Pool {
	t.Helper()
	p := f.pool(t)
	f.addMilk(t, p.ID, "prod-1", "400.000", "16.000", "34.000")
	f.addMilk(t, p.ID, "prod-2", "350.000", "14.700", "29.750")
	f.addMilk(t, p.ID, "prod-3", "250.000", "10.250", "21.250")
	f.addUtilisation(t, p.ID, domain.ClassI, "600.000", "42.0000")
	f.addUtilisation(t, p.ID, domain.ClassIII, "400.000", "36.5000")
	return p
}

func componentPrices(t *testing.T) []domain.ComponentPrice {
	t.Helper()
	return []domain.ComponentPrice{
		{Component: domain.ComponentFat, Price: rate(t, "300.0000", 4)},
		{Component: domain.ComponentSNF, Price: rate(t, "180.0000", 4)},
	}
}

func (f *fixture) value(t *testing.T, p *domain.Pool) *domain.ValuationResult {
	t.Helper()
	ctx := context.Background()

	producers, err := f.repo.ListProducerMilk(ctx, f.tenantID, p.ID)
	if err != nil {
		t.Fatalf("list producer milk: %v", err)
	}
	utilisations, err := f.repo.ListUtilisations(ctx, f.tenantID, p.ID)
	if err != nil {
		t.Fatalf("list utilisations: %v", err)
	}

	res, err := domain.ComputeValuation(domain.ValuationInput{
		Currency:        p.Currency,
		Scale:           p.Scale,
		Producers:       producers,
		Utilisations:    utilisations,
		ComponentPrices: componentPrices(t),
	})
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	return res
}

func (f *fixture) save(t *testing.T, p *domain.Pool, res *domain.ValuationResult) (*domain.PoolValuation, []domain.Allocation) {
	t.Helper()
	valuation, allocations := f.build(p, res)
	saved, stored, err := f.repo.SaveValuation(f.acting(), valuation, allocations)
	if err != nil {
		t.Fatalf("save valuation: %v", err)
	}
	return saved, stored
}

func (f *fixture) build(p *domain.Pool, res *domain.ValuationResult) (*domain.PoolValuation, []domain.Allocation) {
	valuationID := newID("val")
	valuation := &domain.PoolValuation{
		ID:                     valuationID,
		TenantID:               f.tenantID,
		PoolID:                 p.ID,
		ClassifiedValue:        res.ClassifiedValue,
		ComponentValue:         res.ComponentValue,
		ProducerSettlementFund: res.ProducerSettlementFund,
		TotalQuantity:          res.TotalQuantity,
		BlendPrice:             res.BlendPrice,
		RoundingTrail:          res.RoundingTrail,
		Origin:                 origin.Origin{Kind: origin.Derived, DerivationID: valuationID},
		ComputedAt:             time.Now().UTC(),
		CreatedBy:              "tester",
	}

	allocations := make([]domain.Allocation, 0, len(res.Allocations))
	for _, a := range res.Allocations {
		a.ID = newID("alc")
		a.TenantID = f.tenantID
		a.PoolID = p.ID
		a.ValuationID = valuationID
		a.CreatedBy = "tester"
		allocations = append(allocations, a)
	}
	return valuation, allocations
}

// The invariant the whole design rests on has to survive the database: what the
// producers are owed still sums to what the pool was worth, to the minor unit,
// after the amounts have been through PostgreSQL and back.
func TestAllocationsStillConserveThePoolValueAfterARoundTrip(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	p := f.loaded(t)
	res := f.value(t, p)
	saved, _ := f.save(t, p, res)

	readBack, err := f.repo.ListAllocations(ctx, f.tenantID, p.ID, saved.ID)
	if err != nil {
		t.Fatalf("list allocations: %v", err)
	}
	if len(readBack) != 3 {
		t.Fatalf("read back %d allocations, want 3", len(readBack))
	}

	totals := make([]money.Money, 0, len(readBack))
	for _, a := range readBack {
		totals = append(totals, a.Total)
	}
	sum, err := money.Sum(totals)
	if err != nil {
		t.Fatalf("sum allocation totals: %v", err)
	}

	valuation, err := f.repo.GetValuation(ctx, f.tenantID, p.ID)
	if err != nil {
		t.Fatalf("get valuation: %v", err)
	}
	if sum.Value != valuation.ClassifiedValue.Value {
		t.Fatalf("stored allocations total %s but the stored pool is worth %s",
			sum, valuation.ClassifiedValue)
	}
	// The domain tests fix this figure by hand: 600.000 * 42.0000 plus
	// 400.000 * 36.5000.
	if valuation.ClassifiedValue.String() != "39800.00" {
		t.Errorf("classified value came back as %s, want 39800.00", valuation.ClassifiedValue)
	}
	if valuation.ClassifiedValue.Currency != "INR" || valuation.ClassifiedValue.Scale != 2 {
		t.Errorf("classified value came back as %s at scale %d, want INR at scale 2",
			valuation.ClassifiedValue.Currency, valuation.ClassifiedValue.Scale)
	}

	for i, want := range res.Allocations {
		got := readBack[i]
		if got.ProducerRef != want.ProducerRef {
			t.Fatalf("allocation %d is for %s, want %s", i, got.ProducerRef, want.ProducerRef)
		}
		if got.Total.Value != want.Total.Value {
			t.Errorf("%s total came back as %s, want %s", got.ProducerRef, got.Total, want.Total)
		}
		if got.ComponentValue.Value != want.ComponentValue.Value {
			t.Errorf("%s component value came back as %s, want %s",
				got.ProducerRef, got.ComponentValue, want.ComponentValue)
		}
		if got.FundShare.Value != want.FundShare.Value {
			t.Errorf("%s fund share came back as %s, want %s",
				got.ProducerRef, got.FundShare, want.FundShare)
		}
		if got.Weight != want.Weight {
			t.Errorf("%s weight came back as %d, want %d", got.ProducerRef, got.Weight, want.Weight)
		}
	}

	if valuation.TotalQuantity != res.TotalQuantity {
		t.Errorf("total quantity came back as %q, want %q", valuation.TotalQuantity, res.TotalQuantity)
	}
	if valuation.BlendPrice.Numerator != res.BlendPrice.Numerator || valuation.BlendPrice.Scale != res.BlendPrice.Scale {
		t.Errorf("blend price came back as %s, want %s", valuation.BlendPrice, res.BlendPrice)
	}
	if len(valuation.RoundingTrail) != len(res.RoundingTrail) {
		t.Errorf("rounding trail came back with %d steps, want %d",
			len(valuation.RoundingTrail), len(res.RoundingTrail))
	}
}

// Quantities are decimal literals the domain weights at three decimals. A
// quantity that came back as 1234.57 would silently reweight a producer's share
// of the fund.
func TestProducerQuantityKeepsItsThirdDecimal(t *testing.T) {
	f := setup(t)
	p := f.pool(t)

	f.addMilk(t, p.ID, "prod-1", "1234.567", "49.383", "104.938")

	list, err := f.repo.ListProducerMilk(f.acting(), f.tenantID, p.ID)
	if err != nil {
		t.Fatalf("list producer milk: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d rows, want 1", len(list))
	}
	if list[0].Quantity != "1234.567" {
		t.Errorf("quantity came back as %q, want \"1234.567\"", list[0].Quantity)
	}
	if list[0].Components[domain.ComponentFat] != "49.383" {
		t.Errorf("fat came back as %q, want \"49.383\"", list[0].Components[domain.ComponentFat])
	}
	if len(list[0].SlotRefs) != 1 || list[0].SlotRefs[0] != "slot-prod-1" {
		t.Errorf("slot refs came back as %v, want [slot-prod-1]", list[0].SlotRefs)
	}
	if list[0].Origin.Kind != origin.Native {
		t.Errorf("origin came back as %q, want NATIVE", list[0].Origin.Kind)
	}
}

// A valuation without its allocations is a corrupt record: the pool would claim
// a value no producer has a share of.
func TestSaveValuationWritesEverythingOrNothing(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	p := f.loaded(t)
	res := f.value(t, p)

	valuation, allocations := f.build(p, res)
	// The last allocation cannot be stored: its total does not equal its parts.
	// Nothing from the batch may survive that.
	allocations[len(allocations)-1].Total.Value += 1

	if _, _, err := f.repo.SaveValuation(ctx, valuation, allocations); err == nil {
		t.Fatal("an allocation whose total does not equal its parts was stored")
	}

	if _, err := f.repo.GetValuation(ctx, f.tenantID, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the valuation survived a failed batch: %v", err)
	}
	stored, err := f.repo.ListAllocations(ctx, f.tenantID, p.ID, "")
	if err != nil {
		t.Fatalf("list allocations: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("%d allocations survived a failed batch, want 0", len(stored))
	}
}

// The total is what becomes payable and the two parts are what explain it. If
// they can drift apart, the payment advice does not describe the payment.
func TestAllocationTotalMustEqualItsParts(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	p := f.loaded(t)
	res := f.value(t, p)
	valuation, allocations := f.build(p, res)

	for i := range allocations {
		allocations[i].FundShare.Value -= 5
	}

	_, _, err := f.repo.SaveValuation(ctx, valuation, allocations)
	if err == nil {
		t.Fatal("an allocation whose total exceeds component_value + fund_share was stored")
	}
	if !strings.Contains(err.Error(), "allocation_total_is_its_parts") {
		t.Errorf("got %v, want a violation of allocation_total_is_its_parts", err)
	}
}

// Re-valuing a pool replaces its answer rather than adding a second one: two
// live valuations would leave settlement free to pay from either.
func TestSavingAgainSupersedesThePreviousValuation(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	p := f.loaded(t)
	first, _ := f.save(t, p, f.value(t, p))

	f.addUtilisation(t, p.ID, domain.ClassIV, "200.000", "30.0000")
	f.addMilk(t, p.ID, "prod-4", "200.000", "8.000", "17.000")
	second, secondAllocations := f.save(t, p, f.value(t, p))

	if first.ID == second.ID {
		t.Fatal("the second valuation reused the first one's identifier")
	}

	live, err := f.repo.GetValuation(ctx, f.tenantID, p.ID)
	if err != nil {
		t.Fatalf("get valuation: %v", err)
	}
	if live.ID != second.ID {
		t.Errorf("the live valuation is %s, want the newest one %s", live.ID, second.ID)
	}
	if live.ClassifiedValue.Value == first.ClassifiedValue.Value {
		t.Errorf("the live valuation still reports the superseded value %s", live.ClassifiedValue)
	}

	// The superseded valuation's allocations stay readable: a payment made under
	// them must remain explainable.
	old, err := f.repo.ListAllocations(ctx, f.tenantID, p.ID, first.ID)
	if err != nil {
		t.Fatalf("list superseded allocations: %v", err)
	}
	if len(old) != 3 {
		t.Errorf("got %d superseded allocations, want the original 3", len(old))
	}
	if len(secondAllocations) != 4 {
		t.Errorf("the new valuation stored %d allocations, want 4", len(secondAllocations))
	}
}

func (f *fixture) settled(t *testing.T) (*domain.Pool, []domain.Allocation, []domain.ProducerEconomicEvent) {
	t.Helper()
	p := f.loaded(t)
	_, allocations := f.save(t, p, f.value(t, p))

	events := make([]domain.ProducerEconomicEvent, 0, len(allocations))
	for _, a := range allocations {
		events = append(events, domain.ProducerEconomicEvent{
			ID:           newID("evt"),
			TenantID:     f.tenantID,
			PoolID:       p.ID,
			AllocationID: a.ID,
			ProducerRef:  a.ProducerRef,
			Amount:       a.Total,
			Kind:         domain.EventOriginal,
			Origin:       origin.Origin{Kind: origin.Derived, DerivationID: newID("drv")},
			CreatedBy:    "tester",
		})
	}
	stored, err := f.repo.CreateEconomicEvents(f.acting(), events)
	if err != nil {
		t.Fatalf("raise original events: %v", err)
	}
	return p, allocations, stored
}

func TestOriginalEventsCarryTheAllocationTotals(t *testing.T) {
	f := setup(t)
	p, allocations, events := f.settled(t)

	if len(events) != len(allocations) {
		t.Fatalf("raised %d events for %d allocations", len(events), len(allocations))
	}
	byProducer := map[string]domain.ProducerEconomicEvent{}
	for _, e := range events {
		byProducer[e.ProducerRef] = e
	}
	for _, a := range allocations {
		e, ok := byProducer[a.ProducerRef]
		if !ok {
			t.Fatalf("no event was raised for producer %s", a.ProducerRef)
		}
		if e.Amount.Value != a.Total.Value || e.Amount.Currency != a.Total.Currency {
			t.Errorf("%s was raised for %s but is allocated %s", a.ProducerRef, e.Amount, a.Total)
		}
		if e.Kind != domain.EventOriginal {
			t.Errorf("%s event kind is %s, want ORIGINAL", a.ProducerRef, e.Kind)
		}
		if e.SupersedesEventID != "" {
			t.Errorf("an ORIGINAL for %s names %s as superseded", a.ProducerRef, e.SupersedesEventID)
		}
	}

	listed, err := f.repo.ListEconomicEvents(f.acting(), f.tenantID, p.ID, "prod-2", 10, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d events for prod-2, want 1", len(listed))
	}
	if listed[0].ID != byProducer["prod-2"].ID {
		t.Errorf("listed event %s, want %s", listed[0].ID, byProducer["prod-2"].ID)
	}
}

// An INCREMENTAL that names nothing is a difference from an unknown amount, and
// an ORIGINAL that names something claims to be both a first payable and a
// correction of one. Either lets a producer be paid twice with no row saying so.
func TestCorrectionMustNameWhatItSupersedes(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	p, allocations, events := f.settled(t)

	orphan := domain.ProducerEconomicEvent{
		ID:           newID("evt"),
		TenantID:     f.tenantID,
		PoolID:       p.ID,
		AllocationID: allocations[0].ID,
		ProducerRef:  allocations[0].ProducerRef,
		Amount:       allocations[0].Total,
		Kind:         domain.EventIncremental,
		CreatedBy:    "tester",
	}
	_, err := f.repo.CreateEconomicEvents(ctx, []domain.ProducerEconomicEvent{orphan})
	if err == nil {
		t.Fatal("an INCREMENTAL naming no superseded event was stored")
	}
	if !strings.Contains(err.Error(), "correction_names_what_it_supersedes") {
		t.Errorf("got %v, want a violation of correction_names_what_it_supersedes", err)
	}

	duplicate := orphan
	duplicate.ID = newID("evt")
	duplicate.Kind = domain.EventOriginal
	duplicate.SupersedesEventID = events[0].ID
	_, err = f.repo.CreateEconomicEvents(ctx, []domain.ProducerEconomicEvent{duplicate})
	if err == nil {
		t.Fatal("an ORIGINAL naming a superseded event was stored")
	}
	if !strings.Contains(err.Error(), "correction_names_what_it_supersedes") {
		t.Errorf("got %v, want a violation of correction_names_what_it_supersedes", err)
	}

	// The well-formed correction is admitted, so the constraint is not simply
	// refusing everything.
	valid := orphan
	valid.ID = newID("evt")
	valid.SupersedesEventID = events[0].ID
	stored, err := f.repo.CreateEconomicEvents(ctx, []domain.ProducerEconomicEvent{valid})
	if err != nil {
		t.Fatalf("a well-formed INCREMENTAL was refused: %v", err)
	}
	if stored[0].SupersedesEventID != events[0].ID {
		t.Errorf("the correction came back naming %q, want %s", stored[0].SupersedesEventID, events[0].ID)
	}
}

// A partial batch would settle some of a pool's producers and leave the rest
// unpaid with nothing in the data saying which was which.
func TestEconomicEventBatchIsAllOrNothing(t *testing.T) {
	f := setup(t)
	ctx := f.acting()
	p, allocations, _ := f.settled(t)

	before, err := f.repo.ListEconomicEvents(ctx, f.tenantID, p.ID, "", 100, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	batch := []domain.ProducerEconomicEvent{
		{
			ID:           newID("evt"),
			TenantID:     f.tenantID,
			PoolID:       p.ID,
			AllocationID: allocations[0].ID,
			ProducerRef:  allocations[0].ProducerRef,
			Amount:       allocations[0].Total,
			Kind:         domain.EventOriginal,
			CreatedBy:    "tester",
		},
		{
			ID:           newID("evt"),
			TenantID:     f.tenantID,
			PoolID:       p.ID,
			AllocationID: allocations[1].ID,
			ProducerRef:  allocations[1].ProducerRef,
			Amount:       allocations[1].Total,
			Kind:         domain.EventRestatement,
			CreatedBy:    "tester",
		},
	}
	if _, err := f.repo.CreateEconomicEvents(ctx, batch); err == nil {
		t.Fatal("a batch holding a malformed RESTATEMENT was stored")
	}

	after, err := f.repo.ListEconomicEvents(ctx, f.tenantID, p.ID, "", 100, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("%d events survived a failed batch, want the %d that were already there",
			len(after), len(before))
	}
}

// A second row for the same producer would double their weight in the fund
// split and take money from every other producer in the pool.
func TestAProducerMayPoolMilkOnlyOncePerPool(t *testing.T) {
	f := setup(t)
	p := f.pool(t)

	f.addMilk(t, p.ID, "prod-1", "400.000", "16.000", "34.000")

	_, err := f.repo.AddProducerMilk(f.acting(), &domain.ProducerMilk{
		ID:          newID("mlk"),
		TenantID:    f.tenantID,
		PoolID:      p.ID,
		ProducerRef: "prod-1",
		Quantity:    "50.000",
		Components:  map[domain.ComponentKind]string{domain.ComponentFat: "2.000"},
		Origin:      origin.NewNative(),
		CreatedBy:   "tester",
	})
	if !errors.Is(err, ErrDuplicateProducer) {
		t.Fatalf("got %v, want ErrDuplicateProducer", err)
	}

	// The same producer in a different pool is a different delivery.
	other := f.pool(t)
	f.addMilk(t, other.ID, "prod-1", "50.000", "2.000", "4.250")
}

// Two policies in force at once would give a late correction two different
// answers about whether it may reopen a settled pool.
func TestOnlyOneRetroactivityPolicyMayBeInForce(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	if _, err := f.repo.CreateRetroactivityPolicy(ctx, f.policy("standing", domain.RetroApplyIncremental, 90, y2020, nil)); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	overlapping := f.policy("second", domain.RetroRecalculate, 30, y2024, nil)
	if _, err := f.repo.CreateRetroactivityPolicy(ctx, overlapping); !errors.Is(err, ErrOverlappingPolicy) {
		t.Fatalf("got %v, want ErrOverlappingPolicy", err)
	}
}

func TestEffectivePolicyIsTheOneInForceAtTheInstantAsked(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	closed := y2024
	if _, err := f.repo.CreateRetroactivityPolicy(ctx, f.policy("early", domain.RetroDoNotReopen, 0, y2020, &closed)); err != nil {
		t.Fatalf("create early policy: %v", err)
	}
	if _, err := f.repo.CreateRetroactivityPolicy(ctx, f.policy("current", domain.RetroApplyIncremental, 90, y2024, nil)); err != nil {
		t.Fatalf("a policy taking over where the first ended was refused: %v", err)
	}

	early, err := f.repo.GetEffectiveRetroactivityPolicy(ctx, f.tenantID, time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("get effective policy in 2022: %v", err)
	}
	if early.Mode != domain.RetroDoNotReopen {
		t.Errorf("2022 resolved to mode %s, want DO_NOT_REOPEN", early.Mode)
	}

	current, err := f.repo.GetEffectiveRetroactivityPolicy(ctx, f.tenantID, y2026)
	if err != nil {
		t.Fatalf("get effective policy in 2026: %v", err)
	}
	if current.Mode != domain.RetroApplyIncremental {
		t.Errorf("2026 resolved to mode %s, want APPLY_INCREMENTAL", current.Mode)
	}
	if current.MaxLookbackDays != 90 {
		t.Errorf("lookback came back as %d days, want 90", current.MaxLookbackDays)
	}
	if current.MinimumAdjustment.String() != "25.00" || current.MinimumAdjustment.Currency != "INR" {
		t.Errorf("materiality floor came back as %s %s, want INR 25.00",
			current.MinimumAdjustment.Currency, current.MinimumAdjustment)
	}

	if _, err := f.repo.GetEffectiveRetroactivityPolicy(ctx, f.tenantID, time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrNotFound) {
		t.Errorf("an instant before any policy resolved: %v", err)
	}
}

func (f *fixture) policy(name string, mode domain.RetroactivityMode, lookback int32, from time.Time, to *time.Time) *domain.RecoveryRetroactivityPolicy {
	return &domain.RecoveryRetroactivityPolicy{
		ID:                newID("rtp"),
		TenantID:          f.tenantID,
		Name:              name,
		Mode:              mode,
		MaxLookbackDays:   lookback,
		MinimumAdjustment: money.Money{Value: 2500, Scale: 2, Currency: "INR"},
		EffectiveFrom:     from,
		EffectiveTo:       to,
		CreatedBy:         "tester",
	}
}

func TestSetPoolStatusMovesThePoolThroughItsLifecycle(t *testing.T) {
	f := setup(t)
	// SetPoolStatus now records what the status was, and an audit entry refuses
	// to be written without a tenant and an actor to attribute it to. A bare
	// context here is the path a gateway never produces.
	ctx := tenantctx.WithActor(
		tenantdb.WithTenant(context.Background(), f.tenantID),
		tenantctx.Actor{ID: "integration-test"})
	p := f.pool(t)

	if p.Status != domain.PoolOpen {
		t.Fatalf("a new pool is %s, want OPEN", p.Status)
	}
	valued, err := f.repo.SetPoolStatus(ctx, f.tenantID, p.ID, domain.PoolValued, "auditor-1")
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if valued.Status != domain.PoolValued {
		t.Errorf("status = %s, want VALUED", valued.Status)
	}
	if valued.UpdatedBy != "auditor-1" {
		t.Errorf("the status change was attributed to %q, want auditor-1", valued.UpdatedBy)
	}

	if _, err := f.repo.SetPoolStatus(ctx, f.tenantID, newID("pol"), domain.PoolSettled, "auditor-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("setting the status of a pool that does not exist gave %v, want ErrNotFound", err)
	}
}

func TestListPoolsFiltersByPeriod(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	january := f.pool(t)
	march, err := f.repo.CreatePool(ctx, &domain.Pool{
		ID:          newID("pol"),
		TenantID:    f.tenantID,
		Name:        "march-2026",
		PeriodStart: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Unit:        "LITRE",
		Currency:    "INR",
		Scale:       2,
		CreatedBy:   "tester",
	})
	if err != nil {
		t.Fatalf("create march pool: %v", err)
	}

	all, err := f.repo.ListPools(ctx, f.tenantID, time.Time{}, time.Time{}, 10, 0)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("an unfiltered listing returned %d pools, want 2", len(all))
	}

	q1, err := f.repo.ListPools(ctx, f.tenantID,
		time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 10, 0)
	if err != nil {
		t.Fatalf("list pools by period: %v", err)
	}
	if len(q1) != 1 {
		t.Fatalf("the period filter returned %d pools, want only march", len(q1))
	}
	if q1[0].ID != march.ID {
		t.Errorf("the period filter returned %s, want march %s", q1[0].ID, march.ID)
	}
	if q1[0].ID == january.ID {
		t.Error("a pool that closed before the window was returned")
	}
}

func TestTenantIsolation(t *testing.T) {
	a := setup(t)
	b := setup(t)
	ctx := b.acting()

	p, allocations, events := a.settled(t)

	if _, err := b.repo.GetPool(ctx, b.tenantID, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant read another tenant's pool: %v", err)
	}
	if _, err := b.repo.GetValuation(ctx, b.tenantID, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant read another tenant's valuation: %v", err)
	}

	milk, err := b.repo.ListProducerMilk(ctx, b.tenantID, p.ID)
	if err != nil {
		t.Fatalf("list producer milk: %v", err)
	}
	if len(milk) != 0 {
		t.Errorf("listing leaked %d rows of another tenant's producer milk", len(milk))
	}

	leaked, err := b.repo.ListAllocations(ctx, b.tenantID, p.ID, allocations[0].ValuationID)
	if err != nil {
		t.Fatalf("list allocations: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("listing leaked %d of another tenant's allocations", len(leaked))
	}

	otherEvents, err := b.repo.ListEconomicEvents(ctx, b.tenantID, "", "", 100, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, e := range otherEvents {
		if e.TenantID != b.tenantID {
			t.Errorf("listing leaked event %s belonging to tenant %s", e.ID, e.TenantID)
		}
	}
	if len(events) == 0 {
		t.Fatal("the owning tenant has no events, so the isolation check proves nothing")
	}

	if _, err := a.repo.CreateRetroactivityPolicy(ctx, a.policy("a", domain.RetroRecalculate, 30, y2020, nil)); err != nil {
		t.Fatalf("create policy for tenant a: %v", err)
	}
	// The exclusion constraint is keyed by tenant, so another tenant's policy
	// over the same period is not an overlap.
	if _, err := b.repo.CreateRetroactivityPolicy(ctx, b.policy("b", domain.RetroRecalculate, 30, y2020, nil)); err != nil {
		t.Fatalf("a second tenant's policy over the same period was refused: %v", err)
	}
	if _, err := b.repo.GetEffectiveRetroactivityPolicy(ctx, b.tenantID, y2026); err != nil {
		t.Fatalf("tenant b cannot read its own policy: %v", err)
	}
}
