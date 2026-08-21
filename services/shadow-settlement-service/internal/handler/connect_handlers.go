package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/domain"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/service"
)

type ComponentProto struct {
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
	// Amount is a decimal literal such as "1234.56"; the scale is taken from
	// the enclosing settlement so every line agrees on precision.
	Amount   string `json:"amount"`
	Quantity string `json:"quantity,omitempty"`
	Rate     string `json:"rate,omitempty"`
}

type IngestAssertionRequest struct {
	TenantID             string           `json:"tenant_id"`
	SourceSystemID       string           `json:"source_system_id"`
	ExternalSettlementID string           `json:"external_settlement_id"`
	ProducerRef          string           `json:"producer_ref"`
	PeriodStart          string           `json:"period_start"`
	PeriodEnd            string           `json:"period_end"`
	Currency             string           `json:"currency"`
	AmountScale          int32            `json:"amount_scale"`
	Total                string           `json:"total"`
	Components           []ComponentProto `json:"components"`
	AssertedAt           string           `json:"asserted_at"`
	ImportBatchID        string           `json:"import_batch_id"`
	SourceRecordID       string           `json:"source_record_id"`
	// RawPayload is the source record verbatim. It is hashed for replay
	// detection and never persisted.
	RawPayload json.RawMessage `json:"raw_payload"`
	CreatedBy  string          `json:"created_by"`
}

type IngestAssertionResponse struct {
	Assertion *AssertionProto `json:"assertion"`
	// Created is false when the payload had already been ingested, which makes
	// a replayed import batch observably idempotent to the caller.
	Created bool `json:"created"`
}

type AssertionProto struct {
	ID                   string           `json:"id"`
	TenantID             string           `json:"tenant_id"`
	SourceSystemID       string           `json:"source_system_id"`
	ExternalSettlementID string           `json:"external_settlement_id"`
	ProducerRef          string           `json:"producer_ref"`
	PeriodStart          string           `json:"period_start"`
	PeriodEnd            string           `json:"period_end"`
	Currency             string           `json:"currency"`
	AmountScale          int32            `json:"amount_scale"`
	Total                string           `json:"total"`
	Components           []ComponentProto `json:"components"`
	AssertedAt           string           `json:"asserted_at"`
	OriginKind           string           `json:"origin_kind"`
	ImportBatchID        string           `json:"import_batch_id"`
	SourceRecordID       string           `json:"source_record_id"`
	SourcePayloadHash    string           `json:"source_payload_hash"`
	ValidFrom            string           `json:"valid_from"`
	ValidTo              string           `json:"valid_to"`
	RecordedAt           string           `json:"recorded_at"`
	SupersededAt         string           `json:"superseded_at,omitempty"`
}

type RecordComputationRequest struct {
	TenantID      string           `json:"tenant_id"`
	AssertionID   string           `json:"assertion_id"`
	ProducerRef   string           `json:"producer_ref"`
	PeriodStart   string           `json:"period_start"`
	PeriodEnd     string           `json:"period_end"`
	Currency      string           `json:"currency"`
	AmountScale   int32            `json:"amount_scale"`
	Total         string           `json:"total"`
	Components    []ComponentProto `json:"components"`
	PolicyVersion string           `json:"policy_version"`
	RateCardID    string           `json:"rate_card_id"`
	InputDigest   string           `json:"input_digest"`
	AsOf          string           `json:"as_of"`
	CreatedBy     string           `json:"created_by"`
}

type RecordComputationResponse struct {
	Computation *ComputationProto `json:"computation"`
}

type ComputationProto struct {
	ID            string           `json:"id"`
	TenantID      string           `json:"tenant_id"`
	AssertionID   string           `json:"assertion_id,omitempty"`
	ProducerRef   string           `json:"producer_ref"`
	PeriodStart   string           `json:"period_start"`
	PeriodEnd     string           `json:"period_end"`
	Currency      string           `json:"currency"`
	AmountScale   int32            `json:"amount_scale"`
	Total         string           `json:"total"`
	Components    []ComponentProto `json:"components"`
	PolicyVersion string           `json:"policy_version"`
	RateCardID    string           `json:"rate_card_id"`
	InputDigest   string           `json:"input_digest"`
	AsOf          string           `json:"as_of"`
	DerivationID  string           `json:"derivation_id"`
	ComputedAt    string           `json:"computed_at"`
	RecordedAt    string           `json:"recorded_at"`
}

type AdjudicateRequest struct {
	TenantID      string `json:"tenant_id"`
	AssertionID   string `json:"assertion_id"`
	ComputationID string `json:"computation_id"`
	Actor         string `json:"actor"`
}

