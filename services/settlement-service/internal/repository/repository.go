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
	// RaiseAdjustment records money owed after a cycle was already settled.
	RaiseAdjustment(ctx context.Context, p *domain.ProducerPayable, actor string) (*domain.ProducerPayable, error)
	// ApprovePayable signs off one payable, which is how an adjustment raised
	// after its cycle was approved gets approved at all.
	ApprovePayable(ctx context.Context, tenantID, id, actor string) (*domain.ProducerPayable, error)
	HoldPayable(ctx context.Context, tenantID, id, reason, actor string) (*domain.ProducerPayable, error)

	Statement(ctx context.Context, tenantID, cycleID, producerRef string) (*domain.Statement, error)

	// The notification outbox. NotificationsToDeliver reads across tenants
	// through a definer-rights function; the other two run under the row's
	// tenant, which the caller puts on the context.
	NotificationsToDeliver(ctx context.Context, limit int) ([]*domain.QueuedNotification, error)
	NotificationDelivered(ctx context.Context, id string) error
	NotificationFailed(ctx context.Context, id, reason string) error
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
				 carried_forward_minor_units,status,kind)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			r.ids.New(), tenantID, cycleID, p.ProducerRef,
			p.Net.Currency, p.Net.Scale,
			p.Gross.Value, p.Deducted.Value, p.Net.Value,
			p.CarriedForward.Value, string(domain.Payable), string(domain.KindSettlement)); err != nil {
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

	var status, name string
	if err := tx.QueryRow(ctx,
		`SELECT status, name FROM payment_cycles WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		tenantID, cycleID).Scan(&status, &name); err != nil {
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
	approved, err := tx.Exec(ctx,
		`UPDATE producer_payables SET status=$3, approved_at=NOW(), approved_by=$4, updated_at=NOW()
		 WHERE tenant_id=$1 AND cycle_id=$2 AND status='PAYABLE' AND kind='SETTLEMENT'`,
		tenantID, cycleID, string(domain.PayableApproved), actor)
	if err != nil {
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
	if err := r.queueForRoles(ctx, tx, tenantID, actor, domain.QueuedNotification{
		Event: domain.EventCycleApproved,
		Title: fmt.Sprintf("Cycle %s approved", name),
		Body: fmt.Sprintf("%s approved the %s cycle: %d producers' payments may now be paid",
			actor, name, approved.RowsAffected()),
		ReferenceType: "payment_cycle", ReferenceID: cycleID,
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
	COALESCE(payment_reference,''),COALESCE(held_reason,''),created_at,
	kind,COALESCE(adjusts_payable_id,''),COALESCE(reason,'')`

// RaiseAdjustment records money that turned out to be owed after a cycle was
// settled.
//
// The cycle is not required to be in any particular state, and that is the
// point: an adjustment exists precisely because the cycle is finished. Refusing
// one against a paid cycle would leave a wrong payment with no remedy, which is
// the situation this was added to end.
func (r *repo) RaiseAdjustment(ctx context.Context, p *domain.ProducerPayable, actor string) (*domain.ProducerPayable, error) {
	if p.Reason == "" {
		return nil, domain.ErrNoAdjustmentReason
	}
	if p.Net.IsZero() {
		return nil, domain.ErrZeroAdjustment
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// The cycle has to exist and belong to this tenant. Without the check an
	// adjustment could name any cycle id and the composite foreign key would be
	// the only thing objecting, several layers down.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM payment_cycles WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`,
		p.TenantID, p.CycleID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	if p.AdjustsPayableID != "" {
		var adjusts bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM producer_payables
			 WHERE tenant_id=$1 AND id=$2 AND producer_ref=$3)`,
			p.TenantID, p.AdjustsPayableID, p.ProducerRef).Scan(&adjusts); err != nil {
			return nil, err
		}
		// An adjustment that names another producer's payment is either a typo
		// or somebody moving money between members with a reason field.
		if !adjusts {
			return nil, fmt.Errorf("%w: payable %s is not %s's", ErrNotFound,
				p.AdjustsPayableID, p.ProducerRef)
		}
	}

	p.ID = r.ids.New()
	p.Kind = domain.KindAdjustment
	p.Status = domain.Payable
	if _, err := tx.Exec(ctx, `
		INSERT INTO producer_payables
			(id,tenant_id,cycle_id,producer_ref,currency,amount_scale,
			 gross_minor_units,deducted_minor_units,net_minor_units,
			 carried_forward_minor_units,status,kind,adjusts_payable_id,reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,0,$7,0,$8,$9,nullif($10,''),$11)`,
		p.ID, p.TenantID, p.CycleID, p.ProducerRef, p.Net.Currency, p.Net.Scale,
		p.Net.Value, string(p.Status), string(p.Kind), p.AdjustsPayableID, p.Reason); err != nil {
		return nil, fmt.Errorf("raise adjustment: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "raise_adjustment", ResourceType: "producer_payable", ResourceID: p.ID,
		After: map[string]any{
			"cycle_id": p.CycleID, "producer_ref": p.ProducerRef,
			"amount": p.Net.String(), "reason": p.Reason,
			"adjusts_payable_id": p.AdjustsPayableID,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := r.queueForRoles(ctx, tx, p.TenantID, actor, domain.QueuedNotification{
		Event: domain.EventAdjustmentRaised,
		Title: fmt.Sprintf("Adjustment of %s raised for %s", p.Net, p.ProducerRef),
		Body: fmt.Sprintf("%s raised an adjustment of %s for %s outside the settlement: %s",
			actor, p.Net, p.ProducerRef, p.Reason),
		ReferenceType: "producer_payable", ReferenceID: p.ID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPayable(ctx, p.TenantID, p.ID)
}

// ApprovePayable signs off one payable.
//
// ApproveCycle approves a whole gathered cycle at once and refuses to run
// against a cycle that has already been approved — which is every cycle an
// adjustment is raised against. So an adjustment needs its own approval, and it
// gets the same rule: only a PAYABLE one can be approved, and a held one stays
// held until somebody deals with the hold.
func (r *repo) ApprovePayable(ctx context.Context, tenantID, id, actor string) (*domain.ProducerPayable, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// What a message about this payable needs, read under the same lock as the
	// status. The notification is queued in this transaction, so it has to be
	// composed from what this transaction saw.
	var status, producerRef, cycleID, currency string
	var scale int32
	var net int64
	if err := tx.QueryRow(ctx,
		`SELECT status, producer_ref, cycle_id, currency, amount_scale, net_minor_units
		   FROM producer_payables WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		tenantID, id).Scan(&status, &producerRef, &cycleID, &currency, &scale, &net); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != string(domain.Payable) {
		return nil, &domain.ErrWrongStatus{
			What: "payable", ID: id, Is: status,
			Wanted: string(domain.Payable), Action: "approve",
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE producer_payables SET status=$3, approved_at=NOW(), approved_by=$4, updated_at=NOW()
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, string(domain.PayableApproved), actor); err != nil {
		return nil, err
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "approve_payable", ResourceType: "producer_payable", ResourceID: id,
		Before:      map[string]any{"status": status},
		After:       map[string]any{"status": string(domain.PayableApproved)},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := r.queueForRoles(ctx, tx, tenantID, actor, domain.QueuedNotification{
		Event:         domain.EventPayableApproved,
		Title:         fmt.Sprintf("Payment to %s approved", producerRef),
		Body:          fmt.Sprintf("%s's payment of %s was approved by %s and may now be paid", producerRef, amount(net, scale, currency), actor),
		ReferenceType: "producer_payable", ReferenceID: id,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPayable(ctx, tenantID, id)
}

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

	// What a message about this payable needs, read under the same lock as the
	// status. The notification is queued in this transaction, so it has to be
	// composed from what this transaction saw.
	var status, producerRef, cycleID, currency string
	var scale int32
	var net int64
	if err := tx.QueryRow(ctx,
		`SELECT status, producer_ref, cycle_id, currency, amount_scale, net_minor_units
		   FROM producer_payables WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		tenantID, id).Scan(&status, &producerRef, &cycleID, &currency, &scale, &net); err != nil {
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
	body := fmt.Sprintf("%s was paid %s by %s", producerRef, amount(net, scale, currency), actor)
	if reference != "" {
		body += ", reference " + reference
	}
	if err := r.queueForRoles(ctx, tx, tenantID, actor, domain.QueuedNotification{
		Event:         domain.EventPayablePaid,
		Title:         fmt.Sprintf("%s paid", producerRef),
		Body:          body,
		ReferenceType: "producer_payable", ReferenceID: id,
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

	// What a message about this payable needs, read under the same lock as the
	// status. The notification is queued in this transaction, so it has to be
	// composed from what this transaction saw.
	var status, producerRef, cycleID, currency string
	var scale int32
	var net int64
	if err := tx.QueryRow(ctx,
		`SELECT status, producer_ref, cycle_id, currency, amount_scale, net_minor_units
		   FROM producer_payables WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		tenantID, id).Scan(&status, &producerRef, &cycleID, &currency, &scale, &net); err != nil {
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
	if err := r.queueForRoles(ctx, tx, tenantID, actor, domain.QueuedNotification{
		Event:         domain.EventPayableHeld,
		Title:         fmt.Sprintf("Payment to %s held", producerRef),
		Body:          fmt.Sprintf("%s's payment of %s is held: %s", producerRef, amount(net, scale, currency), reason),
		ReferenceType: "producer_payable", ReferenceID: id,
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

	// The settlement row specifically. Without the kind filter this returns
	// whichever of the fortnight and its adjustments the database happened to
	// yield, and a statement would show an adjustment where the fortnight goes.
	p, err := scanPayable(r.db.QueryRow(ctx,
		`SELECT `+payableCols+` FROM producer_payables
		 WHERE tenant_id=$1 AND cycle_id=$2 AND producer_ref=$3 AND kind='SETTLEMENT'`,
		tenantID, cycleID, producerRef))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Checked rather than assumed. The query above filters on kind, and the
	// partial unique index means at most one row can match — but a query that
	// lost the filter would return an arbitrary one of several rows without
	// complaining, and the statement would show a correction where the
	// fortnight goes. Which row comes back first is physical order, so no test
	// can force the bad case; this makes it loud if it ever happens.
	if p != nil && p.Kind != domain.KindSettlement {
		return nil, fmt.Errorf("the statement for %s in cycle %s picked up a %s payable where "+
			"the fortnight's own figures belong", producerRef, cycleID, p.Kind)
	}
	s.Payable = p

	arows, err := r.db.Query(ctx,
		`SELECT `+payableCols+` FROM producer_payables
		 WHERE tenant_id=$1 AND cycle_id=$2 AND producer_ref=$3 AND kind='ADJUSTMENT'
		 ORDER BY created_at, id`, tenantID, cycleID, producerRef)
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		a, err := scanPayable(arows)
		if err != nil {
			arows.Close()
			return nil, err
		}
		s.Adjustments = append(s.Adjustments, a)
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return nil, err
	}

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
	var kind string
	err := s.Scan(&p.ID, &p.TenantID, &p.CycleID, &p.ProducerRef, &currency, &scale,
		&gross, &deducted, &net, &carried,
		&status, &p.ApprovedAt, &p.ApprovedBy, &p.PaidAt, &p.PaidBy,
		&p.PaymentReference, &p.HeldReason, &p.CreatedAt,
		&kind, &p.AdjustsPayableID, &p.Reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Status = domain.PayableStatus(status)
	p.Kind = domain.PayableKind(kind)
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

// ---------------------------------------------------------------------------
// Notifications owed
// ---------------------------------------------------------------------------

// queueForRoles writes one outbox row per role the event is routed to, inside
// the transaction that makes the change.
//
// Inside it, deliberately. A message sent from the handler after the commit is
// lost whenever notification-service is down, and lost silently — the hold
// succeeds and nobody is told. A row in the same transaction exists exactly when
// the change does, and stays until it has been delivered.
func (r *repo) queueForRoles(ctx context.Context, tx pgx.Tx, tenantID, actor string, n domain.QueuedNotification) error {
	for _, role := range domain.RecipientsFor(n.Event) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_outbox
				(id, tenant_id, event, recipient_type, recipient_id, title, body,
				 reference_type, reference_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			r.ids.New(), tenantID, string(n.Event), domain.RecipientRole, role,
			n.Title, n.Body, n.ReferenceType, n.ReferenceID, actor); err != nil {
			return fmt.Errorf("queue notification for %s: %w", role, err)
		}
	}
	return nil
}

// amount renders a figure for a message, from the columns a row holds it in.
func amount(minorUnits int64, scale int32, currency string) string {
	m, err := money.New(minorUnits, scale, currency)
	if err != nil {
		// A figure that cannot be rendered is still a figure somebody should be
		// told about; the raw units are better than an empty string.
		return fmt.Sprintf("%d (scale %d) %s", minorUnits, scale, currency)
	}
	return m.String()
}

const outboxCols = `id, tenant_id, event, recipient_type, recipient_id, title, body,
	reference_type, reference_id, created_at, created_by, attempts,
	COALESCE(last_error, ''), delivered_at`

// NotificationsToDeliver is every row due for a try, across every tenant.
//
// Through the definer-rights function in schema.sql, because the isolation
// policies refuse a read with no tenant set — correctly — and the sweep has no
// single tenant. This is the one place settlement reads across tenants, and the
// function limits it to this table and this question.
func (r *repo) NotificationsToDeliver(ctx context.Context, limit int) ([]*domain.QueuedNotification, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+outboxCols+` FROM gavya_settlement_notifications_to_deliver($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("read the notification outbox: %w", err)
	}
	defer rows.Close()
	var out []*domain.QueuedNotification
	for rows.Next() {
		n := &domain.QueuedNotification{}
		var event string
		if err := rows.Scan(&n.ID, &n.TenantID, &event, &n.RecipientType, &n.RecipientID,
			&n.Title, &n.Body, &n.ReferenceType, &n.ReferenceID, &n.CreatedAt, &n.CreatedBy,
			&n.Attempts, &n.LastError, &n.DeliveredAt); err != nil {
			return nil, err
		}
		n.Event = domain.NotificationEvent(event)
		out = append(out, n)
	}
	return out, rows.Err()
}

// NotificationDelivered records that notification-service accepted a message.
//
// Under the row's tenant, which the caller has put on the context. Machine
// state: the delivery bookkeeping of a message, not a change anybody made.
func (r *repo) NotificationDelivered(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE notification_outbox
		   SET attempts = attempts + 1, last_attempt_at = NOW(), last_error = NULL,
		       delivered_at = NOW()
		 WHERE id = $1 AND delivered_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// NotificationFailed records one failed try and why.
func (r *repo) NotificationFailed(ctx context.Context, id, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE notification_outbox
		   SET attempts = attempts + 1, last_attempt_at = NOW(), last_error = $2
		 WHERE id = $1 AND delivered_at IS NULL`, id, reason)
	return err
}
