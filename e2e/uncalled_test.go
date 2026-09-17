//go:build e2e

// What the coverage count turned up on its first run.
//
// Three routes reported as never called. One of them — ingestion-service's
// ListSessions — had genuinely never been called by anything: in the permission
// table, registered by its service, and never shown to answer. That is not a
// hypothetical state for this repository. Procedures have been found here that
// were registered, reachable and failed on their first real call, and each time
// the thing that had been true was "no test ever called it".
//
// The other two were covered, on the ML-enabled platform, which the count was
// not asking. That is written up in coverage_test.go. Two tests remain here for
// them anyway, and each covers the half its ML-platform sibling cannot: those
// tests skip where cargo is absent, so on a machine with no Rust toolchain the
// routes went back to uncovered. The cases below need no toolchain.
package e2e

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// acceptRunReq is declared in decisions_test.go. The reply is read into a
// fuller shape here because this test asserts on who accepted the run and when,
// and runProto carries neither.
type acceptedRunResp struct {
	Run *fullRunProto `json:"run"`
}

// A reconciliation that did not converge cannot be accepted.
//
// TestAReconciliationRunIsAcceptedOnce covers the ordinary path, and it has to
// run on the ML-enabled platform: accepting needs a run that converged and
// converging needs the reconciler. That leaves the refusal untested wherever
// cargo is absent — and the refusal is the half that matters, because accepting
// an unconverged run states that a period closed on an arithmetic that never
// closed.
//
// So it is tested here, on the plain platform, where the absence of the tier is
// what produces the unconverged run. The one case that needs no toolchain is the
// one that needed covering.
func TestARunThatDidNotConvergeCannotBeAccepted(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	window := balancedWindow(t, p, newID("rt"), "4990.000")
	out, err := svcclient.Call[reconcileReq, getRunResp](ctx, p.balance(),
		balanceSvc+"/Reconcile", reconcileReq{
			TenantID: p.tenant, WindowID: window, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	run := out.Run
	if run.Converged {
		t.Fatalf("the window converged without the reconciler, so the refusal below " +
			"is not being reached and this test proves nothing")
	}
	if run.AcceptedAt != "" || run.AcceptedBy != "" {
		t.Fatalf("a run came back already accepted: at %q by %q", run.AcceptedAt, run.AcceptedBy)
	}

	if _, err := svcclient.Call[acceptRunReq, acceptedRunResp](ctx, p.balance(),
		balanceSvc+"/AcceptRun", acceptRunReq{
			TenantID: p.tenant, RunID: run.ID, Actor: "supervisor",
		}, p.opts()); err == nil {
		t.Error("a run that did not converge was accepted")
	} else if code := codeOf(t, err); code != connect.CodeFailedPrecondition {
		// By cause. failed_precondition tells the caller the run is not in a
		// state to be accepted; invalid_argument would send them to look at what
		// they sent, which is fine.
		t.Errorf("accepting an unconverged run is %s, want failed_precondition", code)
	}

	// The run is still there afterwards, unaccepted. A refusal that also
	// discarded the evidence would leave nothing to say why the period is open.
	after, err := svcclient.Call[getRunReq, getRunResp](ctx, p.balance(),
		balanceSvc+"/GetRun", getRunReq{TenantID: p.tenant, ID: run.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetRun after the refusal: %v", err)
	}
	if after.Run.AcceptedAt != "" || after.Run.AcceptedBy != "" {
		t.Errorf("the refused run reads back accepted by %q at %q",
			after.Run.AcceptedBy, after.Run.AcceptedAt)
	}

	// And a run that does not exist is not_found rather than the caller's
	// malformed argument: two different things to go and look at.
	if _, err := svcclient.Call[acceptRunReq, acceptedRunResp](ctx, p.balance(),
		balanceSvc+"/AcceptRun", acceptRunReq{
			TenantID: p.tenant, RunID: newID("run"), Actor: "supervisor",
		}, p.opts()); err == nil {
		t.Error("a run that does not exist was accepted")
	} else if code := codeOf(t, err); code != connect.CodeNotFound {
		t.Errorf("accepting a run that does not exist is %s, want not_found", code)
	}
}

type listIngestionSessionsReq struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type ingestionSessionProto struct {
	ID                string `json:"id"`
	DeviceID          string `json:"device_id"`
	ExternalSessionID string `json:"external_session_id"`
	Status            string `json:"status"`
	LastSequence      int64  `json:"last_sequence"`
	RecordCount       int64  `json:"record_count"`
}

type listIngestionSessionsResp struct {
	Sessions []*ingestionSessionProto `json:"sessions"`
}

// A device's capture sessions, which is how somebody finds the morning a
// particular bench recorded.
//
// The filter is the assertion worth making. A ListSessions that ignored
// device_id would answer "which sessions did this bench run" with every bench in
// the society, and the answer would look perfectly plausible — a list of
// sessions, in order, with real counts on them.
func TestADevicesSessionsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	mine := aCollector(t, p)
	mine.deliver(t, 1, `{"litres":12.5}`)
	mine.deliver(t, 2, `{"litres":9.0}`)

	theirs := aCollector(t, p)
	theirs.deliver(t, 1, `{"litres":4.0}`)

	listed, err := svcclient.Call[listIngestionSessionsReq, listIngestionSessionsResp](
		ctx, p.ingestion(), ingestionSvc+"/ListSessions",
		listIngestionSessionsReq{TenantID: p.tenant, DeviceID: mine.device, Limit: 100},
		p.opts())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.Sessions) == 0 {
		t.Fatal("a device that has just recorded a morning has no sessions")
	}

	var found *ingestionSessionProto
	for _, s := range listed.Sessions {
		if s.DeviceID != mine.device {
			t.Errorf("a session belonging to device %s came back in device %s's list",
				s.DeviceID, mine.device)
		}
		if s.ExternalSessionID == mine.session {
			found = s
		}
	}
	if found == nil {
		t.Fatalf("session %s is not in its own device's list", mine.session)
	}
	if found.Status != "OPEN" {
		t.Errorf("the session reads as %q and it has not been closed", found.Status)
	}
	// Two records delivered, so a count that is right for the wrong reason —
	// always zero, always one — does not pass.
	if found.RecordCount != 2 {
		t.Errorf("the session holds %d records and two were delivered", found.RecordCount)
	}
	if found.LastSequence != 2 {
		t.Errorf("the session's last sequence is %d and two were delivered",
			found.LastSequence)
	}
}

// The review queue: observations the anomaly tier marked for a person to look at.
//
// Flagged here with SQL rather than by the ML tier, and that is deliberate. The
// tier is Rust and the suite skips where cargo is absent, so a test that reached
// this route only through it would leave the route uncovered on any machine
// without a Rust toolchain — which is a coverage check that passes or fails
// depending on what is installed.
//
// What is being tested is observation-service's query, not the scoring: that the
// filter returns the flagged reading and does not return the one beside it. The
// scoring itself is tested against the real tier in ml_test.go.
func TestTheReviewQueueHoldsTheFlaggedReadingAndNotItsNeighbour(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	subject := subjectProto{Kind: "CATTLE", ID: newID("cat")}
	at := time.Now().UTC().Format(time.RFC3339)

	ordinary := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT",
		Value: 4.15, Origin: originProto{Kind: "NATIVE"},
		ValidFrom: at, CreatedBy: "operator",
	})
	odd := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT",
		Value: 9.90, Origin: originProto{Kind: "NATIVE"},
		ValidFrom: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		CreatedBy: "operator",
	})

	conn, err := pgx.Connect(ctx, dsn(t, "e2e_observation"))
	if err != nil {
		t.Fatalf("connect to the observation database: %v", err)
	}
	defer conn.Close(ctx)
	// anomaly_scored_at is not optional here: the schema carries
	// anomaly_flag_has_a_score, which refuses a flag with nothing behind it. The
	// first version of this test left it out and was refused, which is the
	// constraint doing exactly what it is for — a review queue full of readings
	// flagged by nothing is a queue nobody can act on.
	if _, err := conn.Exec(ctx,
		`UPDATE observations SET anomaly_flagged = true, anomaly_score = 0.97,
		        anomaly_method = 'e2e', anomaly_model_version = 'e2e',
		        anomaly_scored_at = NOW()
		  WHERE id = $1 AND tenant_id = $2`, odd.ID, p.tenant); err != nil {
		t.Fatalf("flag the observation: %v", err)
	}

	queue, err := svcclient.Call[listFlaggedReq, listFlaggedResp](ctx, p.observation(),
		observationSvc+"/ListFlaggedObservations",
		listFlaggedReq{TenantID: p.tenant, Limit: 100}, p.opts())
	if err != nil {
		t.Fatalf("ListFlaggedObservations: %v", err)
	}

	seen := map[string]bool{}
	for _, o := range queue.Observations {
		seen[o.ID] = true
		if o.TenantID != p.tenant {
			t.Errorf("the queue holds observation %s belonging to tenant %s",
				o.ID, o.TenantID)
		}
	}
	if !seen[odd.ID] {
		t.Errorf("the flagged observation %s is not in the review queue, so nobody "+
			"is ever asked to look at it", odd.ID)
	}
	if seen[ordinary.ID] {
		t.Errorf("observation %s is in the review queue and was never flagged — a "+
			"queue holding everything is one nobody works through", ordinary.ID)
	}
}
