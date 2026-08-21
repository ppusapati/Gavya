package domain

import "fmt"

// Outcome is the decision the admission rules reach about one incoming record.
type Outcome string

const (
	// OutcomeAccepted: a new record in a free slot.
	OutcomeAccepted Outcome = "ACCEPTED"
	// OutcomeDuplicateReplay: this exact record is already admitted. The
	// caller is handed the original. Redelivering a record any number of times
	// must be indistinguishable from delivering it once.
	OutcomeDuplicateReplay Outcome = "DUPLICATE_REPLAY"
	// OutcomeQuarantined: the record cannot be admitted safely and is held for
	// a human. It is never discarded.
	OutcomeQuarantined Outcome = "QUARANTINED"
)

// IncomingRecord is one record as delivered by a device.
type IncomingRecord struct {
	DeviceID          string
	Generation        int64
	ExternalSessionID string
	Sequence          int64
	PayloadHash       string
}

// AdmissionContext is the state the rules are evaluated against. Loading it is
// the repository's job; deciding on it is this package's, so the decision is a
// pure function that can be exhaustively tested without a database.
type AdmissionContext struct {
	Device *Device
	// Generation is the device generation the record claims, if it exists.
	Generation *DeviceGeneration
	// Session is the capture session the record claims, if it exists.
	Session *CaptureSession
	// Existing is the already-admitted record occupying the record's slot, if
	// any.
	Existing *CapturedRecord
}

// Decision is the outcome plus everything needed to act on it.
type Decision struct {
	Outcome Outcome
	// Reason and Detail are set when Outcome is OutcomeQuarantined.
	Reason QuarantineReason
	Detail string
	// Existing is set when Outcome is OutcomeDuplicateReplay, or when a
	// conflict collided with an admitted record.
	Existing *CapturedRecord
	// QuarantineSession is true when the conflict impugns the whole session's
	// identity, not just this record.
	QuarantineSession bool
}

// Admit decides what to do with one incoming record.
//
// The rules are ordered from the most specific evidence to the least. The
// governing principle throughout: a record is only admitted when its identity
// is unambiguous, and it is only ever held — never dropped — when it is not.
func Admit(in IncomingRecord, ctx AdmissionContext) Decision {
	if ctx.Device == nil {
		return quarantine(QuarantineUntrustedSessionIdentity,
			fmt.Sprintf("device %s is not registered", in.DeviceID), true)
	}
	if in.Sequence <= 0 {
		return quarantine(QuarantineSequenceRegression,
			fmt.Sprintf("sequence %d is not positive; device sequences start at 1", in.Sequence), false)
	}
	if in.PayloadHash == "" {
		return quarantine(QuarantineTransportIdentityConflict,
			"record carries no payload hash, so a replay cannot be told from a conflict", false)
	}

	// The slot is already occupied. Whether that is a replay or a collision is
	// decided entirely by the payload hash, and this check comes before every
	// session and generation rule: a record already admitted stays admitted
	// even if the session has since been closed or the generation rolled.
	// Otherwise a device retrying after a reset would be told its own accepted
	// records are now invalid.
	if ctx.Existing != nil {
		if ctx.Existing.PayloadHash == in.PayloadHash {
			return Decision{Outcome: OutcomeDuplicateReplay, Existing: ctx.Existing}
		}
		return Decision{
			Outcome: OutcomeQuarantined,
			Reason:  QuarantineTransportIdentityConflict,
			Detail: fmt.Sprintf(
				"sequence %d in session %s of device generation %d is already held by record %s with a different payload; two records claim one identity",
				in.Sequence, in.ExternalSessionID, in.Generation, ctx.Existing.ID),
			Existing: ctx.Existing,
			// The device's sequence space can no longer be trusted: it has
			// issued one number for two payloads.
			QuarantineSession: true,
		}
	}

	if ctx.Generation == nil {
		return quarantine(QuarantineUntrustedSessionIdentity,
			fmt.Sprintf("device %s has no generation %d on record", in.DeviceID, in.Generation), false)
	}
	if in.Generation > ctx.Device.CurrentGeneration {
		return quarantine(QuarantineUntrustedSessionIdentity,
			fmt.Sprintf("record claims generation %d but device %s has only reached generation %d",
				in.Generation, in.DeviceID, ctx.Device.CurrentGeneration), false)
	}
	if in.Generation < ctx.Device.CurrentGeneration {
		// A late arrival from before a reset. It may well be genuine, which is
		// exactly why it is held rather than dropped: only an operator can say
		// whether those collections were already recovered another way.
		return quarantine(QuarantineStaleGeneration,
			fmt.Sprintf("record belongs to generation %d but device %s is now on generation %d",
				in.Generation, in.DeviceID, ctx.Device.CurrentGeneration), false)
	}
	if !ctx.Generation.IsOpen() {
		return quarantine(QuarantineStaleGeneration,
			fmt.Sprintf("generation %d of device %s was closed at %s",
				in.Generation, in.DeviceID, ctx.Generation.ClosedAt.UTC().Format("2006-01-02T15:04:05Z")), false)
	}

	if ctx.Session == nil {
		return quarantine(QuarantineUntrustedSessionIdentity,
			fmt.Sprintf("session %s was never opened on generation %d of device %s",
				in.ExternalSessionID, in.Generation, in.DeviceID), false)
	}
	// A session identifier reused across generations is a different session.
	// Treating it as a continuation would let a reset device append to the
	// sequence space of the run it had before the reset.
	if ctx.Session.Generation != in.Generation {
		return quarantine(QuarantineUntrustedSessionIdentity,
			fmt.Sprintf("session %s belongs to generation %d, not the claimed generation %d",
				in.ExternalSessionID, ctx.Session.Generation, in.Generation), false)
	}
	if ctx.Session.DeviceID != in.DeviceID {
		return quarantine(QuarantineUntrustedSessionIdentity,
			fmt.Sprintf("session %s belongs to device %s, not %s",
				in.ExternalSessionID, ctx.Session.DeviceID, in.DeviceID), true)
	}
	if !ctx.Session.AcceptsNewRecords() {
		return quarantine(QuarantineSessionNotAccepting,
			fmt.Sprintf("session %s is %s and accepts no new records",
				in.ExternalSessionID, ctx.Session.Status), false)
	}

	// The slot is free, but the sequence is at or below the high water mark.
	// A device that has already sent sequence 10 and now sends 7 with a payload
	// nobody has seen is either misordering its buffer or has been tampered
	// with; either way the record is not safe to admit unexamined.
	if in.Sequence <= ctx.Session.LastSequence {
		return quarantine(QuarantineSequenceRegression,
			fmt.Sprintf("sequence %d is at or below the session high water mark %d but is not a replay of any admitted record",
				in.Sequence, ctx.Session.LastSequence), false)
	}

	return Decision{Outcome: OutcomeAccepted}
}

func quarantine(reason QuarantineReason, detail string, quarantineSession bool) Decision {
	return Decision{
		Outcome:           OutcomeQuarantined,
		Reason:            reason,
		Detail:            detail,
		QuarantineSession: quarantineSession,
	}
}
