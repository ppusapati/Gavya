package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/domain"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/repository"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/service"
)

type DeliverRecordRequest struct {
	TenantID          string          `json:"tenant_id"`
	DeviceID          string          `json:"device_id"`
	Generation        int64           `json:"generation"`
	ExternalSessionID string          `json:"external_session_id"`
	Sequence          int64           `json:"sequence"`
	Payload           json.RawMessage `json:"payload"`
	CapturedAt        string          `json:"captured_at"`
	Actor             string          `json:"actor"`
}

// DeliverRecordResponse tells the device exactly what became of its record, so
// it knows whether to stop retrying.
type DeliverRecordResponse struct {
	Outcome string `json:"outcome"`
	// RecordID is set for both ACCEPTED and DUPLICATE_REPLAY. A device that
	// receives either can safely drop the record from its outbox.
	RecordID string `json:"record_id,omitempty"`
	// QuarantineID is set for QUARANTINED. The record is held, not lost.
	QuarantineID string `json:"quarantine_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Detail       string `json:"detail,omitempty"`
	// ConflictingRecordID names the admitted record a conflict collided with.
	ConflictingRecordID string `json:"conflicting_record_id,omitempty"`
}

type DeliverBatchRequest struct {
	Records []DeliverRecordRequest `json:"records"`
}
type DeliverBatchResponse struct {
	Results []DeliverRecordResponse `json:"results"`
	// Counts let a device confirm the whole upload at a glance.
	Accepted    int `json:"accepted"`
	Replayed    int `json:"replayed"`
	Quarantined int `json:"quarantined"`
}

type RegisterDeviceRequest struct {
	TenantID string `json:"tenant_id"`
	Serial   string `json:"serial"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Actor    string `json:"actor"`
}
type RegisterDeviceResponse struct {
	Device *DeviceProto `json:"device"`
}

type DeviceProto struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	Serial            string `json:"serial"`
	Kind              string `json:"kind"`
	Label             string `json:"label"`
	CurrentGeneration int64  `json:"current_generation"`
	CreatedAt         string `json:"created_at"`
}

type GetDeviceRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetDeviceResponse struct {
	Device *DeviceProto `json:"device"`
}

type ListDevicesRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListDevicesResponse struct {
	Devices []*DeviceProto `json:"devices"`
}

type RollGenerationRequest struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason"`
	Actor    string `json:"actor"`
}
type RollGenerationResponse struct {
	Generation *GenerationProto `json:"generation"`
}

type GenerationProto struct {
	ID         string `json:"id"`
	DeviceID   string `json:"device_id"`
	Generation int64  `json:"generation"`
	Reason     string `json:"reason"`
	OpenedAt   string `json:"opened_at"`
	ClosedAt   string `json:"closed_at,omitempty"`
}

type ListGenerationsRequest struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
}
type ListGenerationsResponse struct {
	Generations []*GenerationProto `json:"generations"`
}

type OpenSessionRequest struct {
	TenantID          string `json:"tenant_id"`
	DeviceID          string `json:"device_id"`
	ExternalSessionID string `json:"external_session_id"`
	OperatorRef       string `json:"operator_ref"`
	Actor             string `json:"actor"`
}
type OpenSessionResponse struct {
	Session *SessionProto `json:"session"`
}

type SessionProto struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	DeviceID          string `json:"device_id"`
	Generation        int64  `json:"generation"`
	ExternalSessionID string `json:"external_session_id"`
	OperatorRef       string `json:"operator_ref"`
	Status            string `json:"status"`
	OpenedAt          string `json:"opened_at"`
	ClosedAt          string `json:"closed_at,omitempty"`
	LastSequence      int64  `json:"last_sequence"`
	RecordCount       int64  `json:"record_count"`
}

type CloseSessionRequest struct {
	TenantID  string `json:"tenant_id"`
	SessionID string `json:"session_id"`
	Actor     string `json:"actor"`
}
type CloseSessionResponse struct {
	Session *SessionProto `json:"session"`
}

type ListSessionsRequest struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListSessionsResponse struct {
	Sessions []*SessionProto `json:"sessions"`
}

