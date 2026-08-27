// Package repository is production's access to its tables.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/quantity"

	"github.com/ppusapati/gavya/services/production-service/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicateCode is a batch code the tenant already uses. Two vessels
	// labelled the same is a recall that reaches the wrong one.
	ErrDuplicateCode = errors.New("a batch with that code already exists")
	// ErrDuplicateInput is the same lot recorded twice into one vessel.
	ErrDuplicateInput = errors.New("that lot is already recorded as an input to this batch")
	// ErrOverlappingVersion is two versions of one recipe in force at once.
	ErrOverlappingVersion = errors.New("that recipe already has a version in force over part of that period")
	// ErrRefused carries a message from one of the triggers — a cycle, an
	// overdraw, a unit mismatch, a held lot. They are refusals a person can act
	// on, and the trigger's own words are better than anything wrapped round
	// them, so they are passed through.
	ErrRefused = errors.New("refused")
)

const serviceName = "production-service"

type IDs interface{ New() string }

type Repository interface {
	CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error)
	GetBatch(ctx context.Context, tenantID, id string) (*domain.Batch, error)
	GetBatchByCode(ctx context.Context, tenantID, code string) (*domain.Batch, error)
	ListBatches(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]*domain.Batch, error)
	SetStatus(ctx context.Context, tenantID, id string, status domain.Status, reason, actor string) (*domain.Batch, error)

	RecordInput(ctx context.Context, in *domain.Input, actor string) (*domain.Input, error)
	Inputs(ctx context.Context, tenantID, batchID string) ([]domain.Input, error)

	// Edges loads every input line touching the given batches, on either side.
	// One call serves one level of a walk.
	Edges(ctx context.Context, tenantID string, batchIDs []string) ([]domain.Edge, error)

	// Remaining is what a batch has left after everything drawn from it.
	Remaining(ctx context.Context, tenantID, batchID string) (quantity.Quantity, error)

	CreateFormulation(ctx context.Context, f *domain.Formulation, ins []domain.FormulationInput) (*domain.Formulation, []domain.FormulationInput, error)
	GetFormulation(ctx context.Context, tenantID, id string) (*domain.Formulation, error)
	FormulationInForce(ctx context.Context, tenantID, code string, at time.Time) (*domain.Formulation, error)
	ListFormulations(ctx context.Context, tenantID string) ([]*domain.Formulation, error)
	FormulationInputs(ctx context.Context, tenantID, formulationID string) ([]domain.FormulationInput, error)

	// Lots is what actually went into a batch, each named by what it is.
	Lots(ctx context.Context, tenantID, batchID string) ([]domain.Lot, error)

	// ObservedHistory is every batch made under a recipe, summarised.
	ObservedHistory(ctx context.Context, tenantID, formulationID string) (*domain.ObservedHistory, error)
}

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

func New(db *pgxpool.Pool, ids IDs) Repository { return &repo{db: db, ids: ids} }

func (r *repo) CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	b.ID = r.ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO production_batches
			(id,tenant_id,batch_code,kind,product_ref,produced_value,produced_unit,
			 produced_at,produced_by,source_kind,source_ref,formulation_id,
			 status,status_reason,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),nullif($11,''),nullif($12,''),$13,nullif($14,''),$15,$15)`,
		b.ID, b.TenantID, b.Code, string(b.Kind), b.ProductRef,
		b.Produced.Value(), string(b.Produced.Unit),
		b.ProducedAt, b.ProducedBy, string(b.SourceKind), b.SourceRef, b.FormulationID,
		string(b.Status), b.StatusReason, b.CreatedBy); err != nil {
		switch sqlState(err) {
		case "23505":
			return nil, fmt.Errorf("%w: %s", ErrDuplicateCode, b.Code)
		case "23503":
			return nil, fmt.Errorf("%w: recipe %s is not one of this tenant's", ErrNotFound,
				b.FormulationID)
		case "23514":
			// The recipe is for a different product, or was not in force on the
			// day. Both are the trigger's words and both are worth reading.
			return nil, fmt.Errorf("%w: %s", ErrRefused, triggerMessage(err))
		}
		return nil, fmt.Errorf("create batch: %w", err)
	}

	after := map[string]any{
		"batch_code": b.Code, "kind": string(b.Kind), "product_ref": b.ProductRef,
		"produced": b.Produced.Describe(), "status": string(b.Status),
	}
	if b.Kind == domain.Raw {
		after["source_kind"], after["source_ref"] = string(b.SourceKind), b.SourceRef
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "create_batch", ResourceType: "production_batch", ResourceID: b.ID,
		After: after, ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return b, tx.Commit(ctx)
}

const batchCols = `id,tenant_id,batch_code,kind,product_ref,produced_value,produced_unit,
	produced_at,produced_by,COALESCE(source_kind,''),COALESCE(source_ref,''),
	COALESCE(formulation_id,''),
	status,COALESCE(status_reason,''),created_at,updated_at,created_by,updated_by`

func (r *repo) GetBatch(ctx context.Context, tenantID, id string) (*domain.Batch, error) {
	return scanBatch(r.db.QueryRow(ctx,
		`SELECT `+batchCols+` FROM production_batches
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id))
}

