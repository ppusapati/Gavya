package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/balance-service/internal/domain"
	"github.com/ppusapati/gavya/services/balance-service/internal/repository"
	"github.com/ppusapati/gavya/services/balance-service/internal/service"
)

type CreateWindowRequest struct {
	TenantID    string `json:"tenant_id"`
	RouteRef    string `json:"route_ref"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit"`
	Actor       string `json:"actor"`
}
type CreateWindowResponse struct {
	Window *WindowProto `json:"window"`
}

type WindowProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	RouteRef    string `json:"route_ref"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type GetWindowRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}
type GetWindowResponse struct {
	Window *WindowProto `json:"window"`
}

type ListWindowsRequest struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status,omitempty"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListWindowsResponse struct {
	Windows []*WindowProto `json:"windows"`
}

type AddFlowRequest struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
	FlowID   string `json:"flow_id"`
	// FromNode and ToNode are node ids; the empty string is the system boundary.
	FromNode     string `json:"from_node"`
	FromNodeKind string `json:"from_node_kind,omitempty"`
	ToNode       string `json:"to_node"`
	ToNodeKind   string `json:"to_node_kind,omitempty"`
	// Measured and StandardUncertainty are decimal literals, not numbers: a
	// JSON float would already have lost the third decimal by the time it
	// arrived.
	Measured            string `json:"measured"`
	StandardUncertainty string `json:"standard_uncertainty,omitempty"`
	Unmeasured          bool   `json:"unmeasured,omitempty"`
	ObservationRef      string `json:"observation_ref,omitempty"`
	Actor               string `json:"actor"`
}
type AddFlowResponse struct {
	Flow *FlowProto `json:"flow"`
}

type FlowProto struct {
	ID                  string `json:"id"`
	TenantID            string `json:"tenant_id"`
	WindowID            string `json:"window_id"`
	FlowID              string `json:"flow_id"`
	FromNode            string `json:"from_node"`
	FromNodeKind        string `json:"from_node_kind,omitempty"`
	ToNode              string `json:"to_node"`
	ToNodeKind          string `json:"to_node_kind,omitempty"`
	Measured            string `json:"measured"`
	StandardUncertainty string `json:"standard_uncertainty,omitempty"`
	Unmeasured          bool   `json:"unmeasured"`
	ObservationRef      string `json:"observation_ref,omitempty"`
	CreatedAt           string `json:"created_at"`
}

type ListFlowsRequest struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
}
type ListFlowsResponse struct {
	Flows []*FlowProto `json:"flows"`
}

type ReconcileRequest struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
	// GrossErrorThreshold is the test statistic above which a flow is reported
	// as carrying a gross error. Zero leaves it to the reconciler's default.
	GrossErrorThreshold float64 `json:"gross_error_threshold,omitempty"`
	Actor               string  `json:"actor"`
}
type ReconcileResponse struct {
	Run *RunProto `json:"run"`
}

type RunProto struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	WindowID       string `json:"window_id"`
	Converged      bool   `json:"converged"`
	ResidualBefore string `json:"residual_before"`
	// ResidualAfter is absent when no model answered; the window's imbalance is
	// still in ResidualBefore.
	ResidualAfter       string            `json:"residual_after,omitempty"`
	ModelVersion        string            `json:"model_version,omitempty"`
	GrossErrorThreshold float64           `json:"gross_error_threshold,omitempty"`
	Reason              string            `json:"reason,omitempty"`
	Flows               []ReconciledProto `json:"flows"`
	SuspectFlowIDs      []string          `json:"suspect_flow_ids,omitempty"`
	AcceptedAt          string            `json:"accepted_at,omitempty"`
	AcceptedBy          string            `json:"accepted_by,omitempty"`
	CreatedAt           string            `json:"created_at"`
}

type ReconciledProto struct {
	FlowID        string  `json:"flow_id"`
	Measured      string  `json:"measured"`
	Reconciled    string  `json:"reconciled"`
	Adjustment    string  `json:"adjustment"`
	TestStatistic float64 `json:"test_statistic"`
	GrossError    bool    `json:"gross_error"`
	Unmeasured    bool    `json:"unmeasured"`
}

type GetRunRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}
type GetRunResponse struct {
	Run *RunProto `json:"run"`
}

type ListRunsRequest struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id,omitempty"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListRunsResponse struct {
	Runs []*RunProto `json:"runs"`
}

type AcceptRunRequest struct {
	TenantID string `json:"tenant_id"`
	RunID    string `json:"run_id"`
	Actor    string `json:"actor"`
}
type AcceptRunResponse struct {
	Run *RunProto `json:"run"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "balance.v1.BalanceService"

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateWindow", connectjson.Unary(h.CreateWindow))
	route("GetWindow", connectjson.Unary(h.GetWindow))
	route("ListWindows", connectjson.Unary(h.ListWindows))
	route("AddFlow", connectjson.Unary(h.AddFlow))
	route("ListFlows", connectjson.Unary(h.ListFlows))
	route("Observability", connectjson.Unary(h.Observability))
	route("Reconcile", connectjson.Unary(h.Reconcile))
	route("GetRun", connectjson.Unary(h.GetRun))
	route("ListRuns", connectjson.Unary(h.ListRuns))
	route("AcceptRun", connectjson.Unary(h.AcceptRun))
}