type QuarantineProto struct {
	ID                  string `json:"id"`
	TenantID            string `json:"tenant_id"`
	Reason              string `json:"reason"`
	Detail              string `json:"detail"`
	DeviceID            string `json:"device_id"`
	Generation          int64  `json:"generation"`
	ExternalSessionID   string `json:"external_session_id"`
	Sequence            int64  `json:"sequence"`
	PayloadHash         string `json:"payload_hash"`
	ConflictingRecordID string `json:"conflicting_record_id,omitempty"`
	CapturedAt          string `json:"captured_at"`
	ReceivedAt          string `json:"received_at"`
	Resolved            bool   `json:"resolved"`
	Resolution          string `json:"resolution,omitempty"`
	ResolvedAt          string `json:"resolved_at,omitempty"`
	ResolvedBy          string `json:"resolved_by,omitempty"`
}

type ListQuarantinedRequest struct {
	TenantID string `json:"tenant_id"`
	Reason   string `json:"reason"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListQuarantinedResponse struct {
	Records []*QuarantineProto `json:"records"`
}

type GetQuarantinedRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetQuarantinedResponse struct {
	Record *QuarantineProto `json:"record"`
	// Payload is returned in full so a reviewer can compare it against the
	// record it collided with.
	Payload json.RawMessage `json:"payload"`
}

type ResolveQuarantineRequest struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	Resolution string `json:"resolution"`
	Actor      string `json:"actor"`
}
type ResolveQuarantineResponse struct {
	Record *QuarantineProto `json:"record"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "ingestion.v1.IngestionService"

func (h *Handler) Register(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("DeliverRecord", connectjson.Unary(h.DeliverRecord))
	route("DeliverBatch", connectjson.Unary(h.DeliverBatch))
	route("RegisterDevice", connectjson.Unary(h.RegisterDevice))
	route("GetDevice", connectjson.Unary(h.GetDevice))
	route("ListDevices", connectjson.Unary(h.ListDevices))
	route("RollGeneration", connectjson.Unary(h.RollGeneration))
	route("ListGenerations", connectjson.Unary(h.ListGenerations))
	route("OpenSession", connectjson.Unary(h.OpenSession))
	route("CloseSession", connectjson.Unary(h.CloseSession))
	route("ListSessions", connectjson.Unary(h.ListSessions))
	route("ListQuarantined", connectjson.Unary(h.ListQuarantined))
	route("GetQuarantined", connectjson.Unary(h.GetQuarantined))
	route("ResolveQuarantine", connectjson.Unary(h.ResolveQuarantine))
}

func (h *Handler) DeliverRecord(ctx context.Context, req *connect.Request[DeliverRecordRequest]) (*connect.Response[DeliverRecordResponse], error) {
	in, err := toDeliverInput(req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	res, err := h.svc.Deliver(ctx, in)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(toDeliverResponse(res)), nil
}

func (h *Handler) DeliverBatch(ctx context.Context, req *connect.Request[DeliverBatchRequest]) (*connect.Response[DeliverBatchResponse], error) {
	ins := make([]service.DeliverInput, 0, len(req.Msg.Records))
	for i := range req.Msg.Records {
		in, err := toDeliverInput(&req.Msg.Records[i])
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("record %d: %w", i, err))
		}
		ins = append(ins, in)
	}

	results, err := h.svc.DeliverBatch(ctx, ins)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out := &DeliverBatchResponse{Results: make([]DeliverRecordResponse, 0, len(results))}
	for _, r := range results {
		resp := toDeliverResponse(r)
		switch r.Decision.Outcome {
		case domain.OutcomeAccepted:
			out.Accepted++
		case domain.OutcomeDuplicateReplay:
			out.Replayed++
		case domain.OutcomeQuarantined:
			out.Quarantined++
		}
		out.Results = append(out.Results, *resp)
	}
	return connect.NewResponse(out), nil
}

func (h *Handler) RegisterDevice(ctx context.Context, req *connect.Request[RegisterDeviceRequest]) (*connect.Response[RegisterDeviceResponse], error) {
	m := req.Msg
	d, err := h.svc.RegisterDevice(ctx, m.TenantID, m.Serial, domain.DeviceKind(m.Kind), m.Label, m.Actor)
	if err != nil {
		// A reinstalled app can act on AlreadyExists — adopt the device it
		// already has and roll a generation — but not on a generic rejection.
		if errors.Is(err, repository.ErrDuplicateSerial) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RegisterDeviceResponse{Device: toDeviceProto(d)}), nil
}

