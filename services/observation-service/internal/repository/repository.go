package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
)

var ErrNotFound = errors.New("not found")

// ErrAlreadyAttached means an advisory estimate is already on the row. Both
// estimates are write-once: an observation that has been settled against must
// not acquire a different stated confidence afterwards.
var ErrAlreadyAttached = errors.New("estimate is already attached")

// SubjectQuery asks for a subject's observations as they were valid at one
// instant and as they were known at another.
type SubjectQuery struct {
	TenantID string
	Subject  domain.SubjectRef
	// Quantity empty returns every quantity for the subject.
	Quantity domain.QuantityKind
	// ValidAt zero drops the valid-time filter, returning every interval.
	ValidAt time.Time
	// AsOf zero means current knowledge.
	AsOf   time.Time
	Limit  int
	Offset int
}

type Repository interface {
	CreateObservation(ctx context.Context, o *domain.Observation) (*domain.Observation, error)
	GetObservation(ctx context.Context, id, tenantID string) (*domain.Observation, error)
	SupersedeObservation(ctx context.Context, tenantID, id, supersededBy string) error
	ListObservationsForSubject(ctx context.Context, q SubjectQuery) ([]*domain.Observation, error)
	ListFlaggedObservations(ctx context.Context, tenantID string, limit, offset int) ([]*domain.Observation, error)
	AttachUncertainty(ctx context.Context, tenantID, id string, est domain.UncertaintyEstimate) error
	AttachAnomaly(ctx context.Context, tenantID, id string, a domain.AnomalyAssessment) error

	CreateInstrument(ctx context.Context, i *domain.Instrument) (*domain.Instrument, error)
	GetInstrument(ctx context.Context, id, tenantID string) (*domain.Instrument, error)
	CreateCertificate(ctx context.Context, c *domain.VerificationCertificate) (*domain.VerificationCertificate, error)
	GetActiveCertificate(ctx context.Context, tenantID, instrumentID string, at time.Time) (*domain.VerificationCertificate, error)
}

type repo struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Repository { return &repo{db: db} }

const observationCols = `id,tenant_id,
	cattle_id,producer_id,route_id,tanker_id,batch_id,
	quantity_kind,value,unit,
	COALESCE(instrument_id,''),session_ref,observed_by,
	origin_kind,COALESCE(source_system_id,''),COALESCE(import_batch_id,''),
	COALESCE(source_record_id,''),COALESCE(source_payload_hash,''),COALESCE(derivation_id,''),
	valid_from,valid_to,recorded_at,superseded_at,COALESCE(superseded_by,''),COALESCE(supersedes,''),
	eligibility_verdict,eligibility_reason,COALESCE(eligibility_certificate_id,''),
	uncertainty_model_id,uncertainty_missing,standard_uncertainty,expanded_uncertainty,
	coverage_factor,coverage_probability,COALESCE(uncertainty_model_version,''),uncertainty_estimated_at,
	anomaly_score,anomaly_flagged,COALESCE(anomaly_method,''),COALESCE(anomaly_model_version,''),
	anomaly_lower_bound,anomaly_upper_bound,COALESCE(anomaly_explanation,''),anomaly_scored_at,
	created_at,created_by`

