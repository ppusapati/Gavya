package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/domain"
)

// ErrDuplicateAssertion means the same source record with the same payload has
// already been ingested. It is not a failure: the caller returns the existing
// assertion so a replayed import batch is a no-op.
var ErrDuplicateAssertion = errors.New("assertion already ingested")

var ErrNotFound = errors.New("not found")

type Repository interface {
	CreateAssertion(ctx context.Context, a *domain.ExternalSettlementAssertion) (*domain.ExternalSettlementAssertion, error)
	GetAssertion(ctx context.Context, id, tenantID string) (*domain.ExternalSettlementAssertion, error)
	FindAssertionByPayload(ctx context.Context, tenantID, sourceSystemID, sourceRecordID, payloadHash string) (*domain.ExternalSettlementAssertion, error)
	SupersedeAssertion(ctx context.Context, tenantID, sourceSystemID, externalSettlementID, supersededBy string) error
	ListAssertions(ctx context.Context, tenantID, producerRef string, limit, offset int) ([]*domain.ExternalSettlementAssertion, error)

	CreateComputation(ctx context.Context, c *domain.ShadowSettlementComputation) (*domain.ShadowSettlementComputation, error)
	GetComputation(ctx context.Context, id, tenantID string) (*domain.ShadowSettlementComputation, error)
	SupersedeComputation(ctx context.Context, tenantID, producerRef string, periodStart, periodEnd time.Time, supersededBy string) error

	CreateDivergence(ctx context.Context, d *domain.SettlementDivergence) (*domain.SettlementDivergence, error)
	GetDivergence(ctx context.Context, id, tenantID string) (*domain.SettlementDivergence, error)
	ListDivergences(ctx context.Context, f DivergenceFilter) ([]*domain.SettlementDivergence, error)
	AttachHypotheses(ctx context.Context, id, tenantID string, hypotheses []domain.MLHypothesis, updatedBy string) error
	ResolveDivergence(ctx context.Context, id, tenantID string, status domain.DivergenceStatus, resolution, resolvedBy string) (*domain.SettlementDivergence, error)
	SummariseDivergences(ctx context.Context, tenantID string, from, to time.Time) ([]domain.ClassSummary, error)
}

