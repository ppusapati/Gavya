package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/milk-service/internal/domain"
	"github.com/ppusapati/gavya/services/milk-service/internal/service"
)

type CreateSessionRequest struct {
	TenantID  string `json:"tenant_id"`
	CattleID  string `json:"cattle_id"`
	ShiftType string `json:"shift_type"`
	// Timezone is required, as an IANA name such as Asia/Kolkata, and there is
	// no default. A shift is "morning" somewhere, and which day a reading falls
	// on is read out of it later — silently defaulting that to UTC files a
	// society's evening collection under the previous day.
	//
	// It is stated once. The first session a tenant opens fixes it, and a later
	// session naming a different one is refused rather than applied.
	Timezone  string `json:"timezone"`
	CreatedBy string `json:"created_by"`
}
type CreateSessionResponse struct {
	Session *SessionProto `json:"session"`
}
type GetSessionRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetSessionResponse struct {
	Session *SessionProto `json:"session"`
}
type ListSessionsRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListSessionsResponse struct {
	Sessions []*SessionProto `json:"sessions"`
}
type UpdateSessionRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Status    string `json:"status"`
	UpdatedBy string `json:"updated_by"`
}
type UpdateSessionResponse struct {
	Session *SessionProto `json:"session"`
}
type RecordMilkRequest struct {
	TenantID       string  `json:"tenant_id"`
	SessionID      string  `json:"session_id"`
	CattleID       string  `json:"cattle_id"`
	QuantityLiters float64 `json:"quantity_liters"`
	CreatedBy      string  `json:"created_by"`
}
type RecordMilkResponse struct {
	Record *RecordProto `json:"record"`
}
type GetRecordRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetRecordResponse struct {
	Record *RecordProto `json:"record"`
}
type ListRecordsRequest struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
}
type ListRecordsResponse struct {
	Records []*RecordProto `json:"records"`
}
type DailyYieldRequest struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
	Date     string `json:"date"`
}
type DailyYieldResponse struct {
	TotalLiters float64 `json:"total_liters"`
}
type RecordQualityRequest struct {
	TenantID   string  `json:"tenant_id"`
	RecordID   string  `json:"record_id"`
	FatPercent float64 `json:"fat_percent"`
	SNFPercent float64 `json:"snf_percent"`
	Lactose    float64 `json:"lactose"`
	CreatedBy  string  `json:"created_by"`
}
type RecordQualityResponse struct {
	Quality *QualityProto `json:"quality"`
}

