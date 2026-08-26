// Package repository is settlement's access to its tables.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrOverlappingCycle is a period already being settled by another cycle.
	ErrOverlappingCycle = errors.New("another cycle already covers part of that period for this society")
	// ErrAlreadyGathered is a collection that some cycle has already taken.
	ErrAlreadyGathered = errors.New("a collection in that period has already been gathered into another cycle")
	// ErrPaidIsFinal is the trigger refusing to let money that has been handed
	// over be rewritten.
	ErrPaidIsFinal = errors.New("a payable that has been paid cannot be changed")
)

type IDs interface{ New() string }

const serviceName = "settlement-service"

type Repository interface {
	CreateCycle(ctx context.Context, c *domain.Cycle) (*domain.Cycle, error)
	GetCycle(ctx context.Context, tenantID, id string) (*domain.Cycle, error)
	ListCycles(ctx context.Context, tenantID, societyCode string, limit, offset int) ([]*domain.Cycle, error)

	// Gather writes a period's lines, the deductions taken, the payables, and
	// the new balances on every recovery served — in one transaction.
	Gather(ctx context.Context, cycleID string, g *Gathered) error
	AbandonCycle(ctx context.Context, tenantID, cycleID, actor string) error
	ApproveCycle(ctx context.Context, tenantID, cycleID, actor string) (*domain.Cycle, error)

	CreateRecovery(ctx context.Context, r *domain.Recovery) (*domain.Recovery, error)
	ListRecoveries(ctx context.Context, tenantID, producerRef string, outstandingOnly bool) ([]*domain.Recovery, error)

	ListPayables(ctx context.Context, tenantID, cycleID string) ([]*domain.ProducerPayable, error)
	GetPayable(ctx context.Context, tenantID, id string) (*domain.ProducerPayable, error)
	MarkPaid(ctx context.Context, tenantID, id, reference, actor string, at time.Time) (*domain.ProducerPayable, error)
	HoldPayable(ctx context.Context, tenantID, id, reason, actor string) (*domain.ProducerPayable, error)

	Statement(ctx context.Context, tenantID, cycleID, producerRef string) (*domain.Statement, error)
}

// Gathered is everything one settlement run produced, ready to be written
// together.
//
// One struct rather than four calls, because these four things are one fact. A
// deduction written without the balance it changed leaves a producer's debt
// looking untouched while their payslip says otherwise, and a payable written
// without its lines is a number with nothing behind it.
type Gathered struct {
	Lines      []*domain.Line
	Deductions []*domain.Deduction
	Payables   []*domain.ProducerPayable
	// Recovered is the new recovered total for each recovery that was served,
	// keyed by recovery id.
	Recovered map[string]money.Money
	At        time.Time
	Actor     string
}

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

func New(db *pgxpool.Pool, ids IDs) Repository { return &repo{db: db, ids: ids} }

// ---------------------------------------------------------------------------
// Cycles
// ---------------------------------------------------------------------------

