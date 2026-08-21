//go:build dbintegration

// Repository tests against a real PostgreSQL.
//
// The slot rules are unit tested as a pure function elsewhere. What can only be
// tested here is whether the schema enforces what those rules assume: that an
// external identifier resolves to one entity at any instant while still being
// reusable over time, and that a conflicted slot cannot name a holder.
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

	"github.com/ppusapati/gavya/libs/integrity/bitemporal"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/canonical-service/internal/domain"
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
	y2020 = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	y2023 = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	y2026 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

type fixture struct {
	repo     Repository
	tenantID string
	sourceID string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return &fixture{repo: New(pool(t)), tenantID: newID("tnt"), sourceID: newID("src")}
}

func (f *fixture) identity(entityID string, from, to time.Time) *domain.ExternalIdentity {
	return &domain.ExternalIdentity{
		ID:             newID("idn"),
		TenantID:       f.tenantID,
		SourceSystemID: f.sourceID,
		EntityKind:     domain.EntityProducer,
		ExternalID:     "P-001",
		EntityID:       entityID,
		Method:         domain.MappingExact,
		ValidFrom:      from,
		ValidTo:        to,
		CreatedBy:      "tester",
	}
}

func (f *fixture) policy(t *testing.T, mode domain.ResolutionMode) *domain.CollectionIdentityPolicy {
	t.Helper()
	p, err := f.repo.CreatePolicy(context.Background(), &domain.CollectionIdentityPolicy{
		ID:       newID("pol"),
		TenantID: f.tenantID,
		Name:     "default",
		Dimensions: []domain.IdentityDimension{
			domain.DimProducer, domain.DimCollectionDate, domain.DimShift,
		},
		Resolution:    mode,
		Version:       1,
		EffectiveFrom: y2020,
		CreatedBy:     "tester",
	})
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	return p
}

func claim(ref string, at time.Time, quality int32) domain.Claim {
	return domain.Claim{
		SourceRef: ref,
		Values: map[domain.IdentityDimension]string{
			domain.DimProducer:       "P-001",
			domain.DimCollectionDate: "2026-02-14",
			domain.DimShift:          "MORNING",
		},
		Origin:     origin.Native,
		RecordedAt: at,
		Quality:    quality,
	}
}

func (f *fixture) place(t *testing.T, p *domain.CollectionIdentityPolicy, c domain.Claim) *ClaimResult {
	t.Helper()
	slotKey, err := domain.SlotKey(p, c)
	if err != nil {
		t.Fatalf("slot key: %v", err)
	}
	res, err := f.repo.ClaimSlot(context.Background(), ClaimInput{
		TenantID: f.tenantID,
		SlotKey:  slotKey,
		Claim:    c,
		Policy:   p,
		Actor:    "tester",
		SlotID:   newID("slt"),
	}, domain.PlaceClaim)
	if err != nil {
		t.Fatalf("claim %s: %v", c.SourceRef, err)
	}
	return res
}

// An identifier reused over time is legitimate; two mappings covering the same
// instant are not.
func TestIdentifierMayBeReusedButNeverOverlap(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := f.repo.CreateIdentity(ctx, f.identity("prodA", y2020, y2023)); err != nil {
		t.Fatalf("first mapping: %v", err)
	}
	if _, err := f.repo.CreateIdentity(ctx, f.identity("prodB", y2023, bitemporal.EndOfTime)); err != nil {
		t.Fatalf("non-overlapping reissue was refused: %v", err)
	}

	overlapping := f.identity("prodC", time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC), y2026)
	if _, err := f.repo.CreateIdentity(ctx, overlapping); !errors.Is(err, ErrOverlappingIdentity) {
		t.Fatalf("got %v, want ErrOverlappingIdentity", err)
	}
}

