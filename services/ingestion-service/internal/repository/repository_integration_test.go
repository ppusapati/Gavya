//go:build dbintegration

// Repository tests against a real PostgreSQL.
//
// The admission rules are unit tested as a pure function elsewhere. What can
// only be tested here is whether the schema and the transaction actually
// deliver the guarantee those rules assume: that a slot is claimed exactly
// once, even when two deliveries race for it.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags dbintegration ./internal/repository/...
package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/domain"
)

var (
	poolOnce sync.Once
	testPool *pgxpool.Pool
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

// freshTenant isolates each test. Every table is tenant scoped, so a distinct
// tenant is a clean namespace without truncating shared tables.
func freshTenant(t *testing.T) string {
	t.Helper()
	return newID("tnt")
}

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
	device   *domain.Device
	session  *domain.CaptureSession
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	r := New(pool(t), sys.IDs{})
	tenantID := freshTenant(t)

	device, err := r.CreateDevice(ctx, &domain.Device{
		ID:        newID("dev"),
		TenantID:  tenantID,
		Serial:    newID("wb"),
		Kind:      domain.DeviceWeighbridge,
		Label:     "test weighbridge",
		CreatedBy: "tester",
		UpdatedBy: "tester",
	}, newID("gen"))
	if err != nil {
		t.Fatalf("create device: %v", err)
	}

	session, err := r.OpenSession(ctx, &domain.CaptureSession{
		ID:                newID("ses"),
		TenantID:          tenantID,
		DeviceID:          device.ID,
		Generation:        device.CurrentGeneration,
		ExternalSessionID: "S-100",
		OperatorRef:       "op-1",
		CreatedBy:         "tester",
	})
	if err != nil {
		t.Fatalf("open session: %v", err)
	}

	return &fixture{repo: r, tenantID: tenantID, device: device, session: session}
}

// runNonce keeps identifiers unique across repeated runs against the same
// database, while the counter keeps them unique within a run.
var (
	runNonce  = time.Now().UnixNano()
	idCounter atomic.Int64
)

