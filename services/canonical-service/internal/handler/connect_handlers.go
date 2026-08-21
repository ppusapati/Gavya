package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/canonical-service/internal/domain"
	"github.com/ppusapati/gavya/services/canonical-service/internal/repository"
	"github.com/ppusapati/gavya/services/canonical-service/internal/service"
)

type MapIdentityRequest struct {
	TenantID       string  `json:"tenant_id"`
	SourceSystemID string  `json:"source_system_id"`
	EntityKind     string  `json:"entity_kind"`
	ExternalID     string  `json:"external_id"`
	EntityID       string  `json:"entity_id"`
	Method         string  `json:"method"`
	Confidence     float64 `json:"confidence,omitempty"`
	Note           string  `json:"note,omitempty"`
	ValidFrom      string  `json:"valid_from"`
	ValidTo        string  `json:"valid_to,omitempty"`
	Actor          string  `json:"actor"`
}
type MapIdentityResponse struct {
	Identity *IdentityProto `json:"identity"`
}

type IdentityProto struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	SourceSystemID string  `json:"source_system_id"`
	EntityKind     string  `json:"entity_kind"`
	ExternalID     string  `json:"external_id"`
	EntityID       string  `json:"entity_id"`
	Method         string  `json:"method"`
	Confidence     float64 `json:"confidence,omitempty"`
	Note           string  `json:"note,omitempty"`
	ValidFrom      string  `json:"valid_from"`
	ValidTo        string  `json:"valid_to"`
	RecordedAt     string  `json:"recorded_at"`
	SupersededAt   string  `json:"superseded_at,omitempty"`
}

type ResolveIdentityRequest struct {
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	EntityKind     string `json:"entity_kind"`
	ExternalID     string `json:"external_id"`
	// AsOf is required: external identifiers are reused, so a mapping has no
	// meaning without an instant to resolve at.
	AsOf string `json:"as_of"`
}
type ResolveIdentityResponse struct {
	Identity *IdentityProto `json:"identity"`
}

type ReverseResolveRequest struct {
	TenantID   string `json:"tenant_id"`
	EntityKind string `json:"entity_kind"`
	EntityID   string `json:"entity_id"`
}
type ReverseResolveResponse struct {
	Identities []*IdentityProto `json:"identities"`
}

type ListIdentitiesRequest struct {
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	Limit          int32  `json:"limit"`
	Offset         int32  `json:"offset"`
}
type ListIdentitiesResponse struct {
	Identities []*IdentityProto `json:"identities"`
}

type RetireIdentityRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Actor    string `json:"actor"`
}
type RetireIdentityResponse struct {
	Retired bool `json:"retired"`
}

type DeclarePolicyRequest struct {
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Dimensions    []string `json:"dimensions"`
	Resolution    string   `json:"resolution"`
	Version       int32    `json:"version"`
	EffectiveFrom string   `json:"effective_from"`
	EffectiveTo   string   `json:"effective_to,omitempty"`
	Actor         string   `json:"actor"`
}
type DeclarePolicyResponse struct {
	Policy *PolicyProto `json:"policy"`
}

type PolicyProto struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Dimensions    []string `json:"dimensions"`
	Resolution    string   `json:"resolution"`
	Version       int32    `json:"version"`
	EffectiveFrom string   `json:"effective_from"`
	EffectiveTo   string   `json:"effective_to,omitempty"`
}

type GetEffectivePolicyRequest struct {
	TenantID string `json:"tenant_id"`
	At       string `json:"at,omitempty"`
}
type GetEffectivePolicyResponse struct {
	Policy *PolicyProto `json:"policy"`
}

type ListPoliciesRequest struct {
	TenantID string `json:"tenant_id"`
}
type ListPoliciesResponse struct {
	Policies []*PolicyProto `json:"policies"`
}

