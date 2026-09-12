package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/ingestion-service/internal/domain"

	"github.com/ppusapati/gavya/libs/integrity/audit"
)

var ErrNotFound = errors.New("not found")

// ErrDuplicateSerial names the one way registering a device fails that the
// device itself can act on. An app that has been reinstalled has lost its
// device id but not its serial, and needs to tell "this bench is already
// provisioned, adopt it and roll a generation" apart from "you sent something
// invalid" — which it cannot do from an opaque constraint violation.
var ErrDuplicateSerial = errors.New("a device with that serial is already registered")

// Decider is the pure admission rule, supplied by the service layer and
// evaluated inside the repository's transaction. Keeping the rule out here and
// the locking in there is what lets the rule be exhaustively unit tested while
// the concurrency stays in one place.
type Decider func(domain.IncomingRecord, domain.AdmissionContext) domain.Decision

// IngestInput is one record as delivered, with everything needed to admit it.
type IngestInput struct {
	TenantID          string
	DeviceID          string
	Generation        int64
	ExternalSessionID string
	Sequence          int64
	PayloadHash       string
	Payload           []byte
	CapturedAt        time.Time
	Actor             string
	// RecordID and QuarantineID are pre-generated so the identifiers are
	// assigned outside the transaction and the retry path stays deterministic.
	RecordID     string
	QuarantineID string
}

// IngestResult reports what happened to one delivered record.
type IngestResult struct {
	Decision   domain.Decision
	Record     *domain.CapturedRecord
	Quarantine *domain.QuarantinedRecord
}