func (r *repo) CreateObservation(ctx context.Context, o *domain.Observation) (*domain.Observation, error) {
	cattle, producer, route, tanker, batch, err := subjectColumns(o.Subject)
	if err != nil {
		return nil, err
	}
	const q = `INSERT INTO observations
		(id,tenant_id,cattle_id,producer_id,route_id,tanker_id,batch_id,
		 quantity_kind,value,unit,instrument_id,session_ref,observed_by,
		 origin_kind,source_system_id,import_batch_id,source_record_id,source_payload_hash,derivation_id,
		 valid_from,valid_to,supersedes,
		 eligibility_verdict,eligibility_reason,eligibility_certificate_id,
		 uncertainty_model_id,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,
		        $14,NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),NULLIF($18,''),NULLIF($19,''),
		        $20,$21,NULLIF($22,''),$23,$24,NULLIF($25,''),$26,$27)
		RETURNING ` + observationCols

	return scanObservation(r.db.QueryRow(ctx, q,
		o.ID, o.TenantID, cattle, producer, route, tanker, batch,
		string(o.Quantity), o.Value, o.Unit, o.InstrumentID, o.SessionRef, o.ObservedBy,
		string(o.Origin.Kind), o.Origin.SourceSystemID, o.Origin.ImportBatchID,
		o.Origin.SourceRecordID, o.Origin.SourcePayloadHash, o.Origin.DerivationID,
		o.ValidFrom, o.ValidTo, o.Supersedes,
		string(o.EligibilityVerdict), o.EligibilityReason, o.EligibilityCertificateID,
		o.UncertaintyModelID, o.CreatedBy))
}

func (r *repo) GetObservation(ctx context.Context, id, tenantID string) (*domain.Observation, error) {
	const q = `SELECT ` + observationCols + ` FROM observations WHERE id=$1 AND tenant_id=$2`
	return scanObservation(r.db.QueryRow(ctx, q, id, tenantID))
}

