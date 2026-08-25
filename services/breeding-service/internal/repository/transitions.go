package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
)

// A breeding record and the state it moves an animal into are one fact, and
// they are written together here.
//
// Each of these replaced a create followed by a separate status update whose
// error was logged and thrown away, leaving the caller told it had worked. The
// results were not money, but they are the animal's reproductive record: a
// pregnancy the system does not know ended keeps the cow in the population
// expected to calve, and a cycle that never left "open" invites a second
// insemination of an animal already carrying.

// RecordInseminationInCycle writes an insemination and moves its cycle to
// inseminated.
//
// The cycle is locked and checked first: inseminating a cycle that has already
// been recorded pregnant is not a state this record can produce, and silently
// moving it backwards would lose the pregnancy.
func (r *repo) RecordInseminationInCycle(ctx context.Context, ins *domain.Insemination) (*domain.Insemination, *domain.BreedingCycle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM breeding_cycles
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		ins.CycleID, ins.TenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock cycle: %w", err)
	}
	if status == domain.CyclePregnant || status == domain.CycleClosed {
		return nil, nil, fmt.Errorf("%w: this cycle is %s", ErrCycleNotOpen, status)
	}

	created, err := scanInsemination(tx.QueryRow(ctx,
		`INSERT INTO inseminations (id,tenant_id,cycle_id,cattle_id,bull_id,semen_batch_id,inseminated_at,method,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+inseminationCols,
		ins.ID, ins.TenantID, ins.CycleID, ins.CattleID, ins.BullID, ins.SemenBatchID,
		ins.InseminatedAt, ins.Method, ins.CreatedBy, ins.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("record insemination: %w", err)
	}

	cycle, err := scanBreedingCycle(tx.QueryRow(ctx,
		`UPDATE breeding_cycles SET status=$3, updated_by=$4, updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+breedingCycleCols,
		ins.CycleID, ins.TenantID, domain.CycleInseminated, ins.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("advance cycle: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return created, cycle, nil
}

// ConfirmPregnancyForCycle writes a pregnancy and moves its cycle to pregnant.
//
// The old path looked up the insemination outside the write and skipped the
// cycle update entirely when that lookup failed — silently, because the result
// was discarded with `if err == nil`. The lookup happens here, inside the
// transaction, and a missing insemination is a refusal rather than a shrug.
func (r *repo) ConfirmPregnancyForCycle(ctx context.Context, p *domain.Pregnancy) (*domain.Pregnancy, *domain.BreedingCycle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var cycleID string
	if err := tx.QueryRow(ctx,
		`SELECT cycle_id FROM inseminations
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		p.InseminationID, p.TenantID).Scan(&cycleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, fmt.Errorf("%w: no insemination %s", ErrNotFound, p.InseminationID)
		}
		return nil, nil, fmt.Errorf("find cycle: %w", err)
	}

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM breeding_cycles
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		cycleID, p.TenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock cycle: %w", err)
	}

	created, err := scanPregnancy(tx.QueryRow(ctx,
		`INSERT INTO pregnancies (id,tenant_id,cattle_id,insemination_id,confirmed_at,expected_calving_date,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+pregnancyCols,
		p.ID, p.TenantID, p.CattleID, p.InseminationID, p.ConfirmedAt, p.ExpectedCalvingDate,
		p.Status, p.CreatedBy, p.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("confirm pregnancy: %w", err)
	}

	cycle, err := scanBreedingCycle(tx.QueryRow(ctx,
		`UPDATE breeding_cycles SET status=$3, updated_by=$4, updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+breedingCycleCols,
		cycleID, p.TenantID, domain.CyclePregnant, p.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("advance cycle: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return created, cycle, nil
}

// RecordCalvingAndClosePregnancy writes a calving record and marks its
// pregnancy delivered.
//
// A calf recorded against a pregnancy still marked ongoing leaves the cow in
// the population the herd expects to calve, so she is neither dried off nor
// bred again on time.
func (r *repo) RecordCalvingAndClosePregnancy(ctx context.Context, c *domain.CalvingRecord) (*domain.CalvingRecord, *domain.Pregnancy, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM pregnancies
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		c.PregnancyID, c.TenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock pregnancy: %w", err)
	}
	// A pregnancy already delivered cannot deliver again. Without this the same
	// pregnancy could carry two calving records, and the herd's calf count would
	// exceed the number of animals that gave birth.
	if status == domain.PregnancyDelivered {
		return nil, nil, fmt.Errorf("%w: this pregnancy has already been delivered", ErrPregnancyClosed)
	}

	created, err := scanCalvingRecord(tx.QueryRow(ctx,
		`INSERT INTO calving_records (id,tenant_id,pregnancy_id,cattle_id,calf_id,calving_date,calf_gender,calf_weight,complications,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+calvingRecordCols,
		c.ID, c.TenantID, c.PregnancyID, c.CattleID, c.CalfID, c.CalvingDate, c.CalfGender,
		c.CalfWeight, c.Complications, c.Status, c.CreatedBy, c.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("record calving: %w", err)
	}

	pregnancy, err := scanPregnancy(tx.QueryRow(ctx,
		`UPDATE pregnancies SET status=$3, updated_by=$4, updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+pregnancyCols,
		c.PregnancyID, c.TenantID, domain.PregnancyDelivered, c.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("close pregnancy: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return created, pregnancy, nil
}