func TestResolveIdentityAnswersForTheInstantAsked(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := f.repo.CreateIdentity(ctx, f.identity("prodA", y2020, y2023)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.repo.CreateIdentity(ctx, f.identity("prodB", y2023, bitemporal.EndOfTime)); err != nil {
		t.Fatalf("create: %v", err)
	}

	early, err := f.repo.ResolveIdentity(ctx, f.tenantID, f.sourceID, domain.EntityProducer, "P-001", time.Date(2021, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolve early: %v", err)
	}
	if early.EntityID != "prodA" {
		t.Errorf("2021 resolved to %s, want prodA", early.EntityID)
	}

	late, err := f.repo.ResolveIdentity(ctx, f.tenantID, f.sourceID, domain.EntityProducer, "P-001", y2026)
	if err != nil {
		t.Fatalf("resolve late: %v", err)
	}
	if late.EntityID != "prodB" {
		t.Errorf("2026 resolved to %s, want prodB", late.EntityID)
	}

	// The boundary is half-open: the instant the second mapping begins belongs
	// to the second mapping.
	boundary, err := f.repo.ResolveIdentity(ctx, f.tenantID, f.sourceID, domain.EntityProducer, "P-001", y2023)
	if err != nil {
		t.Fatalf("resolve boundary: %v", err)
	}
	if boundary.EntityID != "prodB" {
		t.Errorf("the changeover instant resolved to %s, want prodB", boundary.EntityID)
	}

	if _, err := f.repo.ResolveIdentity(ctx, f.tenantID, f.sourceID, domain.EntityProducer, "P-001", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrNotFound) {
		t.Errorf("an instant before any mapping resolved: %v", err)
	}
}

// A settlement computed under an old mapping must remain explainable, so
// retiring a mapping leaves the row readable.
func TestRetiredIdentityStopsResolvingButSurvives(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	in, err := f.repo.CreateIdentity(ctx, f.identity("prodA", y2020, bitemporal.EndOfTime))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.repo.SupersedeIdentity(ctx, f.tenantID, in.ID, "corrected"); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	if _, err := f.repo.ResolveIdentity(ctx, f.tenantID, f.sourceID, domain.EntityProducer, "P-001", y2026); !errors.Is(err, ErrNotFound) {
		t.Errorf("a retired mapping still resolves: %v", err)
	}

	// Retiring frees the interval, so a corrected mapping can take it.
	if _, err := f.repo.CreateIdentity(ctx, f.identity("prodB", y2020, bitemporal.EndOfTime)); err != nil {
		t.Fatalf("the corrected mapping was refused: %v", err)
	}
}

func TestReverseResolveListsEveryIdentifierForAnEntity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	if _, err := f.repo.CreateIdentity(ctx, f.identity("prodA", y2020, y2023)); err != nil {
		t.Fatalf("create: %v", err)
	}
	second := f.identity("prodA", y2023, bitemporal.EndOfTime)
	second.ExternalID = "P-999"
	if _, err := f.repo.CreateIdentity(ctx, second); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := f.repo.ReverseResolve(ctx, f.tenantID, domain.EntityProducer, "prodA")
	if err != nil {
		t.Fatalf("reverse resolve: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d identifiers, want 2", len(list))
	}
}

// Two policies in force at once would make a collection's slot key ambiguous.
func TestOnlyOnePolicyMayBeInForce(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	f.policy(t, domain.ResolveFirstWins)

	overlapping := &domain.CollectionIdentityPolicy{
		ID:            newID("pol"),
		TenantID:      f.tenantID,
		Name:          "second",
		Dimensions:    []domain.IdentityDimension{domain.DimProducer, domain.DimCollectionDate},
		Resolution:    domain.ResolveLastWins,
		Version:       1,
		EffectiveFrom: y2023,
		CreatedBy:     "tester",
	}
	if _, err := f.repo.CreatePolicy(ctx, overlapping); !errors.Is(err, ErrOverlappingPolicy) {
		t.Fatalf("got %v, want ErrOverlappingPolicy", err)
	}
}

func TestPolicyMustIdentifyAProducerAtTheDatabase(t *testing.T) {
	f := setup(t)

	// The domain validates this too; the constraint is the backstop for anything
	// that reaches the table another way.
	_, err := f.repo.CreatePolicy(context.Background(), &domain.CollectionIdentityPolicy{
		ID:            newID("pol"),
		TenantID:      f.tenantID,
		Name:          "no-producer",
		Dimensions:    []domain.IdentityDimension{domain.DimCollectionDate, domain.DimShift},
		Resolution:    domain.ResolveFirstWins,
		Version:       1,
		EffectiveFrom: y2020,
		CreatedBy:     "tester",
	})
	if err == nil {
		t.Fatal("a policy with no producer dimension was stored")
	}
}

func TestClaimEstablishesAndReasserts(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveFirstWins)

	first := f.place(t, p, claim("obs-1", y2026, 0))
	if first.Decision.Outcome != domain.OutcomeEstablished {
		t.Fatalf("got %s, want SLOT_ESTABLISHED", first.Decision.Outcome)
	}
	if first.Slot.AuthoritativeRef != "obs-1" {
		t.Errorf("authoritative = %q, want obs-1", first.Slot.AuthoritativeRef)
	}

	again := f.place(t, p, claim("obs-1", y2026, 0))
	if again.Decision.Outcome != domain.OutcomeReasserted {
		t.Fatalf("got %s, want SLOT_REASSERTED", again.Decision.Outcome)
	}
	if again.Slot.ID != first.Slot.ID {
		t.Error("reasserting created a second slot")
	}
}

