package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/bitemporal"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
)

const (
	// advisoryTimeout bounds an ML call independently of the caller's deadline.
	// An advisory answer is not worth holding a recording open for.
	advisoryTimeout = 5 * time.Second
	// anomalyHistoryLimit caps the series sent for scoring. A longer history
	// costs more than it adds once a baseline is established.
	anomalyHistoryLimit = 200
)

// ErrInvalidArgument marks a caller mistake.
//
// Without it the handler cannot tell "tenant_id is required" from "the database
// is unreachable", and it did not: every failure in this service was reported as
// invalid_argument or not_found and never as internal, so an outage arrived at
// the caller as something they had typed wrong. A reading refused because it is
// finer than its column and a reading refused because the database is down want
// opposite things from whoever sent them.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is, so
// errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(format string, args ...any) error {
	return &invalidArgument{reason: fmt.Sprintf(format, args...)}
}

// RecordObservationInput is one measured fact as its capturer states it.
type RecordObservationInput struct {
	TenantID string
	Subject  domain.SubjectRef
	Quantity domain.QuantityKind
	Value    exact.Fixed

	InstrumentID string
	SessionRef   string
	ObservedBy   string

	Origin origin.Origin

	ValidFrom time.Time
	// ValidTo zero leaves the fact true until something supersedes it.
	ValidTo time.Time
	// Corrects names the observation this one replaces. The named observation is
	// superseded rather than edited, so both readings stay readable.
	Corrects string

	UncertaintyModelID string
	// UncertaintyInputs are the model's covariates: ambient temperature,
	// instrument class, time since verification.
	UncertaintyInputs   map[string]float64
	CoverageProbability float64

	CreatedBy string
}

// RecordObservation validates, qualifies and stores one observation.
//
// The deterministic work happens first and completes before either model is
// consulted: the observation is durable, with its legal-metrology verdict, by
// the time an ML call is made. Neither call can fail the recording.
func (s *Service) RecordObservation(ctx context.Context, in RecordObservationInput) (*domain.Observation, error) {
	if err := validateObservation(in); err != nil {
		return nil, err
	}
	// The reading's one check, and the place it is held at the column's scale,
	// so what is stored, what is answered and what the ML tier is asked about
	// are one number.
	//
	// There was a check before and it caught NaN and infinity only. A reading
	// finer than the column's six decimals was rounded into it by PostgreSQL
	// without anyone being told, and one wider reached the database as a
	// constraint violation reported as an internal failure. NaN and infinity
	// need no check of their own now: neither can be written as a decimal
	// literal, so neither can be read into an exact.Fixed at all.
	//
	// Column and not NonNegativeColumn: TEMPERATURE_C is a quantity kind here,
	// and a cooling tank below zero is the ordinary case.
	//
	// One check, not two. A second in validateObservation refused the same
	// values and was written first; a mutant that emptied it changed nothing,
	// because this line had already refused them. A check that cannot fail is
	// worse than none, because the next person reads it as the one that matters.
	value, err := in.Value.Column(domain.ValueScale, domain.ValuePrecision)
	if err != nil {
		return nil, invalid("%s", exact.Field("value", err))
	}

	validTo := in.ValidTo
	if validTo.IsZero() {
		validTo = bitemporal.EndOfTime
	}
	interval, err := bitemporal.NewInterval(in.ValidFrom, validTo)
	if err != nil {
		return nil, err
	}
	// A half-open interval of zero length is true of no instant, so nothing
	// could ever select it.
	if !interval.To.After(interval.From) {
		return nil, invalid("valid_to must follow valid_from")
	}

	eligibility, err := s.assessEligibility(ctx, in.TenantID, in.InstrumentID, interval.From, in.Quantity)
	if err != nil {
		return nil, err
	}

	id := ulidpkg.New().String()

	// The prior version is closed before the new one lands, so the partial
	// unique index on live versions never sees two at once.
	if in.Corrects != "" {
		if err := s.repo.SupersedeObservation(ctx, in.TenantID, in.Corrects, id); err != nil {
			return nil, fmt.Errorf("supersede observation %s: %w", in.Corrects, err)
		}
	}

	o := &domain.Observation{
		ID:                       id,
		TenantID:                 in.TenantID,
		Subject:                  in.Subject,
		Quantity:                 in.Quantity,
		Value:                    value,
		Unit:                     in.Quantity.Unit(),
		InstrumentID:             in.InstrumentID,
		SessionRef:               in.SessionRef,
		ObservedBy:               in.ObservedBy,
		Origin:                   in.Origin,
		ValidFrom:                interval.From,
		ValidTo:                  interval.To,
		Supersedes:               in.Corrects,
		EligibilityVerdict:       eligibility.Verdict,
		EligibilityReason:        eligibility.Reason,
		EligibilityCertificateID: eligibility.CertificateID,
		UncertaintyModelID:       in.UncertaintyModelID,
		CreatedBy:                in.CreatedBy,
	}

	stored, err := s.repo.CreateObservation(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("create observation: %w", err)
	}
	if stored.EligibilityVerdict != domain.EligibilityEligible {
		s.log.Warnf("observation %s on instrument %q is %s for settlement: %s",
			stored.ID, stored.InstrumentID, stored.EligibilityVerdict, stored.EligibilityReason)
	}

	s.attachUncertainty(ctx, stored, in)
	s.attachAnomaly(ctx, stored)
	return stored, nil
}

