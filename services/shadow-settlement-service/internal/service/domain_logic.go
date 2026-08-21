package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/bitemporal"
	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	ulidpkg "p9e.in/samavaya/packages/ULID"

	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/domain"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/repository"
)

// mlConfidenceFloor is the confidence below which an advisory hypothesis is not
// worth an auditor's attention. The Rust service abstains on its own account
// too; this is a second floor the platform controls.
const mlConfidenceFloor = 0.55

// IngestAssertionInput carries one external settlement exactly as the source
// system stated it.
type IngestAssertionInput struct {
	TenantID             string
	SourceSystemID       string
	ExternalSettlementID string
	ProducerRef          string
	PeriodStart          time.Time
	PeriodEnd            time.Time
	Total                money.Money
	Components           []domain.Component
	AssertedAt           time.Time
	ImportBatchID        string
	SourceRecordID       string
	// RawPayload is the source record as delivered. It is hashed, not stored,
	// and the hash is what makes a replay idempotent.
	RawPayload []byte
	CreatedBy  string
}

// IngestAssertion records an external settlement assertion.
//
// Re-delivering a byte-identical payload returns the assertion already stored
// rather than creating a second one, so replaying an import batch is safe. A
// changed payload for the same external settlement is treated as an amendment:
// the prior version is superseded and a new version recorded, leaving both
// readable.
func (s *Service) IngestAssertion(ctx context.Context, in IngestAssertionInput) (*domain.ExternalSettlementAssertion, bool, error) {
	if err := validateAssertionInput(in); err != nil {
		return nil, false, err
	}

	payloadHash := origin.HashPayload(in.RawPayload)

	existing, err := s.repo.FindAssertionByPayload(ctx, in.TenantID, in.SourceSystemID, in.SourceRecordID, payloadHash)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, false, fmt.Errorf("check for replay: %w", err)
	}
	if existing != nil {
		s.log.Infof("assertion replay ignored: tenant=%s source_record=%s assertion=%s",
			in.TenantID, in.SourceRecordID, existing.ID)
		return existing, false, nil
	}

	recordOrigin, err := origin.NewImported(in.SourceSystemID, in.ImportBatchID, in.SourceRecordID, payloadHash)
	if err != nil {
		return nil, false, err
	}

	id := ulidpkg.New().String()

	// An amendment supersedes the live version before the new one lands, so the
	// partial unique index on live versions never sees two at once.
	if err := s.repo.SupersedeAssertion(ctx, in.TenantID, in.SourceSystemID, in.ExternalSettlementID, id); err != nil {
		return nil, false, fmt.Errorf("supersede prior assertion: %w", err)
	}

	validFrom := in.AssertedAt
	if validFrom.IsZero() {
		validFrom = in.PeriodEnd
	}

	a := &domain.ExternalSettlementAssertion{
		ID:                   id,
		TenantID:             in.TenantID,
		SourceSystemID:       in.SourceSystemID,
		ExternalSettlementID: in.ExternalSettlementID,
		ProducerRef:          in.ProducerRef,
		PeriodStart:          in.PeriodStart,
		PeriodEnd:            in.PeriodEnd,
		Total:                in.Total,
		Components:           in.Components,
		AssertedAt:           in.AssertedAt,
		Origin:               recordOrigin,
		ValidFrom:            validFrom,
		ValidTo:              bitemporal.EndOfTime,
		CreatedBy:            in.CreatedBy,
	}

	out, err := s.repo.CreateAssertion(ctx, a)
	if errors.Is(err, repository.ErrDuplicateAssertion) {
		// Lost a race with a concurrent import of the same record. The winner's
		// row is the correct answer for both callers.
		dup, findErr := s.repo.FindAssertionByPayload(ctx, in.TenantID, in.SourceSystemID, in.SourceRecordID, payloadHash)
		if findErr != nil {
			return nil, false, fmt.Errorf("resolve duplicate assertion: %w", findErr)
		}
		return dup, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("create assertion: %w", err)
	}
	return out, true, nil
}

// RecordComputationInput carries a shadow settlement this platform computed.
type RecordComputationInput struct {
	TenantID      string
	AssertionID   string
	ProducerRef   string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Total         money.Money
	Components    []domain.Component
	PolicyVersion string
	RateCardID    string
	RoundingTrail []money.RoundingStep
	InputDigest   string
	AsOf          time.Time
	CreatedBy     string
}