func (r *repo) CreateCycle(ctx context.Context, c *domain.Cycle) (*domain.Cycle, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	c.ID = r.ids.New()
	c.Status = domain.CycleOpen
	_, err = tx.Exec(ctx, `
		INSERT INTO payment_cycles
			(id,tenant_id,society_code,name,period_start,period_end,currency,amount_scale,
			 deduction_policy,status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`,
		c.ID, c.TenantID, c.SocietyCode, c.Name, c.PeriodStart, c.PeriodEnd,
		c.Currency, c.AmountScale, string(c.Policy), string(c.Status), c.CreatedBy)
	if err != nil {
		if sqlState(err) == "23P01" {
			return nil, ErrOverlappingCycle
		}
		return nil, fmt.Errorf("create cycle: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "open_payment_cycle", ResourceType: "payment_cycle", ResourceID: c.ID,
		After: map[string]any{
			"society_code": c.SocietyCode, "name": c.Name,
			"period_start":     c.PeriodStart.Format("2006-01-02"),
			"period_end":       c.PeriodEnd.Format("2006-01-02"),
			"deduction_policy": string(c.Policy), "currency": c.Currency,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}

const cycleCols = `id,tenant_id,society_code,name,period_start,period_end,currency,amount_scale,
	deduction_policy,status,gathered_at,approved_at,COALESCE(approved_by,''),paid_at,
	created_at,created_by`

func (r *repo) GetCycle(ctx context.Context, tenantID, id string) (*domain.Cycle, error) {
	return scanCycle(r.db.QueryRow(ctx,
		`SELECT `+cycleCols+` FROM payment_cycles
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id))
}

func (r *repo) ListCycles(ctx context.Context, tenantID, societyCode string, limit, offset int) ([]*domain.Cycle, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+cycleCols+` FROM payment_cycles
		WHERE tenant_id=$1 AND deleted_at IS NULL AND ($2='' OR society_code=$2)
		ORDER BY period_start DESC LIMIT $3 OFFSET $4`, tenantID, societyCode, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Cycle
	for rows.Next() {
		c, err := scanCycle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Gather writes an entire settlement run atomically.
//
// The transaction is the point. A run that wrote the deductions and then failed
// before the balances would have taken money from producers without recording
// that their debts had gone down, so the next fortnight would take it again —
// and every intermediate state here is a state in which somebody is being
// charged twice.
func (r *repo) Gather(ctx context.Context, cycleID string, g *Gathered) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var tenantID string
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT tenant_id,status FROM payment_cycles
		 WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, cycleID).Scan(&tenantID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// Re-read under the lock rather than trusting what the service checked
	// before it started computing. Two gathers of the same cycle running at once
	// would otherwise both pass the check and both write, and the collections
	// unique index would catch the lines while the recovery balances had already
	// been advanced twice.
	if status != string(domain.CycleOpen) {
		return &domain.ErrWrongStatus{
			What: "cycle", ID: cycleID, Is: status,
			Wanted: string(domain.CycleOpen), Action: "gather into",
		}
	}

	for _, l := range g.Lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cycle_lines
				(id,tenant_id,cycle_id,producer_ref,collection_id,collected_on,shift,
				 quantity,quantity_unit,rate,currency,amount_scale,amount_minor_units)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),$11,$12,$13)`,
			r.ids.New(), tenantID, cycleID, l.ProducerRef, l.CollectionID,
			l.CollectedOn, l.Shift, l.Quantity, l.QuantityUnit, l.Rate,
			l.Amount.Currency, l.Amount.Scale, l.Amount.Value); err != nil {
			if sqlState(err) == "23505" {
				return fmt.Errorf("%w: collection %s", ErrAlreadyGathered, l.CollectionID)
			}
			return fmt.Errorf("write line for collection %s: %w", l.CollectionID, err)
		}
	}

	for _, d := range g.Deductions {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cycle_deductions
				(id,tenant_id,cycle_id,recovery_id,producer_ref,kind,reference,
				 currency,amount_scale,amount_minor_units)
			VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),$8,$9,$10)`,
			r.ids.New(), tenantID, cycleID, d.RecoveryID, d.ProducerRef,
			string(d.Kind), d.Reference,
			d.Amount.Currency, d.Amount.Scale, d.Amount.Value); err != nil {
			return fmt.Errorf("write deduction against recovery %s: %w", d.RecoveryID, err)
		}
	}

	for id, recovered := range g.Recovered {
		// The status moves to SETTLED in the same statement that takes the
		// balance to the principal, so a debt cannot be left fully recovered and
		// still outstanding — which would show a producer a debt they have
		// already cleared.
		tag, err := tx.Exec(ctx, `
			UPDATE recoveries
			SET recovered_minor_units=$3,
			    status = CASE WHEN $3 >= principal_minor_units THEN 'SETTLED' ELSE status END,
			    updated_at=NOW(), updated_by=$4
			WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,
			tenantID, id, recovered.Value, g.Actor)
		if err != nil {
			if sqlState(err) == "23514" {
				return fmt.Errorf("%w: recovery %s", domain.ErrOverRecovered, id)
			}
			return fmt.Errorf("update recovery %s: %w", id, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("recovery %s was not there to be updated", id)
		}
	}

	for _, p := range g.Payables {
		if _, err := tx.Exec(ctx, `
			INSERT INTO producer_payables
				(id,tenant_id,cycle_id,producer_ref,currency,amount_scale,
				 gross_minor_units,deducted_minor_units,net_minor_units,
				 carried_forward_minor_units,status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			r.ids.New(), tenantID, cycleID, p.ProducerRef,
			p.Net.Currency, p.Net.Scale,
			p.Gross.Value, p.Deducted.Value, p.Net.Value,
			p.CarriedForward.Value, string(domain.Payable)); err != nil {
			return fmt.Errorf("write payable for %s: %w", p.ProducerRef, err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE payment_cycles SET status=$2, gathered_at=$3, updated_at=NOW(), updated_by=$4
		 WHERE tenant_id=$1 AND id=$5`,
		tenantID, string(domain.CycleGathered), g.At, g.Actor, cycleID); err != nil {
		return err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "gather_payment_cycle", ResourceType: "payment_cycle", ResourceID: cycleID,
		After: map[string]any{
			"lines": len(g.Lines), "deductions": len(g.Deductions),
			"payables": len(g.Payables), "recoveries_advanced": len(g.Recovered),
		},
		ServiceName: serviceName,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AbandonCycle releases everything a cycle held.
//
// The lines are deleted rather than marked, because the unique index that stops
// a collection being paid for twice is on the collection, and a soft-deleted
// line would keep it claimed forever. The cycle itself is kept: a society that
// gathered the wrong fortnight should be able to see that it did.
func (r *repo) AbandonCycle(ctx context.Context, tenantID, cycleID, actor string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM payment_cycles WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		tenantID, cycleID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// An approved cycle is one whose figures producers have been shown. A paid
	// one is money that has left. Neither is undone by abandoning it.
	if status == string(domain.CycleApproved) || status == string(domain.CyclePaid) {
		return &domain.ErrWrongStatus{
			What: "cycle", ID: cycleID, Is: status,
			Wanted: "open or gathered", Action: "abandon",
		}
	}

	// The recovery balances this cycle advanced have to come back down, or a
	// producer's debt stays reduced by a payment that is being un-made.
	rows, err := tx.Query(ctx,
		`SELECT recovery_id, amount_minor_units FROM cycle_deductions
		 WHERE tenant_id=$1 AND cycle_id=$2`, tenantID, cycleID)
	if err != nil {
		return err
	}
	type reversal struct {
		id     string
		amount int64
	}
	var reversals []reversal
	for rows.Next() {
		var v reversal
		if err := rows.Scan(&v.id, &v.amount); err != nil {
			rows.Close()
			return err
		}
		reversals = append(reversals, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, v := range reversals {
		if _, err := tx.Exec(ctx, `
			UPDATE recoveries
			SET recovered_minor_units = recovered_minor_units - $3,
			    status = CASE WHEN status='SETTLED' THEN 'OUTSTANDING' ELSE status END,
			    updated_at=NOW(), updated_by=$4
			WHERE tenant_id=$1 AND id=$2`, tenantID, v.id, v.amount, actor); err != nil {
			return fmt.Errorf("reverse the deduction against recovery %s: %w", v.id, err)
		}
	}

	for _, stmt := range []string{
		`DELETE FROM cycle_deductions WHERE tenant_id=$1 AND cycle_id=$2`,
		`DELETE FROM producer_payables WHERE tenant_id=$1 AND cycle_id=$2`,
		`DELETE FROM cycle_lines WHERE tenant_id=$1 AND cycle_id=$2`,
	} {
		if _, err := tx.Exec(ctx, stmt, tenantID, cycleID); err != nil {
			// The trigger on producer_payables refuses to delete one that has
			// been paid, which is how a cycle with money already out of the door
			// is stopped even though its status says otherwise.
			if sqlState(err) == "23514" {
				return fmt.Errorf("%w: this cycle has payables that have already been paid", ErrPaidIsFinal)
			}
			return err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE payment_cycles SET status=$3, gathered_at=NULL, updated_at=NOW(), updated_by=$4
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, cycleID, string(domain.CycleAbandoned), actor); err != nil {
		return err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "abandon_payment_cycle", ResourceType: "payment_cycle", ResourceID: cycleID,
		Before:      map[string]any{"status": status},
		After:       map[string]any{"status": string(domain.CycleAbandoned), "deductions_reversed": len(reversals)},
		ServiceName: serviceName,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ApproveCycle signs off a gathered cycle and its payables together.
func (r *repo) ApproveCycle(ctx context.Context, tenantID, cycleID, actor string) (*domain.Cycle, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM payment_cycles WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		tenantID, cycleID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != string(domain.CycleGathered) {
		return nil, &domain.ErrWrongStatus{
			What: "cycle", ID: cycleID, Is: status,
			Wanted: string(domain.CycleGathered), Action: "approve",
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE payment_cycles SET status=$3, approved_at=NOW(), approved_by=$4,
		     updated_at=NOW(), updated_by=$4
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, cycleID, string(domain.CycleApproved), actor); err != nil {
		return nil, err
	}
	// A held payable stays held. Approving a cycle is approving the figures, and
	// a hold is a decision about one producer that a bulk approval must not
	// quietly undo.
	if _, err := tx.Exec(ctx,
		`UPDATE producer_payables SET status=$3, approved_at=NOW(), approved_by=$4, updated_at=NOW()
		 WHERE tenant_id=$1 AND cycle_id=$2 AND status='PAYABLE'`,
		tenantID, cycleID, string(domain.PayableApproved), actor); err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "approve_payment_cycle", ResourceType: "payment_cycle", ResourceID: cycleID,
		Before:      map[string]any{"status": status},
		After:       map[string]any{"status": string(domain.CycleApproved)},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetCycle(ctx, tenantID, cycleID)
}

// ---------------------------------------------------------------------------
// Recoveries
// ---------------------------------------------------------------------------

func (r *repo) CreateRecovery(ctx context.Context, rec *domain.Recovery) (*domain.Recovery, error) {
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	rec.ID = r.ids.New()
	rec.Status = domain.Outstanding
	if _, err := tx.Exec(ctx, `
		INSERT INTO recoveries
			(id,tenant_id,producer_ref,kind,reference,currency,amount_scale,
			 principal_minor_units,recovered_minor_units,instalment_minor_units,
			 priority,status,opened_on,created_by,updated_by)
		VALUES ($1,$2,$3,$4,nullif($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$14)`,
		rec.ID, rec.TenantID, rec.ProducerRef, string(rec.Kind), rec.Reference,
		rec.Principal.Currency, rec.Principal.Scale,
		rec.Principal.Value, rec.Recovered.Value, rec.Instalment.Value,
		rec.Priority, string(rec.Status), rec.OpenedOn, rec.CreatedBy); err != nil {
		return nil, fmt.Errorf("create recovery: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "open_recovery", ResourceType: "recovery", ResourceID: rec.ID,
		After: map[string]any{
			"producer_ref": rec.ProducerRef, "kind": string(rec.Kind),
			"principal": rec.Principal.String(), "instalment": rec.Instalment.String(),
			"priority": rec.Priority, "opened_on": rec.OpenedOn.Format("2006-01-02"),
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return rec, tx.Commit(ctx)
}

const recoveryCols = `id,tenant_id,producer_ref,kind,COALESCE(reference,''),currency,amount_scale,
	principal_minor_units,recovered_minor_units,instalment_minor_units,
	priority,status,opened_on,created_at,created_by`

// ListRecoveries returns a producer's debts in the order they are served.
//
// Ordered here as well as in the arithmetic. The arithmetic sorts because it
// must not depend on what the database returned; the database orders because a
// person reading this list should see the same sequence the settlement will use.
func (r *repo) ListRecoveries(ctx context.Context, tenantID, producerRef string, outstandingOnly bool) ([]*domain.Recovery, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+recoveryCols+` FROM recoveries
		WHERE tenant_id=$1 AND deleted_at IS NULL
		  AND ($2='' OR producer_ref=$2)
		  AND (NOT $3 OR status='OUTSTANDING')
		ORDER BY producer_ref, priority, opened_on, id`,
		tenantID, producerRef, outstandingOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Recovery
	for rows.Next() {
		rec, err := scanRecovery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Payables
// ---------------------------------------------------------------------------

const payableCols = `id,tenant_id,cycle_id,producer_ref,currency,amount_scale,
	gross_minor_units,deducted_minor_units,net_minor_units,carried_forward_minor_units,
	status,approved_at,COALESCE(approved_by,''),paid_at,COALESCE(paid_by,''),
	COALESCE(payment_reference,''),COALESCE(held_reason,''),created_at`

func (r *repo) ListPayables(ctx context.Context, tenantID, cycleID string) ([]*domain.ProducerPayable, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+payableCols+` FROM producer_payables
		 WHERE tenant_id=$1 AND cycle_id=$2 ORDER BY producer_ref`, tenantID, cycleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ProducerPayable
	for rows.Next() {
		p, err := scanPayable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *repo) GetPayable(ctx context.Context, tenantID, id string) (*domain.ProducerPayable, error) {
	return scanPayable(r.db.QueryRow(ctx,
		`SELECT `+payableCols+` FROM producer_payables WHERE tenant_id=$1 AND id=$2`, tenantID, id))
}

// MarkPaid records that money left the society.
//
// Only an approved payable can be paid, and the check is under a row lock so two
// operators clicking at once cannot both succeed. Paying twice is the failure
// that matters here: the second payment is real money and the row would look
// exactly as it does after the first.
func (r *repo) MarkPaid(ctx context.Context, tenantID, id, reference, actor string, at time.Time) (*domain.ProducerPayable, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM producer_payables WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		tenantID, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != string(domain.PayableApproved) {
		return nil, &domain.ErrWrongStatus{
			What: "payable", ID: id, Is: status,
			Wanted: string(domain.PayableApproved), Action: "pay",
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE producer_payables
		 SET status=$3, paid_at=$4, paid_by=$5, payment_reference=nullif($6,''), updated_at=NOW()
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, string(domain.PayablePaid), at, actor, reference); err != nil {
		if sqlState(err) == "23514" {
			return nil, ErrPaidIsFinal
		}
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "pay_producer", ResourceType: "producer_payable", ResourceID: id,
		Before: map[string]any{"status": status},
		After: map[string]any{
			"status": string(domain.PayablePaid), "payment_reference": reference,
			"paid_at": at,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPayable(ctx, tenantID, id)
}

func (r *repo) HoldPayable(ctx context.Context, tenantID, id, reason, actor string) (*domain.ProducerPayable, error) {
	if reason == "" {
		return nil, errors.New("holding a producer's payment is a thing done to a person, so it has to say why")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM producer_payables WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		tenantID, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status == string(domain.PayablePaid) {
		return nil, ErrPaidIsFinal
	}

	if _, err := tx.Exec(ctx,
		`UPDATE producer_payables SET status=$3, held_reason=$4, updated_at=NOW()
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, string(domain.PayableHeld), reason); err != nil {
		return nil, err
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "hold_producer_payment", ResourceType: "producer_payable", ResourceID: id,
		Before:      map[string]any{"status": status},
		After:       map[string]any{"status": string(domain.PayableHeld), "reason": reason},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPayable(ctx, tenantID, id)
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

// Statement assembles what a producer is handed.
func (r *repo) Statement(ctx context.Context, tenantID, cycleID, producerRef string) (*domain.Statement, error) {
	cycle, err := r.GetCycle(ctx, tenantID, cycleID)
	if err != nil {
		return nil, err
	}
	s := &domain.Statement{
		TenantID: tenantID, ProducerRef: producerRef, Cycle: cycle,
		Quantities: map[string]string{},
	}

	rows, err := r.db.Query(ctx, `
		SELECT id,collection_id,collected_on,shift,quantity,quantity_unit,COALESCE(rate,''),
		       currency,amount_scale,amount_minor_units
		FROM cycle_lines
		WHERE tenant_id=$1 AND cycle_id=$2 AND producer_ref=$3
		ORDER BY collected_on, shift`, tenantID, cycleID, producerRef)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		l := &domain.Line{TenantID: tenantID, CycleID: cycleID, ProducerRef: producerRef}
		if err := rows.Scan(&l.ID, &l.CollectionID, &l.CollectedOn, &l.Shift,
			&l.Quantity, &l.QuantityUnit, &l.Rate,
			&l.Amount.Currency, &l.Amount.Scale, &l.Amount.Value); err != nil {
			rows.Close()
			return nil, err
		}
		s.Lines = append(s.Lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	drows, err := r.db.Query(ctx, `
		SELECT id,recovery_id,kind,COALESCE(reference,''),currency,amount_scale,amount_minor_units
		FROM cycle_deductions
		WHERE tenant_id=$1 AND cycle_id=$2 AND producer_ref=$3
		ORDER BY kind, id`, tenantID, cycleID, producerRef)
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		d := &domain.Deduction{TenantID: tenantID, CycleID: cycleID, ProducerRef: producerRef}
		var kind string
		if err := drows.Scan(&d.ID, &d.RecoveryID, &kind, &d.Reference,
			&d.Amount.Currency, &d.Amount.Scale, &d.Amount.Value); err != nil {
			drows.Close()
			return nil, err
		}
		d.Kind = domain.RecoveryKind(kind)
		s.Deductions = append(s.Deductions, d)
	}
	drows.Close()
	if err := drows.Err(); err != nil {
		return nil, err
	}

	p, err := scanPayable(r.db.QueryRow(ctx,
		`SELECT `+payableCols+` FROM producer_payables
		 WHERE tenant_id=$1 AND cycle_id=$2 AND producer_ref=$3`, tenantID, cycleID, producerRef))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	s.Payable = p

	// The period's quantity, per unit. Litres and kilograms are not added
	// together: a society recording some collections in one and some in the
	// other has a data problem, and one number would hide it.
	qrows, err := r.db.Query(ctx, `
		SELECT quantity_unit, SUM(quantity::numeric)::text
		FROM cycle_lines WHERE tenant_id=$1 AND cycle_id=$2 AND producer_ref=$3
		GROUP BY quantity_unit`, tenantID, cycleID, producerRef)
	if err != nil {
		return nil, err
	}
	defer qrows.Close()
	for qrows.Next() {
		var unit, total string
		if err := qrows.Scan(&unit, &total); err != nil {
			return nil, err
		}
		s.Quantities[unit] = total
	}
	return s, qrows.Err()
}

// ---------------------------------------------------------------------------

type scanner interface{ Scan(dest ...any) error }

func scanCycle(s scanner) (*domain.Cycle, error) {
	var c domain.Cycle
	var policy, status string
	err := s.Scan(&c.ID, &c.TenantID, &c.SocietyCode, &c.Name,
		&c.PeriodStart, &c.PeriodEnd, &c.Currency, &c.AmountScale,
		&policy, &status, &c.GatheredAt, &c.ApprovedAt, &c.ApprovedBy, &c.PaidAt,
		&c.CreatedAt, &c.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Policy = domain.DeductionPolicy(policy)
	c.Status = domain.CycleStatus(status)
	return &c, nil
}

func scanRecovery(s scanner) (*domain.Recovery, error) {
	var r domain.Recovery
	var kind, status string
	var currency string
	var scale int32
	var principal, recovered, instalment int64
	err := s.Scan(&r.ID, &r.TenantID, &r.ProducerRef, &kind, &r.Reference,
		&currency, &scale, &principal, &recovered, &instalment,
		&r.Priority, &status, &r.OpenedOn, &r.CreatedAt, &r.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Kind = domain.RecoveryKind(kind)
	r.Status = domain.RecoveryStatus(status)
	r.Principal = money.Money{Value: principal, Scale: scale, Currency: currency}
	r.Recovered = money.Money{Value: recovered, Scale: scale, Currency: currency}
	r.Instalment = money.Money{Value: instalment, Scale: scale, Currency: currency}
	return &r, nil
}

func scanPayable(s scanner) (*domain.ProducerPayable, error) {
	var p domain.ProducerPayable
	var status, currency string
	var scale int32
	var gross, deducted, net, carried int64
	err := s.Scan(&p.ID, &p.TenantID, &p.CycleID, &p.ProducerRef, &currency, &scale,
		&gross, &deducted, &net, &carried,
		&status, &p.ApprovedAt, &p.ApprovedBy, &p.PaidAt, &p.PaidBy,
		&p.PaymentReference, &p.HeldReason, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Status = domain.PayableStatus(status)
	p.Gross = money.Money{Value: gross, Scale: scale, Currency: currency}
	p.Deducted = money.Money{Value: deducted, Scale: scale, Currency: currency}
	p.Net = money.Money{Value: net, Scale: scale, Currency: currency}
	p.CarriedForward = money.Money{Value: carried, Scale: scale, Currency: currency}
	return &p, nil
}

func sqlState(err error) string {
	type coded interface{ SQLState() string }
	var c coded
	if errors.As(err, &c) {
		return c.SQLState()
	}
	return ""
}
