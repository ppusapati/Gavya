package domain

import (
	"testing"
	"time"
)

func device(currentGen int64) *Device {
	return &Device{
		ID:                "dev-1",
		TenantID:          "tnt",
		Serial:            "WB-0001",
		Kind:              DeviceWeighbridge,
		CurrentGeneration: currentGen,
	}
}

func openGeneration(gen int64) *DeviceGeneration {
	return &DeviceGeneration{
		ID:         "gen-" + time.Now().Format("150405"),
		TenantID:   "tnt",
		DeviceID:   "dev-1",
		Generation: gen,
		Reason:     ReasonInitialProvisioning,
		OpenedAt:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
}

func openSession(gen, lastSeq int64) *CaptureSession {
	return &CaptureSession{
		ID:                "sess-1",
		TenantID:          "tnt",
		DeviceID:          "dev-1",
		Generation:        gen,
		ExternalSessionID: "S-100",
		Status:            SessionOpen,
		OpenedAt:          time.Date(2026, 2, 1, 6, 0, 0, 0, time.UTC),
		LastSequence:      lastSeq,
	}
}

func incoming(gen, seq int64, hash string) IncomingRecord {
	return IncomingRecord{
		DeviceID:          "dev-1",
		Generation:        gen,
		ExternalSessionID: "S-100",
		Sequence:          seq,
		PayloadHash:       hash,
	}
}

func admitted(seq int64, hash string) *CapturedRecord {
	return &CapturedRecord{
		ID:                "rec-existing",
		TenantID:          "tnt",
		DeviceID:          "dev-1",
		Generation:        1,
		ExternalSessionID: "S-100",
		Sequence:          seq,
		PayloadHash:       hash,
	}
}

func TestAdmitAcceptsAFreshRecord(t *testing.T) {
	d := Admit(incoming(1, 5, "sha256:aaa"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 4),
	})
	if d.Outcome != OutcomeAccepted {
		t.Fatalf("got %s (%s), want ACCEPTED", d.Outcome, d.Detail)
	}
}

func TestAdmitTreatsAnIdenticalRedeliveryAsAReplay(t *testing.T) {
	d := Admit(incoming(1, 5, "sha256:aaa"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 5),
		Existing:   admitted(5, "sha256:aaa"),
	})
	if d.Outcome != OutcomeDuplicateReplay {
		t.Fatalf("got %s (%s), want DUPLICATE_REPLAY", d.Outcome, d.Detail)
	}
	if d.Existing == nil || d.Existing.ID != "rec-existing" {
		t.Error("a replay must hand back the record already admitted")
	}
}

func TestReplayIsIdempotentAcrossManyRedeliveries(t *testing.T) {
	ctx := AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 5),
		Existing:   admitted(5, "sha256:aaa"),
	}
	for i := 0; i < 50; i++ {
		if d := Admit(incoming(1, 5, "sha256:aaa"), ctx); d.Outcome != OutcomeDuplicateReplay {
			t.Fatalf("redelivery %d returned %s, want DUPLICATE_REPLAY", i, d.Outcome)
		}
	}
}

