//go:build dbintegration

// Repository tests against a real PostgreSQL.
//
// The eligibility rule is unit tested as a pure function elsewhere. What can
// only be tested here is whether the schema delivers what the service assumes:
// that a subject is typed and singular, that a correction leaves both versions
// readable, that an as-of query answers with what was known then, and that the
// two advisory estimates can be attached exactly once.
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

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/bitemporal"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
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

// newID produces a unique 26 character identifier in the shape the schema
// expects: a short prefix so failures are readable, then digits.
func newID(prefix string) string {
	body := fmt.Sprintf("%s%d", prefix, runNonce+idCounter.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + strings.Repeat("0", 26-len(body))
}

type fixture struct {
	repo     Repository
	tenantID string
	subject  domain.SubjectRef
}

// freshTenant isolates each test. Every table is tenant scoped, so a distinct
// tenant is a clean namespace without truncating shared tables.
func setup(t *testing.T) *fixture {
	t.Helper()
	return &fixture{
		repo:     New(pool(t)),
		tenantID: newID("tnt"),
		subject:  domain.SubjectRef{Kind: domain.SubjectCattle, ID: newID("cow")},
	}
}

func (f *fixture) observation(quantity domain.QuantityKind, value float64, validFrom time.Time) *domain.Observation {
	return &domain.Observation{
		ID:                 newID("obs"),
		TenantID:           f.tenantID,
		Subject:            f.subject,
		Quantity:           quantity,
		Value:              value,
		Unit:               quantity.Unit(),
		SessionRef:         "S-100",
		ObservedBy:         "op-1",
		Origin:             origin.NewNative(),
		ValidFrom:          validFrom,
		ValidTo:            bitemporal.EndOfTime,
		EligibilityVerdict: domain.EligibilityUnknown,
		EligibilityReason:  "no verification certificate is on record for the instrument",
		UncertaintyModelID: "milk-analyser-v1",
		CreatedBy:          "tester",
	}
}

func (f *fixture) record(t *testing.T, quantity domain.QuantityKind, value float64, validFrom time.Time) *domain.Observation {
	t.Helper()
	out, err := f.repo.CreateObservation(context.Background(), f.observation(quantity, value, validFrom))
	if err != nil {
		t.Fatalf("create observation (%s=%v): %v", quantity, value, err)
	}
	return out
}

// dbNow reads the database clock, so as-of boundaries in these tests are taken
// on the same clock that stamps recorded_at.
func dbNow(t *testing.T) time.Time {
	t.Helper()
	var now time.Time
	if err := pool(t).QueryRow(context.Background(), `SELECT now()`).Scan(&now); err != nil {
		t.Fatalf("read database clock: %v", err)
	}
	return now.UTC()
}

var morning = time.Date(2026, 3, 1, 5, 30, 0, 0, time.UTC)

func TestBitemporalRoundTrip(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	created := f.record(t, domain.QuantityVolumeLitres, 12.375, morning)

	got, err := f.repo.GetObservation(ctx, created.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get observation: %v", err)
	}
	if got.Value != 12.375 {
		t.Errorf("value = %v, want 12.375; NUMERIC round trip lost precision", got.Value)
	}
	if got.Unit != "L" {
		t.Errorf("unit = %q, want L", got.Unit)
	}
	if !got.ValidFrom.Equal(morning) {
		t.Errorf("valid_from = %s, want %s", got.ValidFrom, morning)
	}
	if !got.ValidTo.Equal(bitemporal.EndOfTime) {
		t.Errorf("valid_to = %s, want the end-of-time sentinel %s", got.ValidTo, bitemporal.EndOfTime)
	}
	if got.RecordedAt.IsZero() {
		t.Error("recorded_at was not stamped, so the transaction-time axis is unusable")
	}
	if !got.IsCurrent() {
		t.Error("a freshly recorded observation must be the current version")
	}
	if !got.UncertaintyMissing() {
		t.Error("an observation recorded before any estimate must report its uncertainty as missing, not as zero")
	}
	if got.Subject != f.subject {
		t.Errorf("subject = %+v, want %+v", got.Subject, f.subject)
	}
	if got.Origin.Kind != origin.Native {
		t.Errorf("origin kind = %q, want NATIVE", got.Origin.Kind)
	}
}

func TestEverySubjectKindRoundTrips(t *testing.T) {
	ctx := context.Background()
	r := New(pool(t))
	tenantID := newID("tnt")

	for _, kind := range []domain.SubjectKind{
		domain.SubjectCattle, domain.SubjectProducer, domain.SubjectRoute,
		domain.SubjectTanker, domain.SubjectBatch,
	} {
		subject := domain.SubjectRef{Kind: kind, ID: newID("sub")}
		f := &fixture{repo: r, tenantID: tenantID, subject: subject}

		created, err := r.CreateObservation(ctx, f.observation(domain.QuantityMassKG, 420.5, morning))
		if err != nil {
			t.Fatalf("%s: create: %v", kind, err)
		}
		if created.Subject != subject {
			t.Errorf("%s: subject came back %+v, want %+v", kind, created.Subject, subject)
		}

		list, err := r.ListObservationsForSubject(ctx, SubjectQuery{
			TenantID: tenantID, Subject: subject, Limit: 10,
		})
		if err != nil {
			t.Fatalf("%s: list: %v", kind, err)
		}
		if len(list) != 1 || list[0].ID != created.ID {
			t.Errorf("%s: listing returned %d rows, want the one just written", kind, len(list))
		}
	}
}

// A correction is a new row. Both versions stay readable and the superseded one
// keeps the number a payment may already have been made on.
func TestCorrectionLeavesBothVersionsReadable(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	original := f.record(t, domain.QuantityVolumeLitres, 12.5, morning)

	correction := f.observation(domain.QuantityVolumeLitres, 11.25, morning)
	correction.Supersedes = original.ID
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, original.ID, correction.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	stored, err := f.repo.CreateObservation(ctx, correction)
	if err != nil {
		t.Fatalf("create correction: %v", err)
	}

	old, err := f.repo.GetObservation(ctx, original.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get superseded observation: %v", err)
	}
	if old.Value != 12.5 {
		t.Errorf("superseded observation now reads %v, want the original 12.5; the row was mutated", old.Value)
	}
	if old.IsCurrent() {
		t.Error("the corrected observation is still marked current")
	}
	if old.SupersededBy != stored.ID {
		t.Errorf("superseded_by = %q, want the correction %q", old.SupersededBy, stored.ID)
	}
	if !old.ValidFrom.Equal(morning) {
		t.Errorf("superseded observation's valid_from moved to %s", old.ValidFrom)
	}

	fresh, err := f.repo.GetObservation(ctx, stored.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get correction: %v", err)
	}
	if fresh.Value != 11.25 {
		t.Errorf("correction value = %v, want 11.25", fresh.Value)
	}
	if fresh.Supersedes != original.ID {
		t.Errorf("correction supersedes %q, want %q", fresh.Supersedes, original.ID)
	}
	if !fresh.IsCurrent() {
		t.Error("the correction must be the current version")
	}

	// Current knowledge answers with the correction alone.
	live, err := f.repo.ListObservationsForSubject(ctx, SubjectQuery{
		TenantID: f.tenantID, Subject: f.subject, ValidAt: morning, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("%d live versions valid at %s, want 1", len(live), morning)
	}
	if live[0].ID != stored.ID {
		t.Errorf("live version is %q, want the correction %q", live[0].ID, stored.ID)
	}
}

// The point of the transaction-time axis: a settlement replayed at a past
// instant must see the number the platform believed then.
func TestAsOfQueryReturnsWhatWasKnownThen(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	original := f.record(t, domain.QuantityFatPercent, 4.2, morning)

	time.Sleep(10 * time.Millisecond)
	beforeCorrection := dbNow(t)
	time.Sleep(10 * time.Millisecond)

	correction := f.observation(domain.QuantityFatPercent, 3.8, morning)
	correction.Supersedes = original.ID
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, original.ID, correction.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if _, err := f.repo.CreateObservation(ctx, correction); err != nil {
		t.Fatalf("create correction: %v", err)
	}

	then, err := f.repo.ListObservationsForSubject(ctx, SubjectQuery{
		TenantID: f.tenantID, Subject: f.subject, Quantity: domain.QuantityFatPercent,
		ValidAt: morning, AsOf: beforeCorrection, Limit: 10,
	})
	if err != nil {
		t.Fatalf("as-of list: %v", err)
	}
	if len(then) != 1 {
		t.Fatalf("as-of %s returned %d rows, want 1", beforeCorrection, len(then))
	}
	if then[0].ID != original.ID {
		t.Errorf("as-of query returned %q, want the version known then %q", then[0].ID, original.ID)
	}
	if then[0].Value != 4.2 {
		t.Errorf("as-of value = %v, want the 4.2 believed at %s", then[0].Value, beforeCorrection)
	}

	now, err := f.repo.ListObservationsForSubject(ctx, SubjectQuery{
		TenantID: f.tenantID, Subject: f.subject, Quantity: domain.QuantityFatPercent,
		ValidAt: morning, Limit: 10,
	})
	if err != nil {
		t.Fatalf("current list: %v", err)
	}
	if len(now) != 1 || now[0].ID != correction.ID {
		t.Fatalf("current knowledge returned %d rows (first %v), want only the correction", len(now), firstID(now))
	}
	if now[0].Value != 3.8 {
		t.Errorf("current value = %v, want 3.8", now[0].Value)
	}

	// Before anything was recorded the platform knew nothing.
	before, err := f.repo.ListObservationsForSubject(ctx, SubjectQuery{
		TenantID: f.tenantID, Subject: f.subject,
		AsOf: original.RecordedAt.Add(-time.Second), Limit: 10,
	})
	if err != nil {
		t.Fatalf("pre-knowledge list: %v", err)
	}
	if len(before) != 0 {
		t.Errorf("as-of before the first recording returned %d rows, want 0", len(before))
	}
}

func TestValidAtSelectsTheIntervalCoveringTheInstant(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	evening := morning.Add(12 * time.Hour)

	first := f.observation(domain.QuantityVolumeLitres, 10, morning)
	first.ValidTo = evening
	if _, err := f.repo.CreateObservation(ctx, first); err != nil {
		t.Fatalf("create morning observation: %v", err)
	}
	second := f.observation(domain.QuantityVolumeLitres, 9, evening)
	if _, err := f.repo.CreateObservation(ctx, second); err != nil {
		t.Fatalf("create evening observation: %v", err)
	}

	cases := []struct {
		at   time.Time
		want string
	}{
		{morning, first.ID},
		{morning.Add(time.Hour), first.ID},
		{evening.Add(-time.Nanosecond), first.ID},
		{evening, second.ID},
		{evening.Add(72 * time.Hour), second.ID},
	}
	for _, c := range cases {
		got, err := f.repo.ListObservationsForSubject(ctx, SubjectQuery{
			TenantID: f.tenantID, Subject: f.subject, ValidAt: c.at, Limit: 10,
		})
		if err != nil {
			t.Fatalf("valid-at %s: %v", c.at, err)
		}
		if len(got) != 1 {
			t.Errorf("valid-at %s returned %d rows, want 1", c.at, len(got))
			continue
		}
		if got[0].ID != c.want {
			t.Errorf("valid-at %s returned %q, want %q", c.at, got[0].ID, c.want)
		}
	}

	// An instant before the first interval opens is covered by nothing.
	none, err := f.repo.ListObservationsForSubject(ctx, SubjectQuery{
		TenantID: f.tenantID, Subject: f.subject, ValidAt: morning.Add(-time.Hour), Limit: 10,
	})
	if err != nil {
		t.Fatalf("pre-interval list: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("valid-at before any interval returned %d rows, want 0", len(none))
	}
}

// The whole reason the subject is five typed columns: the database can enforce
// that exactly one of them is set. A (subject_type, subject_id) pair could not.
func TestTypedSubjectCheckRejectsTwoSubjects(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":   newID("cow"),
		"producer_id": newID("prd"),
	})
	assertCheckViolation(t, err, "exactly_one_subject")
}

func TestTypedSubjectCheckRejectsZeroSubjects(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{})
	assertCheckViolation(t, err, "exactly_one_subject")
}