// assessEligibility resolves the instrument's certificate and applies the pure
// legal-metrology rule to it.
func (s *Service) assessEligibility(ctx context.Context, tenantID, instrumentID string, at time.Time, quantity domain.QuantityKind) (domain.Eligibility, error) {
	if instrumentID == "" {
		return domain.AssessEligibility(s.regime, nil, at, quantity), nil
	}
	cert, err := s.repo.GetActiveCertificate(ctx, tenantID, instrumentID, at)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return domain.Eligibility{}, fmt.Errorf("load verification certificate: %w", err)
	}
	if errors.Is(err, repository.ErrNotFound) {
		cert = nil
	}
	return domain.AssessEligibility(s.regime, cert, at, quantity), nil
}

// attachUncertainty asks the Rust uncertainty service for the measurement
// budget and stores it.
//
// A failure is logged and dropped. The observation is already durable, and it
// stays readable with its uncertainty fields null and marked missing — a
// settlement must never be blocked because a model was unreachable.
func (s *Service) attachUncertainty(ctx context.Context, o *domain.Observation, in RecordObservationInput) {
	if s.uncertainty == nil || o.UncertaintyModelID == "" {
		return
	}

	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), advisoryTimeout)
	defer cancel()

	resp, err := s.uncertainty.Estimate(callCtx, &mlclient.EstimateUncertaintyRequest{
		TenantID:            o.TenantID,
		UncertaintyModelID:  o.UncertaintyModelID,
		Quantity:            string(o.Quantity),
		MeasuredValue:       o.Value.Float64(),
		Unit:                o.Unit,
		Inputs:              in.UncertaintyInputs,
		CoverageProbability: in.CoverageProbability,
	}, mlclient.CallOptions{TenantID: o.TenantID, RequestID: o.ID})
	if err != nil {
		s.log.Warnf("observation %s: uncertainty estimate unavailable, recorded without one: %v", o.ID, err)
		return
	}
	if resp.StandardUncertainty < 0 || resp.CoverageFactor <= 0 {
		s.log.Warnf("observation %s: uncertainty model %s returned an uninterpretable budget (u=%v k=%v), recorded without one",
			o.ID, o.UncertaintyModelID, resp.StandardUncertainty, resp.CoverageFactor)
		return
	}

	est := domain.UncertaintyEstimate{
		ModelID:             o.UncertaintyModelID,
		ModelVersion:        resp.ModelVersion,
		StandardUncertainty: resp.StandardUncertainty,
		ExpandedUncertainty: resp.ExpandedUncertainty,
		CoverageFactor:      resp.CoverageFactor,
		CoverageProbability: resp.CoverageProbability,
		EstimatedAt:         time.Now().UTC(),
	}
	if err := s.repo.AttachUncertainty(callCtx, o.TenantID, o.ID, est); err != nil {
		s.log.Warnf("observation %s: could not store uncertainty estimate: %v", o.ID, err)
		return
	}
	o.Uncertainty = &est
}

