package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/milk-service/internal/domain"

	ulidpkg "p9e.in/samavaya/packages/ulid"
)

type Repository interface {
	CreateSession(ctx context.Context, s *domain.MilkSession) (*domain.MilkSession, error)
	GetSession(ctx context.Context, id, tenantID string) (*domain.MilkSession, error)
	ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]*domain.MilkSession, error)
	UpdateSessionStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.MilkSession, error)
	CreateRecord(ctx context.Context, r *domain.MilkRecord) (*domain.MilkRecord, error)
	GetRecord(ctx context.Context, id, tenantID string) (*domain.MilkRecord, error)
	ListSessionRecords(ctx context.Context, sessionID, tenantID string) ([]*domain.MilkRecord, error)
	GetDailyYield(ctx context.Context, tenantID, cattleID string, date time.Time) (float64, error)
	CreateQuality(ctx context.Context, q *domain.MilkQuality) (*domain.MilkQuality, error)
	GetQuality(ctx context.Context, recordID, tenantID string) (*domain.MilkQuality, error)
}

type repo struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Repository { return &repo{db: db} }

func (r *repo) CreateSession(ctx context.Context, s *domain.MilkSession) (*domain.MilkSession, error) {
	const q = `INSERT INTO milk_sessions (id,tenant_id,cattle_id,session_date,shift_type,status,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,tenant_id,cattle_id,session_date,shift_type,status,created_at,updated_at,created_by,updated_by,deleted_at`
	row := r.db.QueryRow(ctx, q, s.ID, s.TenantID, s.CattleID, s.SessionDate, s.ShiftType, s.Status, s.CreatedBy, s.UpdatedBy)
	return scanSession(row)
}

func (r *repo) GetSession(ctx context.Context, id, tenantID string) (*domain.MilkSession, error) {
	const q = `SELECT id,tenant_id,cattle_id,session_date,shift_type,status,created_at,updated_at,created_by,updated_by,deleted_at FROM milk_sessions WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`
	return scanSession(r.db.QueryRow(ctx, q, id, tenantID))
}