type AdjudicateResponse struct {
	Divergence *DivergenceProto `json:"divergence"`
}

type EvidenceProto struct {
	Kind               string `json:"kind"`
	ExternalMinorUnits int64  `json:"external_minor_units"`
	ShadowMinorUnits   int64  `json:"shadow_minor_units"`
	DeltaMinorUnits    int64  `json:"delta_minor_units"`
	OnlyExternal       bool   `json:"only_external,omitempty"`
	OnlyShadow         bool   `json:"only_shadow,omitempty"`
	QuantityDiffers    bool   `json:"quantity_differs,omitempty"`
	RateDiffers        bool   `json:"rate_differs,omitempty"`
}

type HypothesisProto struct {
	Classification   string   `json:"classification"`
	Confidence       float64  `json:"confidence"`
	Rationale        string   `json:"rationale"`
	SupportingFields []string `json:"supporting_fields,omitempty"`
	ModelVersion     string   `json:"model_version"`
	// Advisory is always true. It is emitted explicitly so no consumer can
	// mistake a model's hypothesis for the platform's classification.
	Advisory bool `json:"advisory"`
}

type DivergenceProto struct {
	ID              string            `json:"id"`
	TenantID        string            `json:"tenant_id"`
	AssertionID     string            `json:"assertion_id"`
	ComputationID   string            `json:"computation_id"`
	ProducerRef     string            `json:"producer_ref"`
	Currency        string            `json:"currency"`
	AmountScale     int32             `json:"amount_scale"`
	Delta           string            `json:"delta"`
	DeltaMinorUnits int64             `json:"delta_minor_units"`
	Classification  string            `json:"classification"`
	Rationale       string            `json:"rationale"`
	Evidence        []EvidenceProto   `json:"evidence"`
	MLHypotheses    []HypothesisProto `json:"ml_hypotheses,omitempty"`
	Status          string            `json:"status"`
	Resolution      string            `json:"resolution,omitempty"`
	ResolvedAt      string            `json:"resolved_at,omitempty"`
	ResolvedBy      string            `json:"resolved_by,omitempty"`
	NeedsReview     bool              `json:"needs_review"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
}

type GetDivergenceRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetDivergenceResponse struct {
	Divergence *DivergenceProto `json:"divergence"`
}

type ListDivergencesRequest struct {
	TenantID       string `json:"tenant_id"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
	ProducerRef    string `json:"producer_ref"`
	MinAbsDelta    int64  `json:"min_abs_delta"`
	Limit          int32  `json:"limit"`
	Offset         int32  `json:"offset"`
}
type ListDivergencesResponse struct {
	Divergences []*DivergenceProto `json:"divergences"`
}

type ResolveDivergenceRequest struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	Status     string `json:"status"`
	Resolution string `json:"resolution"`
	Actor      string `json:"actor"`
}
type ResolveDivergenceResponse struct {
	Divergence *DivergenceProto `json:"divergence"`
}

