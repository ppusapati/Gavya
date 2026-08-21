package domain

import (
	"time"
)

// DeviceKind names the class of capture hardware a record came from. Different
// kinds have different trust properties, which the admission rules depend on.
type DeviceKind string

const (
	// DeviceWeighbridge and DeviceMilkAnalyser are fixed instruments with their
	// own firmware and their own sequence counters.
	DeviceWeighbridge  DeviceKind = "WEIGHBRIDGE"
	DeviceMilkAnalyser DeviceKind = "MILK_ANALYSER"
	DeviceScale        DeviceKind = "PLATFORM_SCALE"
	// DeviceMobileApp is an app install. Reinstalling resets its counters,
	// which is precisely why generations exist.
	DeviceMobileApp DeviceKind = "MOBILE_APP"
	DeviceManual    DeviceKind = "MANUAL_ENTRY"
)

// Device is a physical or logical capture endpoint.
type Device struct {
	ID       string     `json:"id"`
	TenantID string     `json:"tenant_id"`
	Serial   string     `json:"serial"`
	Kind     DeviceKind `json:"kind"`
	Label    string     `json:"label"`
	// CurrentGeneration is the epoch new records must arrive under. Records
	// claiming an older generation are late arrivals from before a reset.
	CurrentGeneration int64 `json:"current_generation"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// GenerationReason records why a device's identity epoch was rolled.
type GenerationReason string

const (
	ReasonInitialProvisioning GenerationReason = "INITIAL_PROVISIONING"
	ReasonFactoryReset        GenerationReason = "FACTORY_RESET"
	ReasonFirmwareReflash     GenerationReason = "FIRMWARE_REFLASH"
	ReasonAppReinstall        GenerationReason = "APP_REINSTALL"
	ReasonClockReset          GenerationReason = "CLOCK_RESET"
	ReasonSuspectedTampering  GenerationReason = "SUSPECTED_TAMPERING"
	ReasonOperatorRequest     GenerationReason = "OPERATOR_REQUEST"
)

// DeviceGeneration is one identity epoch of a device.
//
// A device that is reset, reflashed or reinstalled restarts its sequence
// counter at one. Without a generation, that restart is indistinguishable from
// a replay of the records it already sent, and the platform would either drop
// real collections or admit duplicates. The generation makes the two cases
// distinguishable, which is the whole point.
type DeviceGeneration struct {
	ID         string           `json:"id"`
	TenantID   string           `json:"tenant_id"`
	DeviceID   string           `json:"device_id"`
	Generation int64            `json:"generation"`
	Reason     GenerationReason `json:"reason"`
	OpenedAt   time.Time        `json:"opened_at"`
	ClosedAt   *time.Time       `json:"closed_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

func (g *DeviceGeneration) IsOpen() bool { return g.ClosedAt == nil }

// SessionStatus tracks a capture session's lifecycle.
type SessionStatus string

const (
	SessionOpen SessionStatus = "OPEN"
	SessionClosed SessionStatus = "CLOSED"
	// SessionAbandoned is a session that was never closed and has aged out. Its
	// records stay valid; only new records are refused.
	SessionAbandoned SessionStatus = "ABANDONED"
	// SessionQuarantined marks a session whose identity is no longer trusted,
	// because a conflict was detected within it.
	SessionQuarantined SessionStatus = "QUARANTINED"
)

// CaptureSession is a bounded run of captures on one device generation.
//
// The device supplies its own session identifier and a sequence number per
// record within it. The pair (session, sequence) is what makes ingestion
// replayable: the same record redelivered any number of times lands in the same
// slot and is admitted once.
type CaptureSession struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
	// Generation binds the session to an epoch. The same external session id
	// under a different generation is a different session, not a continuation.
	Generation        int64  `json:"generation"`
	ExternalSessionID string `json:"external_session_id"`
	OperatorRef       string `json:"operator_ref"`

	Status   SessionStatus `json:"status"`
	OpenedAt time.Time     `json:"opened_at"`
	ClosedAt *time.Time    `json:"closed_at,omitempty"`

	// LastSequence is the highest sequence admitted so far. A device is
	// expected to advance it monotonically.
	LastSequence int64 `json:"last_sequence"`
	RecordCount  int64 `json:"record_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

func (s *CaptureSession) AcceptsNewRecords() bool { return s.Status == SessionOpen }

// CapturedRecord is one admitted measurement payload.
type CapturedRecord struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	DeviceID          string `json:"device_id"`
	Generation        int64  `json:"generation"`
	SessionID         string `json:"session_id"`
	ExternalSessionID string `json:"external_session_id"`
	Sequence          int64  `json:"sequence"`

	// PayloadHash is what distinguishes a benign replay from a conflict: the
	// same slot with the same bytes is a replay, the same slot with different
	// bytes is two different records claiming one identity.
	PayloadHash string `json:"payload_hash"`
	Payload     []byte `json:"payload"`

	// CapturedAt is the device's clock, ReceivedAt the platform's. They can
	// differ by days when a device has been offline, and neither is a
	// substitute for the other.
	CapturedAt time.Time `json:"captured_at"`
	ReceivedAt time.Time `json:"received_at"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// QuarantineReason names why a record could not be safely admitted.
type QuarantineReason string

const (
	// QuarantineTransportIdentityConflict: two different payloads claim the
	// same (device, generation, session, sequence) slot. One of them is wrong
	// and the platform cannot tell which, so neither silently wins.
	QuarantineTransportIdentityConflict QuarantineReason = "TRANSPORT_IDENTITY_CONFLICT"
	// QuarantineSequenceRegression: a sequence at or below the session high
	// water mark that is not a replay of a known record.
	QuarantineSequenceRegression QuarantineReason = "SEQUENCE_REGRESSION"
	// QuarantineUntrustedSessionIdentity: the session was never opened, or was
	// opened under a different generation.
	QuarantineUntrustedSessionIdentity QuarantineReason = "UNTRUSTED_SESSION_IDENTITY"
	// QuarantineStaleGeneration: the record claims a generation the device has
	// already rolled past.
	QuarantineStaleGeneration QuarantineReason = "STALE_GENERATION"
	// QuarantineSessionNotAccepting: the session is closed, abandoned or itself
	// quarantined.
	QuarantineSessionNotAccepting QuarantineReason = "SESSION_NOT_ACCEPTING"
)

// QuarantinedRecord is a record held out of the authoritative stream pending a
// human decision.
//
// Quarantine is deliberately not a rejection: the payload is kept in full, so
// an operator can release it once the ambiguity is resolved. Dropping a
// producer's collection because two devices disagreed about a sequence number
// would be the worst possible outcome.
type QuarantinedRecord struct {
	ID                string           `json:"id"`
	TenantID          string           `json:"tenant_id"`
	Reason            QuarantineReason `json:"reason"`
	Detail            string           `json:"detail"`
	DeviceID          string           `json:"device_id"`
	Generation        int64            `json:"generation"`
	ExternalSessionID string           `json:"external_session_id"`
	Sequence          int64            `json:"sequence"`
	PayloadHash       string           `json:"payload_hash"`
	Payload           []byte           `json:"payload"`
	// ConflictingRecordID names the admitted record this one collided with,
	// when there was one.
	ConflictingRecordID string `json:"conflicting_record_id,omitempty"`

	CapturedAt time.Time `json:"captured_at"`
	ReceivedAt time.Time `json:"received_at"`

	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	// ReleasedRecordID is set when an operator admitted the payload after all.
	ReleasedRecordID string `json:"released_record_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

func (q *QuarantinedRecord) IsResolved() bool { return q.ResolvedAt != nil }
