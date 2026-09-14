package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/origin"

	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
	"github.com/ppusapati/gavya/services/observation-service/internal/service"
)

type OriginProto struct {
	Kind              string `json:"kind"`
	SourceSystemID    string `json:"source_system_id,omitempty"`
	ImportBatchID     string `json:"import_batch_id,omitempty"`
	SourceRecordID    string `json:"source_record_id,omitempty"`
	SourcePayloadHash string `json:"source_payload_hash,omitempty"`
	DerivationID      string `json:"derivation_id,omitempty"`
}

type SubjectProto struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type RecordObservationRequest struct {
	TenantID string       `json:"tenant_id"`
	Subject  SubjectProto `json:"subject"`
	Quantity string       `json:"quantity_kind"`
	// Value is read from the digits that were sent, whether as a JSON string or
	// a bare number, and goes back out as a decimal literal.
	Value exact.Fixed `json:"value"`

	InstrumentID string `json:"instrument_id,omitempty"`
	SessionRef   string `json:"session_ref,omitempty"`
	ObservedBy   string `json:"observed_by,omitempty"`

	Origin OriginProto `json:"origin"`

	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to,omitempty"`
	// Corrects names the observation this one replaces. The replaced reading
	// stays readable; it is superseded, never edited.
	Corrects string `json:"corrects,omitempty"`

	UncertaintyModelID  string             `json:"uncertainty_model_id,omitempty"`
	UncertaintyInputs   map[string]float64 `json:"uncertainty_inputs,omitempty"`
	CoverageProbability float64            `json:"coverage_probability,omitempty"`

	CreatedBy string `json:"created_by"`
}

type RecordObservationResponse struct {
	Observation *ObservationProto `json:"observation"`
}

// UncertaintyProto is absent when no estimate could be obtained. Its absence
// means the value carries no stated confidence, not a perfect one.
type UncertaintyProto struct {
	ModelID             string  `json:"uncertainty_model_id"`
	ModelVersion        string  `json:"model_version,omitempty"`
	StandardUncertainty float64 `json:"standard_uncertainty"`
	ExpandedUncertainty float64 `json:"expanded_uncertainty"`
	CoverageFactor      float64 `json:"coverage_factor"`
	CoverageProbability float64 `json:"coverage_probability"`
	EstimatedAt         string  `json:"estimated_at,omitempty"`
}

type AnomalyProto struct {
	Score        float64 `json:"score"`
	Flagged      bool    `json:"flagged"`
	Method       string  `json:"method,omitempty"`
	ModelVersion string  `json:"model_version,omitempty"`
	// LowerBound and UpperBound are omitted when the band is unbounded.
	LowerBound  *float64 `json:"lower_bound,omitempty"`
	UpperBound  *float64 `json:"upper_bound,omitempty"`
	Explanation string   `json:"explanation,omitempty"`
	ScoredAt    string   `json:"scored_at,omitempty"`
}

type ObservationProto struct {
	ID       string       `json:"id"`
	TenantID string       `json:"tenant_id"`
	Subject  SubjectProto `json:"subject"`
	Quantity string       `json:"quantity_kind"`
	Value    exact.Fixed  `json:"value"`
	Unit     string       `json:"unit"`

	InstrumentID string `json:"instrument_id,omitempty"`
	SessionRef   string `json:"session_ref,omitempty"`
	ObservedBy   string `json:"observed_by,omitempty"`

	Origin OriginProto `json:"origin"`

	ValidFrom    string `json:"valid_from"`
	ValidTo      string `json:"valid_to"`
	RecordedAt   string `json:"recorded_at"`
	SupersededAt string `json:"superseded_at,omitempty"`
	SupersededBy string `json:"superseded_by,omitempty"`
	Supersedes   string `json:"supersedes,omitempty"`
	Current      bool   `json:"current"`

	EligibilityVerdict       string `json:"eligibility_verdict"`
	EligibilityReason        string `json:"eligibility_reason"`
	EligibilityCertificateID string `json:"eligibility_certificate_id,omitempty"`

	Uncertainty *UncertaintyProto `json:"uncertainty,omitempty"`
	// UncertaintyMissing is stated explicitly rather than left to the absence of
	// the object above, so a consumer cannot read a missing estimate as zero.
	UncertaintyMissing bool `json:"uncertainty_missing"`

	Anomaly *AnomalyProto `json:"anomaly,omitempty"`

	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
}

type GetObservationRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetObservationResponse struct {
	Observation *ObservationProto `json:"observation"`
}

type ListObservationsForSubjectRequest struct {
	TenantID string       `json:"tenant_id"`
	Subject  SubjectProto `json:"subject"`
	Quantity string       `json:"quantity_kind,omitempty"`
	// ValidAt selects the fact true of the world at that instant; AsOf selects
	// what the platform knew at that instant. Empty ValidAt returns every
	// interval, empty AsOf returns current knowledge.
	ValidAt string `json:"valid_at,omitempty"`
	AsOf    string `json:"as_of,omitempty"`
	Limit   int32  `json:"limit"`
	Offset  int32  `json:"offset"`
}
type ListObservationsForSubjectResponse struct {
	Observations []*ObservationProto `json:"observations"`
}

type ListFlaggedObservationsRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListFlaggedObservationsResponse struct {
	Observations []*ObservationProto `json:"observations"`
}

type RegisterInstrumentRequest struct {
	TenantID string `json:"tenant_id"`
	Serial   string `json:"serial"`
	Kind     string `json:"kind"`
	Label    string `json:"label,omitempty"`
	Make     string `json:"make,omitempty"`
	Model    string `json:"model,omitempty"`
	Actor    string `json:"actor"`
}
type RegisterInstrumentResponse struct {
	Instrument *InstrumentProto `json:"instrument"`
}

type InstrumentProto struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Serial    string `json:"serial"`
	Kind      string `json:"kind"`
	Label     string `json:"label,omitempty"`
	Make      string `json:"make,omitempty"`
	Model     string `json:"model,omitempty"`
	CreatedAt string `json:"created_at"`
}

type GetInstrumentRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetInstrumentResponse struct {
	Instrument *InstrumentProto `json:"instrument"`
}

type RecordCertificateRequest struct {
	TenantID           string      `json:"tenant_id"`
	InstrumentID       string      `json:"instrument_id"`
	CertificateNumber  string      `json:"certificate_number"`
	VerifyingAuthority string      `json:"verifying_authority"`
	IssuedAt           string      `json:"issued_at"`
	ExpiresAt          string      `json:"expires_at"`
	Origin             OriginProto `json:"origin"`
	CreatedBy          string      `json:"created_by"`
}
type RecordCertificateResponse struct {
	Certificate *CertificateProto `json:"certificate"`
}

type CertificateProto struct {
	ID                 string      `json:"id"`
	TenantID           string      `json:"tenant_id"`
	InstrumentID       string      `json:"instrument_id"`
	CertificateNumber  string      `json:"certificate_number"`
	VerifyingAuthority string      `json:"verifying_authority"`
	IssuedAt           string      `json:"issued_at"`
	ExpiresAt          string      `json:"expires_at"`
	Origin             OriginProto `json:"origin"`
	CreatedAt          string      `json:"created_at"`
}

type GetActiveCertificateRequest struct {
	TenantID     string `json:"tenant_id"`
	InstrumentID string `json:"instrument_id"`
	At           string `json:"at,omitempty"`
	// Quantity, when given, asks for the verdict this certificate would yield
	// for that quantity. The verdict depends on it: a quantity outside legal
	// metrology is eligible whatever the certificate says.
	Quantity string `json:"quantity_kind,omitempty"`
}
type GetActiveCertificateResponse struct {
	Certificate *CertificateProto `json:"certificate"`
	// Eligibility lets a caller see what a settlement would record without
	// recording anything. It is present only when a quantity was named.
	Eligibility *EligibilityProto `json:"eligibility,omitempty"`
}

