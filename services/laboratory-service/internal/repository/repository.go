// Package repository is the laboratory's access to its tables.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/laboratory-service/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicateCode is a sample code the tenant already uses.
	ErrDuplicateCode = errors.New("a sample with that code already exists")
	// ErrDuplicateReading is a second result for one analyte on one instrument.
	ErrDuplicateReading = errors.New("that analyte has already been read on that instrument for this sample")
	// ErrCustodyIsWritten is the trigger refusing to rewrite a handover.
	ErrCustodyIsWritten = errors.New("a recorded handover cannot be changed")
	// ErrBeforeDrawn is the trigger refusing a result timed before its sample.
	ErrBeforeDrawn = errors.New("a sample cannot be read before it exists")
)

const serviceName = "laboratory-service"

type IDs interface{ New() string }

type Repository interface {
	DrawSample(ctx context.Context, s *domain.Sample) (*domain.Sample, error)
	GetSample(ctx context.Context, tenantID, id string) (*domain.Sample, error)
	GetSampleByCode(ctx context.Context, tenantID, code string) (*domain.Sample, error)
	ListSamples(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]*domain.Sample, error)
	BreakSeal(ctx context.Context, tenantID, id, reason, actor string, at time.Time) (*domain.Sample, error)

	RecordHandover(ctx context.Context, h *domain.Handover, actor string) (*domain.Handover, error)
	Chain(ctx context.Context, tenantID, sampleID string) ([]domain.Handover, error)

	RecordResult(ctx context.Context, r *domain.Result) (*domain.Result, error)
	Results(ctx context.Context, tenantID, sampleID string, includeSuperseded bool) ([]*domain.Result, error)
}

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

func New(db *pgxpool.Pool, ids IDs) Repository { return &repo{db: db, ids: ids} }

func (r *repo) DrawSample(ctx context.Context, s *domain.Sample) (*domain.Sample, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	s.ID = r.ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO lab_samples
			(id,tenant_id,sample_code,source_kind,source_ref,drawn_at,drawn_by,
			 seal_number,purpose,duplicates_sample_id,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,nullif($8,''),$9,nullif($10,''),$11,$11)`,
		s.ID, s.TenantID, s.Code, string(s.SourceKind), s.SourceRef,
		s.DrawnAt, s.DrawnBy, s.SealNumber, string(s.Purpose),
		s.DuplicatesSampleID, s.CreatedBy); err != nil {
		if sqlState(err) == "23505" {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateCode, s.Code)
		}
		return nil, fmt.Errorf("draw sample: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "draw_sample", ResourceType: "lab_sample", ResourceID: s.ID,
		After: map[string]any{
			"sample_code": s.Code, "source_kind": string(s.SourceKind),
			"source_ref": s.SourceRef, "purpose": string(s.Purpose),
			"sealed": s.Sealed(), "drawn_by": s.DrawnBy,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return s, tx.Commit(ctx)
}

const sampleCols = `id,tenant_id,sample_code,source_kind,source_ref,drawn_at,drawn_by,
	COALESCE(seal_number,''),seal_broken_at,COALESCE(seal_broken_by,''),COALESCE(seal_broken_reason,''),
	purpose,COALESCE(duplicates_sample_id,''),created_at,created_by`

func (r *repo) GetSample(ctx context.Context, tenantID, id string) (*domain.Sample, error) {
	return scanSample(r.db.QueryRow(ctx,
		`SELECT `+sampleCols+` FROM lab_samples WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,
		tenantID, id))
}

func (r *repo) GetSampleByCode(ctx context.Context, tenantID, code string) (*domain.Sample, error) {
	return scanSample(r.db.QueryRow(ctx,
		`SELECT `+sampleCols+` FROM lab_samples WHERE tenant_id=$1 AND sample_code=$2 AND deleted_at IS NULL`,
		tenantID, code))
}

