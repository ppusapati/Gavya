//go:build dbintegration

// Repository tests against a real PostgreSQL.
//
// The classifier is unit tested as a pure function elsewhere. What can only be
// tested here is whether the schema delivers what the service assumes: that a
// replayed import is refused by the unique index, that superseding leaves both
// versions readable, and that components survive the JSONB round trip.
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
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/domain"
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

func inr(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, 2, "INR")
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return m
}

type fixture struct {
	repo     Repository
	tenantID string
	sourceID string
	batchID  string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return &fixture{
		repo:     New(pool(t)),
		tenantID: newID("tnt"),
		sourceID: newID("src"),
		batchID:  newID("bat"),
	}
}

func (f *fixture) assertion(t *testing.T, externalID, sourceRecordID, total string, comps ...domain.Component) *domain.ExternalSettlementAssertion {
	t.Helper()
	o, err := origin.NewImported(f.sourceID, f.batchID, sourceRecordID, origin.HashPayload([]byte(total)))
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	return &domain.ExternalSettlementAssertion{
		ID:                   newID("asr"),
		TenantID:             f.tenantID,
		SourceSystemID:       f.sourceID,
		ExternalSettlementID: externalID,
		ProducerRef:          "producer:01",
		PeriodStart:          time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:            time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		Total:                inr(t, total),
		Components:           comps,
		AssertedAt:           time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		Origin:               o,
		ValidFrom:            time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:              bitemporal.EndOfTime,
		CreatedBy:            "tester",
	}
}

func (f *fixture) computation(t *testing.T, total string, comps ...domain.Component) *domain.ShadowSettlementComputation {
	t.Helper()
	o, err := origin.NewDerived(newID("drv"))
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	return &domain.ShadowSettlementComputation{
		ID:            newID("shd"),
		TenantID:      f.tenantID,
		ProducerRef:   "producer:01",
		PeriodStart:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:     time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		Total:         inr(t, total),
		Components:    comps,
		PolicyVersion: "policy-2026.02",
		RateCardID:    newID("rct"),
		RoundingTrail: []money.RoundingStep{{
			Operation: "MUL_RATE", Mode: money.RoundHalfUp,
			FromScale: 5, ToScale: 2, Discarded: 3,
			Result: inr(t, total),
		}},
		InputDigest: "sha256:inputs",
		AsOf:        time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		Origin:      o,
		ComputedAt:  time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		CreatedBy:   "tester",
	}
}

// newPair creates a distinct assertion and computation. The schema allows one
// divergence per pair, so a test needing several divergences needs several
// pairs.
func (f *fixture) newPair(t *testing.T, n int) (string, string) {
	t.Helper()
	ctx := context.Background()

	a := f.assertion(t, fmt.Sprintf("EXT-%d", n), fmt.Sprintf("REC-%d", n), "1000.00")
	a.ProducerRef = fmt.Sprintf("producer:%02d", n)
	assertion, err := f.repo.CreateAssertion(ctx, a)
	if err != nil {
		t.Fatalf("create assertion %d: %v", n, err)
	}

	c := f.computation(t, "940.00")
	c.ProducerRef = a.ProducerRef
	computation, err := f.repo.CreateComputation(ctx, c)
	if err != nil {
		t.Fatalf("create computation %d: %v", n, err)
	}
	return assertion.ID, computation.ID
}

func comp(t *testing.T, kind domain.ComponentKind, amount, quantity, rate string) domain.Component {
	t.Helper()
	return domain.Component{Kind: kind, Amount: inr(t, amount), Quantity: quantity, Rate: rate}
}

