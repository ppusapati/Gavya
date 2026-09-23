package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

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
	route("GetReportContent", connectjson.Unary(h.GetReportContent))
	route("ListReportKinds", connectjson.Unary(h.ListReportKinds))
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
	// Timezone is the zone the schedule's times are in, as an IANA name such as
	// Asia/Kolkata. Required, and with no default.
	//
	// Seven in the morning is seven where the society is. A default of UTC here
	// would be this platform deciding what time a co-operative starts work —
	// quietly, differently from what they typed, and by five and a half hours
	// in the country most of them are in.
	Timezone string `json:"timezone"`
	// Parameters must carry a window: yesterday, last_7_days or last_month. The
	// period a scheduled report covers has to move with the firing, and a
	// schedule carrying fixed dates would produce the same report for ever.
	CreatedBy string `json:"created_by"`
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

// GetReportContentRequest asks for the report itself.
type GetReportContentRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

// GetReportContentResponse hands over what the run produced.
//
// The bytes, rather than a path. GetReportDownloadURL is the older procedure
// and it answers with a locator that nothing signs and no browser can fetch —
// this platform has never had object storage, so there has never been a URL to
// give. A report that cannot be read is a report that was not produced, so the
// content travels here.
type GetReportContentResponse struct {
	// Content is the report. Go marshals a []byte as base64, which is what a
	// JSON transport can carry and what every client of this decodes.
	Content []byte `json:"content"`
	// ContentType is what it is, including the charset. A spreadsheet that
	// guesses the encoding gets a producer's name wrong.
	ContentType string `json:"content_type"`
	// Filename is what to save it as. Built from the report rather than from
	// its name, because a name is free text and a filename is not.
	Filename string `json:"filename"`
	// Rows is how many rows of data it carries, not counting the header.
	Rows int64 `json:"row_count"`
	// Truncated says the run stopped at its ceiling. It travels with the
	// content because a truncated total somebody acts on is short by an amount
	// nothing else on the page discloses.
	Truncated bool `json:"truncated"`
	// Bytes is the length, so a caller can check what it decoded is all of it.
	Bytes int64 `json:"bytes"`
}

func (h *Handler) GetReportContent(ctx context.Context, req *connect.Request[GetReportContentRequest]) (*connect.Response[GetReportContentResponse], error) {
	rep, err := h.svc.ReportContent(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	var rows int64
	if rep.RowCount != nil {
		rows = *rep.RowCount
	}
	return connect.NewResponse(&GetReportContentResponse{
		Content:     rep.Content,
		ContentType: rep.ContentType,
		Filename:    filenameFor(rep.ReportType, rep.ID, rep.FileFormat),
		Rows:        rows,
		Truncated:   rep.Truncated,
		Bytes:       int64(len(rep.Content)),
	}), nil
}

// filenameFor builds a name a filesystem will accept.
//
// From the type and the identifier rather than from the report's name, which is
// free text somebody typed and may hold a slash, a quote or a newline. A
// filename assembled from free text is how a download writes somewhere nobody
// meant.
func filenameFor(reportType, id, format string) string {
	safe := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
				b.WriteRune(r)
			default:
				b.WriteRune('_')
			}
		}
		return b.String()
	}
	ext := safe(format)
	if ext == "" {
		ext = "csv"
	}
	return safe(reportType) + "_" + safe(id) + "." + ext
}

// ReportKindProto is one report this platform can produce.
type ReportKindProto struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Needs are the parameter keys without which it cannot run.
	Needs []string `json:"needs"`
	// Schedulable says whether a schedule can ask for it. False for a type that
	// names something a schedule has no way to supply, such as a cycle.
	Schedulable bool `json:"schedulable"`
}

// ListReportKindsRequest carries nothing.
//
// It had a tenant_id, and the reachability check caught that nothing read it:
// the reports this platform produces are the same for every co-operative, so a
// tenant here would be a field a caller supplies, is told nothing about, and
// which changes no answer. The permission still scopes the call — a caller
// needs platform.read to ask — and that is carried by the session rather than
// by the body.
//
// identity-service's ListRolesRequest was found the same way and reads the
// same. A field on the wire that nothing reads is one a caller fills in
// believing it was kept.
type ListReportKindsRequest struct{}

type ListReportKindsResponse struct {
	Kinds []ReportKindProto `json:"kinds"`
}

// ListReportKinds is the catalogue.
//
// Served rather than written into a client, because a client holding its own
// list is one that offers a type the platform stopped producing, or hides one
// it started. The same list the runner reads is the list a person chooses from.
func (h *Handler) ListReportKinds(_ context.Context, _ *connect.Request[ListReportKindsRequest]) (*connect.Response[ListReportKindsResponse], error) {
	out := &ListReportKindsResponse{Kinds: []ReportKindProto{}}
	for _, k := range h.svc.Catalogue() {
		out.Kinds = append(out.Kinds, ReportKindProto{
			Name: k.Name, Summary: k.Summary, Needs: k.Needs, Schedulable: k.Schedulable,
		})
	}
	return connect.NewResponse(out), nil
}

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
	out, err := h.svc.CreateSchedule(ctx, m.TenantID, m.ReportType, m.Schedule, m.Parameters, m.Timezone, m.CreatedBy)
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