// RecordComputation stores a shadow computation, superseding any prior one for
// the same producer and period.
//
// The computation is never paid. Recomputing a period is expected — inputs
// arrive late, policies are corrected — and each recomputation is a new version
// rather than an overwrite.
func (s *Service) RecordComputation(ctx context.Context, in RecordComputationInput) (*domain.ShadowSettlementComputation, error) {
	if err := validateComputationInput(in); err != nil {
		return nil, err
	}

	id := ulidpkg.New().String()
	derivation, err := origin.NewDerived(ulidpkg.New().String())
	if err != nil {
		return nil, err
	}

	if err := s.repo.SupersedeComputation(ctx, in.TenantID, in.ProducerRef, in.PeriodStart, in.PeriodEnd, id); err != nil {
		return nil, fmt.Errorf("supersede prior computation: %w", err)
	}

	computedAt := time.Now().UTC()
	asOf := in.AsOf
	if asOf.IsZero() {
		asOf = computedAt
	}

	c := &domain.ShadowSettlementComputation{
		ID:            id,
		TenantID:      in.TenantID,
		AssertionID:   in.AssertionID,
		ProducerRef:   in.ProducerRef,
		PeriodStart:   in.PeriodStart,
		PeriodEnd:     in.PeriodEnd,
		Total:         in.Total,
		Components:    in.Components,
		PolicyVersion: in.PolicyVersion,
		RateCardID:    in.RateCardID,
		RoundingTrail: in.RoundingTrail,
		InputDigest:   in.InputDigest,
		AsOf:          asOf,
		Origin:        derivation,
		ComputedAt:    computedAt,
		CreatedBy:     in.CreatedBy,
	}

	out, err := s.repo.CreateComputation(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("create computation: %w", err)
	}
	return out, nil
}

// Adjudicate compares an assertion with a computation, records the
// deterministic verdict, and — only when that verdict is UNEXPLAINED — asks the
// Rust divergence service for an advisory hypothesis.
//
// The returned divergence is authoritative regardless of whether the ML tier
// answered, was slow, or is not deployed at all.
func (s *Service) Adjudicate(ctx context.Context, tenantID, assertionID, computationID, actor string) (*domain.SettlementDivergence, error) {
	assertion, err := s.repo.GetAssertion(ctx, assertionID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load assertion: %w", err)
	}
	computation, err := s.repo.GetComputation(ctx, computationID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load computation: %w", err)
	}
	if assertion.ProducerRef != computation.ProducerRef {
		return nil, fmt.Errorf("assertion is for producer %s but computation is for %s",
			assertion.ProducerRef, computation.ProducerRef)
	}

	verdict := domain.Classify(assertion, computation)

	d := &domain.SettlementDivergence{
		ID:             ulidpkg.New().String(),
		TenantID:       tenantID,
		AssertionID:    assertionID,
		ComputationID:  computationID,
		ProducerRef:    assertion.ProducerRef,
		Delta:          verdict.Delta,
		Classification: verdict.Classification,
		Rationale:      verdict.Rationale,
		Evidence:       verdict.Evidence,
		Status:         domain.StatusOpen,
		CreatedBy:      actor,
		UpdatedBy:      actor,
	}
	if !d.NeedsHumanReview() {
		d.Status = domain.StatusAccepted
	}

	stored, err := s.repo.CreateDivergence(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("record divergence: %w", err)
	}

	if verdict.Classification == domain.ClassUnexplained {
		s.attachAdvisory(ctx, stored, verdict, actor)
	}
	return stored, nil
}

// attachAdvisory consults the ML tier for an unexplained divergence.
//
// Failure here is logged and dropped: the divergence is already recorded with
// its authoritative classification, and a human reviews it either way. Blocking
// a settlement comparison on a model being reachable would be the wrong
// trade entirely.
func (s *Service) attachAdvisory(ctx context.Context, d *domain.SettlementDivergence, verdict domain.Verdict, actor string) {
	if s.ml == nil {
		return
	}

	// Bounded independently of the caller's deadline: an advisory answer is not
	// worth holding an adjudication open for.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	resp, err := s.ml.Explain(callCtx, &mlclient.ExplainDivergenceRequest{
		TenantID:        d.TenantID,
		DivergenceID:    d.ID,
		Currency:        d.Delta.Currency,
		DeltaMinorUnits: d.Delta.Value,
		Features:        verdict.Features,
		Labels: map[string]string{
			"producer_ref": d.ProducerRef,
		},
	}, mlclient.CallOptions{TenantID: d.TenantID})
	if err != nil {
		s.log.Warnf("divergence %s: ml explanation unavailable, leaving unexplained: %v", d.ID, err)
		return
	}
	if resp.Abstained || len(resp.Hypotheses) == 0 {
		s.log.Infof("divergence %s: ml tier abstained", d.ID)
		return
	}

	hypotheses := make([]domain.MLHypothesis, 0, len(resp.Hypotheses))
	for _, h := range resp.Hypotheses {
		if h.Confidence < mlConfidenceFloor {
			continue
		}
		hypotheses = append(hypotheses, domain.MLHypothesis{
			Classification:   h.Classification,
			Confidence:       h.Confidence,
			Rationale:        h.Rationale,
			SupportingFields: h.SupportingFields,
			ModelVersion:     resp.ModelVersion,
		})
	}
	if len(hypotheses) == 0 {
		return
	}

	if err := s.repo.AttachHypotheses(callCtx, d.ID, d.TenantID, hypotheses, actor); err != nil {
		s.log.Warnf("divergence %s: could not store ml hypotheses: %v", d.ID, err)
		return
	}
	d.MLHypotheses = hypotheses
}