func TestTypedSubjectCheckRejectsAllFiveSubjects(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":   newID("cow"),
		"producer_id": newID("prd"),
		"route_id":    newID("rte"),
		"tanker_id":   newID("tnk"),
		"batch_id":    newID("bat"),
	})
	assertCheckViolation(t, err, "exactly_one_subject")
}

func TestSchemaRejectsAnUnknownQuantityKind(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":     newID("cow"),
		"quantity_kind": "BUTTERFAT_POINTS",
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("insert of an unknown quantity kind returned %v, want a check violation", err)
	}
}

// Two live versions of one subject's quantity at one instant would be two
// contradictory answers to the same question.
func TestOnlyOneLiveVersionPerSubjectQuantityAndInstant(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	first := f.record(t, domain.QuantityVolumeLitres, 12.5, morning)

	_, err := f.repo.CreateObservation(ctx, f.observation(domain.QuantityVolumeLitres, 11.0, morning))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second live version returned %v, want a unique violation", err)
	}
	if pgErr.ConstraintName != "uq_observation_live_version" {
		t.Fatalf("violated constraint is %q, want uq_observation_live_version; the live-version index is not the one refusing this",
			pgErr.ConstraintName)
	}

	// Superseding the first frees the slot, which is what makes a correction a
	// new row rather than an edit.
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, first.ID, newID("obs")); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if _, err := f.repo.CreateObservation(ctx, f.observation(domain.QuantityVolumeLitres, 11.0, morning)); err != nil {
		t.Fatalf("correction after supersession: %v", err)
	}

	// A different quantity at the same instant is a different question.
	if _, err := f.repo.CreateObservation(ctx, f.observation(domain.QuantityFatPercent, 4.1, morning)); err != nil {
		t.Fatalf("different quantity at the same instant: %v", err)
	}
}