type ClaimSlotRequest struct {
	TenantID  string            `json:"tenant_id"`
	SourceRef string            `json:"source_ref"`
	Values    map[string]string `json:"values"`
	Origin    string            `json:"origin"`
	// RecordedAt orders claims under the first- and last-wins policies.
	RecordedAt string `json:"recorded_at"`
	Quality    int32  `json:"quality,omitempty"`
	// CollectedAt selects the policy version in force when the milk was
	// collected, not the one in force today.
	CollectedAt string `json:"collected_at,omitempty"`
	Actor       string `json:"actor"`
}
type ClaimSlotResponse struct {
	Outcome string     `json:"outcome"`
	Reason  string     `json:"reason"`
	Slot    *SlotProto `json:"slot"`
}

type ContenderProto struct {
	SourceRef  string `json:"source_ref"`
	Origin     string `json:"origin"`
	RecordedAt string `json:"recorded_at"`
	Quality    int32  `json:"quality"`
	Reason     string `json:"reason"`
}

type SlotProto struct {
	ID                  string            `json:"id"`
	TenantID            string            `json:"tenant_id"`
	SlotKey             string            `json:"slot_key"`
	OriginKind          string            `json:"origin_kind"`
	PolicyID            string            `json:"policy_id"`
	PolicyVersion       int32             `json:"policy_version"`
	AuthoritativeRef    string            `json:"authoritative_ref"`
	Status              string            `json:"status"`
	IncumbentRecordedAt string            `json:"incumbent_recorded_at"`
	IncumbentQuality    int32             `json:"incumbent_quality"`
	Contenders          []ContenderProto  `json:"contenders"`
	Values              map[string]string `json:"values"`
	Resolution          string            `json:"resolution,omitempty"`
	ResolvedAt          string            `json:"resolved_at,omitempty"`
	ResolvedBy          string            `json:"resolved_by,omitempty"`
}

type GetSlotRequest struct {
	TenantID   string `json:"tenant_id"`
	SlotKey    string `json:"slot_key"`
	OriginKind string `json:"origin_kind"`
}
type GetSlotResponse struct {
	Slot *SlotProto `json:"slot"`
}

type ListConflictsRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListConflictsResponse struct {
	Slots []*SlotProto `json:"slots"`
}

