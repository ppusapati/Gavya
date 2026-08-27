// Package service records what was sampled, who held it, and what it read — and
// judges whether the result is fit to price milk.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/services/laboratory-service/internal/domain"
	"github.com/ppusapati/gavya/services/laboratory-service/internal/repository"
)

type IDs interface{ New() string }

type Clock interface{ Now() time.Time }

type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

type Service struct {
	repo  repository.Repository
	ids   IDs
	clock Clock
	log   Logger
}

func New(r repository.Repository, ids IDs, clock Clock, log Logger) *Service {
	return &Service{repo: r, ids: ids, clock: clock, log: log}
}

func (s *Service) DrawSample(ctx context.Context, in *domain.Sample) (*domain.Sample, error) {
	if in.Purpose == domain.AsDuplicate && in.DuplicatesSampleID != "" {
		original, err := s.repo.GetSample(ctx, in.TenantID, in.DuplicatesSampleID)
		if err != nil {
			return nil, fmt.Errorf("the sample this duplicates: %w", err)
		}
		// A duplicate is a second bottle from the same milk. One that names a
		// different source is not a duplicate of anything — it is a separate
		// sample with a misleading label, and the label is what somebody reads
		// when the two disagree.
		if original.SourceKind != in.SourceKind || original.SourceRef != in.SourceRef {
			return nil, fmt.Errorf("this duplicate is drawn from %s %s and %s was drawn from "+
				"%s %s; a duplicate is a second bottle of the same milk",
				in.SourceKind, in.SourceRef, original.Code,
				original.SourceKind, original.SourceRef)
		}
	}
	return s.repo.DrawSample(ctx, in)
}

func (s *Service) GetSample(ctx context.Context, tenantID, id string) (*domain.Sample, error) {
	return s.repo.GetSample(ctx, tenantID, id)
}

func (s *Service) ListSamples(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]*domain.Sample, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if from.IsZero() {
		return nil, errors.New("a period has to be given: listing every sample a laboratory has " +
			"ever drawn is not a question anybody is asking")
	}
	if to.IsZero() {
		to = s.clock.Now()
	}
	if to.Before(from) {
		return nil, errors.New("the period ends before it starts")
	}
	return s.repo.ListSamples(ctx, tenantID, from, to, limit)
}

// BreakSeal records that somebody opened the bottle.
//
// The moment is supplied rather than stamped from the clock, and that is the
// same choice every other event here makes: a sample is drawn at a time, handed
// over at a time, read at a time, and all three are told to the platform.
// Stamping now would assume the record is made at the instant of the act, which
// is untrue of a dock at six in the morning and of a laboratory book written up
// at the end of a shift.
//
// It matters because the gap between the break and the analysis is what decides
// whether the seal did its job. A break stamped hours late reads as a sample
// opened long before it was tested, and a reading is refused for a clerk's
// paperwork rather than for anything that happened to the milk.
func (s *Service) BreakSeal(ctx context.Context, tenantID, id, reason, actor string, at time.Time) (*domain.Sample, error) {
	if actor == "" {
		return nil, errors.New("actor is required: breaking a seal is a thing somebody did")
	}
	if at.IsZero() {
		at = s.clock.Now()
	}
	return s.repo.BreakSeal(ctx, tenantID, id, reason, actor, at)
}

// RecordHandover appends a link to a sample's chain of custody.
//
// The giver is checked against who is actually holding the sample before the
// link is written. A chain is only worth having if it is a chain, and the moment
// to catch a break is when somebody is standing there with the bottle — not
// three weeks later when a member is disputing a payment.
//
// It is refused rather than recorded-and-flagged, which is the opposite of what
// this service does with a result. The difference is that a result is a fact
// about milk and a broken chain is a mistake in the paperwork: the fact has to
// be written down whatever its standing, and the mistake can be fixed by asking
// the person in front of you who they got it from.
func (s *Service) RecordHandover(ctx context.Context, h *domain.Handover, actor string) (*domain.Handover, error) {
	switch {
	case actor == "":
		return nil, errors.New("actor is required")
	case h.From == "" || h.To == "":
		return nil, errors.New("a handover must say who handed the sample over and who took it")
	case h.From == h.To:
		return nil, errors.New("a handover to oneself is not a handover")
	case h.At.IsZero():
		return nil, errors.New("a handover must say when it happened")
	}

	sample, err := s.repo.GetSample(ctx, h.TenantID, h.SampleID)
	if err != nil {
		return nil, err
	}
	chain, err := s.repo.Chain(ctx, h.TenantID, h.SampleID)
	if err != nil {
		return nil, err
	}
	finding := domain.Custody(sample, chain, h.At)
	if !finding.Intact {
		return nil, fmt.Errorf("the chain on sample %s is already broken: %s",
			sample.Code, finding.Reason)
	}
	if finding.Holder != h.From {
		return nil, fmt.Errorf("%s is holding sample %s and this says %s handed it over",
			finding.Holder, sample.Code, h.From)
	}
	return s.repo.RecordHandover(ctx, h, actor)
}