// attachAnomaly scores the observation against the subject's own history.
//
// The score is advisory in the strongest sense: it is attached after the
// observation is durable, a flag only adds it to a review queue, and a failure
// leaves the observation exactly as valid as it already was.
func (s *Service) attachAnomaly(ctx context.Context, o *domain.Observation) {
	if s.anomaly == nil {
		return
	}

	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), advisoryTimeout)
	defer cancel()

	history, err := s.history(callCtx, o)
	if err != nil {
		s.log.Warnf("observation %s: could not load history for scoring: %v", o.ID, err)
		return
	}

	resp, err := s.anomaly.ScoreObservation(callCtx, &mlclient.ScoreObservationRequest{
		TenantID:   o.TenantID,
		SubjectRef: o.Subject.Ref(),
		Quantity:   string(o.Quantity),
		Candidate: mlclient.SeriesPoint{
			ObservationID: o.ID,
			ValidAt:       o.ValidFrom.UTC().Format(time.RFC3339),
			Value:         o.Value.Float64(),
		},
		History: history,
	}, mlclient.CallOptions{TenantID: o.TenantID, RequestID: o.ID})
	if err != nil {
		s.log.Warnf("observation %s: anomaly scoring unavailable, recorded unscored: %v", o.ID, err)
		return
	}
	if resp.BaselineInsufficient {
		s.log.Infof("observation %s: scored against an insufficient baseline of %d points; an unflagged score here validates nothing",
			o.ID, len(history))
	}

	a := domain.AnomalyAssessment{
		Score:        resp.Score.Score,
		Flagged:      resp.Score.Flagged,
		Method:       resp.Score.Method,
		ModelVersion: resp.ModelVersion,
		LowerBound:   resp.Score.LowerBound,
		UpperBound:   resp.Score.UpperBound,
		Explanation:  resp.Score.Explanation,
		ScoredAt:     time.Now().UTC(),
	}
	if err := s.repo.AttachAnomaly(callCtx, o.TenantID, o.ID, a); err != nil {
		s.log.Warnf("observation %s: could not store anomaly score: %v", o.ID, err)
		return
	}
	if a.Flagged {
		s.log.Infof("observation %s flagged for review at score %.3f (%s): %s",
			o.ID, a.Score, a.Method, a.Explanation)
	}
	o.Anomaly = &a
}

// history returns the subject's prior readings of the same quantity, with the
// observation being scored removed so it cannot form part of its own baseline.
func (s *Service) history(ctx context.Context, o *domain.Observation) ([]mlclient.SeriesPoint, error) {
	prior, err := s.repo.ListObservationsForSubject(ctx, repository.SubjectQuery{
		TenantID: o.TenantID,
		Subject:  o.Subject,
		Quantity: o.Quantity,
		Limit:    anomalyHistoryLimit + 1,
	})
	if err != nil {
		return nil, err
	}

	points := make([]mlclient.SeriesPoint, 0, len(prior))
	for _, p := range prior {
		if p.ID == o.ID {
			continue
		}
		point := mlclient.SeriesPoint{
			ObservationID: p.ID,
			ValidAt:       p.ValidFrom.UTC().Format(time.RFC3339),
			Value:         p.Value.Float64(),
		}
		// A less precise instrument widens the tolerance band instead of being
		// flagged, but only where an estimate exists to widen it by.
		if p.Uncertainty != nil {
			point.Uncertainty = p.Uncertainty.StandardUncertainty
		}
		points = append(points, point)
	}
	return points, nil
}

func (s *Service) GetObservation(ctx context.Context, id, tenantID string) (*domain.Observation, error) {
	return s.repo.GetObservation(ctx, id, tenantID)
}

// ListObservationsForSubject answers "what did this subject measure, valid at
// this instant, as we knew it then".
func (s *Service) ListObservationsForSubject(ctx context.Context, q repository.SubjectQuery) ([]*domain.Observation, error) {
	if q.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if !q.Subject.Valid() {
		return nil, invalid("subject %s:%s is not a valid reference", q.Subject.Kind, q.Subject.ID)
	}
	if q.Quantity != "" && !q.Quantity.Valid() {
		return nil, invalid("quantity kind %q is not recognised", q.Quantity)
	}
	q.Limit, q.Offset = clampLimit(q.Limit), clampOffset(q.Offset)
	return s.repo.ListObservationsForSubject(ctx, q)
}