// SupersedeObservation closes the live version. This and attaching the two
// advisory estimates are the only writes an existing observation row ever
// receives; the measured value itself is never touched.
func (r *repo) SupersedeObservation(ctx context.Context, tenantID, id, supersededBy string) error {
	const q = `UPDATE observations
		SET superseded_at=NOW(), superseded_by=$3
		WHERE tenant_id=$1 AND id=$2 AND superseded_at IS NULL`
	tag, err := r.db.Exec(ctx, q, tenantID, id, supersededBy)
	if err != nil {
		return fmt.Errorf("supersede observation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repo) ListObservationsForSubject(ctx context.Context, q SubjectQuery) ([]*domain.Observation, error) {
	col, err := subjectColumn(q.Subject.Kind)
	if err != nil {
		return nil, err
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	var validAt *time.Time
	if !q.ValidAt.IsZero() {
		at := q.ValidAt.UTC()
		validAt = &at
	}

	sql := `SELECT ` + observationCols + ` FROM observations
		WHERE tenant_id=$1 AND ` + col + `=$2
		  AND ($3='' OR quantity_kind=$3)
		  AND ($4::timestamptz IS NULL OR (valid_from <= $4 AND valid_to > $4))
		  AND recorded_at <= $5 AND (superseded_at IS NULL OR superseded_at > $5)
		ORDER BY valid_from DESC, recorded_at DESC, id
		LIMIT $6 OFFSET $7`

	rows, err := r.db.Query(ctx, sql, q.TenantID, q.Subject.ID, string(q.Quantity), validAt, asOf, q.Limit, q.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectObservations(rows, q.Limit)
}

func (r *repo) ListFlaggedObservations(ctx context.Context, tenantID string, limit, offset int) ([]*domain.Observation, error) {
	const q = `SELECT ` + observationCols + ` FROM observations
		WHERE tenant_id=$1 AND anomaly_flagged AND superseded_at IS NULL
		ORDER BY anomaly_score DESC NULLS LAST, valid_from DESC, id
		LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectObservations(rows, limit)
}

// AttachUncertainty records the measurement uncertainty budget for one
// observation. It refuses a row that already carries one, so a settled
// observation cannot silently acquire a different stated confidence.
func (r *repo) AttachUncertainty(ctx context.Context, tenantID, id string, est domain.UncertaintyEstimate) error {
	const q = `UPDATE observations
		SET uncertainty_missing=FALSE,
		    uncertainty_model_id=COALESCE(NULLIF($3,''),uncertainty_model_id),
		    standard_uncertainty=$4, expanded_uncertainty=$5,
		    coverage_factor=$6, coverage_probability=$7,
		    uncertainty_model_version=$8, uncertainty_estimated_at=$9
		WHERE tenant_id=$1 AND id=$2 AND uncertainty_missing`
	estimatedAt := est.EstimatedAt
	if estimatedAt.IsZero() {
		estimatedAt = time.Now().UTC()
	}
	tag, err := r.db.Exec(ctx, q, tenantID, id, est.ModelID,
		est.StandardUncertainty, est.ExpandedUncertainty, est.CoverageFactor, est.CoverageProbability,
		est.ModelVersion, estimatedAt)
	if err != nil {
		return fmt.Errorf("attach uncertainty: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.explainNoAttach(ctx, tenantID, id)
	}
	return nil
}

// AttachAnomaly records the ML tier's advisory score. It can neither reject the
// observation nor alter its value; the flag only puts it in a review queue.
func (r *repo) AttachAnomaly(ctx context.Context, tenantID, id string, a domain.AnomalyAssessment) error {
	const q = `UPDATE observations
		SET anomaly_score=$3, anomaly_flagged=$4, anomaly_method=$5, anomaly_model_version=$6,
		    anomaly_lower_bound=$7, anomaly_upper_bound=$8, anomaly_explanation=$9, anomaly_scored_at=$10
		WHERE tenant_id=$1 AND id=$2 AND anomaly_scored_at IS NULL`
	scoredAt := a.ScoredAt
	if scoredAt.IsZero() {
		scoredAt = time.Now().UTC()
	}
	tag, err := r.db.Exec(ctx, q, tenantID, id, a.Score, a.Flagged, a.Method, a.ModelVersion,
		a.LowerBound, a.UpperBound, a.Explanation, scoredAt)
	if err != nil {
		return fmt.Errorf("attach anomaly: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.explainNoAttach(ctx, tenantID, id)
	}
	return nil
}

// explainNoAttach separates "no such observation" from "already attached", so a
// caller's log says which of the two happened.
func (r *repo) explainNoAttach(ctx context.Context, tenantID, id string) error {
	var exists bool
	err := r.db.QueryRow(ctx, `SELECT TRUE FROM observations WHERE id=$1 AND tenant_id=$2`, id, tenantID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrAlreadyAttached
}

const instrumentCols = `id,tenant_id,serial,kind,label,make,model,created_at,created_by`

func (r *repo) CreateInstrument(ctx context.Context, i *domain.Instrument) (*domain.Instrument, error) {
	const q = `INSERT INTO instruments (id,tenant_id,serial,kind,label,make,model,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING ` + instrumentCols
	return scanInstrument(r.db.QueryRow(ctx, q,
		i.ID, i.TenantID, i.Serial, string(i.Kind), i.Label, i.Make, i.Model, i.CreatedBy))
}

func (r *repo) GetInstrument(ctx context.Context, id, tenantID string) (*domain.Instrument, error) {
	const q = `SELECT ` + instrumentCols + ` FROM instruments WHERE id=$1 AND tenant_id=$2`
	return scanInstrument(r.db.QueryRow(ctx, q, id, tenantID))
}

const certificateCols = `id,tenant_id,instrument_id,certificate_number,verifying_authority,issued_at,expires_at,
	origin_kind,COALESCE(source_system_id,''),COALESCE(import_batch_id,''),
	COALESCE(source_record_id,''),COALESCE(source_payload_hash,''),COALESCE(derivation_id,''),
	created_at,created_by`

func (r *repo) CreateCertificate(ctx context.Context, c *domain.VerificationCertificate) (*domain.VerificationCertificate, error) {
	const q = `INSERT INTO verification_certificates
		(id,tenant_id,instrument_id,certificate_number,verifying_authority,issued_at,expires_at,
		 origin_kind,source_system_id,import_batch_id,source_record_id,source_payload_hash,derivation_id,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14)
		RETURNING ` + certificateCols
	return scanCertificate(r.db.QueryRow(ctx, q,
		c.ID, c.TenantID, c.InstrumentID, c.CertificateNumber, c.VerifyingAuthority, c.IssuedAt, c.ExpiresAt,
		string(c.Origin.Kind), c.Origin.SourceSystemID, c.Origin.ImportBatchID,
		c.Origin.SourceRecordID, c.Origin.SourcePayloadHash, c.Origin.DerivationID, c.CreatedBy))
}

// GetActiveCertificate returns the certificate governing an instrument at one
// instant: the one whose period covers it, or failing that the most recently
// issued one.
//
// The fallback matters. Returning nothing for an instrument whose certificate
// has lapsed would make the verdict UNKNOWN, which reads as missing paperwork;
// handing back the lapsed certificate makes it NOT_ELIGIBLE, which is the
// finding an auditor is looking for.
func (r *repo) GetActiveCertificate(ctx context.Context, tenantID, instrumentID string, at time.Time) (*domain.VerificationCertificate, error) {
	const q = `SELECT ` + certificateCols + ` FROM verification_certificates
		WHERE tenant_id=$1 AND instrument_id=$2
		ORDER BY (issued_at <= $3 AND expires_at > $3) DESC, issued_at DESC, id DESC
		LIMIT 1`
	return scanCertificate(r.db.QueryRow(ctx, q, tenantID, instrumentID, at))
}

func subjectColumn(k domain.SubjectKind) (string, error) {
	switch k {
	case domain.SubjectCattle:
		return "cattle_id", nil
	case domain.SubjectProducer:
		return "producer_id", nil
	case domain.SubjectRoute:
		return "route_id", nil
	case domain.SubjectTanker:
		return "tanker_id", nil
	case domain.SubjectBatch:
		return "batch_id", nil
	default:
		return "", fmt.Errorf("subject kind %q has no column", k)
	}
}

// subjectColumns turns a SubjectRef into the five typed column values, exactly
// one of which is non-null.
func subjectColumns(s domain.SubjectRef) (cattle, producer, route, tanker, batch *string, err error) {
	if !s.Valid() {
		return nil, nil, nil, nil, nil, fmt.Errorf("subject %s:%s is not a valid reference", s.Kind, s.ID)
	}
	id := s.ID
	switch s.Kind {
	case domain.SubjectCattle:
		cattle = &id
	case domain.SubjectProducer:
		producer = &id
	case domain.SubjectRoute:
		route = &id
	case domain.SubjectTanker:
		tanker = &id
	case domain.SubjectBatch:
		batch = &id
	}
	return cattle, producer, route, tanker, batch, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func collectObservations(rows pgx.Rows, limit int) ([]*domain.Observation, error) {
	if limit < 0 {
		limit = 0
	}
	out := make([]*domain.Observation, 0, limit)
	for rows.Next() {
		o, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func scanObservation(row scanner) (*domain.Observation, error) {
	var o domain.Observation
	var cattle, producer, route, tanker, batch *string
	var quantity, originKind, verdict string
	var uncertaintyMissing bool
	var stdUnc, expUnc, covFactor, covProb *float64
	var modelVersion string
	var estimatedAt *time.Time
	var anomalyScore *float64
	var anomalyFlagged bool
	var anomalyMethod, anomalyModelVersion, anomalyExplanation string
	var anomalyLower, anomalyUpper *float64
	var anomalyScoredAt *time.Time

	if err := row.Scan(
		&o.ID, &o.TenantID,
		&cattle, &producer, &route, &tanker, &batch,
		&quantity, &o.Value, &o.Unit,
		&o.InstrumentID, &o.SessionRef, &o.ObservedBy,
		&originKind, &o.Origin.SourceSystemID, &o.Origin.ImportBatchID,
		&o.Origin.SourceRecordID, &o.Origin.SourcePayloadHash, &o.Origin.DerivationID,
		&o.ValidFrom, &o.ValidTo, &o.RecordedAt, &o.SupersededAt, &o.SupersededBy, &o.Supersedes,
		&verdict, &o.EligibilityReason, &o.EligibilityCertificateID,
		&o.UncertaintyModelID, &uncertaintyMissing, &stdUnc, &expUnc,
		&covFactor, &covProb, &modelVersion, &estimatedAt,
		&anomalyScore, &anomalyFlagged, &anomalyMethod, &anomalyModelVersion,
		&anomalyLower, &anomalyUpper, &anomalyExplanation, &anomalyScoredAt,
		&o.CreatedAt, &o.CreatedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	subject, err := subjectFromColumns(cattle, producer, route, tanker, batch)
	if err != nil {
		return nil, fmt.Errorf("observation %s: %w", o.ID, err)
	}
	o.Subject = subject
	o.Quantity = domain.QuantityKind(quantity)
	o.Origin.Kind = origin.Kind(originKind)
	o.EligibilityVerdict = domain.EligibilityVerdict(verdict)

	if !uncertaintyMissing {
		o.Uncertainty = &domain.UncertaintyEstimate{
			ModelID:             o.UncertaintyModelID,
			ModelVersion:        modelVersion,
			StandardUncertainty: deref(stdUnc),
			ExpandedUncertainty: deref(expUnc),
			CoverageFactor:      deref(covFactor),
			CoverageProbability: deref(covProb),
		}
		if estimatedAt != nil {
			o.Uncertainty.EstimatedAt = *estimatedAt
		}
	}
	if anomalyScoredAt != nil {
		o.Anomaly = &domain.AnomalyAssessment{
			Score:        deref(anomalyScore),
			Flagged:      anomalyFlagged,
			Method:       anomalyMethod,
			ModelVersion: anomalyModelVersion,
			LowerBound:   anomalyLower,
			UpperBound:   anomalyUpper,
			Explanation:  anomalyExplanation,
			ScoredAt:     *anomalyScoredAt,
		}
	}
	return &o, nil
}

func subjectFromColumns(cattle, producer, route, tanker, batch *string) (domain.SubjectRef, error) {
	var out domain.SubjectRef
	set := 0
	for _, c := range []struct {
		kind domain.SubjectKind
		val  *string
	}{
		{domain.SubjectCattle, cattle},
		{domain.SubjectProducer, producer},
		{domain.SubjectRoute, route},
		{domain.SubjectTanker, tanker},
		{domain.SubjectBatch, batch},
	} {
		if c.val != nil {
			out = domain.SubjectRef{Kind: c.kind, ID: *c.val}
			set++
		}
	}
	if set != 1 {
		return domain.SubjectRef{}, fmt.Errorf("%d subject columns are set, want exactly 1", set)
	}
	return out, nil
}

func scanInstrument(row scanner) (*domain.Instrument, error) {
	var i domain.Instrument
	var kind string
	if err := row.Scan(&i.ID, &i.TenantID, &i.Serial, &kind, &i.Label, &i.Make, &i.Model,
		&i.CreatedAt, &i.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	i.Kind = domain.InstrumentKind(kind)
	return &i, nil
}

func scanCertificate(row scanner) (*domain.VerificationCertificate, error) {
	var c domain.VerificationCertificate
	var originKind string
	if err := row.Scan(&c.ID, &c.TenantID, &c.InstrumentID, &c.CertificateNumber, &c.VerifyingAuthority,
		&c.IssuedAt, &c.ExpiresAt,
		&originKind, &c.Origin.SourceSystemID, &c.Origin.ImportBatchID,
		&c.Origin.SourceRecordID, &c.Origin.SourcePayloadHash, &c.Origin.DerivationID,
		&c.CreatedAt, &c.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Origin.Kind = origin.Kind(originKind)
	return &c, nil
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
