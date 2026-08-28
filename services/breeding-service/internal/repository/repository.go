package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such breeding cycle" from "the
// database is unreachable".
var ErrNotFound = errors.New("not found")

// ErrCycleNotOpen is a breeding cycle that has moved past the point where an
// insemination can be recorded against it.
var ErrCycleNotOpen = errors.New("this breeding cycle is no longer open")

// ErrPregnancyClosed is a pregnancy that has already delivered. Recording a
// second calving against it would put more calves in the herd than there were
// animals to bear them.
var ErrPregnancyClosed = errors.New("this pregnancy has already ended")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const breedingCycleCols = `id,tenant_id,cattle_id,heat_date,status,COALESCE(notes,''),` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const inseminationCols = `id,tenant_id,cycle_id,cattle_id,bull_id,semen_batch_id,inseminated_at,method,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const pregnancyCols = `id,tenant_id,cattle_id,insemination_id,confirmed_at,expected_calving_date,status,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const calvingRecordCols = `id,tenant_id,pregnancy_id,cattle_id,calf_id,calving_date,calf_gender,calf_weight,` +
	`complications,status,created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	// RecordInseminationInCycle writes an insemination and advances its cycle
	// together, so a cycle can never disagree with the records against it.
	RecordInseminationInCycle(ctx context.Context, ins *domain.Insemination) (*domain.Insemination, *domain.BreedingCycle, error)
	// ConfirmPregnancyForCycle writes a pregnancy and advances its cycle.
	ConfirmPregnancyForCycle(ctx context.Context, p *domain.Pregnancy) (*domain.Pregnancy, *domain.BreedingCycle, error)
	// RecordCalvingAndClosePregnancy writes a calving and closes its pregnancy.
	RecordCalvingAndClosePregnancy(ctx context.Context, c *domain.CalvingRecord) (*domain.CalvingRecord, *domain.Pregnancy, error)

	CreateBreedingCycle(ctx context.Context, b *domain.BreedingCycle) (*domain.BreedingCycle, error)
	GetBreedingCycle(ctx context.Context, id, tenantID string) (*domain.BreedingCycle, error)
	ListCattleBreedingCycles(ctx context.Context, tenantID, cattleID string) ([]*domain.BreedingCycle, error)
	UpdateCycleStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.BreedingCycle, error)
	CreateInsemination(ctx context.Context, ins *domain.Insemination) (*domain.Insemination, error)
	GetInsemination(ctx context.Context, id, tenantID string) (*domain.Insemination, error)
	CreatePregnancy(ctx context.Context, p *domain.Pregnancy) (*domain.Pregnancy, error)
	GetPregnancy(ctx context.Context, id, tenantID string) (*domain.Pregnancy, error)
	UpdatePregnancyStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Pregnancy, error)
	ListActivePregnancies(ctx context.Context, tenantID string) ([]*domain.Pregnancy, error)
	CreateCalvingRecord(ctx context.Context, c *domain.CalvingRecord) (*domain.CalvingRecord, error)
}

// IDs supplies the identifier each audit entry carries.
type IDs interface{ New() string }

type repo struct {
	pool *pgxpool.Pool
	ids  IDs
}

func New(pool *pgxpool.Pool, ids IDs) Repository {
	return &repo{pool: pool, ids: ids}
}

const serviceName = "breeding-service"

func (r *repo) CreateBreedingCycle(ctx context.Context, b *domain.BreedingCycle) (*domain.BreedingCycle, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO breeding_cycles (id,tenant_id,cattle_id,heat_date,status,notes,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+breedingCycleCols,
		b.ID, b.TenantID, b.CattleID, b.HeatDate, b.Status, b.Notes, b.CreatedBy, b.UpdatedBy,
	)
	return scanBreedingCycle(row)
}

func (r *repo) GetBreedingCycle(ctx context.Context, id, tenantID string) (*domain.BreedingCycle, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+breedingCycleCols+` FROM breeding_cycles WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanBreedingCycle(row)
}