func (s *Service) ListFlaggedObservations(ctx context.Context, tenantID string, limit, offset int) ([]*domain.Observation, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListFlaggedObservations(ctx, tenantID, clampLimit(limit), clampOffset(offset))
}

func (s *Service) RegisterInstrument(ctx context.Context, tenantID, serial string, kind domain.InstrumentKind, label, manufacturer, model, actor string) (*domain.Instrument, error) {
	switch {
	case tenantID == "":
		return nil, invalid("tenant_id is required")
	case serial == "":
		return nil, invalid("serial is required")
	case actor == "":
		return nil, invalid("actor is required")
	}
	if !kind.Valid() {
		return nil, invalid("instrument kind %q is not recognised", kind)
	}
	return s.repo.CreateInstrument(ctx, &domain.Instrument{
		ID:        ulidpkg.New().String(),
		TenantID:  tenantID,
		Serial:    serial,
		Kind:      kind,
		Label:     label,
		Make:      manufacturer,
		Model:     model,
		CreatedBy: actor,
	})
}

func (s *Service) GetInstrument(ctx context.Context, id, tenantID string) (*domain.Instrument, error) {
	return s.repo.GetInstrument(ctx, id, tenantID)
}

// RecordCertificateInput is one legal-metrology stamping certificate.
type RecordCertificateInput struct {
	TenantID           string
	InstrumentID       string
	CertificateNumber  string
	VerifyingAuthority string
	IssuedAt           time.Time
	ExpiresAt          time.Time
	Origin             origin.Origin
	CreatedBy          string
}

// RecordCertificate stores a stamping certificate.
//
// A blank authority or number is accepted, because an imported instrument
// register often has them blank and the eligibility rule reads that blank as
// UNKNOWN. Refusing the row would instead erase the fact that a certificate was
// claimed at all.
func (s *Service) RecordCertificate(ctx context.Context, in RecordCertificateInput) (*domain.VerificationCertificate, error) {
	switch {
	case in.TenantID == "":
		return nil, invalid("tenant_id is required")
	case in.InstrumentID == "":
		return nil, invalid("instrument_id is required")
	case in.IssuedAt.IsZero():
		return nil, invalid("issued_at is required")
	case in.ExpiresAt.IsZero():
		return nil, invalid("expires_at is required")
	case !in.ExpiresAt.After(in.IssuedAt):
		return nil, invalid("expires_at must follow issued_at: a certificate covering no period verifies nothing")
	case in.CreatedBy == "":
		return nil, invalid("created_by is required")
	}
	if err := in.Origin.Validate(); err != nil {
		return nil, invalid("%s", err)
	}
	if _, err := s.repo.GetInstrument(ctx, in.InstrumentID, in.TenantID); err != nil {
		return nil, fmt.Errorf("load instrument: %w", err)
	}

	return s.repo.CreateCertificate(ctx, &domain.VerificationCertificate{
		ID:                 ulidpkg.New().String(),
		TenantID:           in.TenantID,
		InstrumentID:       in.InstrumentID,
		CertificateNumber:  in.CertificateNumber,
		VerifyingAuthority: in.VerifyingAuthority,
		IssuedAt:           in.IssuedAt.UTC(),
		ExpiresAt:          in.ExpiresAt.UTC(),
		Origin:             in.Origin,
		CreatedBy:          in.CreatedBy,
	})
}

func (s *Service) GetActiveCertificate(ctx context.Context, tenantID, instrumentID string, at time.Time) (*domain.VerificationCertificate, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return s.repo.GetActiveCertificate(ctx, tenantID, instrumentID, at)
}

func validateObservation(in RecordObservationInput) error {
	switch {
	case in.TenantID == "":
		return invalid("tenant_id is required")
	case !in.Subject.Valid():
		return invalid("subject %s:%s is not a valid reference", in.Subject.Kind, in.Subject.ID)
	case !in.Quantity.Valid():
		return invalid("quantity kind %q is not recognised", in.Quantity)
	case in.ValidFrom.IsZero():
		return invalid("valid_from is required: an observation with no instant cannot be settled against")
	case in.CreatedBy == "":
		return invalid("created_by is required")
	}
	if err := in.Origin.Validate(); err != nil {
		return invalid("%s", err)
	}
	return nil
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