func (h *Handler) CreateWindow(ctx context.Context, req *connect.Request[CreateWindowRequest]) (*connect.Response[CreateWindowResponse], error) {
	m := req.Msg

	start, err := parseTime(m.PeriodStart, "period_start")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	end, err := parseTime(m.PeriodEnd, "period_end")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.CreateWindow(ctx, service.CreateWindowInput{
		TenantID:    m.TenantID,
		RouteRef:    m.RouteRef,
		PeriodStart: start,
		PeriodEnd:   end,
		Unit:        domain.Unit(m.Unit),
		Actor:       m.Actor,
	})
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateWindow) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&CreateWindowResponse{Window: toWindowProto(out)}), nil
}

func (h *Handler) GetWindow(ctx context.Context, req *connect.Request[GetWindowRequest]) (*connect.Response[GetWindowResponse], error) {
	out, err := h.svc.GetWindow(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&GetWindowResponse{Window: toWindowProto(out)}), nil
}

func (h *Handler) ListWindows(ctx context.Context, req *connect.Request[ListWindowsRequest]) (*connect.Response[ListWindowsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListWindows(ctx, m.TenantID, domain.WindowStatus(m.Status), int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*WindowProto, 0, len(list))
	for _, w := range list {
		out = append(out, toWindowProto(w))
	}
	return connect.NewResponse(&ListWindowsResponse{Windows: out}), nil
}

func (h *Handler) AddFlow(ctx context.Context, req *connect.Request[AddFlowRequest]) (*connect.Response[AddFlowResponse], error) {
	m := req.Msg

	out, err := h.svc.AddFlow(ctx, service.AddFlowInput{
		TenantID:            m.TenantID,
		WindowID:            m.WindowID,
		FlowID:              m.FlowID,
		From:                domain.BalanceNode{ID: m.FromNode, Kind: domain.NodeKind(m.FromNodeKind)},
		To:                  domain.BalanceNode{ID: m.ToNode, Kind: domain.NodeKind(m.ToNodeKind)},
		Measured:            m.Measured,
		StandardUncertainty: m.StandardUncertainty,
		Unmeasured:          m.Unmeasured,
		ObservationRef:      m.ObservationRef,
		Actor:               m.Actor,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrDuplicateFlow):
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		case errors.Is(err, repository.ErrNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		case errors.Is(err, service.ErrWindowClosed):
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&AddFlowResponse{Flow: toFlowProto(out)}), nil
}

func (h *Handler) ListFlows(ctx context.Context, req *connect.Request[ListFlowsRequest]) (*connect.Response[ListFlowsResponse], error) {
	list, err := h.svc.ListFlows(ctx, req.Msg.TenantID, req.Msg.WindowID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*FlowProto, 0, len(list))
	for i := range list {
		out = append(out, toFlowProto(&list[i]))
	}
	return connect.NewResponse(&ListFlowsResponse{Flows: out}), nil
}

func (h *Handler) Reconcile(ctx context.Context, req *connect.Request[ReconcileRequest]) (*connect.Response[ReconcileResponse], error) {
	m := req.Msg

	run, err := h.svc.Reconcile(ctx, service.ReconcileInput{
		TenantID:            m.TenantID,
		WindowID:            m.WindowID,
		GrossErrorThreshold: m.GrossErrorThreshold,
		RequestID:           req.Header().Get("X-Request-ID"),
		Actor:               m.Actor,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		case errors.Is(err, service.ErrWindowClosed):
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&ReconcileResponse{Run: toRunProto(run)}), nil
}

func (h *Handler) GetRun(ctx context.Context, req *connect.Request[GetRunRequest]) (*connect.Response[GetRunResponse], error) {
	run, err := h.svc.GetRun(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&GetRunResponse{Run: toRunProto(run)}), nil
}

func (h *Handler) ListRuns(ctx context.Context, req *connect.Request[ListRunsRequest]) (*connect.Response[ListRunsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListRuns(ctx, m.TenantID, m.WindowID, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*RunProto, 0, len(list))
	for _, run := range list {
		out = append(out, toRunProto(run))
	}
	return connect.NewResponse(&ListRunsResponse{Runs: out}), nil
}

func (h *Handler) AcceptRun(ctx context.Context, req *connect.Request[AcceptRunRequest]) (*connect.Response[AcceptRunResponse], error) {
	m := req.Msg
	run, err := h.svc.AcceptRun(ctx, m.TenantID, m.RunID, m.Actor)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		case errors.Is(err, repository.ErrAlreadyAccepted):
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		case errors.Is(err, service.ErrRunDidNotConverge):
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&AcceptRunResponse{Run: toRunProto(run)}), nil
}

func toWindowProto(w *domain.BalanceWindow) *WindowProto {
	if w == nil {
		return nil
	}
	return &WindowProto{
		ID:          w.ID,
		TenantID:    w.TenantID,
		RouteRef:    w.RouteRef,
		PeriodStart: w.PeriodStart.Format(time.RFC3339),
		PeriodEnd:   w.PeriodEnd.Format(time.RFC3339),
		Unit:        string(w.Unit),
		Status:      string(w.Status),
		CreatedAt:   w.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   w.UpdatedAt.Format(time.RFC3339),
	}
}

func toFlowProto(f *domain.FlowMeasurement) *FlowProto {
	if f == nil {
		return nil
	}
	return &FlowProto{
		ID:                  f.ID,
		TenantID:            f.TenantID,
		WindowID:            f.WindowID,
		FlowID:              f.FlowID,
		FromNode:            f.From.ID,
		FromNodeKind:        string(f.From.Kind),
		ToNode:              f.To.ID,
		ToNodeKind:          string(f.To.Kind),
		Measured:            f.Measured,
		StandardUncertainty: f.StandardUncertainty,
		Unmeasured:          f.Unmeasured,
		ObservationRef:      f.ObservationRef,
		CreatedAt:           f.CreatedAt.Format(time.RFC3339),
	}
}

func toRunProto(run *domain.ReconciliationRun) *RunProto {
	if run == nil {
		return nil
	}
	flows := make([]ReconciledProto, 0, len(run.Flows))
	var suspects []string
	for _, f := range run.Flows {
		flows = append(flows, ReconciledProto{
			FlowID:        f.FlowID,
			Measured:      f.Measured,
			Reconciled:    f.Reconciled,
			Adjustment:    f.Adjustment,
			TestStatistic: f.TestStatistic,
			GrossError:    f.GrossError,
			Unmeasured:    f.Unmeasured,
		})
		if f.GrossError {
			suspects = append(suspects, f.FlowID)
		}
	}

	p := &RunProto{
		ID:                  run.ID,
		TenantID:            run.TenantID,
		WindowID:            run.WindowID,
		Converged:           run.Converged,
		ResidualBefore:      run.ResidualBefore,
		ModelVersion:        run.ModelVersion,
		GrossErrorThreshold: run.GrossErrorThreshold,
		Reason:              run.Reason,
		Flows:               flows,
		SuspectFlowIDs:      suspects,
		AcceptedBy:          run.AcceptedBy,
		CreatedAt:           run.CreatedAt.Format(time.RFC3339),
	}
	if run.ResidualAfter != nil {
		p.ResidualAfter = *run.ResidualAfter
	}
	if run.AcceptedAt != nil {
		p.AcceptedAt = run.AcceptedAt.Format(time.RFC3339)
	}
	return p
}

// parseTime accepts an empty value: the service names the field that is missing
// rather than the handler rejecting it as unparseable.
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

type ObservabilityRequest struct {
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
}

// ObservabilityResponse is what this window's instruments can and cannot
// establish, before any milk is compared.
type ObservabilityResponse struct {
	// Observable are unmeasured legs the node balances determine uniquely.
	Observable []string `json:"observable"`
	// Unobservable are unmeasured legs they do not. The reconciler will still
	// print a figure for these; it is one of infinitely many that fit.
	Unobservable []string `json:"unobservable"`

	// Redundant are measured legs that can be computed from the others, so a
	// gross error on them is detectable.
	Redundant []string `json:"redundant"`
	// JustDetermined are measured legs that cannot be. Nothing in this window
	// disagrees with them however wrong they are, which makes "the window
	// reconciled" a much weaker statement than it sounds.
	JustDetermined []string `json:"just_determined"`

	FullyObservable bool `json:"fully_observable"`
	FullyRedundant  bool `json:"fully_redundant"`
}

// Observability reports what the layout of instruments in a window can
// establish.
//
// Asked before a route runs rather than after, which is the useful time to find
// out that the only leg anybody can verify is the tanker.
func (h *Handler) Observability(ctx context.Context, req *connect.Request[ObservabilityRequest]) (*connect.Response[ObservabilityResponse], error) {
	o, err := h.svc.Observability(ctx, req.Msg.TenantID, req.Msg.WindowID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		case errors.Is(err, domain.ErrNoFlowsToClassify),
			errors.Is(err, domain.ErrNoInteriorNodes):
			// A window with nothing in it, or one where every leg crosses the
			// boundary. Neither is a fault; both mean the question cannot be
			// answered yet, which is a different thing from the answer being
			// that everything is fine.
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &ObservabilityResponse{
		Observable: o.Observable, Unobservable: o.Unobservable,
		Redundant: o.Redundant, JustDetermined: o.JustDetermined,
		FullyObservable: o.FullyObservable(), FullyRedundant: o.FullyRedundant(),
	}
	// Never null on the wire: a client that renders a missing list as "nothing
	// to worry about" and an empty list as "nothing to worry about" is right
	// only once.
	for _, p := range []*[]string{&out.Observable, &out.Unobservable, &out.Redundant, &out.JustDetermined} {
		if *p == nil {
			*p = []string{}
		}
	}
	return connect.NewResponse(out), nil
}