func TestClaimIsIdempotentOverManyReassertions(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveFirstWins)

	first := f.place(t, p, claim("obs-1", y2026, 0))
	for i := 0; i < 20; i++ {
		res := f.place(t, p, claim("obs-1", y2026, 0))
		if res.Decision.Outcome != domain.OutcomeReasserted {
			t.Fatalf("iteration %d: got %s", i, res.Decision.Outcome)
		}
		if res.Slot.AuthoritativeRef != first.Slot.AuthoritativeRef {
			t.Fatalf("iteration %d: the holder changed", i)
		}
		if len(res.Slot.Contenders) != 0 {
			t.Fatalf("iteration %d: reasserting accumulated %d contenders", i, len(res.Slot.Contenders))
		}
	}
}

func TestFirstWinsRetainsAndRecordsTheContender(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveFirstWins)

	f.place(t, p, claim("obs-early", y2026, 0))
	res := f.place(t, p, claim("obs-late", y2026.Add(time.Hour), 0))

	if res.Decision.Outcome != domain.OutcomeRetained {
		t.Fatalf("got %s, want SLOT_RETAINED: %s", res.Decision.Outcome, res.Decision.Reason)
	}
	if res.Slot.AuthoritativeRef != "obs-early" {
		t.Errorf("authoritative = %q, want obs-early", res.Slot.AuthoritativeRef)
	}
	if len(res.Slot.Contenders) != 1 || res.Slot.Contenders[0].SourceRef != "obs-late" {
		t.Errorf("contenders = %+v, want the losing claim recorded", res.Slot.Contenders)
	}
	if res.Slot.Contenders[0].Reason == "" {
		t.Error("the contender carries no reason a reviewer could read")
	}
}

func TestLastWinsReplacesAndDemotesTheIncumbent(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveLastWins)

	f.place(t, p, claim("obs-early", y2026, 0))
	res := f.place(t, p, claim("obs-late", y2026.Add(time.Hour), 0))

	if res.Decision.Outcome != domain.OutcomeReplaced {
		t.Fatalf("got %s, want SLOT_REPLACED: %s", res.Decision.Outcome, res.Decision.Reason)
	}
	if res.Slot.AuthoritativeRef != "obs-late" {
		t.Errorf("authoritative = %q, want obs-late", res.Slot.AuthoritativeRef)
	}
	if !res.Slot.IncumbentRecordedAt.Equal(y2026.Add(time.Hour)) {
		t.Errorf("incumbent time = %s, want the new holder's", res.Slot.IncumbentRecordedAt)
	}
	if len(res.Slot.Contenders) != 1 || res.Slot.Contenders[0].SourceRef != "obs-early" {
		t.Errorf("contenders = %+v, want the displaced incumbent recorded", res.Slot.Contenders)
	}
}

// A conflicted slot must not name a holder: leaving one in place would let a
// downstream settlement quietly consume a collection under dispute.
func TestConflictClearsTheHolderAndKeepsBothClaims(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveManual)

	f.place(t, p, claim("obs-1", y2026, 0))
	res := f.place(t, p, claim("obs-2", y2026.Add(time.Hour), 0))

	if res.Decision.Outcome != domain.OutcomeConflict {
		t.Fatalf("got %s, want COLLECTION_SLOT_CONFLICT", res.Decision.Outcome)
	}
	if res.Slot.Status != domain.SlotConflict {
		t.Errorf("status = %s, want CONFLICT", res.Slot.Status)
	}
	if res.Slot.AuthoritativeRef != "" {
		t.Errorf("a conflicted slot still names %q as authoritative", res.Slot.AuthoritativeRef)
	}

	refs := map[string]bool{}
	for _, c := range res.Slot.Contenders {
		refs[c.SourceRef] = true
	}
	if !refs["obs-1"] || !refs["obs-2"] {
		t.Errorf("both claims must be kept; contenders = %+v", res.Slot.Contenders)
	}
}