func (r *repo) ListSessions(ctx context.Context, tenantID string, limit, offset int) ([]*domain.MilkSession, error) {
	const q = `SELECT id,tenant_id,cattle_id,session_date,shift_type,status,created_at,updated_at,created_by,updated_by,deleted_at FROM milk_sessions WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY session_date DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.MilkSession
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *repo) UpdateSessionStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.MilkSession, error) {
	const q = `UPDATE milk_sessions SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING id,tenant_id,cattle_id,session_date,shift_type,status,created_at,updated_at,created_by,updated_by,deleted_at`
	return scanSession(r.db.QueryRow(ctx, q, id, tenantID, status, updatedBy))
}

// CreateRecord writes a collection and the record of who wrote it, together.
//
// One transaction, deliberately. Recording the milk and then telling the audit
// service about it leaves every failure between the two — a restart, a dropped
// connection, the audit service down for a minute — as a collection that
// happened and was never attributed. Nothing reports that, because from here the
// write succeeded.
//
// A milk collection is the change this platform exists to be trusted about, so
// it is the one where an unattributed row matters most: the whole argument for
// recomputing a settlement is that the inputs can be traced to who entered them.
func (r *repo) CreateRecord(ctx context.Context, rec *domain.MilkRecord) (*domain.MilkRecord, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// Rollback after a successful commit is a no-op, so this needs no condition
	// and covers every path out, including a panic.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	const q = `INSERT INTO milk_records (id,tenant_id,session_id,cattle_id,quantity_liters,recorded_at,recorded_by,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,tenant_id,session_id,cattle_id,quantity_liters,recorded_at,recorded_by,created_at,updated_at,created_by,updated_by,deleted_at`
	out, err := scanRecord(tx.QueryRow(ctx, q, rec.ID, rec.TenantID, rec.SessionID, rec.CattleID, rec.QuantityLiters, rec.RecordedAt, rec.RecordedBy, rec.CreatedBy, rec.UpdatedBy))
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, ids{}, audit.Entry{
		Action:       "record_milk",
		ResourceType: "milk_record",
		ResourceID:   out.ID,
		// No before: a collection is created, not changed.
		After: map[string]any{
			"session_id":      out.SessionID,
			"cattle_id":       out.CattleID,
			"quantity_liters": out.QuantityLiters,
			"recorded_at":     out.RecordedAt,
			"recorded_by":     out.RecordedBy,
		},
		ServiceName: "milk-service",
	}); err != nil {
		// Returned, not logged and swallowed. Swallowing it would commit the
		// collection with no record of who entered it, which is the arrangement
		// the transaction is here to make impossible.
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// ids gives the audit record its identifier, from the same generator every other
// row in this platform uses.
type ids struct{}

func (ids) New() string { return ulidpkg.New().String() }

func (r *repo) GetRecord(ctx context.Context, id, tenantID string) (*domain.MilkRecord, error) {
	const q = `SELECT id,tenant_id,session_id,cattle_id,quantity_liters,recorded_at,recorded_by,created_at,updated_at,created_by,updated_by,deleted_at FROM milk_records WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`
	return scanRecord(r.db.QueryRow(ctx, q, id, tenantID))
}

func (r *repo) ListSessionRecords(ctx context.Context, sessionID, tenantID string) ([]*domain.MilkRecord, error) {
	const q = `SELECT id,tenant_id,session_id,cattle_id,quantity_liters,recorded_at,recorded_by,created_at,updated_at,created_by,updated_by,deleted_at FROM milk_records WHERE session_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY recorded_at`
	rows, err := r.db.Query(ctx, q, sessionID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.MilkRecord
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetDailyYield sums one animal's readings for one day.
//
// The day is bound as a time.Time and PostgreSQL infers the parameter's type
// from the comparison — `recorded_at::date = $3` types $3 as a date — so pgx
// encodes it as a date and no timezone arithmetic touches it.
//
// That is worth stating because the alternative is a real trap and this query
// looks exactly like it. If the parameter were forced to a timestamptz, the
// comparison would promote the date on the left to midnight in the session's
// timezone and compare it against midnight UTC, and the two agree only where the
// database runs in UTC. This platform is written for India, where every daily
// yield would then come back as zero litres — a number of the right magnitude in
// the right units and entirely wrong, and a cow that gave nothing is a cow
// somebody goes out to look at.
//
// dailyyield_integration_test.go runs this in UTC, in a zone ahead of it and in
// a zone behind it, which is what says the inference holds rather than that it
// happens to work where the tests run.
//
// Which day a reading falls on is decided by the database's timezone, because
// that is what `recorded_at::date` means. That is one stated fact about a
// deployment rather than a disagreement between two.
func (r *repo) GetDailyYield(ctx context.Context, tenantID, cattleID string, date time.Time) (float64, error) {
	const q = `SELECT COALESCE(SUM(quantity_liters),0) FROM milk_records WHERE tenant_id=$1 AND cattle_id=$2 AND recorded_at::date=$3 AND deleted_at IS NULL`
	var total float64
	err := r.db.QueryRow(ctx, q, tenantID, cattleID, date).Scan(&total)
	return total, err
}

func (r *repo) CreateQuality(ctx context.Context, mq *domain.MilkQuality) (*domain.MilkQuality, error) {
	const q = `INSERT INTO milk_quality (id,tenant_id,record_id,fat_percent,snf_percent,lactose,tested_at,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,tenant_id,record_id,fat_percent,snf_percent,lactose,tested_at,created_at,updated_at,created_by,updated_by`
	row := r.db.QueryRow(ctx, q, mq.ID, mq.TenantID, mq.RecordID, mq.FatPercent, mq.SNFPercent, mq.Lactose, mq.TestedAt, mq.CreatedBy, mq.UpdatedBy)
	out := &domain.MilkQuality{}
	err := row.Scan(&out.ID, &out.TenantID, &out.RecordID, &out.FatPercent, &out.SNFPercent, &out.Lactose, &out.TestedAt, &out.CreatedAt, &out.UpdatedAt, &out.CreatedBy, &out.UpdatedBy)
	return out, err
}

func (r *repo) GetQuality(ctx context.Context, recordID, tenantID string) (*domain.MilkQuality, error) {
	const q = `SELECT id,tenant_id,record_id,fat_percent,snf_percent,lactose,tested_at,created_at,updated_at,created_by,updated_by FROM milk_quality WHERE record_id=$1 AND tenant_id=$2`
	out := &domain.MilkQuality{}
	err := r.db.QueryRow(ctx, q, recordID, tenantID).Scan(&out.ID, &out.TenantID, &out.RecordID, &out.FatPercent, &out.SNFPercent, &out.Lactose, &out.TestedAt, &out.CreatedAt, &out.UpdatedAt, &out.CreatedBy, &out.UpdatedBy)
	return out, err
}

type scanner interface{ Scan(dest ...any) error }

func scanSession(s scanner) (*domain.MilkSession, error) {
	out := &domain.MilkSession{}
	err := s.Scan(&out.ID, &out.TenantID, &out.CattleID, &out.SessionDate, &out.ShiftType, &out.Status, &out.CreatedAt, &out.UpdatedAt, &out.CreatedBy, &out.UpdatedBy, &out.DeletedAt)
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return out, nil
}

func scanRecord(s scanner) (*domain.MilkRecord, error) {
	out := &domain.MilkRecord{}
	err := s.Scan(&out.ID, &out.TenantID, &out.SessionID, &out.CattleID, &out.QuantityLiters, &out.RecordedAt, &out.RecordedBy, &out.CreatedAt, &out.UpdatedAt, &out.CreatedBy, &out.UpdatedBy, &out.DeletedAt)
	if err != nil {
		return nil, fmt.Errorf("scan record: %w", err)
	}
	return out, nil
}