type EligibilityProto struct {
	Verdict       string `json:"verdict"`
	Reason        string `json:"reason"`
	CertificateID string `json:"certificate_id,omitempty"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "observation.v1.ObservationService"

func (h *Handler) Register(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("RecordObservation", connectjson.Unary(h.RecordObservation))
	route("GetObservation", connectjson.Unary(h.GetObservation))
	route("ListObservationsForSubject", connectjson.Unary(h.ListObservationsForSubject))
	route("ListFlaggedObservations", connectjson.Unary(h.ListFlaggedObservations))
	route("RegisterInstrument", connectjson.Unary(h.RegisterInstrument))
	route("GetInstrument", connectjson.Unary(h.GetInstrument))
	route("RecordCertificate", connectjson.Unary(h.RecordCertificate))
	route("GetActiveCertificate", connectjson.Unary(h.GetActiveCertificate))
}

func (h *Handler) RecordObservation(ctx context.Context, req *connect.Request[RecordObservationRequest]) (*connect.Response[RecordObservationResponse], error) {
	in, err := toRecordInput(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	o, err := h.svc.RecordObservation(ctx, in)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RecordObservationResponse{Observation: toObservationProto(o)}), nil
}

func (h *Handler) GetObservation(ctx context.Context, req *connect.Request[GetObservationRequest]) (*connect.Response[GetObservationResponse], error) {
	o, err := h.svc.GetObservation(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetObservationResponse{Observation: toObservationProto(o)}), nil
}

func (h *Handler) ListObservationsForSubject(ctx context.Context, req *connect.Request[ListObservationsForSubjectRequest]) (*connect.Response[ListObservationsForSubjectResponse], error) {
	m := req.Msg
	validAt, err := optionalTime(m.ValidAt, "valid_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	asOf, err := optionalTime(m.AsOf, "as_of")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	list, err := h.svc.ListObservationsForSubject(ctx, repository.SubjectQuery{
		TenantID: m.TenantID,
		Subject:  domain.SubjectRef{Kind: domain.SubjectKind(m.Subject.Kind), ID: m.Subject.ID},
		Quantity: domain.QuantityKind(m.Quantity),
		ValidAt:  validAt,
		AsOf:     asOf,
		Limit:    int(m.Limit),
		Offset:   int(m.Offset),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ListObservationsForSubjectResponse{Observations: toObservationProtos(list)}), nil
}

func (h *Handler) ListFlaggedObservations(ctx context.Context, req *connect.Request[ListFlaggedObservationsRequest]) (*connect.Response[ListFlaggedObservationsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListFlaggedObservations(ctx, m.TenantID, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ListFlaggedObservationsResponse{Observations: toObservationProtos(list)}), nil
}

func (h *Handler) RegisterInstrument(ctx context.Context, req *connect.Request[RegisterInstrumentRequest]) (*connect.Response[RegisterInstrumentResponse], error) {
	m := req.Msg
	i, err := h.svc.RegisterInstrument(ctx, m.TenantID, m.Serial, domain.InstrumentKind(m.Kind), m.Label, m.Make, m.Model, m.Actor)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RegisterInstrumentResponse{Instrument: toInstrumentProto(i)}), nil
}

func (h *Handler) GetInstrument(ctx context.Context, req *connect.Request[GetInstrumentRequest]) (*connect.Response[GetInstrumentResponse], error) {
	i, err := h.svc.GetInstrument(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetInstrumentResponse{Instrument: toInstrumentProto(i)}), nil
}

func (h *Handler) RecordCertificate(ctx context.Context, req *connect.Request[RecordCertificateRequest]) (*connect.Response[RecordCertificateResponse], error) {
	m := req.Msg
	issuedAt, err := time.Parse(time.RFC3339, m.IssuedAt)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issued_at must be an RFC3339 timestamp"))
	}
	expiresAt, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expires_at must be an RFC3339 timestamp"))
	}

	c, err := h.svc.RecordCertificate(ctx, service.RecordCertificateInput{
		TenantID:           m.TenantID,
		InstrumentID:       m.InstrumentID,
		CertificateNumber:  m.CertificateNumber,
		VerifyingAuthority: m.VerifyingAuthority,
		IssuedAt:           issuedAt,
		ExpiresAt:          expiresAt,
		Origin:             toOrigin(m.Origin),
		CreatedBy:          m.CreatedBy,
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RecordCertificateResponse{Certificate: toCertificateProto(c)}), nil
}

func (h *Handler) GetActiveCertificate(ctx context.Context, req *connect.Request[GetActiveCertificateRequest]) (*connect.Response[GetActiveCertificateResponse], error) {
	m := req.Msg
	at, err := optionalTime(m.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	c, err := h.svc.GetActiveCertificate(ctx, m.TenantID, m.InstrumentID, at)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	out := &GetActiveCertificateResponse{Certificate: toCertificateProto(c)}
	if m.Quantity != "" {
		verdict := domain.AssessEligibility(h.svc.Regime(), c, at, domain.QuantityKind(m.Quantity))
		out.Eligibility = &EligibilityProto{
			Verdict:       string(verdict.Verdict),
			Reason:        verdict.Reason,
			CertificateID: verdict.CertificateID,
		}
	}
	return connect.NewResponse(out), nil
}

func toRecordInput(m *RecordObservationRequest) (service.RecordObservationInput, error) {
	validFrom, err := time.Parse(time.RFC3339, m.ValidFrom)
	if err != nil {
		return service.RecordObservationInput{}, errors.New("valid_from must be an RFC3339 timestamp")
	}
	validTo, err := optionalTime(m.ValidTo, "valid_to")
	if err != nil {
		return service.RecordObservationInput{}, err
	}

	return service.RecordObservationInput{
		TenantID:            m.TenantID,
		Subject:             domain.SubjectRef{Kind: domain.SubjectKind(m.Subject.Kind), ID: m.Subject.ID},
		Quantity:            domain.QuantityKind(m.Quantity),
		Value:               m.Value,
		InstrumentID:        m.InstrumentID,
		SessionRef:          m.SessionRef,
		ObservedBy:          m.ObservedBy,
		Origin:              toOrigin(m.Origin),
		ValidFrom:           validFrom.UTC(),
		ValidTo:             validTo,
		Corrects:            m.Corrects,
		UncertaintyModelID:  m.UncertaintyModelID,
		UncertaintyInputs:   m.UncertaintyInputs,
		CoverageProbability: m.CoverageProbability,
		CreatedBy:           m.CreatedBy,
	}, nil
}

func optionalTime(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New(field + " must be an RFC3339 timestamp")
	}
	return t.UTC(), nil
}

func toOrigin(p OriginProto) origin.Origin {
	return origin.Origin{
		Kind:              origin.Kind(p.Kind),
		SourceSystemID:    p.SourceSystemID,
		ImportBatchID:     p.ImportBatchID,
		SourceRecordID:    p.SourceRecordID,
		SourcePayloadHash: p.SourcePayloadHash,
		DerivationID:      p.DerivationID,
	}
}

func fromOrigin(o origin.Origin) OriginProto {
	return OriginProto{
		Kind:              string(o.Kind),
		SourceSystemID:    o.SourceSystemID,
		ImportBatchID:     o.ImportBatchID,
		SourceRecordID:    o.SourceRecordID,
		SourcePayloadHash: o.SourcePayloadHash,
		DerivationID:      o.DerivationID,
	}
}

func toObservationProtos(list []*domain.Observation) []*ObservationProto {
	out := make([]*ObservationProto, 0, len(list))
	for _, o := range list {
		out = append(out, toObservationProto(o))
	}
	return out
}

func toObservationProto(o *domain.Observation) *ObservationProto {
	if o == nil {
		return nil
	}
	p := &ObservationProto{
		ID:                       o.ID,
		TenantID:                 o.TenantID,
		Subject:                  SubjectProto{Kind: string(o.Subject.Kind), ID: o.Subject.ID},
		Quantity:                 string(o.Quantity),
		Value:                    o.Value,
		Unit:                     o.Unit,
		InstrumentID:             o.InstrumentID,
		SessionRef:               o.SessionRef,
		ObservedBy:               o.ObservedBy,
		Origin:                   fromOrigin(o.Origin),
		ValidFrom:                o.ValidFrom.Format(time.RFC3339),
		ValidTo:                  o.ValidTo.Format(time.RFC3339),
		RecordedAt:               o.RecordedAt.Format(time.RFC3339),
		SupersededBy:             o.SupersededBy,
		Supersedes:               o.Supersedes,
		Current:                  o.IsCurrent(),
		EligibilityVerdict:       string(o.EligibilityVerdict),
		EligibilityReason:        o.EligibilityReason,
		EligibilityCertificateID: o.EligibilityCertificateID,
		UncertaintyMissing:       o.UncertaintyMissing(),
		CreatedAt:                o.CreatedAt.Format(time.RFC3339),
		CreatedBy:                o.CreatedBy,
	}
	if o.SupersededAt != nil {
		p.SupersededAt = o.SupersededAt.Format(time.RFC3339)
	}
	if u := o.Uncertainty; u != nil {
		p.Uncertainty = &UncertaintyProto{
			ModelID:             u.ModelID,
			ModelVersion:        u.ModelVersion,
			StandardUncertainty: u.StandardUncertainty,
			ExpandedUncertainty: u.ExpandedUncertainty,
			CoverageFactor:      u.CoverageFactor,
			CoverageProbability: u.CoverageProbability,
		}
		if !u.EstimatedAt.IsZero() {
			p.Uncertainty.EstimatedAt = u.EstimatedAt.Format(time.RFC3339)
		}
	}
	if a := o.Anomaly; a != nil {
		p.Anomaly = &AnomalyProto{
			Score:        a.Score,
			Flagged:      a.Flagged,
			Method:       a.Method,
			ModelVersion: a.ModelVersion,
			LowerBound:   a.LowerBound,
			UpperBound:   a.UpperBound,
			Explanation:  a.Explanation,
		}
		if !a.ScoredAt.IsZero() {
			p.Anomaly.ScoredAt = a.ScoredAt.Format(time.RFC3339)
		}
	}
	return p
}

func toInstrumentProto(i *domain.Instrument) *InstrumentProto {
	if i == nil {
		return nil
	}
	return &InstrumentProto{
		ID:        i.ID,
		TenantID:  i.TenantID,
		Serial:    i.Serial,
		Kind:      string(i.Kind),
		Label:     i.Label,
		Make:      i.Make,
		Model:     i.Model,
		CreatedAt: i.CreatedAt.Format(time.RFC3339),
	}
}

func toCertificateProto(c *domain.VerificationCertificate) *CertificateProto {
	if c == nil {
		return nil
	}
	return &CertificateProto{
		ID:                 c.ID,
		TenantID:           c.TenantID,
		InstrumentID:       c.InstrumentID,
		CertificateNumber:  c.CertificateNumber,
		VerifyingAuthority: c.VerifyingAuthority,
		IssuedAt:           c.IssuedAt.Format(time.RFC3339),
		ExpiresAt:          c.ExpiresAt.Format(time.RFC3339),
		Origin:             fromOrigin(c.Origin),
		CreatedAt:          c.CreatedAt.Format(time.RFC3339),
	}
}