func TestAssertionRoundTripsComponentsAndOrigin(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	in := f.assertion(t, "EXT-1", "REC-1", "1000.00",
		comp(t, domain.ComponentBasePrice, "1050.00", "320.000", "3.2812"),
		comp(t, domain.ComponentLoanRecovery, "-50.00", "", ""),
	)

	out, err := f.repo.CreateAssertion(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := f.repo.GetAssertion(ctx, out.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Total.String() != "1000.00" || got.Total.Currency != "INR" {
		t.Errorf("total = %s %s, want INR 1000.00", got.Total.Currency, got.Total)
	}
	if len(got.Components) != 2 {
		t.Fatalf("got %d components, want 2", len(got.Components))
	}
	if got.Components[0].Quantity != "320.000" || got.Components[0].Rate != "3.2812" {
		t.Errorf("quantity/rate did not survive the round trip: %+v", got.Components[0])
	}
	if got.Components[0].Amount.String() != "1050.00" {
		t.Errorf("component amount = %s, want 1050.00", got.Components[0].Amount)
	}
	if !got.Origin.IsImported() {
		t.Errorf("origin kind = %s, want IMPORTED", got.Origin.Kind)
	}
	if got.Origin.SourcePayloadHash != in.Origin.SourcePayloadHash {
		t.Error("payload hash did not survive the round trip")
	}
	if err := got.Origin.Validate(); err != nil {
		t.Errorf("reloaded origin is not valid: %v", err)
	}
	if !got.ValidTo.Equal(bitemporal.EndOfTime) {
		t.Errorf("valid_to = %s, want the end-of-time sentinel", got.ValidTo)
	}
}

// Replaying an import must be refused by the database, not merely by the
// service's pre-check, so a race cannot admit the same record twice.
func TestDuplicatePayloadIsRefusedByTheDatabase(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	first := f.assertion(t, "EXT-1", "REC-1", "1000.00")
	if _, err := f.repo.CreateAssertion(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Same source record, same payload hash, different id: a replay.
	replay := f.assertion(t, "EXT-1", "REC-1", "1000.00")
	_, err := f.repo.CreateAssertion(ctx, replay)
	if !errors.Is(err, ErrDuplicateAssertion) {
		t.Fatalf("got %v, want ErrDuplicateAssertion", err)
	}
}

func TestFindByPayloadLocatesAReplay(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	in := f.assertion(t, "EXT-1", "REC-1", "1000.00")
	out, err := f.repo.CreateAssertion(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	found, err := f.repo.FindAssertionByPayload(ctx, f.tenantID, f.sourceID, "REC-1", in.Origin.SourcePayloadHash)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.ID != out.ID {
		t.Errorf("found %s, want %s", found.ID, out.ID)
	}

	if _, err := f.repo.FindAssertionByPayload(ctx, f.tenantID, f.sourceID, "REC-1", "sha256:different"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a different payload hash matched: %v", err)
	}
}

// An amendment supersedes rather than overwrites: both versions stay readable,
// which is what makes "as we knew it on date X" answerable.
func TestAmendmentSupersedesAndBothVersionsRemain(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	v1, err := f.repo.CreateAssertion(ctx, f.assertion(t, "EXT-1", "REC-1", "1000.00"))
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}

	v2in := f.assertion(t, "EXT-1", "REC-2", "1100.00")
	if err := f.repo.SupersedeAssertion(ctx, f.tenantID, f.sourceID, "EXT-1", v2in.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	v2, err := f.repo.CreateAssertion(ctx, v2in)
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}

	oldVersion, err := f.repo.GetAssertion(ctx, v1.ID, f.tenantID)
	if err != nil {
		t.Fatalf("the superseded version is no longer readable: %v", err)
	}
	if oldVersion.SupersededAt == nil {
		t.Error("v1 was not marked superseded")
	}
	if oldVersion.SupersededBy != v2.ID {
		t.Errorf("v1 superseded_by = %q, want %s", oldVersion.SupersededBy, v2.ID)
	}
	if oldVersion.Total.String() != "1000.00" {
		t.Errorf("the superseded version's total was mutated to %s", oldVersion.Total)
	}

	// Only the live version is listed.
	live, err := f.repo.ListAssertions(ctx, f.tenantID, "producer:01", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("got %d live assertions, want 1", len(live))
	}
	if live[0].ID != v2.ID {
		t.Errorf("live assertion is %s, want %s", live[0].ID, v2.ID)
	}
}

func TestComputationRoundTripsRoundingTrail(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	in := f.computation(t, "1000.00", comp(t, domain.ComponentBasePrice, "1000.00", "300.000", "3.3333"))
	out, err := f.repo.CreateComputation(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := f.repo.GetComputation(ctx, out.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.RoundingTrail) != 1 {
		t.Fatalf("got %d rounding steps, want 1", len(got.RoundingTrail))
	}
	if got.RoundingTrail[0].Mode != money.RoundHalfUp {
		t.Errorf("rounding mode = %s, want HALF_UP", got.RoundingTrail[0].Mode)
	}
	if got.RoundingTrail[0].Discarded != 3 {
		t.Errorf("discarded = %d, want 3", got.RoundingTrail[0].Discarded)
	}
	if got.InputDigest != "sha256:inputs" {
		t.Errorf("input digest = %q", got.InputDigest)
	}
	if !got.Origin.IsDerived() {
		t.Errorf("origin kind = %s, want DERIVED", got.Origin.Kind)
	}
}

// The end-to-end path: ingest, compute, classify, persist, read back.
func TestDivergenceRoundTripAndTriage(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	assertion, err := f.repo.CreateAssertion(ctx, f.assertion(t, "EXT-1", "REC-1", "1050.00",
		comp(t, domain.ComponentBasePrice, "1050.00", "300.000", "3.5000")))
	if err != nil {
		t.Fatalf("create assertion: %v", err)
	}
	computation, err := f.repo.CreateComputation(ctx, f.computation(t, "1000.00",
		comp(t, domain.ComponentBasePrice, "1000.00", "300.000", "3.3333")))
	if err != nil {
		t.Fatalf("create computation: %v", err)
	}

	verdict := domain.Classify(assertion, computation)
	if verdict.Classification != domain.ClassPolicy {
		t.Fatalf("classification = %s, want POLICY_DIFFERENCE: %s", verdict.Classification, verdict.Rationale)
	}

	stored, err := f.repo.CreateDivergence(ctx, &domain.SettlementDivergence{
		ID:             newID("dvg"),
		TenantID:       f.tenantID,
		AssertionID:    assertion.ID,
		ComputationID:  computation.ID,
		ProducerRef:    assertion.ProducerRef,
		Delta:          verdict.Delta,
		Classification: verdict.Classification,
		Rationale:      verdict.Rationale,
		Evidence:       verdict.Evidence,
		Status:         domain.StatusOpen,
		CreatedBy:      "tester",
		UpdatedBy:      "tester",
	})
	if err != nil {
		t.Fatalf("create divergence: %v", err)
	}

	got, err := f.repo.GetDivergence(ctx, stored.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Delta.Value != 5000 {
		t.Errorf("delta = %d minor units, want 5000", got.Delta.Value)
	}
	if len(got.Evidence) == 0 {
		t.Error("evidence did not survive the round trip")
	}
	if !got.NeedsHumanReview() {
		t.Error("a policy difference must reach a reviewer")
	}

	// Triage listing.
	open, err := f.repo.ListDivergences(ctx, DivergenceFilter{
		TenantID: f.tenantID, Status: "OPEN", MinAbsDelta: 1, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("got %d open divergences, want 1", len(open))
	}

	// A threshold above the delta hides it.
	quiet, err := f.repo.ListDivergences(ctx, DivergenceFilter{
		TenantID: f.tenantID, MinAbsDelta: 100000, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list with threshold: %v", err)
	}
	if len(quiet) != 0 {
		t.Errorf("got %d divergences above the threshold, want 0", len(quiet))
	}
}

// The ML tier is advisory: it may attach hypotheses only to UNEXPLAINED, and
// it can never change the classification.
func TestHypothesesAttachOnlyToUnexplained(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	hypotheses := []domain.MLHypothesis{{
		Classification: "INPUT_DIFFERENCE",
		Confidence:     0.71,
		Rationale:      "quantity movement accounts for most of the delta",
		ModelVersion:   "divergence-knn-evidence-1.0.0",
	}}

	assertionA, computationA := f.newPair(t, 1)
	unexplained, err := f.repo.CreateDivergence(ctx, &domain.SettlementDivergence{
		ID: newID("dvg"), TenantID: f.tenantID,
		AssertionID: assertionA, ComputationID: computationA,
		ProducerRef: "producer:01", Delta: inr(t, "60.00"),
		Classification: domain.ClassUnexplained,
		Rationale:      "no attribution found",
		Status:         domain.StatusOpen,
		CreatedBy:      "tester", UpdatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create unexplained: %v", err)
	}

	if err := f.repo.AttachHypotheses(ctx, unexplained.ID, f.tenantID, hypotheses, "tester"); err != nil {
		t.Fatalf("attach to unexplained: %v", err)
	}

	got, err := f.repo.GetDivergence(ctx, unexplained.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.MLHypotheses) != 1 {
		t.Fatalf("got %d hypotheses, want 1", len(got.MLHypotheses))
	}
	if got.Classification != domain.ClassUnexplained {
		t.Errorf("attaching a hypothesis changed the classification to %s", got.Classification)
	}

	// A deterministically classified divergence refuses hypotheses outright.
	assertionB, computationB := f.newPair(t, 2)
	classified, err := f.repo.CreateDivergence(ctx, &domain.SettlementDivergence{
		ID: newID("dvg"), TenantID: f.tenantID,
		AssertionID: assertionB, ComputationID: computationB,
		ProducerRef: "producer:02", Delta: inr(t, "60.00"),
		Classification: domain.ClassInput,
		Rationale:      "quantity differs",
		Status:         domain.StatusOpen,
		CreatedBy:      "tester", UpdatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create classified: %v", err)
	}
	if err := f.repo.AttachHypotheses(ctx, classified.ID, f.tenantID, hypotheses, "tester"); !errors.Is(err, ErrNotFound) {
		t.Errorf("hypotheses were attached to a deterministically classified divergence: %v", err)
	}
}

func TestResolveRecordsTheDecisionWithoutChangingTheFinding(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	assertion, err := f.repo.CreateAssertion(ctx, f.assertion(t, "EXT-1", "REC-1", "1000.00"))
	if err != nil {
		t.Fatalf("create assertion: %v", err)
	}
	computation, err := f.repo.CreateComputation(ctx, f.computation(t, "940.00"))
	if err != nil {
		t.Fatalf("create computation: %v", err)
	}

	d, err := f.repo.CreateDivergence(ctx, &domain.SettlementDivergence{
		ID: newID("dvg"), TenantID: f.tenantID,
		AssertionID: assertion.ID, ComputationID: computation.ID,
		ProducerRef: "producer:01", Delta: inr(t, "60.00"),
		Classification: domain.ClassUnexplained,
		Rationale:      "no attribution found",
		Status:         domain.StatusOpen,
		CreatedBy:      "tester", UpdatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	resolved, err := f.repo.ResolveDivergence(ctx, d.ID, f.tenantID, domain.StatusShadowWins,
		"source system omitted a late collection; shadow total is correct", "auditor-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.StatusShadowWins {
		t.Errorf("status = %s, want SHADOW_CONFIRMED", resolved.Status)
	}
	if resolved.ResolvedBy != "auditor-1" || resolved.ResolvedAt == nil {
		t.Error("the resolution was not attributed")
	}
	if resolved.Classification != domain.ClassUnexplained {
		t.Errorf("resolving changed the classification to %s", resolved.Classification)
	}
}

func TestSummariseGroupsByClassification(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	for i, spec := range []struct {
		class domain.Classification
		delta string
	}{
		{domain.ClassUnexplained, "60.00"},
		{domain.ClassUnexplained, "40.00"},
		{domain.ClassRounding, "0.01"},
	} {
		assertionID, computationID := f.newPair(t, i+1)
		if _, err := f.repo.CreateDivergence(ctx, &domain.SettlementDivergence{
			ID: newID("dvg"), TenantID: f.tenantID,
			AssertionID: assertionID, ComputationID: computationID,
			ProducerRef:    fmt.Sprintf("producer:%02d", i+1),
			Delta:          inr(t, spec.delta),
			Classification: spec.class,
			Rationale:      "test",
			Status:         domain.StatusOpen,
			CreatedBy:      "tester", UpdatedBy: "tester",
		}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	summaries, err := f.repo.SummariseDivergences(ctx, f.tenantID, from, to)
	if err != nil {
		t.Fatalf("summarise: %v", err)
	}

	byClass := map[domain.Classification]domain.ClassSummary{}
	for _, s := range summaries {
		byClass[s.Classification] = s
	}
	if got := byClass[domain.ClassUnexplained]; got.Count != 2 || got.TotalAbsMinorUnits != 10000 {
		t.Errorf("unexplained summary = %+v, want count 2 and 10000 minor units", got)
	}
	if got := byClass[domain.ClassRounding]; got.Count != 1 {
		t.Errorf("rounding summary = %+v, want count 1", got)
	}
}

// A MATCH with a non-zero delta is a contradiction the database refuses.
func TestMatchWithANonZeroDeltaIsRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	assertion, err := f.repo.CreateAssertion(ctx, f.assertion(t, "EXT-1", "REC-1", "1000.00"))
	if err != nil {
		t.Fatalf("create assertion: %v", err)
	}
	computation, err := f.repo.CreateComputation(ctx, f.computation(t, "940.00"))
	if err != nil {
		t.Fatalf("create computation: %v", err)
	}

	_, err = f.repo.CreateDivergence(ctx, &domain.SettlementDivergence{
		ID: newID("dvg"), TenantID: f.tenantID,
		AssertionID: assertion.ID, ComputationID: computation.ID,
		ProducerRef: "producer:01", Delta: inr(t, "60.00"),
		Classification: domain.ClassMatch,
		Rationale:      "contradictory",
		Status:         domain.StatusOpen,
		CreatedBy:      "tester", UpdatedBy: "tester",
	})
	if err == nil {
		t.Fatal("a MATCH carrying a non-zero delta was accepted")
	}
}

func TestTenantIsolation(t *testing.T) {
	a := setup(t)
	b := setup(t)
	ctx := context.Background()

	assertion, err := a.repo.CreateAssertion(ctx, a.assertion(t, "EXT-1", "REC-1", "1000.00"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := b.repo.GetAssertion(ctx, assertion.ID, b.tenantID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant read another tenant's assertion: %v", err)
	}

	list, err := b.repo.ListAssertions(ctx, b.tenantID, "", 100, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, a := range list {
		if a.TenantID != b.tenantID {
			t.Errorf("listing leaked an assertion belonging to tenant %s", a.TenantID)
		}
	}
}
