package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/audit-service/internal/domain"
	"github.com/ppusapati/gavya/services/audit-service/internal/repository"
	"github.com/ppusapati/gavya/services/audit-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "audit.v1.AuditService"

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateAuditLog", connectjson.Unary(h.CreateAuditLog))
	route("GetAuditLog", connectjson.Unary(h.GetAuditLog))
	route("ListAuditLogs", connectjson.Unary(h.ListAuditLogs))
	route("ListAuditLogsByResource", connectjson.Unary(h.ListAuditLogsByResource))
	route("ListAuditLogsByActor", connectjson.Unary(h.ListAuditLogsByActor))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing audit log from an unreachable database — and makes
// an unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateAuditLogRequest struct {
	TenantID     string `json:"tenant_id"`
	ActorID      string `json:"actor_id"`
	ActorType    string `json:"actor_type"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	OldValue     string `json:"old_value"`
	NewValue     string `json:"new_value"`
	IPAddress    string `json:"ip_address"`
	UserAgent    string `json:"user_agent"`
	ServiceName  string `json:"service_name"`
	TraceID      string `json:"trace_id"`
	CreatedBy    string `json:"created_by"`
}

type AuditLogResponse struct {
	AuditLog *domain.AuditLog `json:"audit_log"`
}

type GetAuditLogRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListAuditLogsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListAuditLogsByResourceRequest struct {
	TenantID     string `json:"tenant_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type ListAuditLogsByActorRequest struct {
	TenantID string `json:"tenant_id"`
	ActorID  string `json:"actor_id"`
}

type ListAuditLogsResponse struct {
	AuditLogs []*domain.AuditLog `json:"audit_logs"`
}

func (h *Handler) CreateAuditLog(ctx context.Context, req *connect.Request[CreateAuditLogRequest]) (*connect.Response[AuditLogResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateAuditLog(ctx, m.TenantID, m.ActorID, m.ActorType, m.Action,
		m.ResourceType, m.ResourceID, m.OldValue, m.NewValue, m.IPAddress,
		m.UserAgent, m.ServiceName, m.TraceID, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&AuditLogResponse{AuditLog: out}), nil
}

func (h *Handler) GetAuditLog(ctx context.Context, req *connect.Request[GetAuditLogRequest]) (*connect.Response[AuditLogResponse], error) {
	out, err := h.svc.GetAuditLog(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&AuditLogResponse{AuditLog: out}), nil
}

func (h *Handler) ListAuditLogs(ctx context.Context, req *connect.Request[ListAuditLogsRequest]) (*connect.Response[ListAuditLogsResponse], error) {
	out, err := h.svc.ListAuditLogs(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListAuditLogsResponse{AuditLogs: out}), nil
}

func (h *Handler) ListAuditLogsByResource(ctx context.Context, req *connect.Request[ListAuditLogsByResourceRequest]) (*connect.Response[ListAuditLogsResponse], error) {
	m := req.Msg
	out, err := h.svc.ListAuditLogsByResource(ctx, m.TenantID, m.ResourceType, m.ResourceID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListAuditLogsResponse{AuditLogs: out}), nil
}

func (h *Handler) ListAuditLogsByActor(ctx context.Context, req *connect.Request[ListAuditLogsByActorRequest]) (*connect.Response[ListAuditLogsResponse], error) {
	m := req.Msg
	out, err := h.svc.ListAuditLogsByActor(ctx, m.TenantID, m.ActorID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListAuditLogsResponse{AuditLogs: out}), nil
}