func (h *Handler) GetDevice(ctx context.Context, req *connect.Request[GetDeviceRequest]) (*connect.Response[GetDeviceResponse], error) {
	d, err := h.svc.GetDevice(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetDeviceResponse{Device: toDeviceProto(d)}), nil
}

func (h *Handler) ListDevices(ctx context.Context, req *connect.Request[ListDevicesRequest]) (*connect.Response[ListDevicesResponse], error) {
	list, err := h.svc.ListDevices(ctx, req.Msg.TenantID, int(req.Msg.Limit), int(req.Msg.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*DeviceProto, 0, len(list))
	for _, d := range list {
		out = append(out, toDeviceProto(d))
	}
	return connect.NewResponse(&ListDevicesResponse{Devices: out}), nil
}

func (h *Handler) RollGeneration(ctx context.Context, req *connect.Request[RollGenerationRequest]) (*connect.Response[RollGenerationResponse], error) {
	m := req.Msg
	g, err := h.svc.RollGeneration(ctx, m.TenantID, m.DeviceID, domain.GenerationReason(m.Reason), m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RollGenerationResponse{Generation: toGenerationProto(g)}), nil
}

func (h *Handler) ListGenerations(ctx context.Context, req *connect.Request[ListGenerationsRequest]) (*connect.Response[ListGenerationsResponse], error) {
	list, err := h.svc.ListGenerations(ctx, req.Msg.TenantID, req.Msg.DeviceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*GenerationProto, 0, len(list))
	for _, g := range list {
		out = append(out, toGenerationProto(g))
	}
	return connect.NewResponse(&ListGenerationsResponse{Generations: out}), nil
}

func (h *Handler) OpenSession(ctx context.Context, req *connect.Request[OpenSessionRequest]) (*connect.Response[OpenSessionResponse], error) {
	m := req.Msg
	s, err := h.svc.OpenSession(ctx, m.TenantID, m.DeviceID, m.ExternalSessionID, m.OperatorRef, m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&OpenSessionResponse{Session: toSessionProto(s)}), nil
}

func (h *Handler) CloseSession(ctx context.Context, req *connect.Request[CloseSessionRequest]) (*connect.Response[CloseSessionResponse], error) {
	m := req.Msg
	s, err := h.svc.CloseSession(ctx, m.TenantID, m.SessionID, m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&CloseSessionResponse{Session: toSessionProto(s)}), nil
}

func (h *Handler) ListSessions(ctx context.Context, req *connect.Request[ListSessionsRequest]) (*connect.Response[ListSessionsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListSessions(ctx, m.TenantID, m.DeviceID, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*SessionProto, 0, len(list))
	for _, s := range list {
		out = append(out, toSessionProto(s))
	}
	return connect.NewResponse(&ListSessionsResponse{Sessions: out}), nil
}

func (h *Handler) ListQuarantined(ctx context.Context, req *connect.Request[ListQuarantinedRequest]) (*connect.Response[ListQuarantinedResponse], error) {
	m := req.Msg
	list, err := h.svc.ListQuarantined(ctx, m.TenantID, m.Reason, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*QuarantineProto, 0, len(list))
	for _, q := range list {
		out = append(out, toQuarantineProto(q))
	}
	return connect.NewResponse(&ListQuarantinedResponse{Records: out}), nil
}

func (h *Handler) GetQuarantined(ctx context.Context, req *connect.Request[GetQuarantinedRequest]) (*connect.Response[GetQuarantinedResponse], error) {
	q, err := h.svc.GetQuarantined(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetQuarantinedResponse{
		Record:  toQuarantineProto(q),
		Payload: json.RawMessage(q.Payload),
	}), nil
}

func (h *Handler) ResolveQuarantine(ctx context.Context, req *connect.Request[ResolveQuarantineRequest]) (*connect.Response[ResolveQuarantineResponse], error) {
	m := req.Msg
	q, err := h.svc.ResolveQuarantine(ctx, m.TenantID, m.ID, m.Resolution, m.Actor)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ResolveQuarantineResponse{Record: toQuarantineProto(q)}), nil
}

func toDeliverInput(m *DeliverRecordRequest) (service.DeliverInput, error) {
	capturedAt, err := time.Parse(time.RFC3339, m.CapturedAt)
	if err != nil {
		return service.DeliverInput{}, errors.New("captured_at must be an RFC3339 timestamp")
	}
	return service.DeliverInput{
		TenantID:          m.TenantID,
		DeviceID:          m.DeviceID,
		Generation:        m.Generation,
		ExternalSessionID: m.ExternalSessionID,
		Sequence:          m.Sequence,
		Payload:           m.Payload,
		CapturedAt:        capturedAt.UTC(),
		Actor:             m.Actor,
	}, nil
}

func toDeliverResponse(r *repository.IngestResult) *DeliverRecordResponse {
	out := &DeliverRecordResponse{Outcome: string(r.Decision.Outcome)}
	if r.Record != nil {
		out.RecordID = r.Record.ID
	}
	if r.Quarantine != nil {
		out.QuarantineID = r.Quarantine.ID
		out.Reason = string(r.Quarantine.Reason)
		out.Detail = r.Quarantine.Detail
		out.ConflictingRecordID = r.Quarantine.ConflictingRecordID
		// A quarantined record is not admitted, so the record id here would
		// only be the one it collided with. Reporting it as RecordID would tell
		// a device its payload was stored when it was not.
		out.RecordID = ""
	}
	return out
}

func toDeviceProto(d *domain.Device) *DeviceProto {
	if d == nil {
		return nil
	}
	return &DeviceProto{
		ID:                d.ID,
		TenantID:          d.TenantID,
		Serial:            d.Serial,
		Kind:              string(d.Kind),
		Label:             d.Label,
		CurrentGeneration: d.CurrentGeneration,
		CreatedAt:         d.CreatedAt.Format(time.RFC3339),
	}
}

func toGenerationProto(g *domain.DeviceGeneration) *GenerationProto {
	if g == nil {
		return nil
	}
	p := &GenerationProto{
		ID:         g.ID,
		DeviceID:   g.DeviceID,
		Generation: g.Generation,
		Reason:     string(g.Reason),
		OpenedAt:   g.OpenedAt.Format(time.RFC3339),
	}
	if g.ClosedAt != nil {
		p.ClosedAt = g.ClosedAt.Format(time.RFC3339)
	}
	return p
}

func toSessionProto(s *domain.CaptureSession) *SessionProto {
	if s == nil {
		return nil
	}
	p := &SessionProto{
		ID:                s.ID,
		TenantID:          s.TenantID,
		DeviceID:          s.DeviceID,
		Generation:        s.Generation,
		ExternalSessionID: s.ExternalSessionID,
		OperatorRef:       s.OperatorRef,
		Status:            string(s.Status),
		OpenedAt:          s.OpenedAt.Format(time.RFC3339),
		LastSequence:      s.LastSequence,
		RecordCount:       s.RecordCount,
	}
	if s.ClosedAt != nil {
		p.ClosedAt = s.ClosedAt.Format(time.RFC3339)
	}
	return p
}

func toQuarantineProto(q *domain.QuarantinedRecord) *QuarantineProto {
	if q == nil {
		return nil
	}
	p := &QuarantineProto{
		ID:                  q.ID,
		TenantID:            q.TenantID,
		Reason:              string(q.Reason),
		Detail:              q.Detail,
		DeviceID:            q.DeviceID,
		Generation:          q.Generation,
		ExternalSessionID:   q.ExternalSessionID,
		Sequence:            q.Sequence,
		PayloadHash:         q.PayloadHash,
		ConflictingRecordID: q.ConflictingRecordID,
		CapturedAt:          q.CapturedAt.Format(time.RFC3339),
		ReceivedAt:          q.ReceivedAt.Format(time.RFC3339),
		Resolved:            q.IsResolved(),
		Resolution:          q.Resolution,
		ResolvedBy:          q.ResolvedBy,
	}
	if q.ResolvedAt != nil {
		p.ResolvedAt = q.ResolvedAt.Format(time.RFC3339)
	}
	return p
}
