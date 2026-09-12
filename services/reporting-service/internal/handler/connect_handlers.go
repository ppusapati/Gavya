package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
	"github.com/ppusapati/gavya/services/reporting-service/internal/repository"
	"github.com/ppusapati/gavya/services/reporting-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "reporting.v1.ReportingService"

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

	route("RequestReport", connectjson.Unary(h.RequestReport))
	route("GetReport", connectjson.Unary(h.GetReport))
	route("ListReports", connectjson.Unary(h.ListReports))
	route("GetReportDownloadURL", connectjson.Unary(h.GetReportDownloadURL))
	route("CreateSchedule", connectjson.Unary(h.CreateSchedule))
	route("ListSchedules", connectjson.Unary(h.ListSchedules))
	route("UpdateSchedule", connectjson.Unary(h.UpdateSchedule))
	route("DeleteSchedule", connectjson.Unary(h.DeleteSchedule))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing report from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
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

type RequestReportRequest struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	ReportType  string `json:"report_type"`
	Parameters  string `json:"parameters"`
	FileFormat  string `json:"file_format"`
	RequestedBy string `json:"requested_by"`
	CreatedBy   string `json:"created_by"`
}

type ReportResponse struct {
	Report *domain.Report `json:"report"`
}

type GetReportRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListReportsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListReportsResponse struct {
	Reports []*domain.Report `json:"reports"`
}

type GetReportDownloadURLRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type GetReportDownloadURLResponse struct {
	URL string `json:"url"`
}

type CreateScheduleRequest struct {
	TenantID   string `json:"tenant_id"`
	ReportType string `json:"report_type"`
	Schedule   string `json:"schedule"`
	Parameters string `json:"parameters"`
	CreatedBy  string `json:"created_by"`
}

type ScheduleResponse struct {
	Schedule *domain.ReportSchedule `json:"schedule"`
}

type ListSchedulesRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListSchedulesResponse struct {
	Schedules []*domain.ReportSchedule `json:"schedules"`
}

type UpdateScheduleRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	IsActive  bool   `json:"is_active"`
	UpdatedBy string `json:"updated_by"`
}

type DeleteScheduleRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type DeleteScheduleResponse struct{}

func (h *Handler) RequestReport(ctx context.Context, req *connect.Request[RequestReportRequest]) (*connect.Response[ReportResponse], error) {
	m := req.Msg
	out, err := h.svc.RequestReport(ctx, m.TenantID, m.Name, m.ReportType, m.Parameters, m.FileFormat, m.RequestedBy, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ReportResponse{Report: out}), nil
}

func (h *Handler) GetReport(ctx context.Context, req *connect.Request[GetReportRequest]) (*connect.Response[ReportResponse], error) {
	out, err := h.svc.GetReport(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ReportResponse{Report: out}), nil
}

func (h *Handler) ListReports(ctx context.Context, req *connect.Request[ListReportsRequest]) (*connect.Response[ListReportsResponse], error) {
	out, err := h.svc.ListReports(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListReportsResponse{Reports: out}), nil
}

func (h *Handler) GetReportDownloadURL(ctx context.Context, req *connect.Request[GetReportDownloadURLRequest]) (*connect.Response[GetReportDownloadURLResponse], error) {
	url, err := h.svc.GetReportDownloadURL(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetReportDownloadURLResponse{URL: url}), nil
}

func (h *Handler) CreateSchedule(ctx context.Context, req *connect.Request[CreateScheduleRequest]) (*connect.Response[ScheduleResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateSchedule(ctx, m.TenantID, m.ReportType, m.Schedule, m.Parameters, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ScheduleResponse{Schedule: out}), nil
}

func (h *Handler) ListSchedules(ctx context.Context, req *connect.Request[ListSchedulesRequest]) (*connect.Response[ListSchedulesResponse], error) {
	out, err := h.svc.ListSchedules(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListSchedulesResponse{Schedules: out}), nil
}

func (h *Handler) UpdateSchedule(ctx context.Context, req *connect.Request[UpdateScheduleRequest]) (*connect.Response[ScheduleResponse], error) {
	m := req.Msg
	out, err := h.svc.UpdateSchedule(ctx, m.ID, m.TenantID, m.IsActive, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ScheduleResponse{Schedule: out}), nil
}

func (h *Handler) DeleteSchedule(ctx context.Context, req *connect.Request[DeleteScheduleRequest]) (*connect.Response[DeleteScheduleResponse], error) {
	m := req.Msg
	if err := h.svc.DeleteSchedule(ctx, m.ID, m.TenantID, m.UpdatedBy); err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&DeleteScheduleResponse{}), nil
}