func TestAdmitQuarantinesTwoPayloadsClaimingOneSlot(t *testing.T) {
	d := Admit(incoming(1, 5, "sha256:bbb"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 5),
		Existing:   admitted(5, "sha256:aaa"),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineTransportIdentityConflict {
		t.Errorf("reason = %s, want TRANSPORT_IDENTITY_CONFLICT", d.Reason)
	}
	if !d.QuarantineSession {
		t.Error("a device issuing one sequence for two payloads must have its session quarantined")
	}
	if d.Existing == nil {
		t.Error("the conflicting record must be named so a reviewer can compare them")
	}
}

// A record already admitted stays admitted. A device retrying after a reset
// must not be told its own accepted records have become invalid.
func TestReplayIsStillARepayAfterTheSessionClosed(t *testing.T) {
	closed := openSession(1, 5)
	closedAt := time.Date(2026, 2, 1, 18, 0, 0, 0, time.UTC)
	closed.Status, closed.ClosedAt = SessionClosed, &closedAt

	d := Admit(incoming(1, 5, "sha256:aaa"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    closed,
		Existing:   admitted(5, "sha256:aaa"),
	})
	if d.Outcome != OutcomeDuplicateReplay {
		t.Fatalf("got %s (%s), want DUPLICATE_REPLAY", d.Outcome, d.Detail)
	}
}

func TestReplayIsStillAReplayAfterTheGenerationRolled(t *testing.T) {
	d := Admit(incoming(1, 5, "sha256:aaa"), AdmissionContext{
		Device:     device(2),
		Generation: openGeneration(1),
		Session:    openSession(1, 5),
		Existing:   admitted(5, "sha256:aaa"),
	})
	if d.Outcome != OutcomeDuplicateReplay {
		t.Fatalf("got %s (%s), want DUPLICATE_REPLAY", d.Outcome, d.Detail)
	}
}

// The central reason generations exist: a reinstalled device restarts at
// sequence 1, and that must not collide with the run it had before.
func TestResetDeviceRestartingAtSequenceOneIsAccepted(t *testing.T) {
	d := Admit(IncomingRecord{
		DeviceID:          "dev-1",
		Generation:        2,
		ExternalSessionID: "S-200",
		Sequence:          1,
		PayloadHash:       "sha256:new",
	}, AdmissionContext{
		Device:     device(2),
		Generation: openGeneration(2),
		Session: &CaptureSession{
			ID: "sess-2", TenantID: "tnt", DeviceID: "dev-1",
			Generation: 2, ExternalSessionID: "S-200",
			Status: SessionOpen, LastSequence: 0,
		},
		// No Existing: the generation gives it a slot of its own even though
		// sequence 1 of generation 1 was used long ago.
	})
	if d.Outcome != OutcomeAccepted {
		t.Fatalf("got %s (%s), want ACCEPTED", d.Outcome, d.Detail)
	}
}

func TestAdmitQuarantinesAStaleGeneration(t *testing.T) {
	d := Admit(incoming(1, 9, "sha256:late"), AdmissionContext{
		Device:     device(3),
		Generation: openGeneration(1),
		Session:    openSession(1, 8),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineStaleGeneration {
		t.Errorf("reason = %s, want STALE_GENERATION", d.Reason)
	}
}

func TestAdmitQuarantinesAGenerationTheDeviceHasNotReached(t *testing.T) {
	d := Admit(incoming(5, 1, "sha256:x"), AdmissionContext{
		Device:     device(2),
		Generation: openGeneration(5),
		Session:    openSession(5, 0),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineUntrustedSessionIdentity {
		t.Errorf("reason = %s, want UNTRUSTED_SESSION_IDENTITY", d.Reason)
	}
}

func TestAdmitQuarantinesAnUnopenedSession(t *testing.T) {
	d := Admit(incoming(1, 1, "sha256:x"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    nil,
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineUntrustedSessionIdentity {
		t.Errorf("reason = %s, want UNTRUSTED_SESSION_IDENTITY", d.Reason)
	}
}

func TestAdmitQuarantinesASessionIdReusedAcrossGenerations(t *testing.T) {
	// The session on record belongs to generation 1; the record claims 2.
	d := Admit(incoming(2, 1, "sha256:x"), AdmissionContext{
		Device:     device(2),
		Generation: openGeneration(2),
		Session:    openSession(1, 12),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineUntrustedSessionIdentity {
		t.Errorf("reason = %s, want UNTRUSTED_SESSION_IDENTITY", d.Reason)
	}
}

func TestAdmitQuarantinesASessionBelongingToAnotherDevice(t *testing.T) {
	other := openSession(1, 3)
	other.DeviceID = "dev-2"

	d := Admit(incoming(1, 4, "sha256:x"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    other,
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if !d.QuarantineSession {
		t.Error("a session claimed by two devices must be quarantined")
	}
}

func TestAdmitQuarantinesASequenceRegression(t *testing.T) {
	// High water mark is 10; an unseen payload arrives at 7.
	d := Admit(incoming(1, 7, "sha256:unseen"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 10),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineSequenceRegression {
		t.Errorf("reason = %s, want SEQUENCE_REGRESSION", d.Reason)
	}
}

func TestAdmitAllowsASequenceGap(t *testing.T) {
	// Gaps are normal: a device buffers, drops a reading, or the operator
	// voids one. Only going backwards is suspicious.
	d := Admit(incoming(1, 20, "sha256:x"), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 10),
	})
	if d.Outcome != OutcomeAccepted {
		t.Fatalf("got %s (%s), want ACCEPTED", d.Outcome, d.Detail)
	}
}

func TestAdmitRejectsNewRecordsOnAClosedSession(t *testing.T) {
	for _, status := range []SessionStatus{SessionClosed, SessionAbandoned, SessionQuarantined} {
		s := openSession(1, 5)
		s.Status = status

		d := Admit(incoming(1, 6, "sha256:new"), AdmissionContext{
			Device:     device(1),
			Generation: openGeneration(1),
			Session:    s,
		})
		if d.Outcome != OutcomeQuarantined {
			t.Errorf("%s session: got %s, want QUARANTINED", status, d.Outcome)
			continue
		}
		if d.Reason != QuarantineSessionNotAccepting {
			t.Errorf("%s session: reason = %s, want SESSION_NOT_ACCEPTING", status, d.Reason)
		}
	}
}

func TestAdmitQuarantinesAClosedGeneration(t *testing.T) {
	g := openGeneration(1)
	closedAt := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	g.ClosedAt = &closedAt

	d := Admit(incoming(1, 6, "sha256:x"), AdmissionContext{
		Device:     device(1),
		Generation: g,
		Session:    openSession(1, 5),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineStaleGeneration {
		t.Errorf("reason = %s, want STALE_GENERATION", d.Reason)
	}
}

func TestAdmitQuarantinesAnUnregisteredDevice(t *testing.T) {
	d := Admit(incoming(1, 1, "sha256:x"), AdmissionContext{Device: nil})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if !d.QuarantineSession {
		t.Error("records from an unknown device must not establish a trusted session")
	}
}

func TestAdmitRejectsNonPositiveSequences(t *testing.T) {
	for _, seq := range []int64{0, -1} {
		d := Admit(incoming(1, seq, "sha256:x"), AdmissionContext{
			Device:     device(1),
			Generation: openGeneration(1),
			Session:    openSession(1, 0),
		})
		if d.Outcome != OutcomeQuarantined || d.Reason != QuarantineSequenceRegression {
			t.Errorf("sequence %d: got %s/%s, want QUARANTINED/SEQUENCE_REGRESSION", seq, d.Outcome, d.Reason)
		}
	}
}

func TestAdmitRejectsAMissingPayloadHash(t *testing.T) {
	d := Admit(incoming(1, 1, ""), AdmissionContext{
		Device:     device(1),
		Generation: openGeneration(1),
		Session:    openSession(1, 0),
	})
	if d.Outcome != OutcomeQuarantined {
		t.Fatalf("got %s, want QUARANTINED", d.Outcome)
	}
	if d.Reason != QuarantineTransportIdentityConflict {
		t.Errorf("reason = %s, want TRANSPORT_IDENTITY_CONFLICT", d.Reason)
	}
}

// Nothing the rules can produce may discard a payload.
func TestNoInputIsEverDropped(t *testing.T) {
	cases := []struct {
		name string
		in   IncomingRecord
		ctx  AdmissionContext
	}{
		{"unregistered device", incoming(1, 1, "h"), AdmissionContext{}},
		{"no generation", incoming(1, 1, "h"), AdmissionContext{Device: device(1)}},
		{"no session", incoming(1, 1, "h"), AdmissionContext{Device: device(1), Generation: openGeneration(1)}},
		{"stale generation", incoming(1, 1, "h"), AdmissionContext{Device: device(9), Generation: openGeneration(1), Session: openSession(1, 0)}},
		{"regression", incoming(1, 2, "h"), AdmissionContext{Device: device(1), Generation: openGeneration(1), Session: openSession(1, 9)}},
		{"conflict", incoming(1, 5, "other"), AdmissionContext{Device: device(1), Generation: openGeneration(1), Session: openSession(1, 5), Existing: admitted(5, "h")}},
		{"accepted", incoming(1, 6, "h"), AdmissionContext{Device: device(1), Generation: openGeneration(1), Session: openSession(1, 5)}},
		{"replay", incoming(1, 5, "h"), AdmissionContext{Device: device(1), Generation: openGeneration(1), Session: openSession(1, 5), Existing: admitted(5, "h")}},
	}
	for _, c := range cases {
		d := Admit(c.in, c.ctx)
		switch d.Outcome {
		case OutcomeAccepted, OutcomeDuplicateReplay, OutcomeQuarantined:
		default:
			t.Errorf("%s: outcome %q is outside the closed vocabulary", c.name, d.Outcome)
		}
		if d.Outcome == OutcomeQuarantined && d.Detail == "" {
			t.Errorf("%s: quarantined without a detail an operator could act on", c.name)
		}
		if d.Outcome == OutcomeQuarantined && d.Reason == "" {
			t.Errorf("%s: quarantined without a reason", c.name)
		}
	}
}