type SessionProto struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	CattleID  string `json:"cattle_id"`
	ShiftType string `json:"shift_type"`
	Status    string `json:"status"`
}
type RecordProto struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	SessionID      string  `json:"session_id"`
	CattleID       string  `json:"cattle_id"`
	QuantityLiters float64 `json:"quantity_liters"`
}
type QualityProto struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	RecordID   string  `json:"record_id"`
	FatPercent float64 `json:"fat_percent"`
	SNFPercent float64 `json:"snf_percent"`
	Lactose    float64 `json:"lactose"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "milk.v1.MilkService"

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateSession", connectjson.Unary(h.CreateSession))
	route("GetSession", connectjson.Unary(h.GetSession))
	route("ListSessions", connectjson.Unary(h.ListSessions))
	route("UpdateSessionStatus", connectjson.Unary(h.UpdateSessionStatus))
	route("RecordMilk", connectjson.Unary(h.RecordMilk))
	route("GetRecord", connectjson.Unary(h.GetRecord))
	route("ListSessionRecords", connectjson.Unary(h.ListSessionRecords))
	route("GetDailyYield", connectjson.Unary(h.GetDailyYield))
	route("RecordQuality", connectjson.Unary(h.RecordQuality))
}

func (h *Handler) CreateSession(ctx context.Context, req *connect.Request[CreateSessionRequest]) (*connect.Response[CreateSessionResponse], error) {
	m := req.Msg
	sess, err := h.svc.CreateSession(ctx, &domain.MilkSession{TenantID: m.TenantID, CattleID: m.CattleID, ShiftType: m.ShiftType, CreatedBy: m.CreatedBy}, m.Timezone)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&CreateSessionResponse{Session: toSessionProto(sess)}), nil
}

func (h *Handler) GetSession(ctx context.Context, req *connect.Request[GetSessionRequest]) (*connect.Response[GetSessionResponse], error) {
	sess, err := h.svc.GetSession(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetSessionResponse{Session: toSessionProto(sess)}), nil
}

func (h *Handler) ListSessions(ctx context.Context, req *connect.Request[ListSessionsRequest]) (*connect.Response[ListSessionsResponse], error) {
	list, err := h.svc.ListSessions(ctx, req.Msg.TenantID, int(req.Msg.Limit), int(req.Msg.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	protos := make([]*SessionProto, 0, len(list))
	for _, s := range list {
		protos = append(protos, toSessionProto(s))
	}
	return connect.NewResponse(&ListSessionsResponse{Sessions: protos}), nil
}

func (h *Handler) UpdateSessionStatus(ctx context.Context, req *connect.Request[UpdateSessionRequest]) (*connect.Response[UpdateSessionResponse], error) {
	m := req.Msg
	sess, err := h.svc.UpdateSessionStatus(ctx, m.ID, m.TenantID, m.Status, m.UpdatedBy)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&UpdateSessionResponse{Session: toSessionProto(sess)}), nil
}

func (h *Handler) RecordMilk(ctx context.Context, req *connect.Request[RecordMilkRequest]) (*connect.Response[RecordMilkResponse], error) {
	m := req.Msg
	rec, err := h.svc.RecordMilk(ctx, &domain.MilkRecord{TenantID: m.TenantID, SessionID: m.SessionID, CattleID: m.CattleID, QuantityLiters: m.QuantityLiters, CreatedBy: m.CreatedBy, RecordedBy: m.CreatedBy})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RecordMilkResponse{Record: toRecordProto(rec)}), nil
}

func (h *Handler) GetRecord(ctx context.Context, req *connect.Request[GetRecordRequest]) (*connect.Response[GetRecordResponse], error) {
	rec, err := h.svc.GetRecord(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetRecordResponse{Record: toRecordProto(rec)}), nil
}

func (h *Handler) ListSessionRecords(ctx context.Context, req *connect.Request[ListRecordsRequest]) (*connect.Response[ListRecordsResponse], error) {
	list, err := h.svc.ListSessionRecords(ctx, req.Msg.SessionID, req.Msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	protos := make([]*RecordProto, 0, len(list))
	for _, r := range list {
		protos = append(protos, toRecordProto(r))
	}
	return connect.NewResponse(&ListRecordsResponse{Records: protos}), nil
}

func (h *Handler) GetDailyYield(ctx context.Context, req *connect.Request[DailyYieldRequest]) (*connect.Response[DailyYieldResponse], error) {
	// The error used to be discarded. A malformed date became the zero time —
	// the first of January, year one — and the query summed the readings
	// recorded that day, of which there are none. The caller was told the animal
	// gave nothing, which is a fact somebody acts on, rather than that the
	// request was malformed, which is a fact they can fix.
	date, err := time.Parse("2006-01-02", req.Msg.Date)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("date must be a calendar day as YYYY-MM-DD, and %q is not one",
				req.Msg.Date))
	}
	total, err := h.svc.GetDailyYield(ctx, req.Msg.TenantID, req.Msg.CattleID, date)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&DailyYieldResponse{TotalLiters: total}), nil
}

func (h *Handler) RecordQuality(ctx context.Context, req *connect.Request[RecordQualityRequest]) (*connect.Response[RecordQualityResponse], error) {
	m := req.Msg
	mq, err := h.svc.RecordQuality(ctx, &domain.MilkQuality{TenantID: m.TenantID, RecordID: m.RecordID, FatPercent: m.FatPercent, SNFPercent: m.SNFPercent, Lactose: m.Lactose, CreatedBy: m.CreatedBy})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&RecordQualityResponse{Quality: toQualityProto(mq)}), nil
}

func toSessionProto(s *domain.MilkSession) *SessionProto {
	return &SessionProto{ID: s.ID, TenantID: s.TenantID, CattleID: s.CattleID, ShiftType: s.ShiftType, Status: s.Status}
}
func toRecordProto(r *domain.MilkRecord) *RecordProto {
	return &RecordProto{ID: r.ID, TenantID: r.TenantID, SessionID: r.SessionID, CattleID: r.CattleID, QuantityLiters: r.QuantityLiters}
}
func toQualityProto(mq *domain.MilkQuality) *QualityProto {
	return &QualityProto{ID: mq.ID, TenantID: mq.TenantID, RecordID: mq.RecordID, FatPercent: mq.FatPercent, SNFPercent: mq.SNFPercent, Lactose: mq.Lactose}
}