func (r *repo) ListSamples(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]*domain.Sample, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+sampleCols+` FROM lab_samples
		 WHERE tenant_id=$1 AND deleted_at IS NULL AND drawn_at >= $2 AND drawn_at <= $3
		 ORDER BY drawn_at, sample_code LIMIT $4`, tenantID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Sample
	for rows.Next() {
		s, err := scanSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *repo) BreakSeal(ctx context.Context, tenantID, id, reason, actor string, at time.Time) (*domain.Sample, error) {
	if reason == "" {
		return nil, errors.New("breaking a seal must say why; the seal is what rules out the " +
			"sample having been changed, and breaking it silently defeats it")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	tag, err := tx.Exec(ctx, `
		UPDATE lab_samples
		SET seal_broken_at=$3, seal_broken_by=$4, seal_broken_reason=$5, updated_at=NOW(), updated_by=$4
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND seal_broken_at IS NULL`,
		tenantID, id, at, actor, reason)
	if err != nil {
		return nil, fmt.Errorf("break seal: %w", err)
	}
	if tag.RowsAffected() != 1 {
		// Either the sample is not there or its seal is already broken. Both are
		// worth telling apart, so the caller is sent to look.
		s, err := r.GetSample(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("the seal on sample %s was already broken at %s by %s",
			s.Code, s.SealBrokenAt.UTC().Format(time.RFC3339), s.SealBrokenBy)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "break_seal", ResourceType: "lab_sample", ResourceID: id,
		After:       map[string]any{"at": at, "reason": reason},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetSample(ctx, tenantID, id)
}

// RecordHandover appends one link to a sample's chain of custody.
//
// The sequence is taken under a row lock on the sample rather than read and
// then written. Two clerks recording a handover at once would otherwise both
// read the same last sequence, and the second would be refused by the unique
// index with a message about an index rather than about a race.
func (r *repo) RecordHandover(ctx context.Context, h *domain.Handover, actor string) (*domain.Handover, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT true FROM lab_samples WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		h.TenantID, h.SampleID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var next int32
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(sequence),0)+1 FROM lab_custody WHERE tenant_id=$1 AND sample_id=$2`,
		h.TenantID, h.SampleID).Scan(&next); err != nil {
		return nil, err
	}
	h.Sequence = next
	h.ID = r.ids.New()

	if _, err := tx.Exec(ctx, `
		INSERT INTO lab_custody (id,tenant_id,sample_id,sequence,at,from_holder,to_holder,note,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,nullif($8,''),$9)`,
		h.ID, h.TenantID, h.SampleID, h.Sequence, h.At, h.From, h.To, h.Note, actor); err != nil {
		return nil, fmt.Errorf("record handover: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "record_custody", ResourceType: "lab_sample", ResourceID: h.SampleID,
		After: map[string]any{
			"sequence": h.Sequence, "at": h.At, "from": h.From, "to": h.To,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return h, tx.Commit(ctx)
}

func (r *repo) Chain(ctx context.Context, tenantID, sampleID string) ([]domain.Handover, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id,tenant_id,sample_id,sequence,at,from_holder,to_holder,COALESCE(note,'')
		 FROM lab_custody WHERE tenant_id=$1 AND sample_id=$2 ORDER BY sequence`,
		tenantID, sampleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Handover
	for rows.Next() {
		var h domain.Handover
		if err := rows.Scan(&h.ID, &h.TenantID, &h.SampleID, &h.Sequence,
			&h.At, &h.From, &h.To, &h.Note); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *repo) RecordResult(ctx context.Context, res *domain.Result) (*domain.Result, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	res.ID = r.ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO lab_results
			(id,tenant_id,sample_id,analyte,value_numerator,value_scale,method,instrument_ref,
			 instrument_valid_until,instrument_certificate,analysed_at,analysed_by,
			 eligibility,eligibility_reason,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),$11,$12,$13,$14,$15,$15)`,
		res.ID, res.TenantID, res.SampleID, string(res.Analyte),
		res.Reading.Value, res.Reading.Scale, res.Method, res.Instrument.Ref,
		res.Instrument.ValidUntil, res.Instrument.CertificateRef,
		res.AnalysedAt, res.AnalysedBy,
		string(res.Eligibility), res.EligibilityReason, res.CreatedBy); err != nil {
		switch sqlState(err) {
		case "23505":
			return nil, fmt.Errorf("%w: %s on %s", ErrDuplicateReading, res.Analyte, res.Instrument.Ref)
		case "23514":
			return nil, fmt.Errorf("%w: %s", ErrBeforeDrawn, err.Error())
		}
		return nil, fmt.Errorf("record result: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "record_result", ResourceType: "lab_result", ResourceID: res.ID,
		After: map[string]any{
			"sample_id": res.SampleID, "analyte": string(res.Analyte),
			"reading": res.Reading.String(), "instrument": res.Instrument.Ref,
			"eligibility": string(res.Eligibility), "reason": res.EligibilityReason,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return res, tx.Commit(ctx)
}

const resultCols = `id,tenant_id,sample_id,analyte,value_numerator,value_scale,method,
	instrument_ref,instrument_valid_until,COALESCE(instrument_certificate,''),
	analysed_at,analysed_by,eligibility,eligibility_reason,
	superseded_at,COALESCE(superseded_by,''),COALESCE(supersedes,''),COALESCE(correction_reason,''),
	created_at,created_by`

func (r *repo) Results(ctx context.Context, tenantID, sampleID string, includeSuperseded bool) ([]*domain.Result, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+resultCols+` FROM lab_results
		 WHERE tenant_id=$1 AND sample_id=$2 AND deleted_at IS NULL
		   AND ($3 OR superseded_at IS NULL)
		 ORDER BY analyte, instrument_ref, created_at`,
		tenantID, sampleID, includeSuperseded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Result
	for rows.Next() {
		var res domain.Result
		var analyte, eligibility string
		if err := rows.Scan(&res.ID, &res.TenantID, &res.SampleID, &analyte,
			&res.Reading.Value, &res.Reading.Scale, &res.Method,
			&res.Instrument.Ref, &res.Instrument.ValidUntil, &res.Instrument.CertificateRef,
			&res.AnalysedAt, &res.AnalysedBy, &eligibility, &res.EligibilityReason,
			&res.SupersededAt, &res.SupersededBy, &res.Supersedes, &res.CorrectionReason,
			&res.CreatedAt, &res.CreatedBy); err != nil {
			return nil, err
		}
		res.Analyte = domain.Analyte(analyte)
		res.Eligibility = domain.Eligibility(eligibility)
		out = append(out, &res)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanSample(s scanner) (*domain.Sample, error) {
	var out domain.Sample
	var kind, purpose string
	err := s.Scan(&out.ID, &out.TenantID, &out.Code, &kind, &out.SourceRef,
		&out.DrawnAt, &out.DrawnBy,
		&out.SealNumber, &out.SealBrokenAt, &out.SealBrokenBy, &out.SealBrokenReason,
		&purpose, &out.DuplicatesSampleID, &out.CreatedAt, &out.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.SourceKind = domain.SourceKind(kind)
	out.Purpose = domain.Purpose(purpose)
	return &out, nil
}

func sqlState(err error) string {
	type coded interface{ SQLState() string }
	var c coded
	if errors.As(err, &c) {
		return c.SQLState()
	}
	return ""
}