func (r *repo) ListCattleBreedingCycles(ctx context.Context, tenantID, cattleID string) ([]*domain.BreedingCycle, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+breedingCycleCols+` FROM breeding_cycles WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY heat_date DESC`,
		tenantID, cattleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.BreedingCycle
	for rows.Next() {
		b, err := scanBreedingCycle(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// UpdateCycleStatus moves a breeding cycle between states and records what it was in.
//
// A breeding cycle's status is a judgement about an animal, and judgements get
// revised. Which way it was revised is what somebody asks about later.
//
// The entry goes in the same transaction as the change, so a state that moved
// and the record of it moving cannot come apart.
func (r *repo) UpdateCycleStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.BreedingCycle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Read under the lock the update will take, so a concurrent transition
	// cannot land in between and be recorded as the state this one started from.
	var before string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM breeding_cycles WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&before); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`UPDATE breeding_cycles SET status=$3,updated_by=$4,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+breedingCycleCols,
		id, tenantID, status, updatedBy,
	)
	out, err := scanBreedingCycle(row)
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_cycle_status", ResourceType: "breeding_cycle", ResourceID: id,
		Before:      map[string]any{"status": before},
		After:       map[string]any{"status": status},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (r *repo) CreateInsemination(ctx context.Context, ins *domain.Insemination) (*domain.Insemination, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO inseminations (id,tenant_id,cycle_id,cattle_id,bull_id,semen_batch_id,inseminated_at,method,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+inseminationCols,
		ins.ID, ins.TenantID, ins.CycleID, ins.CattleID, ins.BullID, ins.SemenBatchID,
		ins.InseminatedAt, ins.Method, ins.CreatedBy, ins.UpdatedBy,
	)
	return scanInsemination(row)
}

func (r *repo) GetInsemination(ctx context.Context, id, tenantID string) (*domain.Insemination, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+inseminationCols+` FROM inseminations WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanInsemination(row)
}

func (r *repo) CreatePregnancy(ctx context.Context, p *domain.Pregnancy) (*domain.Pregnancy, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO pregnancies (id,tenant_id,cattle_id,insemination_id,confirmed_at,expected_calving_date,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+pregnancyCols,
		p.ID, p.TenantID, p.CattleID, p.InseminationID, p.ConfirmedAt, p.ExpectedCalvingDate,
		p.Status, p.CreatedBy, p.UpdatedBy,
	)
	return scanPregnancy(row)
}

func (r *repo) GetPregnancy(ctx context.Context, id, tenantID string) (*domain.Pregnancy, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+pregnancyCols+` FROM pregnancies WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanPregnancy(row)
}

// UpdatePregnancyStatus moves a pregnancy between states and records what it was in.
//
// A pregnancy confirmed and later recorded as lost, or the reverse, is a
// correction to a record about an animal. Without the previous state there is
// nothing to say a correction happened at all.
//
// The entry goes in the same transaction as the change, so a state that moved
// and the record of it moving cannot come apart.
func (r *repo) UpdatePregnancyStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Pregnancy, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Read under the lock the update will take, so a concurrent transition
	// cannot land in between and be recorded as the state this one started from.
	var before string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM pregnancies WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&before); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`UPDATE pregnancies SET status=$3,updated_by=$4,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+pregnancyCols,
		id, tenantID, status, updatedBy,
	)
	out, err := scanPregnancy(row)
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_pregnancy_status", ResourceType: "pregnancy", ResourceID: id,
		Before:      map[string]any{"status": before},
		After:       map[string]any{"status": status},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (r *repo) ListActivePregnancies(ctx context.Context, tenantID string) ([]*domain.Pregnancy, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+pregnancyCols+` FROM pregnancies WHERE tenant_id=$1 AND status='active' AND deleted_at IS NULL ORDER BY expected_calving_date`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Pregnancy
	for rows.Next() {
		p, err := scanPregnancy(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *repo) CreateCalvingRecord(ctx context.Context, c *domain.CalvingRecord) (*domain.CalvingRecord, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO calving_records (id,tenant_id,pregnancy_id,cattle_id,calf_id,calving_date,calf_gender,calf_weight,complications,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+calvingRecordCols,
		c.ID, c.TenantID, c.PregnancyID, c.CattleID, c.CalfID, c.CalvingDate, c.CalfGender,
		c.CalfWeight, c.Complications, c.Status, c.CreatedBy, c.UpdatedBy,
	)
	return scanCalvingRecord(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBreedingCycle(s scanner) (*domain.BreedingCycle, error) {
	b := &domain.BreedingCycle{}
	err := s.Scan(&b.ID, &b.TenantID, &b.CattleID, &b.HeatDate, &b.Status, &b.Notes,
		&b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

func scanInsemination(s scanner) (*domain.Insemination, error) {
	ins := &domain.Insemination{}
	err := s.Scan(&ins.ID, &ins.TenantID, &ins.CycleID, &ins.CattleID, &ins.BullID, &ins.SemenBatchID,
		&ins.InseminatedAt, &ins.Method, &ins.CreatedAt, &ins.UpdatedAt, &ins.CreatedBy, &ins.UpdatedBy, &ins.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ins, nil
}

func scanPregnancy(s scanner) (*domain.Pregnancy, error) {
	p := &domain.Pregnancy{}
	err := s.Scan(&p.ID, &p.TenantID, &p.CattleID, &p.InseminationID, &p.ConfirmedAt,
		&p.ExpectedCalvingDate, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func scanCalvingRecord(s scanner) (*domain.CalvingRecord, error) {
	c := &domain.CalvingRecord{}
	err := s.Scan(&c.ID, &c.TenantID, &c.PregnancyID, &c.CattleID, &c.CalfID, &c.CalvingDate,
		&c.CalfGender, &c.CalfWeight, &c.Complications, &c.Status,
		&c.CreatedAt, &c.UpdatedAt, &c.CreatedBy, &c.UpdatedBy, &c.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// ensure time import is used
var _ = time.Now
