package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

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
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateAuditLog", connectjson.Unary(h.CreateAuditLog))
	route("GetAuditLog", connectjson.Unary(h.GetAuditLog))
	route("ListAuditLogs", connectjson.Unary(h.ListAuditLogs))
	route("ListAuditLogsByResource", connectjson.Unary(h.ListAuditLogsByResource))
	route("ListAuditLogsByActor", connectjson.Unary(h.ListAuditLogsByActor))
	route("SealAuditChain", connectjson.Unary(h.SealAuditChain))
	route("VerifyAuditChain", connectjson.Unary(h.VerifyAuditChain))
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

// -------------------------------------------------------------------------
// The trail about the trail
// -------------------------------------------------------------------------

type SealAuditChainRequest struct {
	TenantID string `json:"tenant_id"`
	// Limit bounds one run, so a tenant with a long backlog is caught up over
	// several passes rather than in one transaction that holds the sealer's lock
	// for minutes.
	Limit int `json:"limit,omitempty"`
}

type SealAuditChainResponse struct {
	Sealed      int64  `json:"sealed"`
	LastSeq     int64  `json:"last_seq"`
	LastHash    string `json:"last_hash"`
	AnchoredAt  int64  `json:"anchored_at_seq,omitempty"`
	AnchorTaken bool   `json:"anchor_taken"`
	AnchorHash  string `json:"anchor_hash,omitempty"`
}

// SealAuditChain links a tenant's new rows into its chain and anchors the head.
func (h *Handler) SealAuditChain(ctx context.Context, req *connect.Request[SealAuditChainRequest]) (*connect.Response[SealAuditChainResponse], error) {
	sealed, anchor, err := h.svc.SealAndAnchor(ctx, req.Msg.TenantID, req.Msg.Limit)
	if err != nil {
		return nil, classify(err)
	}
	out := &SealAuditChainResponse{
		Sealed: sealed.Sealed, LastSeq: sealed.LastSeq, LastHash: sealed.LastHash,
		AnchorTaken: anchor.Taken, AnchorHash: anchor.RowHash,
	}
	if anchor.Seq != nil {
		out.AnchoredAt = *anchor.Seq
	}
	return connect.NewResponse(out), nil
}

type VerifyAuditChainRequest struct {
	TenantID string `json:"tenant_id"`
}

type VerifyAuditChainResponse struct {
	Intact      bool   `json:"intact"`
	RowsChecked int64  `json:"rows_checked"`
	BrokenAtSeq int64  `json:"broken_at_seq,omitempty"`
	BrokenID    string `json:"broken_id,omitempty"`
	Detail      string `json:"detail,omitempty"`

	// Reported alongside the verdict rather than separately, because "intact"
	// on its own invites the reading that everything is accounted for, when what
	// it means is that everything sealed is accounted for.
	TotalRows        int64  `json:"total_rows"`
	SealedRows       int64  `json:"sealed_rows"`
	UnsealedRows     int64  `json:"unsealed_rows"`
	UnsealedForSecs  int64  `json:"unsealed_for_seconds,omitempty"`
	OldestUnsealedAt string `json:"oldest_unsealed_at,omitempty"`
}

// VerifyAuditChain recomputes a tenant's chain and reports what it found.
func (h *Handler) VerifyAuditChain(ctx context.Context, req *connect.Request[VerifyAuditChainRequest]) (*connect.Response[VerifyAuditChainResponse], error) {
	r, err := h.svc.VerifyChain(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	out := &VerifyAuditChainResponse{
		Intact: r.Intact, RowsChecked: r.RowsChecked, BrokenID: r.BrokenID, Detail: r.Detail,
		TotalRows: r.TotalRows, SealedRows: r.SealedRows, UnsealedRows: r.UnsealedRows,
	}
	if r.BrokenAt != nil {
		out.BrokenAtSeq = *r.BrokenAt
	}
	if r.OldestUnsealedAt != nil {
		out.OldestUnsealedAt = r.OldestUnsealedAt.UTC().Format(time.RFC3339)
		out.UnsealedForSecs = int64(r.UnsealedFor(time.Now()).Seconds())
	}
	return connect.NewResponse(out), nil
}