// newID produces a unique 26 character identifier in the shape the schema
// expects: a short prefix so failures are readable, then digits.
func newID(prefix string) string {
	body := fmt.Sprintf("%s%d", prefix, runNonce+idCounter.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + strings.Repeat("0", 26-len(body))
}

func (f *fixture) deliver(t *testing.T, sequence int64, payload string) *IngestResult {
	t.Helper()
	res, err := f.repo.Ingest(f.acting(), IngestInput{
		TenantID:          f.tenantID,
		DeviceID:          f.device.ID,
		Generation:        f.device.CurrentGeneration,
		ExternalSessionID: "S-100",
		Sequence:          sequence,
		PayloadHash:       "sha256:" + payload,
		Payload:           []byte(fmt.Sprintf(`{"litres":%q}`, payload)),
		CapturedAt:        time.Now().UTC(),
		Actor:             "tester",
		RecordID:          newID("rec"),
		QuarantineID:      newID("qtn"),
	}, domain.Admit)
	if err != nil {
		t.Fatalf("ingest seq %d: %v", sequence, err)
	}
	return res
}

func TestIngestAcceptsThenReplays(t *testing.T) {
	f := setup(t)

	first := f.deliver(t, 1, "aaa")
	if first.Decision.Outcome != domain.OutcomeAccepted {
		t.Fatalf("first delivery: got %s (%s), want ACCEPTED", first.Decision.Outcome, first.Decision.Detail)
	}
	if first.Record == nil {
		t.Fatal("accepted delivery returned no record")
	}

	second := f.deliver(t, 1, "aaa")
	if second.Decision.Outcome != domain.OutcomeDuplicateReplay {
		t.Fatalf("redelivery: got %s (%s), want DUPLICATE_REPLAY", second.Decision.Outcome, second.Decision.Detail)
	}
	if second.Record.ID != first.Record.ID {
		t.Errorf("replay returned record %s, want the original %s", second.Record.ID, first.Record.ID)
	}
}

func TestIngestIsIdempotentOverManyRedeliveries(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	first := f.deliver(t, 1, "aaa")
	for i := 0; i < 20; i++ {
		res := f.deliver(t, 1, "aaa")
		if res.Decision.Outcome != domain.OutcomeDuplicateReplay {
			t.Fatalf("redelivery %d: got %s", i, res.Decision.Outcome)
		}
		if res.Record.ID != first.Record.ID {
			t.Fatalf("redelivery %d returned a different record", i)
		}
	}

	session, err := f.repo.GetSession(ctx, f.session.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.RecordCount != 1 {
		t.Errorf("session record_count = %d after 21 deliveries of one record, want 1", session.RecordCount)
	}
}

// The guarantee the whole design rests on: concurrent deliveries of the same
// record admit it exactly once.
func TestConcurrentDeliveriesOfOneRecordAdmitItOnce(t *testing.T) {
	f := setup(t)

	const racers = 8
	var wg sync.WaitGroup
	outcomes := make([]domain.Outcome, racers)
	recordIDs := make([]string, racers)
	errs := make([]error, racers)

	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := f.repo.Ingest(f.acting(), IngestInput{
				TenantID:          f.tenantID,
				DeviceID:          f.device.ID,
				Generation:        f.device.CurrentGeneration,
				ExternalSessionID: "S-100",
				Sequence:          7,
				PayloadHash:       "sha256:race",
				Payload:           []byte(`{"litres":"12.5"}`),
				CapturedAt:        time.Now().UTC(),
				Actor:             "tester",
				RecordID:          newID("rec"),
				QuarantineID:      newID("qtn"),
			}, domain.Admit)
			if err != nil {
				errs[i] = err
				return
			}
			outcomes[i] = res.Decision.Outcome
			if res.Record != nil {
				recordIDs[i] = res.Record.ID
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d failed: %v", i, err)
		}
	}

	var accepted int
	winner := ""
	for i, o := range outcomes {
		switch o {
		case domain.OutcomeAccepted:
			accepted++
			winner = recordIDs[i]
		case domain.OutcomeDuplicateReplay:
		default:
			t.Errorf("racer %d got %s, want ACCEPTED or DUPLICATE_REPLAY", i, o)
		}
	}
	if accepted != 1 {
		t.Fatalf("%d racers were told they created the record, want exactly 1", accepted)
	}
	for i, rid := range recordIDs {
		if rid != winner {
			t.Errorf("racer %d saw record %s, want the single admitted record %s", i, rid, winner)
		}
	}

	session, err := f.repo.GetSession(f.acting(), f.session.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.RecordCount != 1 {
		t.Errorf("session record_count = %d after a race over one record, want 1", session.RecordCount)
	}
}

func TestIngestQuarantinesAConflictAndKeepsThePayload(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	f.deliver(t, 1, "aaa")
	conflict := f.deliver(t, 1, "bbb")

	if conflict.Decision.Outcome != domain.OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", conflict.Decision.Outcome)
	}
	if conflict.Decision.Reason != domain.QuarantineTransportIdentityConflict {
		t.Errorf("reason = %s, want TRANSPORT_IDENTITY_CONFLICT", conflict.Decision.Reason)
	}
	if conflict.Quarantine == nil {
		t.Fatal("no quarantine row was written; the payload would have been lost")
	}
	if len(conflict.Quarantine.Payload) == 0 {
		t.Error("quarantine row kept no payload")
	}
	if conflict.Quarantine.ConflictingRecordID == "" {
		t.Error("quarantine row does not name the record it collided with")
	}

	// The conflict impugns the session's sequence space, so it is quarantined.
	session, err := f.repo.GetSession(ctx, f.session.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.Status != domain.SessionQuarantined {
		t.Errorf("session status = %s, want QUARANTINED", session.Status)
	}

	held, err := f.repo.ListQuarantined(ctx, f.tenantID, "", 10, 0)
	if err != nil {
		t.Fatalf("list quarantined: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("got %d quarantined records, want 1", len(held))
	}
}

func TestSessionHighWaterMarkAdvancesOnlyForward(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	f.deliver(t, 1, "a")
	f.deliver(t, 5, "b")

	session, err := f.repo.GetSession(ctx, f.session.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.LastSequence != 5 {
		t.Fatalf("last_sequence = %d, want 5", session.LastSequence)
	}

	// A gap filler at 3 is below the mark and is not a replay, so it is held.
	res := f.deliver(t, 3, "c")
	if res.Decision.Outcome != domain.OutcomeQuarantined {
		t.Errorf("gap filler: got %s, want QUARANTINED", res.Decision.Outcome)
	}
	if res.Decision.Reason != domain.QuarantineSequenceRegression {
		t.Errorf("reason = %s, want SEQUENCE_REGRESSION", res.Decision.Reason)
	}

	session, err = f.repo.GetSession(ctx, f.session.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.LastSequence != 5 {
		t.Errorf("last_sequence regressed to %d", session.LastSequence)
	}
}

// The reason generations exist: a reset device restarts at sequence 1, and
// that must not collide with the run it had before the reset.
func TestRollingAGenerationGivesAFreshSequenceSpace(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	if res := f.deliver(t, 1, "before"); res.Decision.Outcome != domain.OutcomeAccepted {
		t.Fatalf("pre-reset delivery: got %s", res.Decision.Outcome)
	}

	gen, err := f.repo.RollGeneration(ctx, f.tenantID, f.device.ID, domain.ReasonFactoryReset, newID("gen"), "tester")
	if err != nil {
		t.Fatalf("roll generation: %v", err)
	}
	if gen.Generation != 2 {
		t.Fatalf("new generation = %d, want 2", gen.Generation)
	}

	// The old session belonged to the epoch that just ended.
	old, err := f.repo.GetSession(ctx, f.session.ID, f.tenantID)
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if old.Status != domain.SessionAbandoned {
		t.Errorf("pre-reset session status = %s, want ABANDONED", old.Status)
	}

	if _, err := f.repo.OpenSession(ctx, &domain.CaptureSession{
		ID: newID("ses"), TenantID: f.tenantID, DeviceID: f.device.ID,
		Generation: 2, ExternalSessionID: "S-200", CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("open post-reset session: %v", err)
	}

	res, err := f.repo.Ingest(ctx, IngestInput{
		TenantID:          f.tenantID,
		DeviceID:          f.device.ID,
		Generation:        2,
		ExternalSessionID: "S-200",
		Sequence:          1,
		PayloadHash:       "sha256:after",
		Payload:           []byte(`{"litres":"9.0"}`),
		CapturedAt:        time.Now().UTC(),
		Actor:             "tester",
		RecordID:          newID("rec"),
		QuarantineID:      newID("qtn"),
	}, domain.Admit)
	if err != nil {
		t.Fatalf("post-reset ingest: %v", err)
	}
	if res.Decision.Outcome != domain.OutcomeAccepted {
		t.Fatalf("sequence 1 of generation 2: got %s (%s), want ACCEPTED",
			res.Decision.Outcome, res.Decision.Detail)
	}
}

func TestStaleGenerationIsHeldNotDropped(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	if _, err := f.repo.RollGeneration(ctx, f.tenantID, f.device.ID, domain.ReasonAppReinstall, newID("gen"), "tester"); err != nil {
		t.Fatalf("roll generation: %v", err)
	}

	// A record from before the reset arrives late.
	res, err := f.repo.Ingest(ctx, IngestInput{
		TenantID:          f.tenantID,
		DeviceID:          f.device.ID,
		Generation:        1,
		ExternalSessionID: "S-100",
		Sequence:          9,
		PayloadHash:       "sha256:late",
		Payload:           []byte(`{"litres":"11.0"}`),
		CapturedAt:        time.Now().UTC(),
		Actor:             "tester",
		RecordID:          newID("rec"),
		QuarantineID:      newID("qtn"),
	}, domain.Admit)
	if err != nil {
		t.Fatalf("late ingest: %v", err)
	}
	if res.Decision.Outcome != domain.OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", res.Decision.Outcome)
	}
	if res.Decision.Reason != domain.QuarantineStaleGeneration {
		t.Errorf("reason = %s, want STALE_GENERATION", res.Decision.Reason)
	}
	if res.Quarantine == nil || len(res.Quarantine.Payload) == 0 {
		t.Error("a late record's payload was not preserved")
	}
}

func TestOpenSessionIsIdempotent(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	again, err := f.repo.OpenSession(ctx, &domain.CaptureSession{
		ID: newID("ses"), TenantID: f.tenantID, DeviceID: f.device.ID,
		Generation: f.device.CurrentGeneration, ExternalSessionID: "S-100",
		CreatedBy: "tester",
	})
	if err != nil {
		t.Fatalf("reopen session: %v", err)
	}
	if again.ID != f.session.ID {
		t.Errorf("reopening produced a new session %s, want the existing %s", again.ID, f.session.ID)
	}
}

func TestResolveQuarantineRecordsTheDecision(t *testing.T) {
	f := setup(t)
	ctx := f.acting()

	f.deliver(t, 1, "aaa")
	conflict := f.deliver(t, 1, "bbb")

	resolved, err := f.repo.ResolveQuarantine(ctx, f.tenantID, conflict.Quarantine.ID,
		"device firmware bug confirmed; original reading stands", "auditor-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !resolved.IsResolved() {
		t.Error("record is not marked resolved")
	}
	if resolved.ResolvedBy != "auditor-1" {
		t.Errorf("resolved_by = %q, want auditor-1", resolved.ResolvedBy)
	}

	open, err := f.repo.ListQuarantined(ctx, f.tenantID, "", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("%d records still open after resolution, want 0", len(open))
	}
}

func TestTenantIsolation(t *testing.T) {
	a := setup(t)
	b := setup(t)

	a.deliver(t, 1, "aaa")

	// Tenant b must not see tenant a's device, session or records.
	if _, err := b.repo.GetDevice(context.Background(), a.device.ID, b.tenantID); err == nil {
		t.Error("a tenant read another tenant's device")
	}
	if _, err := b.repo.GetSession(context.Background(), a.session.ID, b.tenantID); err == nil {
		t.Error("a tenant read another tenant's session")
	}

	sessions, err := b.repo.ListSessions(context.Background(), b.tenantID, "", 100, 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, s := range sessions {
		if s.TenantID != b.tenantID {
			t.Errorf("listing leaked a session belonging to tenant %s", s.TenantID)
		}
	}
}