type ResolveConflictRequest struct {
	TenantID         string `json:"tenant_id"`
	SlotID           string `json:"slot_id"`
	AuthoritativeRef string `json:"authoritative_ref"`
	Resolution       string `json:"resolution"`
	Actor            string `json:"actor"`
}
type ResolveConflictResponse struct {
	Slot *SlotProto `json:"slot"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) MapIdentity(ctx context.Context, req *connect.Request[MapIdentityRequest]) (*connect.Response[MapIdentityResponse], error) {
	m := req.Msg

	validFrom, err := parseTime(m.ValidFrom, "valid_from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	validTo, err := parseTime(m.ValidTo, "valid_to")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.MapIdentity(ctx, service.MapIdentityInput{
		TenantID:       m.TenantID,
		SourceSystemID: m.SourceSystemID,
		EntityKind:     domain.EntityKind(m.EntityKind),
		ExternalID:     m.ExternalID,
		EntityID:       m.EntityID,
		Method:         domain.MappingMethod(m.Method),
		Confidence:     m.Confidence,
		Note:           m.Note,
		ValidFrom:      validFrom,
		ValidTo:        validTo,
		Actor:          m.Actor,
	})
	if err != nil {
		if errors.Is(err, repository.ErrOverlappingIdentity) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&MapIdentityResponse{Identity: toIdentityProto(out)}), nil
}

func (h *Handler) ResolveIdentity(ctx context.Context, req *connect.Request[ResolveIdentityRequest]) (*connect.Response[ResolveIdentityResponse], error) {
	m := req.Msg
	asOf, err := parseTime(m.AsOf, "as_of")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.ResolveIdentity(ctx, m.TenantID, m.SourceSystemID, domain.EntityKind(m.EntityKind), m.ExternalID, asOf)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ResolveIdentityResponse{Identity: toIdentityProto(out)}), nil
}

func (h *Handler) ReverseResolve(ctx context.Context, req *connect.Request[ReverseResolveRequest]) (*connect.Response[ReverseResolveResponse], error) {
	m := req.Msg
	list, err := h.svc.ReverseResolve(ctx, m.TenantID, domain.EntityKind(m.EntityKind), m.EntityID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&ReverseResolveResponse{Identities: toIdentityProtos(list)}), nil
}

func (h *Handler) ListIdentities(ctx context.Context, req *connect.Request[ListIdentitiesRequest]) (*connect.Response[ListIdentitiesResponse], error) {
	m := req.Msg
	list, err := h.svc.ListIdentities(ctx, m.TenantID, m.SourceSystemID, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&ListIdentitiesResponse{Identities: toIdentityProtos(list)}), nil
}

func (h *Handler) RetireIdentity(ctx context.Context, req *connect.Request[RetireIdentityRequest]) (*connect.Response[RetireIdentityResponse], error) {
	m := req.Msg
	if err := h.svc.RetireIdentity(ctx, m.TenantID, m.ID, m.Actor); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RetireIdentityResponse{Retired: true}), nil
}

func (h *Handler) DeclarePolicy(ctx context.Context, req *connect.Request[DeclarePolicyRequest]) (*connect.Response[DeclarePolicyResponse], error) {
	m := req.Msg

	effectiveFrom, err := parseTime(m.EffectiveFrom, "effective_from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var effectiveTo *time.Time
	if m.EffectiveTo != "" {
		t, err := parseTime(m.EffectiveTo, "effective_to")
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		effectiveTo = &t
	}

	dims := make([]domain.IdentityDimension, 0, len(m.Dimensions))
	for _, d := range m.Dimensions {
		dims = append(dims, domain.IdentityDimension(d))
	}

	out, err := h.svc.DeclarePolicy(ctx, service.DeclarePolicyInput{
		TenantID:      m.TenantID,
		Name:          m.Name,
		Dimensions:    dims,
		Resolution:    domain.ResolutionMode(m.Resolution),
		Version:       m.Version,
		EffectiveFrom: effectiveFrom,
		EffectiveTo:   effectiveTo,
		Actor:         m.Actor,
	})
	if err != nil {
		if errors.Is(err, repository.ErrOverlappingPolicy) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&DeclarePolicyResponse{Policy: toPolicyProto(out)}), nil
}

func (h *Handler) GetEffectivePolicy(ctx context.Context, req *connect.Request[GetEffectivePolicyRequest]) (*connect.Response[GetEffectivePolicyResponse], error) {
	at, err := parseTime(req.Msg.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out, err := h.svc.GetEffectivePolicy(ctx, req.Msg.TenantID, at)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetEffectivePolicyResponse{Policy: toPolicyProto(out)}), nil
}

func (h *Handler) ListPolicies(ctx context.Context, req *connect.Request[ListPoliciesRequest]) (*connect.Response[ListPoliciesResponse], error) {
	list, err := h.svc.ListPolicies(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*PolicyProto, 0, len(list))
	for _, p := range list {
		out = append(out, toPolicyProto(p))
	}
	return connect.NewResponse(&ListPoliciesResponse{Policies: out}), nil
}

func (h *Handler) ClaimSlot(ctx context.Context, req *connect.Request[ClaimSlotRequest]) (*connect.Response[ClaimSlotResponse], error) {
	m := req.Msg

	recordedAt, err := parseTime(m.RecordedAt, "recorded_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	collectedAt, err := parseTime(m.CollectedAt, "collected_at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	values := make(map[domain.IdentityDimension]string, len(m.Values))
	for k, v := range m.Values {
		values[domain.IdentityDimension(k)] = v
	}

	res, err := h.svc.Claim(ctx, service.ClaimInput{
		TenantID:    m.TenantID,
		SourceRef:   m.SourceRef,
		Values:      values,
		Origin:      origin.Kind(m.Origin),
		RecordedAt:  recordedAt,
		Quality:     m.Quality,
		CollectedAt: collectedAt,
		Actor:       m.Actor,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ClaimSlotResponse{
		Outcome: string(res.Decision.Outcome),
		Reason:  res.Decision.Reason,
		Slot:    toSlotProto(res.Slot),
	}), nil
}

func (h *Handler) GetSlot(ctx context.Context, req *connect.Request[GetSlotRequest]) (*connect.Response[GetSlotResponse], error) {
	m := req.Msg
	out, err := h.svc.GetSlot(ctx, m.TenantID, m.SlotKey, origin.Kind(m.OriginKind))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetSlotResponse{Slot: toSlotProto(out)}), nil
}

func (h *Handler) ListConflicts(ctx context.Context, req *connect.Request[ListConflictsRequest]) (*connect.Response[ListConflictsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListConflicts(ctx, m.TenantID, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*SlotProto, 0, len(list))
	for _, s := range list {
		out = append(out, toSlotProto(s))
	}
	return connect.NewResponse(&ListConflictsResponse{Slots: out}), nil
}

func (h *Handler) ResolveConflict(ctx context.Context, req *connect.Request[ResolveConflictRequest]) (*connect.Response[ResolveConflictResponse], error) {
	m := req.Msg
	out, err := h.svc.ResolveConflict(ctx, m.TenantID, m.SlotID, m.AuthoritativeRef, m.Resolution, m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ResolveConflictResponse{Slot: toSlotProto(out)}), nil
}

func toIdentityProtos(list []*domain.ExternalIdentity) []*IdentityProto {
	out := make([]*IdentityProto, 0, len(list))
	for _, i := range list {
		out = append(out, toIdentityProto(i))
	}
	return out
}

func toIdentityProto(i *domain.ExternalIdentity) *IdentityProto {
	if i == nil {
		return nil
	}
	p := &IdentityProto{
		ID:             i.ID,
		TenantID:       i.TenantID,
		SourceSystemID: i.SourceSystemID,
		EntityKind:     string(i.EntityKind),
		ExternalID:     i.ExternalID,
		EntityID:       i.EntityID,
		Method:         string(i.Method),
		Confidence:     i.Confidence,
		Note:           i.Note,
		ValidFrom:      i.ValidFrom.Format(time.RFC3339),
		ValidTo:        i.ValidTo.Format(time.RFC3339),
		RecordedAt:     i.RecordedAt.Format(time.RFC3339),
	}
	if i.SupersededAt != nil {
		p.SupersededAt = i.SupersededAt.Format(time.RFC3339)
	}
	return p
}

func toPolicyProto(p *domain.CollectionIdentityPolicy) *PolicyProto {
	if p == nil {
		return nil
	}
	dims := make([]string, 0, len(p.Dimensions))
	for _, d := range p.Dimensions {
		dims = append(dims, string(d))
	}
	out := &PolicyProto{
		ID:            p.ID,
		TenantID:      p.TenantID,
		Name:          p.Name,
		Dimensions:    dims,
		Resolution:    string(p.Resolution),
		Version:       p.Version,
		EffectiveFrom: p.EffectiveFrom.Format(time.RFC3339),
	}
	if p.EffectiveTo != nil {
		out.EffectiveTo = p.EffectiveTo.Format(time.RFC3339)
	}
	return out
}

func toSlotProto(s *domain.AuthoritativeCollectionSlot) *SlotProto {
	if s == nil {
		return nil
	}
	contenders := make([]ContenderProto, 0, len(s.Contenders))
	for _, c := range s.Contenders {
		contenders = append(contenders, ContenderProto{
			SourceRef:  c.SourceRef,
			Origin:     string(c.Origin),
			RecordedAt: c.RecordedAt.Format(time.RFC3339),
			Quality:    c.Quality,
			Reason:     c.Reason,
		})
	}
	values := make(map[string]string, len(s.Values))
	for k, v := range s.Values {
		values[string(k)] = v
	}

	p := &SlotProto{
		ID:                  s.ID,
		TenantID:            s.TenantID,
		SlotKey:             s.SlotKey,
		OriginKind:          string(s.OriginKind),
		PolicyID:            s.PolicyID,
		PolicyVersion:       s.PolicyVersion,
		AuthoritativeRef:    s.AuthoritativeRef,
		Status:              string(s.Status),
		IncumbentRecordedAt: s.IncumbentRecordedAt.Format(time.RFC3339),
		IncumbentQuality:    s.IncumbentQuality,
		Contenders:          contenders,
		Values:              values,
		Resolution:          s.Resolution,
		ResolvedBy:          s.ResolvedBy,
	}
	if s.ResolvedAt != nil {
		p.ResolvedAt = s.ResolvedAt.Format(time.RFC3339)
	}
	return p
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