type SummariseRequest struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to"`
}
type ClassSummaryProto struct {
	Classification     string `json:"classification"`
	Currency           string `json:"currency"`
	Count              int64  `json:"count"`
	TotalAbsMinorUnits int64  `json:"total_abs_minor_units"`
}
type SummariseResponse struct {
	Summaries []ClassSummaryProto `json:"summaries"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) IngestAssertion(ctx context.Context, req *connect.Request[IngestAssertionRequest]) (*connect.Response[IngestAssertionResponse], error) {
	m := req.Msg

	total, err := money.Parse(m.Total, m.AmountScale, m.Currency)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	comps, err := toDomainComponents(m.Components, m.AmountScale, m.Currency)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	periodStart, err := parseDate(m.PeriodStart, "period_start")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	periodEnd, err := parseDate(m.PeriodEnd, "period_end")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	assertedAt, err := parseTime(m.AssertedAt, "asserted_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, created, err := h.svc.IngestAssertion(ctx, service.IngestAssertionInput{
		TenantID:             m.TenantID,
		SourceSystemID:       m.SourceSystemID,
		ExternalSettlementID: m.ExternalSettlementID,
		ProducerRef:          m.ProducerRef,
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		Total:                total,
		Components:           comps,
		AssertedAt:           assertedAt,
		ImportBatchID:        m.ImportBatchID,
		SourceRecordID:       m.SourceRecordID,
		RawPayload:           m.RawPayload,
		CreatedBy:            m.CreatedBy,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&IngestAssertionResponse{Assertion: toAssertionProto(out), Created: created}), nil
}

func (h *Handler) RecordComputation(ctx context.Context, req *connect.Request[RecordComputationRequest]) (*connect.Response[RecordComputationResponse], error) {
	m := req.Msg

	total, err := money.Parse(m.Total, m.AmountScale, m.Currency)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	comps, err := toDomainComponents(m.Components, m.AmountScale, m.Currency)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	periodStart, err := parseDate(m.PeriodStart, "period_start")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	periodEnd, err := parseDate(m.PeriodEnd, "period_end")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	asOf, err := parseTime(m.AsOf, "as_of")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.RecordComputation(ctx, service.RecordComputationInput{
		TenantID:      m.TenantID,
		AssertionID:   m.AssertionID,
		ProducerRef:   m.ProducerRef,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		Total:         total,
		Components:    comps,
		PolicyVersion: m.PolicyVersion,
		RateCardID:    m.RateCardID,
		InputDigest:   m.InputDigest,
		AsOf:          asOf,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RecordComputationResponse{Computation: toComputationProto(out)}), nil
}

func (h *Handler) Adjudicate(ctx context.Context, req *connect.Request[AdjudicateRequest]) (*connect.Response[AdjudicateResponse], error) {
	m := req.Msg
	out, err := h.svc.Adjudicate(ctx, m.TenantID, m.AssertionID, m.ComputationID, m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&AdjudicateResponse{Divergence: toDivergenceProto(out)}), nil
}

func (h *Handler) GetDivergence(ctx context.Context, req *connect.Request[GetDivergenceRequest]) (*connect.Response[GetDivergenceResponse], error) {
	out, err := h.svc.GetDivergence(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetDivergenceResponse{Divergence: toDivergenceProto(out)}), nil
}

func (h *Handler) ListDivergences(ctx context.Context, req *connect.Request[ListDivergencesRequest]) (*connect.Response[ListDivergencesResponse], error) {
	m := req.Msg
	list, err := h.svc.ListDivergences(ctx, repository.DivergenceFilter{
		TenantID:       m.TenantID,
		Status:         m.Status,
		Classification: m.Classification,
		ProducerRef:    m.ProducerRef,
		MinAbsDelta:    m.MinAbsDelta,
		Limit:          int(m.Limit),
		Offset:         int(m.Offset),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*DivergenceProto, 0, len(list))
	for _, d := range list {
		out = append(out, toDivergenceProto(d))
	}
	return connect.NewResponse(&ListDivergencesResponse{Divergences: out}), nil
}

func (h *Handler) ResolveDivergence(ctx context.Context, req *connect.Request[ResolveDivergenceRequest]) (*connect.Response[ResolveDivergenceResponse], error) {
	m := req.Msg
	out, err := h.svc.ResolveDivergence(ctx, m.TenantID, m.ID, domain.DivergenceStatus(m.Status), m.Resolution, m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ResolveDivergenceResponse{Divergence: toDivergenceProto(out)}), nil
}

func (h *Handler) Summarise(ctx context.Context, req *connect.Request[SummariseRequest]) (*connect.Response[SummariseResponse], error) {
	from, err := parseTime(req.Msg.From, "from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	to, err := parseTime(req.Msg.To, "to")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	list, err := h.svc.Summarise(ctx, req.Msg.TenantID, from, to)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out := make([]ClassSummaryProto, 0, len(list))
	for _, s := range list {
		out = append(out, ClassSummaryProto{
			Classification:     string(s.Classification),
			Currency:           s.Currency,
			Count:              s.Count,
			TotalAbsMinorUnits: s.TotalAbsMinorUnits,
		})
	}
	return connect.NewResponse(&SummariseResponse{Summaries: out}), nil
}

func toDomainComponents(in []ComponentProto, scale int32, currency string) ([]domain.Component, error) {
	out := make([]domain.Component, 0, len(in))
	for _, c := range in {
		amount, err := money.Parse(c.Amount, scale, currency)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.Component{
			Kind:     domain.ComponentKind(c.Kind),
			Label:    c.Label,
			Amount:   amount,
			Quantity: c.Quantity,
			Rate:     c.Rate,
		})
	}
	return out, nil
}

func toComponentProtos(in []domain.Component) []ComponentProto {
	out := make([]ComponentProto, 0, len(in))
	for _, c := range in {
		out = append(out, ComponentProto{
			Kind:     string(c.Kind),
			Label:    c.Label,
			Amount:   c.Amount.String(),
			Quantity: c.Quantity,
			Rate:     c.Rate,
		})
	}
	return out
}

func toAssertionProto(a *domain.ExternalSettlementAssertion) *AssertionProto {
	if a == nil {
		return nil
	}
	p := &AssertionProto{
		ID:                   a.ID,
		TenantID:             a.TenantID,
		SourceSystemID:       a.SourceSystemID,
		ExternalSettlementID: a.ExternalSettlementID,
		ProducerRef:          a.ProducerRef,
		PeriodStart:          a.PeriodStart.Format(time.DateOnly),
		PeriodEnd:            a.PeriodEnd.Format(time.DateOnly),
		Currency:             a.Total.Currency,
		AmountScale:          a.Total.Scale,
		Total:                a.Total.String(),
		Components:           toComponentProtos(a.Components),
		AssertedAt:           a.AssertedAt.Format(time.RFC3339),
		OriginKind:           string(a.Origin.Kind),
		ImportBatchID:        a.Origin.ImportBatchID,
		SourceRecordID:       a.Origin.SourceRecordID,
		SourcePayloadHash:    a.Origin.SourcePayloadHash,
		ValidFrom:            a.ValidFrom.Format(time.RFC3339),
		ValidTo:              a.ValidTo.Format(time.RFC3339),
		RecordedAt:           a.RecordedAt.Format(time.RFC3339),
	}
	if a.SupersededAt != nil {
		p.SupersededAt = a.SupersededAt.Format(time.RFC3339)
	}
	return p
}

func toComputationProto(c *domain.ShadowSettlementComputation) *ComputationProto {
	if c == nil {
		return nil
	}
	return &ComputationProto{
		ID:            c.ID,
		TenantID:      c.TenantID,
		AssertionID:   c.AssertionID,
		ProducerRef:   c.ProducerRef,
		PeriodStart:   c.PeriodStart.Format(time.DateOnly),
		PeriodEnd:     c.PeriodEnd.Format(time.DateOnly),
		Currency:      c.Total.Currency,
		AmountScale:   c.Total.Scale,
		Total:         c.Total.String(),
		Components:    toComponentProtos(c.Components),
		PolicyVersion: c.PolicyVersion,
		RateCardID:    c.RateCardID,
		InputDigest:   c.InputDigest,
		AsOf:          c.AsOf.Format(time.RFC3339),
		DerivationID:  c.Origin.DerivationID,
		ComputedAt:    c.ComputedAt.Format(time.RFC3339),
		RecordedAt:    c.RecordedAt.Format(time.RFC3339),
	}
}

func toDivergenceProto(d *domain.SettlementDivergence) *DivergenceProto {
	if d == nil {
		return nil
	}
	evidence := make([]EvidenceProto, 0, len(d.Evidence))
	for _, e := range d.Evidence {
		evidence = append(evidence, EvidenceProto{
			Kind:               string(e.Kind),
			ExternalMinorUnits: e.ExternalMinorUnits,
			ShadowMinorUnits:   e.ShadowMinorUnits,
			DeltaMinorUnits:    e.DeltaMinorUnits,
			OnlyExternal:       e.OnlyExternal,
			OnlyShadow:         e.OnlyShadow,
			QuantityDiffers:    e.QuantityDiffers,
			RateDiffers:        e.RateDiffers,
		})
	}
	hyp := make([]HypothesisProto, 0, len(d.MLHypotheses))
	for _, h := range d.MLHypotheses {
		hyp = append(hyp, HypothesisProto{
			Classification:   h.Classification,
			Confidence:       h.Confidence,
			Rationale:        h.Rationale,
			SupportingFields: h.SupportingFields,
			ModelVersion:     h.ModelVersion,
			Advisory:         true,
		})
	}

	p := &DivergenceProto{
		ID:              d.ID,
		TenantID:        d.TenantID,
		AssertionID:     d.AssertionID,
		ComputationID:   d.ComputationID,
		ProducerRef:     d.ProducerRef,
		Currency:        d.Delta.Currency,
		AmountScale:     d.Delta.Scale,
		Delta:           d.Delta.String(),
		DeltaMinorUnits: d.Delta.Value,
		Classification:  string(d.Classification),
		Rationale:       d.Rationale,
		Evidence:        evidence,
		MLHypotheses:    hyp,
		Status:          string(d.Status),
		Resolution:      d.Resolution,
		ResolvedBy:      d.ResolvedBy,
		NeedsReview:     d.NeedsHumanReview(),
		CreatedAt:       d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       d.UpdatedAt.Format(time.RFC3339),
	}
	if d.ResolvedAt != nil {
		p.ResolvedAt = d.ResolvedAt.Format(time.RFC3339)
	}
	return p
}

func parseDate(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New(field + " is required")
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, errors.New(field + " must be a YYYY-MM-DD date")
	}
	return t, nil
}

// parseTime accepts an empty value: several timestamps are optional and the
// service substitutes a sensible default rather than rejecting the call.
func parseTime(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New(field + " must be an RFC3339 timestamp")
	}
	return t.UTC(), nil
}