func (r *repo) GetBatchByCode(ctx context.Context, tenantID, code string) (*domain.Batch, error) {
	return scanBatch(r.db.QueryRow(ctx,
		`SELECT `+batchCols+` FROM production_batches
		 WHERE tenant_id=$1 AND batch_code=$2 AND deleted_at IS NULL`, tenantID, code))
}

func (r *repo) ListBatches(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]*domain.Batch, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+batchCols+` FROM production_batches
		 WHERE tenant_id=$1 AND deleted_at IS NULL AND produced_at >= $2 AND produced_at <= $3
		 ORDER BY produced_at, batch_code LIMIT $4`, tenantID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetStatus quarantines, recalls, releases or disposes of a batch.
//
// The before-state goes into the audit entry as well as the after. A hold that
// was lifted is the thing somebody asks about afterwards, and a record that
// only says what the status became cannot answer it.
func (r *repo) SetStatus(ctx context.Context, tenantID, id string, status domain.Status, reason, actor string) (*domain.Batch, error) {
	if !domain.ValidStatus(status) {
		return nil, errors.New("a batch must be OPEN, RELEASED, QUARANTINED, RECALLED or DISPOSED")
	}
	if status.NeedsReason() && reason == "" {
		return nil, domain.ErrHoldNeedsReason
	}
	before, err := r.GetBatch(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if before.Status.Held() && !status.Held() && reason == "" {
		return nil, errors.New("lifting a hold must say why; a batch that was quarantined and is " +
			"now released with no explanation is one nobody can defend afterwards")
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, `
		UPDATE production_batches SET status=$3, status_reason=nullif($4,''),
			updated_at=NOW(), updated_by=$5
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,
		tenantID, id, string(status), reason, actor); err != nil {
		return nil, fmt.Errorf("set status: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "set_batch_status", ResourceType: "production_batch", ResourceID: id,
		Before: map[string]any{
			"status": string(before.Status), "status_reason": before.StatusReason,
		},
		After: map[string]any{
			"batch_code": before.Code, "status": string(status), "status_reason": reason,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetBatch(ctx, tenantID, id)
}

// RecordInput writes one line saying a lot went into a batch.
//
// Four database rules can refuse it and every one of them is a real thing that
// happens in a plant: a cycle, drawing more than the lot holds, a unit mismatch,
// and a lot under hold. Their messages are written for the person who caused
// them, so they are passed through rather than translated into something about
// a constraint.
func (r *repo) RecordInput(ctx context.Context, in *domain.Input, actor string) (*domain.Input, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	in.ID = r.ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO production_inputs
			(id,tenant_id,output_batch_id,input_batch_id,consumed_value,consumed_unit,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		in.ID, in.TenantID, in.OutputBatchID, in.InputBatchID,
		in.Consumed.Value(), string(in.Consumed.Unit), actor); err != nil {
		switch sqlState(err) {
		case "23505":
			return nil, ErrDuplicateInput
		case "23503":
			return nil, fmt.Errorf("%w: one of those batches does not exist in this tenant", ErrNotFound)
		case "23514":
			return nil, fmt.Errorf("%w: %s", ErrRefused, triggerMessage(err))
		}
		return nil, fmt.Errorf("record input: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "record_input", ResourceType: "production_batch", ResourceID: in.OutputBatchID,
		After: map[string]any{
			"input_batch_id": in.InputBatchID, "consumed": in.Consumed.Describe(),
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return in, tx.Commit(ctx)
}

func (r *repo) Inputs(ctx context.Context, tenantID, batchID string) ([]domain.Input, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id,tenant_id,output_batch_id,input_batch_id,consumed_value,consumed_unit,created_at,created_by
		 FROM production_inputs WHERE tenant_id=$1 AND output_batch_id=$2
		 ORDER BY created_at, id`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Input
	for rows.Next() {
		var in domain.Input
		var value int64
		var unit string
		if err := rows.Scan(&in.ID, &in.TenantID, &in.OutputBatchID, &in.InputBatchID,
			&value, &unit, &in.CreatedAt, &in.CreatedBy); err != nil {
			return nil, err
		}
		q, err := quantity.New(value, quantity.Unit(unit))
		if err != nil {
			return nil, err
		}
		in.Consumed = q
		out = append(out, in)
	}
	return out, rows.Err()
}

// Edges serves one level of a walk: every input line with one end in the given
// set. Both ends are queried in one statement so that a walk in either
// direction costs the same, and so that a caller cannot get the direction of
// the query wrong — the direction lives in the walk, where it is tested.
func (r *repo) Edges(ctx context.Context, tenantID string, batchIDs []string) ([]domain.Edge, error) {
	if len(batchIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT output_batch_id,input_batch_id,consumed_value,consumed_unit
		 FROM production_inputs
		 WHERE tenant_id=$1 AND (output_batch_id = ANY($2) OR input_batch_id = ANY($2))`,
		tenantID, batchIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Edge
	for rows.Next() {
		var e domain.Edge
		var value int64
		var unit string
		if err := rows.Scan(&e.OutputBatchID, &e.InputBatchID, &value, &unit); err != nil {
			return nil, err
		}
		q, err := quantity.New(value, quantity.Unit(unit))
		if err != nil {
			return nil, err
		}
		e.Consumed = q
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *repo) Remaining(ctx context.Context, tenantID, batchID string) (quantity.Quantity, error) {
	var value int64
	var unit string
	err := r.db.QueryRow(ctx,
		`SELECT remaining_value, produced_unit FROM gavya_batch_balance
		 WHERE tenant_id=$1 AND id=$2`, tenantID, batchID).Scan(&value, &unit)
	if errors.Is(err, pgx.ErrNoRows) {
		return quantity.Quantity{}, ErrNotFound
	}
	if err != nil {
		return quantity.Quantity{}, err
	}
	return quantity.New(value, quantity.Unit(unit))
}

type scanner interface{ Scan(dest ...any) error }

func scanBatch(s scanner) (*domain.Batch, error) {
	var out domain.Batch
	var kind, unit, sourceKind, status string
	var value int64
	err := s.Scan(&out.ID, &out.TenantID, &out.Code, &kind, &out.ProductRef,
		&value, &unit, &out.ProducedAt, &out.ProducedBy,
		&sourceKind, &out.SourceRef, &out.FormulationID,
		&status, &out.StatusReason,
		&out.CreatedAt, &out.UpdatedAt, &out.CreatedBy, &out.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	q, err := quantity.New(value, quantity.Unit(unit))
	if err != nil {
		return nil, err
	}
	out.Produced = q
	out.Kind = domain.Kind(kind)
	out.SourceKind = domain.SourceKind(sourceKind)
	out.Status = domain.Status(status)
	return &out, nil
}

// triggerMessage pulls the trigger's own sentence out of the driver's error.
//
// pgx renders a raised exception as `ERROR: <message> (SQLSTATE 23514)`, and
// what a person needs is the message. Where the shape is not what is expected
// the whole error is returned rather than an empty string, because a refusal
// with no reason on it is worse than a verbose one.
func triggerMessage(err error) string {
	s := err.Error()
	if i := strings.Index(s, "ERROR: "); i >= 0 {
		s = s[i+len("ERROR: "):]
	}
	if i := strings.Index(s, " (SQLSTATE"); i >= 0 {
		s = s[:i]
	}
	if strings.TrimSpace(s) == "" {
		return err.Error()
	}
	return s
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
// Formulations
// ---------------------------------------------------------------------------

func (r *repo) CreateFormulation(ctx context.Context, f *domain.Formulation, ins []domain.FormulationInput) (*domain.Formulation, []domain.FormulationInput, error) {
	if err := f.Validate(); err != nil {
		return nil, nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	f.ID = r.ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO production_formulations
			(id,tenant_id,code,name,output_product_ref,output_unit,expected_yield_ppm,
			 expectation_basis,valid_from,valid_to,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,nullif($8,''),$9,$10,$11,$11)`,
		f.ID, f.TenantID, f.Code, f.Name, f.OutputProductRef, string(f.OutputUnit),
		f.ExpectedYieldPPM, f.ExpectationBasis, f.ValidFrom, f.ValidTo, f.CreatedBy); err != nil {
		if sqlState(err) == "23P01" {
			return nil, nil, fmt.Errorf("%w: recipe %s already has a version in force over part "+
				"of that period, and two targets in force on one day means the variance depends "+
				"on which row was read first", ErrOverlappingVersion, f.Code)
		}
		return nil, nil, fmt.Errorf("create formulation: %w", err)
	}

	for i := range ins {
		// The identifiers are this layer's to fill in, so they are filled in
		// before the row is validated rather than after. Validating first asked
		// an ingredient for a tenant and a recipe that the caller has no way to
		// know and this function had not yet decided.
		ins[i].ID = r.ids.New()
		ins[i].TenantID = f.TenantID
		ins[i].FormulationID = f.ID
		if err := ins[i].Validate(f); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO production_formulation_inputs
				(id,tenant_id,formulation_id,product_ref,expected_share_ppm,
				 share_tolerance_ppm,required,created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			ins[i].ID, ins[i].TenantID, ins[i].FormulationID, ins[i].ProductRef,
			ins[i].ExpectedSharePPM, ins[i].ShareTolerancePPM, ins[i].Required,
			f.CreatedBy); err != nil {
			switch sqlState(err) {
			case "23505":
				return nil, nil, fmt.Errorf("%w: %s is listed twice in recipe %s",
					ErrDuplicateInput, ins[i].ProductRef, f.Code)
			case "23514":
				return nil, nil, fmt.Errorf("%w: %s", ErrRefused, triggerMessage(err))
			}
			return nil, nil, fmt.Errorf("add ingredient: %w", err)
		}
	}

	products := make([]string, 0, len(ins))
	for _, in := range ins {
		products = append(products, in.ProductRef)
	}
	after := map[string]any{
		"code": f.Code, "output_product_ref": f.OutputProductRef,
		"valid_from": f.ValidFrom, "ingredients": products,
	}
	if f.ExpectedYieldPPM != nil {
		after["expected_yield_ppm"] = *f.ExpectedYieldPPM
		after["expectation_basis"] = f.ExpectationBasis
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "create_formulation", ResourceType: "production_formulation", ResourceID: f.ID,
		After: after, ServiceName: serviceName,
	}); err != nil {
		return nil, nil, err
	}
	return f, ins, tx.Commit(ctx)
}

const formulationCols = `id,tenant_id,code,name,output_product_ref,output_unit,
	expected_yield_ppm,COALESCE(expectation_basis,''),valid_from,valid_to,
	created_at,updated_at,created_by,updated_by`

func (r *repo) GetFormulation(ctx context.Context, tenantID, id string) (*domain.Formulation, error) {
	return scanFormulation(r.db.QueryRow(ctx,
		`SELECT `+formulationCols+` FROM production_formulations
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id))
}

// FormulationInForce is the version of a recipe that was on the wall at a
// moment.
//
// The moment is required and there is no default of "now". A batch made in
// March asks for March; answering with today's recipe is the retroactive
// problem versioning exists to prevent, arriving through a convenience.
func (r *repo) FormulationInForce(ctx context.Context, tenantID, code string, at time.Time) (*domain.Formulation, error) {
	if at.IsZero() {
		return nil, errors.New("looking up the recipe in force needs the moment to ask about")
	}
	return scanFormulation(r.db.QueryRow(ctx,
		`SELECT `+formulationCols+` FROM production_formulations
		 WHERE tenant_id=$1 AND code=$2 AND deleted_at IS NULL
		   AND valid_from <= $3 AND (valid_to IS NULL OR valid_to > $3)`,
		tenantID, code, at))
}

func (r *repo) ListFormulations(ctx context.Context, tenantID string) ([]*domain.Formulation, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+formulationCols+` FROM production_formulations
		 WHERE tenant_id=$1 AND deleted_at IS NULL
		 ORDER BY code, valid_from DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Formulation
	for rows.Next() {
		f, err := scanFormulation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *repo) FormulationInputs(ctx context.Context, tenantID, formulationID string) ([]domain.FormulationInput, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id,tenant_id,formulation_id,product_ref,expected_share_ppm,
		        share_tolerance_ppm,required,created_at,created_by
		 FROM production_formulation_inputs
		 WHERE tenant_id=$1 AND formulation_id=$2 ORDER BY product_ref`,
		tenantID, formulationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FormulationInput
	for rows.Next() {
		var in domain.FormulationInput
		if err := rows.Scan(&in.ID, &in.TenantID, &in.FormulationID, &in.ProductRef,
			&in.ExpectedSharePPM, &in.ShareTolerancePPM, &in.Required,
			&in.CreatedAt, &in.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// Lots is what actually went into a batch, each named by what it is.
//
// The product of each lot is joined here rather than fetched one at a time,
// because a vat filled from forty tankers is one query and not forty.
func (r *repo) Lots(ctx context.Context, tenantID, batchID string) ([]domain.Lot, error) {
	rows, err := r.db.Query(ctx,
		`SELECT src.id, src.batch_code, src.product_ref, i.consumed_value, i.consumed_unit
		   FROM production_inputs i
		   JOIN production_batches src
		     ON src.tenant_id = i.tenant_id AND src.id = i.input_batch_id
		  WHERE i.tenant_id=$1 AND i.output_batch_id=$2
		  ORDER BY src.batch_code`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Lot
	for rows.Next() {
		var id, code, product, unit string
		var value int64
		if err := rows.Scan(&id, &code, &product, &value, &unit); err != nil {
			return nil, err
		}
		out = append(out, domain.NewLot(id, code, product, value, unit))
	}
	return out, rows.Err()
}

// ObservedHistory is every batch made under a recipe, summarised, so a plant
// can read its own numbers and decide its own target.
func (r *repo) ObservedHistory(ctx context.Context, tenantID, formulationID string) (*domain.ObservedHistory, error) {
	var h domain.ObservedHistory
	err := r.db.QueryRow(ctx, `
		SELECT formulation_id, code, output_product_ref, expected_yield_ppm,
		       batches_counted, batches_needing_a_density,
		       lowest_ppm, lower_quartile_ppm, median_ppm, upper_quartile_ppm, highest_ppm
		  FROM gavya_formulation_observed_yield
		 WHERE tenant_id=$1 AND formulation_id=$2`, tenantID, formulationID).Scan(
		&h.FormulationID, &h.Code, &h.OutputProductRef, &h.ExpectedPPM,
		&h.BatchesCounted, &h.BatchesNeedingADensity,
		&h.LowestPPM, &h.LowerQuartilePPM, &h.MedianPPM, &h.UpperQuartilePPM, &h.HighestPPM)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func scanFormulation(s scanner) (*domain.Formulation, error) {
	var out domain.Formulation
	var unit string
	err := s.Scan(&out.ID, &out.TenantID, &out.Code, &out.Name,
		&out.OutputProductRef, &unit,
		&out.ExpectedYieldPPM, &out.ExpectationBasis, &out.ValidFrom, &out.ValidTo,
		&out.CreatedAt, &out.UpdatedAt, &out.CreatedBy, &out.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.OutputUnit = unit
	return &out, nil
}