// DivergenceFilter drives the integrity workspace's triage queue.
type DivergenceFilter struct {
	TenantID       string
	Status         string
	Classification string
	ProducerRef    string
	// MinAbsDelta hides noise so a reviewer sees material exposure first.
	MinAbsDelta int64
	Limit       int
	Offset      int
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "shadow-settlement-service"

type repo struct {
	db  *pgxpool.Pool
	ids audit.IDs
}

func New(db *pgxpool.Pool, ids audit.IDs) Repository { return &repo{db: db, ids: ids} }

const assertionCols = `id,tenant_id,source_system_id,external_settlement_id,producer_ref,period_start,period_end,
	currency,amount_scale,total_minor_units,components,asserted_at,
	origin_kind,import_batch_id,source_record_id,source_payload_hash,
	valid_from,valid_to,recorded_at,superseded_at,COALESCE(superseded_by,''),created_at,created_by`

func (r *repo) CreateAssertion(ctx context.Context, a *domain.ExternalSettlementAssertion) (*domain.ExternalSettlementAssertion, error) {
	comps, err := json.Marshal(a.Components)
	if err != nil {
		return nil, fmt.Errorf("encode components: %w", err)
	}
	const q = `INSERT INTO external_settlement_assertions
		(id,tenant_id,source_system_id,external_settlement_id,producer_ref,period_start,period_end,
		 currency,amount_scale,total_minor_units,components,asserted_at,
		 origin_kind,import_batch_id,source_record_id,source_payload_hash,
		 valid_from,valid_to,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'IMPORTED',$13,$14,$15,$16,$17,$18)
		RETURNING ` + assertionCols

	row := r.db.QueryRow(ctx, q,
		a.ID, a.TenantID, a.SourceSystemID, a.ExternalSettlementID, a.ProducerRef, a.PeriodStart, a.PeriodEnd,
		a.Total.Currency, a.Total.Scale, a.Total.Value, comps, a.AssertedAt,
		a.Origin.ImportBatchID, a.Origin.SourceRecordID, a.Origin.SourcePayloadHash,
		a.ValidFrom, a.ValidTo, a.CreatedBy)

	out, err := scanAssertion(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateAssertion
	}
	return out, err
}

func (r *repo) GetAssertion(ctx context.Context, id, tenantID string) (*domain.ExternalSettlementAssertion, error) {
	const q = `SELECT ` + assertionCols + ` FROM external_settlement_assertions WHERE id=$1 AND tenant_id=$2`
	return scanAssertion(r.db.QueryRow(ctx, q, id, tenantID))
}

func (r *repo) FindAssertionByPayload(ctx context.Context, tenantID, sourceSystemID, sourceRecordID, payloadHash string) (*domain.ExternalSettlementAssertion, error) {
	const q = `SELECT ` + assertionCols + ` FROM external_settlement_assertions
		WHERE tenant_id=$1 AND source_system_id=$2 AND source_record_id=$3 AND source_payload_hash=$4`
	return scanAssertion(r.db.QueryRow(ctx, q, tenantID, sourceSystemID, sourceRecordID, payloadHash))
}

// SupersedeAssertion closes the live version of an external settlement. This is
// the only permitted write to an existing assertion row.
func (r *repo) SupersedeAssertion(ctx context.Context, tenantID, sourceSystemID, externalSettlementID, supersededBy string) error {
	const q = `UPDATE external_settlement_assertions
		SET superseded_at=NOW(), superseded_by=$4
		WHERE tenant_id=$1 AND source_system_id=$2 AND external_settlement_id=$3 AND superseded_at IS NULL`
	_, err := r.db.Exec(ctx, q, tenantID, sourceSystemID, externalSettlementID, supersededBy)
	return err
}

func (r *repo) ListAssertions(ctx context.Context, tenantID, producerRef string, limit, offset int) ([]*domain.ExternalSettlementAssertion, error) {
	const q = `SELECT ` + assertionCols + ` FROM external_settlement_assertions
		WHERE tenant_id=$1 AND ($2='' OR producer_ref=$2) AND superseded_at IS NULL
		ORDER BY period_start DESC, id LIMIT $3 OFFSET $4`
	rows, err := r.db.Query(ctx, q, tenantID, producerRef, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.ExternalSettlementAssertion, 0, limit)
	for rows.Next() {
		a, err := scanAssertion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const computationCols = `id,tenant_id,COALESCE(assertion_id,''),producer_ref,period_start,period_end,
	currency,amount_scale,total_minor_units,components,
	policy_version,rate_card_id,rounding_trail,input_digest,as_of,
	derivation_id,computed_at,recorded_at,created_at,created_by`

func (r *repo) CreateComputation(ctx context.Context, c *domain.ShadowSettlementComputation) (*domain.ShadowSettlementComputation, error) {
	comps, err := json.Marshal(c.Components)
	if err != nil {
		return nil, fmt.Errorf("encode components: %w", err)
	}
	trail, err := json.Marshal(c.RoundingTrail)
	if err != nil {
		return nil, fmt.Errorf("encode rounding trail: %w", err)
	}
	const q = `INSERT INTO shadow_settlement_computations
		(id,tenant_id,assertion_id,producer_ref,period_start,period_end,
		 currency,amount_scale,total_minor_units,components,
		 policy_version,rate_card_id,rounding_trail,input_digest,as_of,
		 origin_kind,derivation_id,computed_at,created_by)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'DERIVED',$16,$17,$18)
		RETURNING ` + computationCols

	row := r.db.QueryRow(ctx, q,
		c.ID, c.TenantID, c.AssertionID, c.ProducerRef, c.PeriodStart, c.PeriodEnd,
		c.Total.Currency, c.Total.Scale, c.Total.Value, comps,
		c.PolicyVersion, c.RateCardID, trail, c.InputDigest, c.AsOf,
		c.Origin.DerivationID, c.ComputedAt, c.CreatedBy)

	return scanComputation(row)
}

func (r *repo) GetComputation(ctx context.Context, id, tenantID string) (*domain.ShadowSettlementComputation, error) {
	const q = `SELECT ` + computationCols + ` FROM shadow_settlement_computations WHERE id=$1 AND tenant_id=$2`
	return scanComputation(r.db.QueryRow(ctx, q, id, tenantID))
}

func (r *repo) SupersedeComputation(ctx context.Context, tenantID, producerRef string, periodStart, periodEnd time.Time, supersededBy string) error {
	const q = `UPDATE shadow_settlement_computations
		SET superseded_at=NOW(), superseded_by=$5
		WHERE tenant_id=$1 AND producer_ref=$2 AND period_start=$3 AND period_end=$4 AND superseded_at IS NULL`
	_, err := r.db.Exec(ctx, q, tenantID, producerRef, periodStart, periodEnd, supersededBy)
	return err
}

const divergenceCols = `id,tenant_id,assertion_id,computation_id,producer_ref,
	currency,amount_scale,delta_minor_units,classification,rationale,evidence,ml_hypotheses,
	status,COALESCE(resolution,''),resolved_at,COALESCE(resolved_by,''),
	created_at,updated_at,created_by,updated_by,deleted_at`

func (r *repo) CreateDivergence(ctx context.Context, d *domain.SettlementDivergence) (*domain.SettlementDivergence, error) {
	evidence, err := json.Marshal(d.Evidence)
	if err != nil {
		return nil, fmt.Errorf("encode evidence: %w", err)
	}
	hyp, err := json.Marshal(d.MLHypotheses)
	if err != nil {
		return nil, fmt.Errorf("encode hypotheses: %w", err)
	}
	const q = `INSERT INTO settlement_divergences
		(id,tenant_id,assertion_id,computation_id,producer_ref,
		 currency,amount_scale,delta_minor_units,classification,rationale,evidence,ml_hypotheses,
		 status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14)
		RETURNING ` + divergenceCols

	row := r.db.QueryRow(ctx, q,
		d.ID, d.TenantID, d.AssertionID, d.ComputationID, d.ProducerRef,
		d.Delta.Currency, d.Delta.Scale, d.Delta.Value, string(d.Classification), d.Rationale, evidence, hyp,
		string(d.Status), d.CreatedBy)

	return scanDivergence(row)
}

func (r *repo) GetDivergence(ctx context.Context, id, tenantID string) (*domain.SettlementDivergence, error) {
	const q = `SELECT ` + divergenceCols + ` FROM settlement_divergences WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`
	return scanDivergence(r.db.QueryRow(ctx, q, id, tenantID))
}

func (r *repo) ListDivergences(ctx context.Context, f DivergenceFilter) ([]*domain.SettlementDivergence, error) {
	const q = `SELECT ` + divergenceCols + ` FROM settlement_divergences
		WHERE tenant_id=$1 AND deleted_at IS NULL
		  AND ($2='' OR status=$2)
		  AND ($3='' OR classification=$3)
		  AND ($4='' OR producer_ref=$4)
		  AND abs(delta_minor_units) >= $5
		ORDER BY abs(delta_minor_units) DESC, created_at DESC, id
		LIMIT $6 OFFSET $7`

	rows, err := r.db.Query(ctx, q, f.TenantID, f.Status, f.Classification, f.ProducerRef, f.MinAbsDelta, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.SettlementDivergence, 0, f.Limit)
	for rows.Next() {
		d, err := scanDivergence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AttachHypotheses stores the ML tier's advisory explanation. It deliberately
// cannot touch classification: a model never changes the authoritative verdict.
func (r *repo) AttachHypotheses(ctx context.Context, id, tenantID string, hypotheses []domain.MLHypothesis, updatedBy string) error {
	payload, err := json.Marshal(hypotheses)
	if err != nil {
		return fmt.Errorf("encode hypotheses: %w", err)
	}
	const q = `UPDATE settlement_divergences
		SET ml_hypotheses=$3, updated_at=NOW(), updated_by=$4
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL AND classification='UNEXPLAINED'`
	tag, err := r.db.Exec(ctx, q, id, tenantID, payload, updatedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repo) ResolveDivergence(ctx context.Context, id, tenantID string, status domain.DivergenceStatus, resolution, resolvedBy string) (*domain.SettlementDivergence, error) {
	// A transaction, so the decision and its record commit together. This is
	// somebody deciding which of two figures for one producer's fortnight is the
	// right one; a decision with no entry beside it is a figure that changed for
	// no reason anybody can find.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	const q = `UPDATE settlement_divergences
		SET status=$3, resolution=$4, resolved_at=NOW(), resolved_by=$5,
		    updated_at=NOW(), updated_by=$5
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		RETURNING ` + divergenceCols
	d, err := scanDivergence(tx.QueryRow(ctx, q, id, tenantID, string(status), resolution, resolvedBy))
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "resolve_settlement_divergence", ResourceType: "settlement_divergence",
		ResourceID: id,
		After: map[string]any{
			"status":            string(status),
			"resolution":        resolution,
			"producer_ref":      d.ProducerRef,
			"classification":    string(d.Classification),
			"delta_minor_units": d.Delta.Value,
			"currency":          d.Delta.Currency,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return d, nil
}

func (r *repo) SummariseDivergences(ctx context.Context, tenantID string, from, to time.Time) ([]domain.ClassSummary, error) {
	// Grouping by amount_scale as well as currency is not a refinement: summing
	// minor units across scales would add paise to millirupees and report the
	// result as money.
	const q = `SELECT classification, count(*), COALESCE(sum(abs(delta_minor_units)),0), currency, amount_scale
		FROM settlement_divergences
		WHERE tenant_id=$1 AND deleted_at IS NULL AND created_at >= $2 AND created_at < $3
		GROUP BY classification, currency, amount_scale
		ORDER BY 3 DESC`
	rows, err := r.db.Query(ctx, q, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ClassSummary
	for rows.Next() {
		var s domain.ClassSummary
		var class string
		if err := rows.Scan(&class, &s.Count, &s.TotalAbsMinorUnits, &s.Currency, &s.AmountScale); err != nil {
			return nil, err
		}
		s.Classification = domain.Classification(class)
		out = append(out, s)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAssertion(row scanner) (*domain.ExternalSettlementAssertion, error) {
	var a domain.ExternalSettlementAssertion
	var comps []byte
	var originKind string
	if err := row.Scan(
		&a.ID, &a.TenantID, &a.SourceSystemID, &a.ExternalSettlementID, &a.ProducerRef, &a.PeriodStart, &a.PeriodEnd,
		&a.Total.Currency, &a.Total.Scale, &a.Total.Value, &comps, &a.AssertedAt,
		&originKind, &a.Origin.ImportBatchID, &a.Origin.SourceRecordID, &a.Origin.SourcePayloadHash,
		&a.ValidFrom, &a.ValidTo, &a.RecordedAt, &a.SupersededAt, &a.SupersededBy, &a.CreatedAt, &a.CreatedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.Origin.Kind = origin.Kind(originKind)
	a.Origin.SourceSystemID = a.SourceSystemID
	if err := json.Unmarshal(comps, &a.Components); err != nil {
		return nil, fmt.Errorf("decode components: %w", err)
	}
	return &a, nil
}

func scanComputation(row scanner) (*domain.ShadowSettlementComputation, error) {
	var c domain.ShadowSettlementComputation
	var comps, trail []byte
	if err := row.Scan(
		&c.ID, &c.TenantID, &c.AssertionID, &c.ProducerRef, &c.PeriodStart, &c.PeriodEnd,
		&c.Total.Currency, &c.Total.Scale, &c.Total.Value, &comps,
		&c.PolicyVersion, &c.RateCardID, &trail, &c.InputDigest, &c.AsOf,
		&c.Origin.DerivationID, &c.ComputedAt, &c.RecordedAt, &c.CreatedAt, &c.CreatedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Origin.Kind = origin.Derived
	if err := json.Unmarshal(comps, &c.Components); err != nil {
		return nil, fmt.Errorf("decode components: %w", err)
	}
	if err := json.Unmarshal(trail, &c.RoundingTrail); err != nil {
		return nil, fmt.Errorf("decode rounding trail: %w", err)
	}
	return &c, nil
}

func scanDivergence(row scanner) (*domain.SettlementDivergence, error) {
	var d domain.SettlementDivergence
	var evidence, hyp []byte
	var class, status string
	var delta money.Money
	if err := row.Scan(
		&d.ID, &d.TenantID, &d.AssertionID, &d.ComputationID, &d.ProducerRef,
		&delta.Currency, &delta.Scale, &delta.Value, &class, &d.Rationale, &evidence, &hyp,
		&status, &d.Resolution, &d.ResolvedAt, &d.ResolvedBy,
		&d.CreatedAt, &d.UpdatedAt, &d.CreatedBy, &d.UpdatedBy, &d.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Delta = delta
	d.Classification = domain.Classification(class)
	d.Status = domain.DivergenceStatus(status)
	if err := json.Unmarshal(evidence, &d.Evidence); err != nil {
		return nil, fmt.Errorf("decode evidence: %w", err)
	}
	if err := json.Unmarshal(hyp, &d.MLHypotheses); err != nil {
		return nil, fmt.Errorf("decode hypotheses: %w", err)
	}
	return &d, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