func TestConflictAppearsInTheTriageQueueAndResolves(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := f.policy(t, domain.ResolveManual)

	f.place(t, p, claim("obs-1", y2026, 0))
	conflicted := f.place(t, p, claim("obs-2", y2026.Add(time.Hour), 0))

	open, err := f.repo.ListConflicts(ctx, f.tenantID, 10, 0)
	if err != nil {
		t.Fatalf("list conflicts: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(open))
	}

	resolved, err := f.repo.ResolveConflict(ctx, f.tenantID, conflicted.Slot.ID, "obs-1",
		"operator re-entered the collection; the original stands", "auditor-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.SlotSettled {
		t.Errorf("status = %s, want SETTLED", resolved.Status)
	}
	if resolved.AuthoritativeRef != "obs-1" {
		t.Errorf("authoritative = %q, want obs-1", resolved.AuthoritativeRef)
	}
	if resolved.ResolvedBy != "auditor-1" || resolved.ResolvedAt == nil {
		t.Error("the resolution was not attributed")
	}
	if len(resolved.Contenders) == 0 {
		t.Error("resolving discarded the record of what was set aside")
	}

	after, err := f.repo.ListConflicts(ctx, f.tenantID, 10, 0)
	if err != nil {
		t.Fatalf("list after resolve: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("%d conflicts still open after resolution", len(after))
	}
}

// In shadow mode an imported collection and the platform's own sit side by
// side; forcing them into one slot would report the entire import as a conflict.
func TestNativeAndImportedClaimsOccupySeparateSlots(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveManual)

	native := claim("obs-native", y2026, 0)
	imported := claim("rec-imported", y2026, 0)
	imported.Origin = origin.Imported

	nativeRes := f.place(t, p, native)
	importedRes := f.place(t, p, imported)

	if nativeRes.Decision.Outcome != domain.OutcomeEstablished {
		t.Fatalf("native claim: got %s, want SLOT_ESTABLISHED", nativeRes.Decision.Outcome)
	}
	if importedRes.Decision.Outcome != domain.OutcomeEstablished {
		t.Fatalf("imported claim: got %s, want SLOT_ESTABLISHED (not a conflict with the native one)",
			importedRes.Decision.Outcome)
	}
	if nativeRes.Slot.ID == importedRes.Slot.ID {
		t.Error("native and imported claims shared a slot")
	}
	if nativeRes.Slot.SlotKey != importedRes.Slot.SlotKey {
		t.Error("the same collection produced different slot keys across origins")
	}
}

// Two claims for the same collection must not both see an empty slot and both
// take it.
func TestConcurrentClaimsResolveToOneHolder(t *testing.T) {
	f := setup(t)
	p := f.policy(t, domain.ResolveFirstWins)

	slotKey, err := domain.SlotKey(p, claim("x", y2026, 0))
	if err != nil {
		t.Fatal(err)
	}

	const racers = 8
	var wg sync.WaitGroup
	results := make([]*ClaimResult, racers)
	errs := make([]error, racers)

	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c := claim(fmt.Sprintf("obs-%d", i), y2026.Add(time.Duration(i)*time.Minute), 0)
			res, err := f.repo.ClaimSlot(context.Background(), ClaimInput{
				TenantID: f.tenantID,
				SlotKey:  slotKey,
				Claim:    c,
				Policy:   p,
				Actor:    "tester",
				SlotID:   newID("slt"),
			}, domain.PlaceClaim)
			results[i], errs[i] = res, err
		}(i)
	}
	close(start)
	wg.Wait()

	var established int
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if results[i].Decision.Outcome == domain.OutcomeEstablished {
			established++
		}
	}
	if established != 1 {
		t.Fatalf("%d racers established the slot, want exactly 1", established)
	}

	final, err := f.repo.GetSlot(context.Background(), f.tenantID, slotKey, origin.Native)
	if err != nil {
		t.Fatalf("get slot: %v", err)
	}
	// FIRST_WINS with distinct times must settle on the earliest claim.
	if final.Status == domain.SlotSettled && final.AuthoritativeRef != "obs-0" {
		t.Errorf("authoritative = %q, want obs-0 under FIRST_WINS", final.AuthoritativeRef)
	}
}

func TestTenantIsolation(t *testing.T) {
	a := setup(t)
	b := setup(t)
	ctx := context.Background()

	if _, err := a.repo.CreateIdentity(ctx, a.identity("prodA", y2020, bitemporal.EndOfTime)); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := b.repo.ResolveIdentity(ctx, b.tenantID, a.sourceID, domain.EntityProducer, "P-001", y2026); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant resolved another tenant's mapping: %v", err)
	}

	list, err := b.repo.ListIdentities(ctx, b.tenantID, "", 100, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, i := range list {
		if i.TenantID != b.tenantID {
			t.Errorf("listing leaked a mapping belonging to tenant %s", i.TenantID)
		}
	}
}