func TestSupersedeIsRefusedTwice(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	o := f.record(t, domain.QuantityVolumeLitres, 12.5, morning)
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, o.ID, newID("obs")); err != nil {
		t.Fatalf("first supersede: %v", err)
	}
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, o.ID, newID("obs")); !errors.Is(err, ErrNotFound) {
		t.Errorf("second supersede returned %v, want ErrNotFound; an already-closed version must not be reclosed", err)
	}
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, newID("obs"), newID("obs")); !errors.Is(err, ErrNotFound) {
		t.Errorf("superseding an unknown observation returned %v, want ErrNotFound", err)
	}
}

func TestAttachUncertainty(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	o := f.record(t, domain.QuantityVolumeLitres, 12.5, morning)
	est := domain.UncertaintyEstimate{
		ModelID:             "milk-analyser-v1",
		ModelVersion:        "2026.02.11",
		StandardUncertainty: 0.041,
		ExpandedUncertainty: 0.082,
		CoverageFactor:      2.0,
		CoverageProbability: 0.95,
		EstimatedAt:         time.Now().UTC(),
	}
	if err := f.repo.AttachUncertainty(ctx, f.tenantID, o.ID, est); err != nil {
		t.Fatalf("attach uncertainty: %v", err)
	}

	got, err := f.repo.GetObservation(ctx, o.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Uncertainty == nil {
		t.Fatal("no uncertainty came back after attaching one")
	}
	if got.UncertaintyMissing() {
		t.Error("observation still reports its uncertainty as missing")
	}
	if got.Uncertainty.StandardUncertainty != 0.041 {
		t.Errorf("standard uncertainty = %v, want 0.041", got.Uncertainty.StandardUncertainty)
	}
	if got.Uncertainty.ExpandedUncertainty != 0.082 {
		t.Errorf("expanded uncertainty = %v, want 0.082", got.Uncertainty.ExpandedUncertainty)
	}
	if got.Uncertainty.CoverageFactor != 2.0 {
		t.Errorf("coverage factor = %v, want 2.0", got.Uncertainty.CoverageFactor)
	}
	if got.Uncertainty.CoverageProbability != 0.95 {
		t.Errorf("coverage probability = %v, want 0.95", got.Uncertainty.CoverageProbability)
	}
	if got.Uncertainty.ModelVersion != "2026.02.11" {
		t.Errorf("model version = %q, want 2026.02.11", got.Uncertainty.ModelVersion)
	}
	if got.Value != 12.5 {
		t.Errorf("attaching an estimate changed the measured value to %v", got.Value)
	}

	if err := f.repo.AttachUncertainty(ctx, f.tenantID, o.ID, est); !errors.Is(err, ErrAlreadyAttached) {
		t.Errorf("second attach returned %v, want ErrAlreadyAttached; the estimate is write-once", err)
	}
	if err := f.repo.AttachUncertainty(ctx, f.tenantID, newID("obs"), est); !errors.Is(err, ErrNotFound) {
		t.Errorf("attaching to an unknown observation returned %v, want ErrNotFound", err)
	}
}

// An unreachable model must leave the observation recorded and readable, with
// nothing that could be mistaken for a real estimate.
func TestObservationWithoutAnEstimateIsStillReadable(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	o := f.record(t, domain.QuantityVolumeLitres, 12.5, morning)

	got, err := f.repo.GetObservation(ctx, o.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Uncertainty != nil {
		t.Errorf("uncertainty = %+v, want nil until one is attached", got.Uncertainty)
	}
	if got.Anomaly != nil {
		t.Errorf("anomaly = %+v, want nil until one is attached", got.Anomaly)
	}
	if got.UncertaintyModelID != "milk-analyser-v1" {
		t.Errorf("uncertainty model id = %q, want it retained so the estimate can be recomputed later", got.UncertaintyModelID)
	}
}

func TestAttachAnomalyWithBounds(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	o := f.record(t, domain.QuantityVolumeLitres, 31.0, morning)
	lower, upper := 8.0, 16.0
	a := domain.AnomalyAssessment{
		Score:        4.31,
		Flagged:      true,
		Method:       "robust_z",
		ModelVersion: "2026.01.30",
		LowerBound:   &lower,
		UpperBound:   &upper,
		Explanation:  "31.0 L is 4.3 robust standard deviations above this cow's median yield",
		ScoredAt:     time.Now().UTC(),
	}
	if err := f.repo.AttachAnomaly(ctx, f.tenantID, o.ID, a); err != nil {
		t.Fatalf("attach anomaly: %v", err)
	}

	got, err := f.repo.GetObservation(ctx, o.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Anomaly == nil {
		t.Fatal("no anomaly came back after attaching one")
	}
	if got.Anomaly.Score != 4.31 || !got.Anomaly.Flagged {
		t.Errorf("score = %v flagged = %v, want 4.31 and true", got.Anomaly.Score, got.Anomaly.Flagged)
	}
	if got.Anomaly.Method != "robust_z" || got.Anomaly.ModelVersion != "2026.01.30" {
		t.Errorf("method/version = %q/%q, want robust_z/2026.01.30", got.Anomaly.Method, got.Anomaly.ModelVersion)
	}
	if !got.Anomaly.Bounded() {
		t.Fatal("bounds did not survive the round trip")
	}
	if *got.Anomaly.LowerBound != 8.0 || *got.Anomaly.UpperBound != 16.0 {
		t.Errorf("bounds = [%v,%v], want [8,16]", *got.Anomaly.LowerBound, *got.Anomaly.UpperBound)
	}
	if got.Anomaly.Explanation == "" {
		t.Error("the explanation an operator would read was not stored")
	}
	// A flag marks for review; it must never alter the measured fact.
	if got.Value != 31.0 {
		t.Errorf("value = %v, want the recorded 31.0", got.Value)
	}
	if !got.IsCurrent() {
		t.Error("a flagged observation must remain current; a flag is not a rejection")
	}

	if err := f.repo.AttachAnomaly(ctx, f.tenantID, o.ID, a); !errors.Is(err, ErrAlreadyAttached) {
		t.Errorf("second attach returned %v, want ErrAlreadyAttached; the score is write-once", err)
	}
}

// Nil bounds mean the band is unbounded. They must not come back as zero, which
// would read as an infinitely tight band.
func TestAttachAnomalyWithUnboundedBand(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	o := f.record(t, domain.QuantityVolumeLitres, 12.5, morning)
	if err := f.repo.AttachAnomaly(ctx, f.tenantID, o.ID, domain.AnomalyAssessment{
		Score:       0,
		Flagged:     false,
		Method:      "robust_z",
		Explanation: "no baseline could be established from 1 prior reading",
	}); err != nil {
		t.Fatalf("attach anomaly: %v", err)
	}

	got, err := f.repo.GetObservation(ctx, o.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Anomaly == nil {
		t.Fatal("an unflagged score must still be recorded")
	}
	if got.Anomaly.LowerBound != nil || got.Anomaly.UpperBound != nil {
		t.Errorf("bounds = [%v,%v], want both nil for an unbounded band",
			got.Anomaly.LowerBound, got.Anomaly.UpperBound)
	}
	if got.Anomaly.Bounded() {
		t.Error("an unbounded band reports itself as bounded")
	}
	if got.Anomaly.ScoredAt.IsZero() {
		t.Error("scored_at was not stamped, so an unscored row cannot be told from an unflagged one")
	}
}

func TestFlaggedQueueHoldsLiveFlagsOnly(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	flagged := f.record(t, domain.QuantityVolumeLitres, 31.0, morning)
	clean := f.record(t, domain.QuantityFatPercent, 4.1, morning)

	if err := f.repo.AttachAnomaly(ctx, f.tenantID, flagged.ID, domain.AnomalyAssessment{
		Score: 4.31, Flagged: true, Method: "robust_z", Explanation: "far above the median",
	}); err != nil {
		t.Fatalf("attach flagged: %v", err)
	}
	if err := f.repo.AttachAnomaly(ctx, f.tenantID, clean.ID, domain.AnomalyAssessment{
		Score: 0.4, Flagged: false, Method: "robust_z", Explanation: "within band",
	}); err != nil {
		t.Fatalf("attach clean: %v", err)
	}

	queue, err := f.repo.ListFlaggedObservations(ctx, f.tenantID, 50, 0)
	if err != nil {
		t.Fatalf("list flagged: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("%d observations in the review queue, want 1", len(queue))
	}
	if queue[0].ID != flagged.ID {
		t.Errorf("queue holds %q, want the flagged %q", queue[0].ID, flagged.ID)
	}

	// Correcting the observation supersedes it, which is how a flag is cleared.
	if err := f.repo.SupersedeObservation(ctx, f.tenantID, flagged.ID, newID("obs")); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	queue, err = f.repo.ListFlaggedObservations(ctx, f.tenantID, 50, 0)
	if err != nil {
		t.Fatalf("list flagged after correction: %v", err)
	}
	if len(queue) != 0 {
		t.Errorf("%d observations still queued after the flagged one was corrected, want 0", len(queue))
	}
}

func TestCertificateLookupPrefersTheCoveringPeriod(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	instrument, err := f.repo.CreateInstrument(ctx, &domain.Instrument{
		ID: newID("ins"), TenantID: f.tenantID, Serial: newID("wb"),
		Kind: domain.InstrumentWeighbridge, Label: "dock 1", CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}

	old := certificate(f.tenantID, instrument.ID, "AP/LM/2024/1",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	current := certificate(f.tenantID, instrument.ID, "AP/LM/2026/1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, c := range []*domain.VerificationCertificate{old, current} {
		if _, err := f.repo.CreateCertificate(ctx, c); err != nil {
			t.Fatalf("create certificate %s: %v", c.CertificateNumber, err)
		}
	}

	got, err := f.repo.GetActiveCertificate(ctx, f.tenantID, instrument.ID, morning)
	if err != nil {
		t.Fatalf("get active certificate: %v", err)
	}
	if got.ID != current.ID {
		t.Errorf("at %s the lookup returned %s, want the covering certificate %s",
			morning, got.CertificateNumber, current.CertificateNumber)
	}

	// An instant covered by the older certificate resolves to that one, so a
	// historical observation is judged against the paperwork of its own day.
	back := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	got, err = f.repo.GetActiveCertificate(ctx, f.tenantID, instrument.ID, back)
	if err != nil {
		t.Fatalf("get historical certificate: %v", err)
	}
	if got.ID != old.ID {
		t.Errorf("at %s the lookup returned %s, want %s", back, got.CertificateNumber, old.CertificateNumber)
	}
}

// A lapsed certificate must come back rather than nothing: returning nothing
// would make the verdict UNKNOWN, hiding a payment made on an unverified
// instrument behind what looks like missing paperwork.
func TestCertificateLookupFallsBackToTheLatestLapsedOne(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	instrument, err := f.repo.CreateInstrument(ctx, &domain.Instrument{
		ID: newID("ins"), TenantID: f.tenantID, Serial: newID("ma"),
		Kind: domain.InstrumentMilkAnalyser, CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	lapsed := certificate(f.tenantID, instrument.ID, "AP/LM/2024/9",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := f.repo.CreateCertificate(ctx, lapsed); err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	got, err := f.repo.GetActiveCertificate(ctx, f.tenantID, instrument.ID, morning)
	if err != nil {
		t.Fatalf("get active certificate: %v", err)
	}
	if got.ID != lapsed.ID {
		t.Fatalf("lookup returned %s, want the lapsed %s", got.CertificateNumber, lapsed.CertificateNumber)
	}
	verdict := domain.AssessEligibility(got, morning, domain.QuantityMassKG)
	if verdict.Verdict != domain.EligibilityNotEligible {
		t.Errorf("verdict on a lapsed certificate = %s (%s), want NOT_ELIGIBLE", verdict.Verdict, verdict.Reason)
	}

	// An instrument with no certificate at all is unknown, not ineligible.
	bare, err := f.repo.CreateInstrument(ctx, &domain.Instrument{
		ID: newID("ins"), TenantID: f.tenantID, Serial: newID("ma"),
		Kind: domain.InstrumentMilkAnalyser, CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create bare instrument: %v", err)
	}
	if _, err := f.repo.GetActiveCertificate(ctx, f.tenantID, bare.ID, morning); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup on an uncertificated instrument returned %v, want ErrNotFound", err)
	}
	if v := domain.AssessEligibility(nil, morning, domain.QuantityMassKG); v.Verdict != domain.EligibilityUnknown {
		t.Errorf("verdict with no certificate = %s, want UNKNOWN", v.Verdict)
	}
}

// A certificate imported with blank paperwork is stored as blank, which is what
// makes its verdict UNKNOWN rather than a guess.
func TestBlankCertificateFieldsSurviveAndYieldUnknown(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	instrument, err := f.repo.CreateInstrument(ctx, &domain.Instrument{
		ID: newID("ins"), TenantID: f.tenantID, Serial: newID("wb"),
		Kind: domain.InstrumentWeighbridge, CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}

	imported, err := origin.NewImported(newID("src"), newID("bat"), "CERT-ROW-17", origin.HashPayload([]byte("blank")))
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	blank := certificate(f.tenantID, instrument.ID, "",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	blank.VerifyingAuthority = ""
	blank.Origin = imported
	if _, err := f.repo.CreateCertificate(ctx, blank); err != nil {
		t.Fatalf("create blank certificate: %v", err)
	}

	got, err := f.repo.GetActiveCertificate(ctx, f.tenantID, instrument.ID, morning)
	if err != nil {
		t.Fatalf("get active certificate: %v", err)
	}
	if got.CertificateNumber != "" || got.VerifyingAuthority != "" {
		t.Errorf("blank fields came back as %q/%q, want both empty", got.CertificateNumber, got.VerifyingAuthority)
	}
	if got.Origin.Kind != origin.Imported || got.Origin.SourceRecordID != "CERT-ROW-17" {
		t.Errorf("origin = %+v, want the imported provenance", got.Origin)
	}
	if v := domain.AssessEligibility(got, morning, domain.QuantityMassKG); v.Verdict != domain.EligibilityUnknown {
		t.Errorf("verdict on a blank certificate = %s (%s), want UNKNOWN", v.Verdict, v.Reason)
	}
}

func TestImportedOriginRoundTrips(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	imported, err := origin.NewImported(newID("src"), newID("bat"), "ROW-4711", origin.HashPayload([]byte("12.5")))
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	o := f.observation(domain.QuantityVolumeLitres, 12.5, morning)
	o.Origin = imported

	stored, err := f.repo.CreateObservation(ctx, o)
	if err != nil {
		t.Fatalf("create imported observation: %v", err)
	}
	if stored.Origin != imported {
		t.Fatalf("origin = %+v, want %+v", stored.Origin, imported)
	}
	if err := stored.Origin.Validate(); err != nil {
		t.Errorf("stored origin does not validate: %v", err)
	}
}

// The schema refuses an imported observation that cannot be traced back to its
// source record.
func TestSchemaRejectsAnIncompleteImportedOrigin(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":   newID("cow"),
		"origin_kind": "IMPORTED",
	})
	assertCheckViolation(t, err, "observation_origin_is_complete")
}

func TestSchemaRejectsAnInvertedValidInterval(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":  newID("cow"),
		"valid_from": morning,
		"valid_to":   morning.Add(-time.Hour),
	})
	assertCheckViolation(t, err, "valid_interval_ordered")
}

func TestSchemaRejectsAnUnattributedSupersession(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":     newID("cow"),
		"superseded_at": time.Now().UTC(),
	})
	assertCheckViolation(t, err, "supersession_is_attributed")
}

// A budget missing the coverage factor it was expanded by cannot be interpreted,
// so a half-populated one is refused outright.
func TestSchemaRejectsAHalfPopulatedUncertaintyBudget(t *testing.T) {
	f := setup(t)
	err := rawInsert(t, f.tenantID, map[string]any{
		"cattle_id":            newID("cow"),
		"uncertainty_missing":  false,
		"standard_uncertainty": 0.04,
	})
	assertCheckViolation(t, err, "uncertainty_is_all_or_nothing")
}

func TestTenantIsolation(t *testing.T) {
	a := setup(t)
	b := setup(t)
	ctx := context.Background()

	o := a.record(t, domain.QuantityVolumeLitres, 12.5, morning)
	if err := a.repo.AttachAnomaly(ctx, a.tenantID, o.ID, domain.AnomalyAssessment{
		Score: 4.0, Flagged: true, Method: "robust_z", Explanation: "high",
	}); err != nil {
		t.Fatalf("attach anomaly: %v", err)
	}

	if _, err := b.repo.GetObservation(ctx, o.ID, b.tenantID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant read another tenant's observation: %v", err)
	}
	if err := b.repo.SupersedeObservation(ctx, b.tenantID, o.ID, newID("obs")); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant superseded another tenant's observation: %v", err)
	}
	if err := b.repo.AttachUncertainty(ctx, b.tenantID, o.ID, domain.UncertaintyEstimate{
		StandardUncertainty: 1, ExpandedUncertainty: 2, CoverageFactor: 2, CoverageProbability: 0.95,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant attached an estimate to another tenant's observation: %v", err)
	}

	// Even asking for tenant a's subject under tenant b returns nothing.
	leaked, err := b.repo.ListObservationsForSubject(ctx, SubjectQuery{
		TenantID: b.tenantID, Subject: a.subject, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("listing leaked %d of another tenant's observations", len(leaked))
	}

	queue, err := b.repo.ListFlaggedObservations(ctx, b.tenantID, 50, 0)
	if err != nil {
		t.Fatalf("list flagged: %v", err)
	}
	for _, q := range queue {
		if q.TenantID != b.tenantID {
			t.Errorf("review queue leaked an observation belonging to tenant %s", q.TenantID)
		}
	}

	// The observation itself is untouched by any of the above.
	still, err := a.repo.GetObservation(ctx, o.ID, a.tenantID)
	if err != nil {
		t.Fatalf("get after cross-tenant attempts: %v", err)
	}
	if !still.IsCurrent() || still.Value != 12.5 || still.Uncertainty != nil {
		t.Errorf("cross-tenant calls altered the observation: %+v", still)
	}
}

func TestInstrumentTenantIsolation(t *testing.T) {
	a := setup(t)
	b := setup(t)
	ctx := context.Background()

	instrument, err := a.repo.CreateInstrument(ctx, &domain.Instrument{
		ID: newID("ins"), TenantID: a.tenantID, Serial: newID("wb"),
		Kind: domain.InstrumentWeighbridge, CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	if _, err := b.repo.GetInstrument(ctx, instrument.ID, b.tenantID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant read another tenant's instrument: %v", err)
	}
	if _, err := b.repo.GetActiveCertificate(ctx, b.tenantID, instrument.ID, morning); !errors.Is(err, ErrNotFound) {
		t.Errorf("a tenant read another tenant's certificates: %v", err)
	}
}

func certificate(tenantID, instrumentID, number string, issued, expires time.Time) *domain.VerificationCertificate {
	return &domain.VerificationCertificate{
		ID:                 newID("crt"),
		TenantID:           tenantID,
		InstrumentID:       instrumentID,
		CertificateNumber:  number,
		VerifyingAuthority: "Controller of Legal Metrology, Andhra Pradesh",
		IssuedAt:           issued,
		ExpiresAt:          expires,
		Origin:             origin.NewNative(),
		CreatedBy:          "tester",
	}
}

// rawInsert writes an observation row bypassing the repository, which is the
// only way to reach the constraints that exist to catch a caller who does the
// same by accident.
func rawInsert(t *testing.T, tenantID string, overrides map[string]any) error {
	t.Helper()
	row := map[string]any{
		"id":                  newID("obs"),
		"tenant_id":           tenantID,
		"quantity_kind":       string(domain.QuantityVolumeLitres),
		"value":               12.5,
		"unit":                "L",
		"origin_kind":         string(origin.Native),
		"valid_from":          morning,
		"valid_to":            bitemporal.EndOfTime,
		"eligibility_verdict": string(domain.EligibilityUnknown),
		"eligibility_reason":  "raw insert",
		"created_by":          "tester",
	}
	for k, v := range overrides {
		row[k] = v
	}

	cols := make([]string, 0, len(row))
	placeholders := make([]string, 0, len(row))
	args := make([]any, 0, len(row))
	for col, val := range row {
		cols = append(cols, col)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)+1))
		args = append(args, val)
	}

	sql := fmt.Sprintf("INSERT INTO observations (%s) VALUES (%s)",
		strings.Join(cols, ","), strings.Join(placeholders, ","))
	_, err := pool(t).Exec(context.Background(), sql, args...)
	return err
}

func assertCheckViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("insert returned %v, want a constraint violation from %s", err, constraint)
	}
	if pgErr.Code != "23514" {
		t.Fatalf("insert returned SQLSTATE %s (%s), want 23514 from %s", pgErr.Code, pgErr.Message, constraint)
	}
	if pgErr.ConstraintName != constraint {
		t.Errorf("violated constraint is %q, want %q", pgErr.ConstraintName, constraint)
	}
}

func firstID(list []*domain.Observation) string {
	if len(list) == 0 {
		return "<none>"
	}
	return list[0].ID
}