func (s *Service) Chain(ctx context.Context, tenantID, sampleID string) ([]domain.Handover, error) {
	return s.repo.Chain(ctx, tenantID, sampleID)
}

// RecordResult writes a reading and the platform's judgement of it together.
//
// The judgement is made here rather than left to the caller, and stored rather
// than recomputed on read. A certificate renewed next month must not
// retroactively make last month's result eligible, and a caller that computed
// its own verdict would be a second implementation of the rule that decides
// whether a fortnight can be paid.
func (s *Service) RecordResult(ctx context.Context, r *domain.Result) (*domain.Result, error) {
	switch {
	case r.CreatedBy == "":
		return nil, errors.New("actor is required")
	case !domain.ValidAnalyte(r.Analyte):
		return nil, domain.ErrNoAnalyte
	case r.Method == "":
		return nil, domain.ErrNoMethod
	case r.Instrument.Ref == "":
		return nil, domain.ErrNoInstrument
	case r.AnalysedAt.IsZero():
		return nil, errors.New("a result must say when it was read")
	case r.AnalysedBy == "":
		return nil, errors.New("a result must say who read it; a reading nobody is named for " +
			"cannot be checked against who was holding the sample")
	}

	sample, err := s.repo.GetSample(ctx, r.TenantID, r.SampleID)
	if err != nil {
		return nil, err
	}
	chain, err := s.repo.Chain(ctx, r.TenantID, r.SampleID)
	if err != nil {
		return nil, err
	}

	r.Eligibility, r.EligibilityReason = domain.Judge(sample, chain, r)
	saved, err := s.repo.RecordResult(ctx, r)
	if err != nil {
		return nil, err
	}
	if saved.Eligibility != domain.Eligible {
		s.log.Infof("result %s on sample %s is %s: %s",
			saved.ID, sample.Code, saved.Eligibility, saved.EligibilityReason)
	}
	return saved, nil
}

// Report is everything known about one sample.
type Report struct {
	Sample  *domain.Sample
	Chain   []domain.Handover
	Custody domain.CustodyFinding
	// Analytes groups the live results, with the spread where a laboratory read
	// one twice.
	Analytes []domain.Disagreement
	// Eligible is how many live results are fit to price milk, and Total how
	// many there are. A caller shown only the readings cannot tell a sample
	// whose figures are usable from one whose figures are not.
	Eligible int
	Total    int
}

// Report assembles what is known about a sample: the chain, the readings, and
// which of them are fit to price milk.
func (s *Service) Report(ctx context.Context, tenantID, sampleID string) (*Report, error) {
	sample, err := s.repo.GetSample(ctx, tenantID, sampleID)
	if err != nil {
		return nil, err
	}
	chain, err := s.repo.Chain(ctx, tenantID, sampleID)
	if err != nil {
		return nil, err
	}
	results, err := s.repo.Results(ctx, tenantID, sampleID, false)
	if err != nil {
		return nil, err
	}

	out := &Report{
		Sample: sample, Chain: chain,
		// Asked as at now, which is the honest question for a report: who has
		// the bottle at this moment. Each result carries its own judgement,
		// made as at its own analysis.
		Custody:  domain.Custody(sample, chain, s.clock.Now()),
		Analytes: domain.Compare(results),
		Total:    len(results),
	}
	for _, r := range results {
		if r.Eligibility == domain.Eligible {
			out.Eligible++
		}
	}
	return out, nil
}