type Repository interface {
	Ingest(ctx context.Context, in IngestInput, decide Decider) (*IngestResult, error)

	CreateDevice(ctx context.Context, d *domain.Device, generationID string) (*domain.Device, error)
	GetDevice(ctx context.Context, id, tenantID string) (*domain.Device, error)
	ListDevices(ctx context.Context, tenantID string, limit, offset int) ([]*domain.Device, error)
	RollGeneration(ctx context.Context, tenantID, deviceID string, reason domain.GenerationReason, generationID, actor string) (*domain.DeviceGeneration, error)
	ListGenerations(ctx context.Context, tenantID, deviceID string) ([]*domain.DeviceGeneration, error)

	OpenSession(ctx context.Context, s *domain.CaptureSession) (*domain.CaptureSession, error)
	CloseSession(ctx context.Context, tenantID, sessionID, actor string) (*domain.CaptureSession, error)
	GetSession(ctx context.Context, id, tenantID string) (*domain.CaptureSession, error)
	ListSessions(ctx context.Context, tenantID, deviceID string, limit, offset int) ([]*domain.CaptureSession, error)

	ListQuarantined(ctx context.Context, tenantID, reason string, limit, offset int) ([]*domain.QuarantinedRecord, error)
	GetQuarantined(ctx context.Context, id, tenantID string) (*domain.QuarantinedRecord, error)
	ResolveQuarantine(ctx context.Context, tenantID, id, resolution, actor string) (*domain.QuarantinedRecord, error)
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "ingestion-service"

type repo struct {
	db  *pgxpool.Pool
	ids audit.IDs
}

func New(db *pgxpool.Pool, ids audit.IDs) Repository { return &repo{db: db, ids: ids} }

// Ingest admits, replays or quarantines one record atomically.
//
// The transaction takes a row lock on the capture session, which serialises
// every delivery within one session. That is the natural concurrency unit: two
// deliveries for the same slot must not both see an empty slot and both insert,
// and the session's sequence high water mark must not be advanced by a read
// that another delivery has already invalidated.
func (r *repo) Ingest(ctx context.Context, in IngestInput, decide Decider) (*IngestResult, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	admissionCtx, sessionRowID, err := r.loadContext(ctx, tx, in)
	if err != nil {
		return nil, err
	}

	incoming := domain.IncomingRecord{
		DeviceID:          in.DeviceID,
		Generation:        in.Generation,
		ExternalSessionID: in.ExternalSessionID,
		Sequence:          in.Sequence,
		PayloadHash:       in.PayloadHash,
	}
	decision := decide(incoming, admissionCtx)

	result := &IngestResult{Decision: decision}

	switch decision.Outcome {
	case domain.OutcomeDuplicateReplay:
		result.Record = decision.Existing

	case domain.OutcomeAccepted:
		rec, err := insertRecord(ctx, tx, in, sessionRowID)
		if err != nil {
			return nil, err
		}
		if err := advanceSession(ctx, tx, in.TenantID, sessionRowID, in.Sequence, in.Actor); err != nil {
			return nil, err
		}
		result.Record = rec

	case domain.OutcomeQuarantined:
		var conflictingID string
		if decision.Existing != nil {
			conflictingID = decision.Existing.ID
		}
		q, err := insertQuarantine(ctx, tx, in, decision, conflictingID)
		if err != nil {
			return nil, err
		}
		result.Quarantine = q
		result.Record = decision.Existing

		if decision.QuarantineSession && sessionRowID != "" {
			if err := quarantineSession(ctx, tx, in.TenantID, sessionRowID, in.Actor); err != nil {
				return nil, err
			}
		}

	default:
		return nil, fmt.Errorf("admission returned an unknown outcome %q", decision.Outcome)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return result, nil
}

// loadContext gathers the device, generation, session and any record already
// occupying the slot, locking the session row for the rest of the transaction.
func (r *repo) loadContext(ctx context.Context, tx pgx.Tx, in IngestInput) (domain.AdmissionContext, string, error) {
	var out domain.AdmissionContext

	device, err := scanDevice(tx.QueryRow(ctx,
		`SELECT id,tenant_id,serial,kind,label,current_generation,created_at,updated_at,created_by,updated_by,deleted_at
		 FROM devices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		in.DeviceID, in.TenantID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, "", fmt.Errorf("load device: %w", err)
	}
	out.Device = device

	gen, err := scanGeneration(tx.QueryRow(ctx,
		`SELECT id,tenant_id,device_id,generation,reason,opened_at,closed_at,created_at,created_by
		 FROM device_generations WHERE tenant_id=$1 AND device_id=$2 AND generation=$3`,
		in.TenantID, in.DeviceID, in.Generation))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, "", fmt.Errorf("load generation: %w", err)
	}
	out.Generation = gen

	// FOR UPDATE serialises concurrent deliveries within this session.
	session, err := scanSession(tx.QueryRow(ctx,
		`SELECT id,tenant_id,device_id,generation,external_session_id,operator_ref,status,
		        opened_at,closed_at,last_sequence,record_count,created_at,updated_at,created_by,updated_by
		 FROM capture_sessions
		 WHERE tenant_id=$1 AND device_id=$2 AND generation=$3 AND external_session_id=$4
		 FOR UPDATE`,
		in.TenantID, in.DeviceID, in.Generation, in.ExternalSessionID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, "", fmt.Errorf("load session: %w", err)
	}
	out.Session = session

	var sessionRowID string
	if session != nil {
		sessionRowID = session.ID
	}

	existing, err := scanRecord(tx.QueryRow(ctx,
		`SELECT id,tenant_id,device_id,generation,session_id,external_session_id,sequence,
		        payload_hash,payload,captured_at,received_at,created_at,created_by
		 FROM captured_records
		 WHERE tenant_id=$1 AND device_id=$2 AND generation=$3 AND external_session_id=$4 AND sequence=$5`,
		in.TenantID, in.DeviceID, in.Generation, in.ExternalSessionID, in.Sequence))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, "", fmt.Errorf("load existing record: %w", err)
	}
	out.Existing = existing

	return out, sessionRowID, nil
}

func insertRecord(ctx context.Context, tx pgx.Tx, in IngestInput, sessionRowID string) (*domain.CapturedRecord, error) {
	const q = `INSERT INTO captured_records
		(id,tenant_id,device_id,generation,session_id,external_session_id,sequence,payload_hash,payload,captured_at,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id,tenant_id,device_id,generation,session_id,external_session_id,sequence,
		          payload_hash,payload,captured_at,received_at,created_at,created_by`
	rec, err := scanRecord(tx.QueryRow(ctx, q,
		in.RecordID, in.TenantID, in.DeviceID, in.Generation, sessionRowID, in.ExternalSessionID,
		in.Sequence, in.PayloadHash, in.Payload, in.CapturedAt, in.Actor))
	if err != nil {
		return nil, fmt.Errorf("insert record: %w", err)
	}
	return rec, nil
}

// advanceSession moves the high water mark forward only. GREATEST guards
// against a gap-filling record lowering it.
func advanceSession(ctx context.Context, tx pgx.Tx, tenantID, sessionRowID string, sequence int64, actor string) error {
	const q = `UPDATE capture_sessions
		SET last_sequence=GREATEST(last_sequence,$3), record_count=record_count+1, updated_at=NOW(), updated_by=$4
		WHERE tenant_id=$1 AND id=$2`
	_, err := tx.Exec(ctx, q, tenantID, sessionRowID, sequence, actor)
	if err != nil {
		return fmt.Errorf("advance session: %w", err)
	}
	return nil
}

func quarantineSession(ctx context.Context, tx pgx.Tx, tenantID, sessionRowID, actor string) error {
	const q = `UPDATE capture_sessions
		SET status='QUARANTINED', updated_at=NOW(), updated_by=$3
		WHERE tenant_id=$1 AND id=$2 AND status='OPEN'`
	_, err := tx.Exec(ctx, q, tenantID, sessionRowID, actor)
	if err != nil {
		return fmt.Errorf("quarantine session: %w", err)
	}
	return nil
}

func insertQuarantine(ctx context.Context, tx pgx.Tx, in IngestInput, d domain.Decision, conflictingID string) (*domain.QuarantinedRecord, error) {
	const q = `INSERT INTO quarantined_records
		(id,tenant_id,reason,detail,device_id,generation,external_session_id,sequence,
		 payload_hash,payload,conflicting_record_id,captured_at,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13)
		RETURNING id,tenant_id,reason,detail,device_id,generation,external_session_id,sequence,
		          payload_hash,payload,COALESCE(conflicting_record_id,''),captured_at,received_at,
		          resolved_at,COALESCE(resolved_by,''),COALESCE(resolution,''),COALESCE(released_record_id,''),
		          created_at,created_by`
	rec, err := scanQuarantine(tx.QueryRow(ctx, q,
		in.QuarantineID, in.TenantID, string(d.Reason), d.Detail, in.DeviceID, in.Generation,
		in.ExternalSessionID, in.Sequence, in.PayloadHash, in.Payload, conflictingID, in.CapturedAt, in.Actor))
	if err != nil {
		return nil, fmt.Errorf("insert quarantine: %w", err)
	}
	return rec, nil
}

// CreateDevice registers a device and opens its first generation together: a
// device with no generation could never admit a record.
func (r *repo) CreateDevice(ctx context.Context, d *domain.Device, generationID string) (*domain.Device, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	out, err := scanDevice(tx.QueryRow(ctx,
		`INSERT INTO devices (id,tenant_id,serial,kind,label,current_generation,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,1,$6,$6)
		 RETURNING id,tenant_id,serial,kind,label,current_generation,created_at,updated_at,created_by,updated_by,deleted_at`,
		d.ID, d.TenantID, d.Serial, string(d.Kind), d.Label, d.CreatedBy))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateSerial
		}
		return nil, fmt.Errorf("insert device: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO device_generations (id,tenant_id,device_id,generation,reason,created_by)
		 VALUES ($1,$2,$3,1,'INITIAL_PROVISIONING',$4)`,
		generationID, d.TenantID, d.ID, d.CreatedBy); err != nil {
		return nil, fmt.Errorf("open initial generation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

func (r *repo) GetDevice(ctx context.Context, id, tenantID string) (*domain.Device, error) {
	return scanDevice(r.db.QueryRow(ctx,
		`SELECT id,tenant_id,serial,kind,label,current_generation,created_at,updated_at,created_by,updated_by,deleted_at
		 FROM devices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, tenantID))
}

func (r *repo) ListDevices(ctx context.Context, tenantID string, limit, offset int) ([]*domain.Device, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id,tenant_id,serial,kind,label,current_generation,created_at,updated_at,created_by,updated_by,deleted_at
		 FROM devices WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY serial LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.Device, 0, limit)
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RollGeneration closes the device's current epoch and opens the next.
//
// Both halves happen in one transaction: a device left with no open generation
// would silently quarantine everything it sent, and one left with two would
// make "the current epoch" ambiguous.
func (r *repo) RollGeneration(ctx context.Context, tenantID, deviceID string, reason domain.GenerationReason, generationID, actor string) (*domain.DeviceGeneration, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var next int64
	if err := tx.QueryRow(ctx,
		`UPDATE devices SET current_generation=current_generation+1, updated_at=NOW(), updated_by=$3
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		 RETURNING current_generation`,
		deviceID, tenantID, actor).Scan(&next); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("bump device generation: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE device_generations SET closed_at=NOW()
		 WHERE tenant_id=$1 AND device_id=$2 AND closed_at IS NULL`,
		tenantID, deviceID); err != nil {
		return nil, fmt.Errorf("close prior generation: %w", err)
	}

	// Sessions belong to the epoch that has just ended; none of them can accept
	// another record, so they are closed out rather than left dangling open.
	if _, err := tx.Exec(ctx,
		`UPDATE capture_sessions SET status='ABANDONED', updated_at=NOW(), updated_by=$3
		 WHERE tenant_id=$1 AND device_id=$2 AND status='OPEN' AND generation < $4`,
		tenantID, deviceID, actor, next); err != nil {
		return nil, fmt.Errorf("abandon prior sessions: %w", err)
	}

	gen, err := scanGeneration(tx.QueryRow(ctx,
		`INSERT INTO device_generations (id,tenant_id,device_id,generation,reason,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id,tenant_id,device_id,generation,reason,opened_at,closed_at,created_at,created_by`,
		generationID, tenantID, deviceID, next, string(reason), actor))
	if err != nil {
		return nil, fmt.Errorf("open next generation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return gen, nil
}

func (r *repo) ListGenerations(ctx context.Context, tenantID, deviceID string) ([]*domain.DeviceGeneration, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id,tenant_id,device_id,generation,reason,opened_at,closed_at,created_at,created_by
		 FROM device_generations WHERE tenant_id=$1 AND device_id=$2 ORDER BY generation DESC`,
		tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.DeviceGeneration
	for rows.Next() {
		g, err := scanGeneration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *repo) OpenSession(ctx context.Context, s *domain.CaptureSession) (*domain.CaptureSession, error) {
	const q = `INSERT INTO capture_sessions
		(id,tenant_id,device_id,generation,external_session_id,operator_ref,status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,'OPEN',$7,$7)
		ON CONFLICT (tenant_id,device_id,generation,external_session_id) DO UPDATE
		    SET updated_at=NOW()
		RETURNING id,tenant_id,device_id,generation,external_session_id,operator_ref,status,
		          opened_at,closed_at,last_sequence,record_count,created_at,updated_at,created_by,updated_by`
	// Re-opening is idempotent: a device that retries its session handshake
	// after a dropped connection gets the session it already has, not an error.
	return scanSession(r.db.QueryRow(ctx, q,
		s.ID, s.TenantID, s.DeviceID, s.Generation, s.ExternalSessionID, s.OperatorRef, s.CreatedBy))
}

func (r *repo) CloseSession(ctx context.Context, tenantID, sessionID, actor string) (*domain.CaptureSession, error) {
	const q = `UPDATE capture_sessions
		SET status='CLOSED', closed_at=NOW(), updated_at=NOW(), updated_by=$3
		WHERE tenant_id=$1 AND id=$2 AND status='OPEN'
		RETURNING id,tenant_id,device_id,generation,external_session_id,operator_ref,status,
		          opened_at,closed_at,last_sequence,record_count,created_at,updated_at,created_by,updated_by`
	return scanSession(r.db.QueryRow(ctx, q, tenantID, sessionID, actor))
}

func (r *repo) GetSession(ctx context.Context, id, tenantID string) (*domain.CaptureSession, error) {
	return scanSession(r.db.QueryRow(ctx,
		`SELECT id,tenant_id,device_id,generation,external_session_id,operator_ref,status,
		        opened_at,closed_at,last_sequence,record_count,created_at,updated_at,created_by,updated_by
		 FROM capture_sessions WHERE id=$1 AND tenant_id=$2`, id, tenantID))
}

func (r *repo) ListSessions(ctx context.Context, tenantID, deviceID string, limit, offset int) ([]*domain.CaptureSession, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id,tenant_id,device_id,generation,external_session_id,operator_ref,status,
		        opened_at,closed_at,last_sequence,record_count,created_at,updated_at,created_by,updated_by
		 FROM capture_sessions WHERE tenant_id=$1 AND ($2='' OR device_id=$2)
		 ORDER BY opened_at DESC LIMIT $3 OFFSET $4`,
		tenantID, deviceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.CaptureSession, 0, limit)
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

const quarantineCols = `id,tenant_id,reason,detail,device_id,generation,external_session_id,sequence,
	payload_hash,payload,COALESCE(conflicting_record_id,''),captured_at,received_at,
	resolved_at,COALESCE(resolved_by,''),COALESCE(resolution,''),COALESCE(released_record_id,''),
	created_at,created_by`

func (r *repo) ListQuarantined(ctx context.Context, tenantID, reason string, limit, offset int) ([]*domain.QuarantinedRecord, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+quarantineCols+` FROM quarantined_records
		 WHERE tenant_id=$1 AND ($2='' OR reason=$2) AND resolved_at IS NULL
		 ORDER BY received_at DESC LIMIT $3 OFFSET $4`,
		tenantID, reason, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.QuarantinedRecord, 0, limit)
	for rows.Next() {
		q, err := scanQuarantine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *repo) GetQuarantined(ctx context.Context, id, tenantID string) (*domain.QuarantinedRecord, error) {
	return scanQuarantine(r.db.QueryRow(ctx,
		`SELECT `+quarantineCols+` FROM quarantined_records WHERE id=$1 AND tenant_id=$2`, id, tenantID))
}

func (r *repo) ResolveQuarantine(ctx context.Context, tenantID, id, resolution, actor string) (*domain.QuarantinedRecord, error) {
	// In a transaction with its record. A quarantined reading is one the platform
	// refused; somebody letting it through is overriding that refusal, and an
	// override nobody can be asked about is the same as no refusal at all.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// The columns the original statement set, unchanged. My first version of this
	// added an updated_at that the table does not have: wrapping a statement in a
	// transaction is not a licence to rewrite it, and the e2e suite caught it.
	const q = `UPDATE quarantined_records
		SET resolved_at=NOW(), resolved_by=$4, resolution=$3
		WHERE tenant_id=$1 AND id=$2 AND resolved_at IS NULL
		RETURNING ` + quarantineCols
	rec, err := scanQuarantine(tx.QueryRow(ctx, q, tenantID, id, resolution, actor))
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "resolve_quarantine", ResourceType: "quarantined_record", ResourceID: id,
		After: map[string]any{
			"resolution": resolution,
			"reason":     rec.Reason,
			"device_id":  rec.DeviceID,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return rec, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(row scanner) (*domain.Device, error) {
	var d domain.Device
	var kind string
	if err := row.Scan(&d.ID, &d.TenantID, &d.Serial, &kind, &d.Label, &d.CurrentGeneration,
		&d.CreatedAt, &d.UpdatedAt, &d.CreatedBy, &d.UpdatedBy, &d.DeletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Kind = domain.DeviceKind(kind)
	return &d, nil
}

func scanGeneration(row scanner) (*domain.DeviceGeneration, error) {
	var g domain.DeviceGeneration
	var reason string
	if err := row.Scan(&g.ID, &g.TenantID, &g.DeviceID, &g.Generation, &reason,
		&g.OpenedAt, &g.ClosedAt, &g.CreatedAt, &g.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	g.Reason = domain.GenerationReason(reason)
	return &g, nil
}

func scanSession(row scanner) (*domain.CaptureSession, error) {
	var s domain.CaptureSession
	var status string
	if err := row.Scan(&s.ID, &s.TenantID, &s.DeviceID, &s.Generation, &s.ExternalSessionID, &s.OperatorRef,
		&status, &s.OpenedAt, &s.ClosedAt, &s.LastSequence, &s.RecordCount,
		&s.CreatedAt, &s.UpdatedAt, &s.CreatedBy, &s.UpdatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Status = domain.SessionStatus(status)
	return &s, nil
}

func scanRecord(row scanner) (*domain.CapturedRecord, error) {
	var r domain.CapturedRecord
	if err := row.Scan(&r.ID, &r.TenantID, &r.DeviceID, &r.Generation, &r.SessionID, &r.ExternalSessionID,
		&r.Sequence, &r.PayloadHash, &r.Payload, &r.CapturedAt, &r.ReceivedAt, &r.CreatedAt, &r.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

func scanQuarantine(row scanner) (*domain.QuarantinedRecord, error) {
	var q domain.QuarantinedRecord
	var reason string
	if err := row.Scan(&q.ID, &q.TenantID, &reason, &q.Detail, &q.DeviceID, &q.Generation,
		&q.ExternalSessionID, &q.Sequence, &q.PayloadHash, &q.Payload, &q.ConflictingRecordID,
		&q.CapturedAt, &q.ReceivedAt, &q.ResolvedAt, &q.ResolvedBy, &q.Resolution, &q.ReleasedRecordID,
		&q.CreatedAt, &q.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	q.Reason = domain.QuarantineReason(reason)
	return &q, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