func (s *Service) GetDivergence(ctx context.Context, id, tenantID string) (*domain.SettlementDivergence, error) {
	return s.repo.GetDivergence(ctx, id, tenantID)
}

func (s *Service) ListDivergences(ctx context.Context, f repository.DivergenceFilter) ([]*domain.SettlementDivergence, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return s.repo.ListDivergences(ctx, f)
}

// ResolveDivergence records a reviewer's adjudication.
//
// The classification is untouched: the deterministic finding stands as the
// record of what the platform observed, and the resolution records what a human
// decided about it.
func (s *Service) ResolveDivergence(ctx context.Context, tenantID, id string, status domain.DivergenceStatus, resolution, actor string) (*domain.SettlementDivergence, error) {
	switch status {
	case domain.StatusUnderReview, domain.StatusAccepted, domain.StatusExternalWins, domain.StatusShadowWins, domain.StatusResolved:
	default:
		return nil, fmt.Errorf("status %q is not a valid resolution", status)
	}
	if resolution == "" {
		return nil, errors.New("a resolution note is required so the decision is auditable")
	}
	if actor == "" {
		return nil, errors.New("resolving actor is required")
	}
	return s.repo.ResolveDivergence(ctx, id, tenantID, status, resolution, actor)
}

func (s *Service) GetAssertion(ctx context.Context, id, tenantID string) (*domain.ExternalSettlementAssertion, error) {
	return s.repo.GetAssertion(ctx, id, tenantID)
}

func (s *Service) GetComputation(ctx context.Context, id, tenantID string) (*domain.ShadowSettlementComputation, error) {
	return s.repo.GetComputation(ctx, id, tenantID)
}

func (s *Service) ListAssertions(ctx context.Context, tenantID, producerRef string, limit, offset int) ([]*domain.ExternalSettlementAssertion, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.repo.ListAssertions(ctx, tenantID, producerRef, limit, offset)
}

// Summarise reports the divergence mix for a period. This is the number the
// programme is steered by: the share of disagreement that is understood.
func (s *Service) Summarise(ctx context.Context, tenantID string, from, to time.Time) ([]domain.ClassSummary, error) {
	if to.Before(from) {
		return nil, errors.New("period end precedes period start")
	}
	return s.repo.SummariseDivergences(ctx, tenantID, from, to)
}

func validateAssertionInput(in IngestAssertionInput) error {
	switch {
	case in.TenantID == "":
		return errors.New("tenant_id is required")
	case in.SourceSystemID == "":
		return errors.New("source_system_id is required")
	case in.ExternalSettlementID == "":
		return errors.New("external_settlement_id is required")
	case in.ProducerRef == "":
		return errors.New("producer_ref is required")
	case in.ImportBatchID == "":
		return errors.New("import_batch_id is required")
	case in.SourceRecordID == "":
		return errors.New("source_record_id is required")
	case len(in.RawPayload) == 0:
		return errors.New("raw payload is required: without it a replay cannot be detected")
	case in.Total.Currency == "":
		return errors.New("currency is required")
	case in.PeriodEnd.Before(in.PeriodStart):
		return errors.New("period end precedes period start")
	case in.CreatedBy == "":
		return errors.New("created_by is required")
	}
	return nil
}

func validateComputationInput(in RecordComputationInput) error {
	switch {
	case in.TenantID == "":
		return errors.New("tenant_id is required")
	case in.ProducerRef == "":
		return errors.New("producer_ref is required")
	case in.Total.Currency == "":
		return errors.New("currency is required")
	case in.PolicyVersion == "":
		return errors.New("policy_version is required: a shadow total is not defensible without the rules that produced it")
	case in.RateCardID == "":
		return errors.New("rate_card_id is required")
	case in.InputDigest == "":
		return errors.New("input_digest is required: a shadow total must name the inputs it consumed")
	case in.PeriodEnd.Before(in.PeriodStart):
		return errors.New("period end precedes period start")
	case in.CreatedBy == "":
		return errors.New("created_by is required")
	}
	return nil
}
