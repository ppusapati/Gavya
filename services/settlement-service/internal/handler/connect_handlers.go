package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
	"github.com/ppusapati/gavya/services/settlement-service/internal/procurement"
	"github.com/ppusapati/gavya/services/settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/settlement-service/internal/service"
	"github.com/ppusapati/gavya/services/settlement-service/internal/statement"
)

const ServiceName = "settlement.v1.SettlementService"

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}
	route("OpenCycle", connectjson.Unary(h.OpenCycle))
	route("GetCycle", connectjson.Unary(h.GetCycle))
	route("ListCycles", connectjson.Unary(h.ListCycles))
	route("GatherCycle", connectjson.Unary(h.GatherCycle))
	route("ApproveCycle", connectjson.Unary(h.ApproveCycle))
	route("AbandonCycle", connectjson.Unary(h.AbandonCycle))

	route("OpenRecovery", connectjson.Unary(h.OpenRecovery))
	route("ListRecoveries", connectjson.Unary(h.ListRecoveries))

	route("ListPayables", connectjson.Unary(h.ListPayables))
	route("GetPayable", connectjson.Unary(h.GetPayable))
	route("MarkPaid", connectjson.Unary(h.MarkPaid))
	route("HoldPayable", connectjson.Unary(h.HoldPayable))
	route("RaiseAdjustment", connectjson.Unary(h.RaiseAdjustment))
	route("ApprovePayable", connectjson.Unary(h.ApprovePayable))

	route("GetProducerStatement", connectjson.Unary(h.GetProducerStatement))
	route("PrintProducerStatement", connectjson.Unary(h.PrintProducerStatement))
	route("PrintCycleStatements", connectjson.Unary(h.PrintCycleStatements))
}

// ---------------------------------------------------------------------------
// Cycles
// ---------------------------------------------------------------------------

type CycleProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	SocietyCode string `json:"society_code"`
	Name        string `json:"name"`

	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`

	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`

	DeductionPolicy string `json:"deduction_policy"`
	Status          string `json:"status"`

	GatheredAt string `json:"gathered_at,omitempty"`
	ApprovedAt string `json:"approved_at,omitempty"`
	ApprovedBy string `json:"approved_by,omitempty"`
	PaidAt     string `json:"paid_at,omitempty"`
}

type OpenCycleRequest struct {
	TenantID    string `json:"tenant_id"`
	SocietyCode string `json:"society_code"`
	Name        string `json:"name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	// DeductionPolicy has no default here either. A request that omits it is
	// refused rather than filled in, because the answer changes what several
	// hundred people take home and neither choice is obviously right.
	DeductionPolicy string `json:"deduction_policy"`
	Actor           string `json:"actor"`
}

type CycleResponse struct {
	Cycle *CycleProto `json:"cycle"`
}

func (h *Handler) OpenCycle(ctx context.Context, req *connect.Request[OpenCycleRequest]) (*connect.Response[CycleResponse], error) {
	m := req.Msg
	start, err := parseDate(m.PeriodStart, "period_start")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	end, err := parseDate(m.PeriodEnd, "period_end")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	c, err := h.svc.OpenCycle(ctx, &domain.Cycle{
		TenantID: m.TenantID, SocietyCode: m.SocietyCode, Name: m.Name,
		PeriodStart: start, PeriodEnd: end,
		Currency: m.Currency, AmountScale: m.AmountScale,
		Policy: domain.DeductionPolicy(m.DeductionPolicy), CreatedBy: m.Actor,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CycleResponse{Cycle: fromCycle(c)}), nil
}

type GetCycleRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

func (h *Handler) GetCycle(ctx context.Context, req *connect.Request[GetCycleRequest]) (*connect.Response[CycleResponse], error) {
	c, err := h.svc.GetCycle(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CycleResponse{Cycle: fromCycle(c)}), nil
}

type ListCyclesRequest struct {
	TenantID    string `json:"tenant_id"`
	SocietyCode string `json:"society_code,omitempty"`
	Limit       int32  `json:"limit,omitempty"`
	Offset      int32  `json:"offset,omitempty"`
}

type ListCyclesResponse struct {
	Cycles []*CycleProto `json:"cycles"`
}

func (h *Handler) ListCycles(ctx context.Context, req *connect.Request[ListCyclesRequest]) (*connect.Response[ListCyclesResponse], error) {
	list, err := h.svc.ListCycles(ctx, req.Msg.TenantID, req.Msg.SocietyCode,
		int(req.Msg.Limit), int(req.Msg.Offset))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*CycleProto, 0, len(list))
	for _, c := range list {
		out = append(out, fromCycle(c))
	}
	return connect.NewResponse(&ListCyclesResponse{Cycles: out}), nil
}

type CycleActionRequest struct {
	TenantID string `json:"tenant_id"`
	CycleID  string `json:"cycle_id"`
	Actor    string `json:"actor"`
}

func (h *Handler) GatherCycle(ctx context.Context, req *connect.Request[CycleActionRequest]) (*connect.Response[CycleResponse], error) {
	c, err := h.svc.Gather(ctx, req.Msg.TenantID, req.Msg.CycleID, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CycleResponse{Cycle: fromCycle(c)}), nil
}

func (h *Handler) ApproveCycle(ctx context.Context, req *connect.Request[CycleActionRequest]) (*connect.Response[CycleResponse], error) {
	c, err := h.svc.ApproveCycle(ctx, req.Msg.TenantID, req.Msg.CycleID, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CycleResponse{Cycle: fromCycle(c)}), nil
}

type AbandonCycleResponse struct {
	Abandoned bool `json:"abandoned"`
}

func (h *Handler) AbandonCycle(ctx context.Context, req *connect.Request[CycleActionRequest]) (*connect.Response[AbandonCycleResponse], error) {
	if err := h.svc.AbandonCycle(ctx, req.Msg.TenantID, req.Msg.CycleID, req.Msg.Actor); err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&AbandonCycleResponse{Abandoned: true}), nil
}

// ---------------------------------------------------------------------------
// Recoveries
// ---------------------------------------------------------------------------

type RecoveryProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref"`

	Kind      string `json:"kind"`
	Reference string `json:"reference,omitempty"`

	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`

	Principal   string `json:"principal"`
	Recovered   string `json:"recovered"`
	Outstanding string `json:"outstanding"`
	Instalment  string `json:"instalment"`

	PrincipalMinorUnits   int64 `json:"principal_minor_units"`
	RecoveredMinorUnits   int64 `json:"recovered_minor_units"`
	OutstandingMinorUnits int64 `json:"outstanding_minor_units"`

	Priority int32  `json:"priority"`
	Status   string `json:"status"`
	OpenedOn string `json:"opened_on"`
}

type OpenRecoveryRequest struct {
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref"`
	Kind        string `json:"kind"`
	Reference   string `json:"reference,omitempty"`

	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	Principal   string `json:"principal"`
	// Instalment omitted means there is no instalment: the whole balance is
	// recovered as soon as there is milk to recover it from.
	Instalment string `json:"instalment,omitempty"`
	// AlreadyRecovered carries a debt that was partly repaid before it reached
	// this platform. A society migrating an advance ledger has these, and
	// starting them at zero would recover the same money a second time.
	AlreadyRecovered string `json:"already_recovered,omitempty"`

	Priority int32  `json:"priority"`
	OpenedOn string `json:"opened_on"`
	Actor    string `json:"actor"`
}

type RecoveryResponse struct {
	Recovery *RecoveryProto `json:"recovery"`
}

func (h *Handler) OpenRecovery(ctx context.Context, req *connect.Request[OpenRecoveryRequest]) (*connect.Response[RecoveryResponse], error) {
	m := req.Msg
	opened, err := parseDate(m.OpenedOn, "opened_on")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	principal, err := money.Parse(m.Principal, m.AmountScale, m.Currency)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("principal: "+err.Error()))
	}
	rec := &domain.Recovery{
		TenantID: m.TenantID, ProducerRef: m.ProducerRef,
		Kind: domain.RecoveryKind(m.Kind), Reference: m.Reference,
		Principal: principal, Priority: m.Priority, OpenedOn: opened,
		CreatedBy: m.Actor,
	}
	for _, f := range []struct {
		in  string
		out *money.Money
		who string
	}{
		{m.Instalment, &rec.Instalment, "instalment"},
		{m.AlreadyRecovered, &rec.Recovered, "already_recovered"},
	} {
		if f.in == "" {
			*f.out = money.Zero(m.AmountScale, m.Currency)
			continue
		}
		v, err := money.Parse(f.in, m.AmountScale, m.Currency)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(f.who+": "+err.Error()))
		}
		*f.out = v
	}

	saved, err := h.svc.OpenRecovery(ctx, rec)
	if err != nil {
		return nil, classify(err)
	}
	proto, err := fromRecovery(saved)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&RecoveryResponse{Recovery: proto}), nil
}

type ListRecoveriesRequest struct {
	TenantID        string `json:"tenant_id"`
	ProducerRef     string `json:"producer_ref,omitempty"`
	OutstandingOnly bool   `json:"outstanding_only,omitempty"`
}

type ListRecoveriesResponse struct {
	Recoveries []*RecoveryProto `json:"recoveries"`
}

func (h *Handler) ListRecoveries(ctx context.Context, req *connect.Request[ListRecoveriesRequest]) (*connect.Response[ListRecoveriesResponse], error) {
	list, err := h.svc.ListRecoveries(ctx, req.Msg.TenantID, req.Msg.ProducerRef, req.Msg.OutstandingOnly)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*RecoveryProto, 0, len(list))
	for _, r := range list {
		proto, err := fromRecovery(r)
		if err != nil {
			return nil, classify(err)
		}
		out = append(out, proto)
	}
	return connect.NewResponse(&ListRecoveriesResponse{Recoveries: out}), nil
}

// ---------------------------------------------------------------------------
// Payables
// ---------------------------------------------------------------------------

type PayableProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	CycleID     string `json:"cycle_id"`
	ProducerRef string `json:"producer_ref"`

	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`

	Gross          string `json:"gross"`
	Deducted       string `json:"deducted"`
	Net            string `json:"net"`
	CarriedForward string `json:"carried_forward"`

	GrossMinorUnits    int64 `json:"gross_minor_units"`
	DeductedMinorUnits int64 `json:"deducted_minor_units"`
	NetMinorUnits      int64 `json:"net_minor_units"`

	Kind             string `json:"kind"`
	AdjustsPayableID string `json:"adjusts_payable_id,omitempty"`
	Reason           string `json:"reason,omitempty"`

	Status           string `json:"status"`
	ApprovedAt       string `json:"approved_at,omitempty"`
	ApprovedBy       string `json:"approved_by,omitempty"`
	PaidAt           string `json:"paid_at,omitempty"`
	PaidBy           string `json:"paid_by,omitempty"`
	PaymentReference string `json:"payment_reference,omitempty"`
	HeldReason       string `json:"held_reason,omitempty"`
}

type ListPayablesRequest struct {
	TenantID string `json:"tenant_id"`
	CycleID  string `json:"cycle_id"`
}

type ListPayablesResponse struct {
	Payables []*PayableProto `json:"payables"`
	// TotalNet is what the cycle will actually pay out.
	TotalNet           string `json:"total_net,omitempty"`
	TotalNetMinorUnits int64  `json:"total_net_minor_units"`
	TotalGross         string `json:"total_gross,omitempty"`
	Currency           string `json:"currency,omitempty"`
}

func (h *Handler) ListPayables(ctx context.Context, req *connect.Request[ListPayablesRequest]) (*connect.Response[ListPayablesResponse], error) {
	list, err := h.svc.ListPayables(ctx, req.Msg.TenantID, req.Msg.CycleID)
	if err != nil {
		return nil, classify(err)
	}
	out := &ListPayablesResponse{Payables: make([]*PayableProto, 0, len(list))}
	var net, gross money.Money
	for i, p := range list {
		out.Payables = append(out.Payables, fromPayable(p))
		if i == 0 {
			net = money.Zero(p.Net.Scale, p.Net.Currency)
			gross = net
		}
		if net, err = money.Add(net, p.Net); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		if gross, err = money.Add(gross, p.Gross); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
	}
	if len(list) > 0 {
		out.TotalNet, out.TotalNetMinorUnits = net.String(), net.Value
		out.TotalGross, out.Currency = gross.String(), net.Currency
	}
	return connect.NewResponse(out), nil
}

type GetPayableRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type PayableResponse struct {
	Payable *PayableProto `json:"payable"`
}

func (h *Handler) GetPayable(ctx context.Context, req *connect.Request[GetPayableRequest]) (*connect.Response[PayableResponse], error) {
	p, err := h.svc.GetPayable(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PayableResponse{Payable: fromPayable(p)}), nil
}

type MarkPaidRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	// PaymentReference is the cheque number or the bank's transaction id. Not
	// required, because cash across a counter has none.
	PaymentReference string `json:"payment_reference,omitempty"`
	Actor            string `json:"actor"`
}

func (h *Handler) MarkPaid(ctx context.Context, req *connect.Request[MarkPaidRequest]) (*connect.Response[PayableResponse], error) {
	p, err := h.svc.MarkPaid(ctx, req.Msg.TenantID, req.Msg.ID, req.Msg.PaymentReference, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PayableResponse{Payable: fromPayable(p)}), nil
}

type HoldPayableRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Reason   string `json:"reason"`
	Actor    string `json:"actor"`
}

func (h *Handler) HoldPayable(ctx context.Context, req *connect.Request[HoldPayableRequest]) (*connect.Response[PayableResponse], error) {
	p, err := h.svc.HoldPayable(ctx, req.Msg.TenantID, req.Msg.ID, req.Msg.Reason, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PayableResponse{Payable: fromPayable(p)}), nil
}

// ---------------------------------------------------------------------------
// Statement
// ---------------------------------------------------------------------------

type StatementLineProto struct {
	CollectedOn  string `json:"collected_on"`
	Shift        string `json:"shift"`
	Quantity     string `json:"quantity"`
	QuantityUnit string `json:"quantity_unit"`
	Rate         string `json:"rate,omitempty"`
	Amount       string `json:"amount"`
	CollectionID string `json:"collection_id"`
}

type StatementDeductionProto struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference,omitempty"`
	Amount    string `json:"amount"`
}

type GetProducerStatementRequest struct {
	TenantID    string `json:"tenant_id"`
	CycleID     string `json:"cycle_id"`
	ProducerRef string `json:"producer_ref"`
}

// StatementProto is the document a producer is handed.
//
// Every figure on it is a string in the form it should be read in, because the
// one thing this page must not do is arrive at a client that renders 3770.5 as
// 3770.5 and a society that wrote 3770.50.
type StatementProto struct {
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref"`

	SocietyCode string `json:"society_code"`
	CycleID     string `json:"cycle_id"`
	CycleName   string `json:"cycle_name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	CycleStatus string `json:"cycle_status"`

	Lines      []StatementLineProto      `json:"lines"`
	Deductions []StatementDeductionProto `json:"deductions"`

	// Quantities is kept per unit rather than summed across them: litres and
	// kilograms differ by about three per cent and adding them would hide a
	// society recording both.
	Quantities map[string]string `json:"quantities"`

	Currency       string `json:"currency"`
	Gross          string `json:"gross"`
	Deducted       string `json:"deducted"`
	Net            string `json:"net"`
	CarriedForward string `json:"carried_forward,omitempty"`

	Status           string `json:"status,omitempty"`
	PaidAt           string `json:"paid_at,omitempty"`
	PaymentReference string `json:"payment_reference,omitempty"`
	HeldReason       string `json:"held_reason,omitempty"`

	// Adjustments raised against this cycle after it was settled, kept separate
	// from the fortnight's own figures. A member needs to see that the period
	// came to one number and that a further amount moved afterwards, with the
	// reason; folding them together would show a net nobody was handed.
	Adjustments []StatementAdjustmentProto `json:"adjustments,omitempty"`
}

// StatementAdjustmentProto is one correction to a settled fortnight.
type StatementAdjustmentProto struct {
	ID     string `json:"id"`
	Amount string `json:"amount"`
	Reason string `json:"reason"`
	Status string `json:"status"`
	PaidAt string `json:"paid_at,omitempty"`
}

type GetProducerStatementResponse struct {
	Statement *StatementProto `json:"statement"`
}

func (h *Handler) GetProducerStatement(ctx context.Context, req *connect.Request[GetProducerStatementRequest]) (*connect.Response[GetProducerStatementResponse], error) {
	m := req.Msg
	s, err := h.svc.Statement(ctx, m.TenantID, m.CycleID, m.ProducerRef)
	if err != nil {
		return nil, classify(err)
	}

	out := &StatementProto{
		TenantID: s.TenantID, ProducerRef: s.ProducerRef,
		SocietyCode: s.Cycle.SocietyCode, CycleID: s.Cycle.ID, CycleName: s.Cycle.Name,
		PeriodStart: s.Cycle.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:   s.Cycle.PeriodEnd.UTC().Format("2006-01-02"),
		CycleStatus: string(s.Cycle.Status),
		Currency:    s.Cycle.Currency,
		Quantities:  s.Quantities,
		Lines:       make([]StatementLineProto, 0, len(s.Lines)),
		Deductions:  make([]StatementDeductionProto, 0, len(s.Deductions)),
	}
	for _, l := range s.Lines {
		out.Lines = append(out.Lines, StatementLineProto{
			CollectedOn: l.CollectedOn.UTC().Format("2006-01-02"), Shift: l.Shift,
			Quantity: l.Quantity, QuantityUnit: l.QuantityUnit, Rate: l.Rate,
			Amount: l.Amount.String(), CollectionID: l.CollectionID,
		})
	}
	for _, d := range s.Deductions {
		out.Deductions = append(out.Deductions, StatementDeductionProto{
			Kind: string(d.Kind), Reference: d.Reference, Amount: d.Amount.String(),
		})
	}

	if p := s.Payable; p != nil {
		out.Gross, out.Deducted, out.Net = p.Gross.String(), p.Deducted.String(), p.Net.String()
		out.Status = string(p.Status)
		out.HeldReason, out.PaymentReference = p.HeldReason, p.PaymentReference
		if !p.CarriedForward.IsZero() {
			out.CarriedForward = p.CarriedForward.String()
		}
		if p.PaidAt != nil {
			out.PaidAt = p.PaidAt.UTC().Format(time.RFC3339)
		}
	}
	for _, a := range s.Adjustments {
		row := StatementAdjustmentProto{
			ID: a.ID, Amount: a.Net.String(), Reason: a.Reason, Status: string(a.Status),
		}
		if a.PaidAt != nil {
			row.PaidAt = a.PaidAt.UTC().Format(time.RFC3339)
		}
		out.Adjustments = append(out.Adjustments, row)
	}
	return connect.NewResponse(&GetProducerStatementResponse{Statement: out}), nil
}

// ---------------------------------------------------------------------------

func fromCycle(c *domain.Cycle) *CycleProto {
	p := &CycleProto{
		ID: c.ID, TenantID: c.TenantID, SocietyCode: c.SocietyCode, Name: c.Name,
		PeriodStart: c.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:   c.PeriodEnd.UTC().Format("2006-01-02"),
		Currency:    c.Currency, AmountScale: c.AmountScale,
		DeductionPolicy: string(c.Policy), Status: string(c.Status),
		ApprovedBy: c.ApprovedBy,
	}
	for _, f := range []struct {
		in  *time.Time
		out *string
	}{{c.GatheredAt, &p.GatheredAt}, {c.ApprovedAt, &p.ApprovedAt}, {c.PaidAt, &p.PaidAt}} {
		if f.in != nil {
			*f.out = f.in.UTC().Format(time.RFC3339)
		}
	}
	return p
}

// fromRecovery renders one recovery for the wire.
//
// It returns an error because OutstandingAmount can fail, and the error used to
// be discarded. money.Sub refuses to subtract amounts in different currencies or
// at different scales, and it refuses an overflow; on any of those the zero Money
// comes back, whose String is "0" and whose Value is 0. A loan would then be
// reported to the caller as fully repaid.
//
// It cannot happen on the path this has today: the repository builds Principal
// and Recovered from the same row's currency and scale, so the only remaining
// failure is an overflow no real figure reaches. That is an argument for the
// error never firing, not for throwing it away — the day something makes it
// reachable, "this producer owes nothing" is the worst answer this service could
// give, and it would give it silently.
func fromRecovery(r *domain.Recovery) (*RecoveryProto, error) {
	outstanding, err := r.OutstandingAmount()
	if err != nil {
		return nil, fmt.Errorf("recovery %s: what is still owed on it cannot be worked "+
			"out: %w", r.ID, err)
	}
	return &RecoveryProto{
		ID: r.ID, TenantID: r.TenantID, ProducerRef: r.ProducerRef,
		Kind: string(r.Kind), Reference: r.Reference,
		Currency: r.Principal.Currency, AmountScale: r.Principal.Scale,
		Principal: r.Principal.String(), Recovered: r.Recovered.String(),
		Outstanding: outstanding.String(), Instalment: r.Instalment.String(),
		PrincipalMinorUnits: r.Principal.Value, RecoveredMinorUnits: r.Recovered.Value,
		OutstandingMinorUnits: outstanding.Value,
		Priority:              r.Priority, Status: string(r.Status),
		OpenedOn: r.OpenedOn.UTC().Format("2006-01-02"),
	}, nil
}

func fromPayable(p *domain.ProducerPayable) *PayableProto {
	out := &PayableProto{
		ID: p.ID, TenantID: p.TenantID, CycleID: p.CycleID, ProducerRef: p.ProducerRef,
		Currency: p.Net.Currency, AmountScale: p.Net.Scale,
		Gross: p.Gross.String(), Deducted: p.Deducted.String(), Net: p.Net.String(),
		CarriedForward:     p.CarriedForward.String(),
		GrossMinorUnits:    p.Gross.Value,
		DeductedMinorUnits: p.Deducted.Value,
		NetMinorUnits:      p.Net.Value,
		Kind:               string(p.Kind),
		AdjustsPayableID:   p.AdjustsPayableID,
		Reason:             p.Reason,
		Status:             string(p.Status),
		ApprovedBy:         p.ApprovedBy, PaidBy: p.PaidBy,
		PaymentReference: p.PaymentReference, HeldReason: p.HeldReason,
	}
	if p.ApprovedAt != nil {
		out.ApprovedAt = p.ApprovedAt.UTC().Format(time.RFC3339)
	}
	if p.PaidAt != nil {
		out.PaidAt = p.PaidAt.UTC().Format(time.RFC3339)
	}
	return out
}

func parseDate(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New(field + " is required")
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New(field + ": " + s + " is neither a date nor a timestamp")
}

// classify maps a failure onto the code that describes it.
func classify(err error) error {
	var wrongStatus *domain.ErrWrongStatus
	var orphaned *procurement.ErrMilkBelongsToNoSociety
	if errors.As(err, &orphaned) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// A page too narrow for its figures is the caller's setting, not a fault
	// here. A statement that does not reconcile is neither — it means the stored
	// figures disagree with each other, which is a question for a person.
	var narrow *statement.ErrTooNarrow
	if errors.As(err, &narrow) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	var noPayable *statement.ErrNoPayable
	if errors.As(err, &noPayable) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	var wonky *statement.ErrDoesNotReconcile
	if errors.As(err, &wonky) {
		return connect.NewError(connect.CodeInternal, err)
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrOverlappingCycle),
		errors.Is(err, repository.ErrAlreadyGathered):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.As(err, &wrongStatus),
		errors.Is(err, repository.ErrPaidIsFinal),
		errors.Is(err, procurement.ErrNoCollections),
		errors.Is(err, service.ErrNoMilkSource),
		errors.Is(err, domain.ErrOverRecovered):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrNoSociety), errors.Is(err, domain.ErrNoPeriod),
		errors.Is(err, domain.ErrBackwards), errors.Is(err, domain.ErrNoPolicy),
		errors.Is(err, domain.ErrNoProducer), errors.Is(err, domain.ErrNoPrincipal),
		errors.Is(err, domain.ErrNoPriority),
		errors.Is(err, domain.ErrNoAdjustmentReason), errors.Is(err, domain.ErrZeroAdjustment),
		errors.Is(err, money.ErrCurrencyMismatch), errors.Is(err, money.ErrScaleMismatch):
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// PrintProducerStatement lays one member's settlement out for a printer.

type PrintStatementRequest struct {
	TenantID    string `json:"tenant_id"`
	CycleID     string `json:"cycle_id"`
	ProducerRef string `json:"producer_ref"`

	// Width is the printer. Omitted means the 80-column dot matrix most of
	// these societies own.
	Width int32 `json:"width,omitempty"`
	// SocietyName is what the co-operative calls itself. A member recognises
	// "Kothapalli Milk Producers" and has never seen "SOC_KOTHAPALLI".
	SocietyName string `json:"society_name,omitempty"`
	// Labels lets a society supply its own words. Any field left empty keeps
	// the English default; supplying some and not others is a half-translated
	// page, so a society that supplies any should supply all.
	Labels map[string]string `json:"labels,omitempty"`
}

type PrintStatementResponse struct {
	ProducerRef string `json:"producer_ref"`
	// Page is the statement, newline-separated, ready to send to a printer.
	Page string `json:"page"`
	// Width is what it was laid out for, echoed back so a caller that omitted
	// it knows what it got.
	Width int32 `json:"width"`
}

func (h *Handler) PrintProducerStatement(ctx context.Context, req *connect.Request[PrintStatementRequest]) (*connect.Response[PrintStatementResponse], error) {
	m := req.Msg
	o := printOptions(*m)
	page, err := h.svc.PrintStatement(ctx, m.TenantID, m.CycleID, m.ProducerRef, o)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PrintStatementResponse{
		ProducerRef: m.ProducerRef, Page: page, Width: int32(effectiveWidth(o)),
	}), nil
}

type PrintCycleRequest struct {
	TenantID    string            `json:"tenant_id"`
	CycleID     string            `json:"cycle_id"`
	Width       int32             `json:"width,omitempty"`
	SocietyName string            `json:"society_name,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type PrintCycleResponse struct {
	Statements []PrintStatementResponse `json:"statements"`
	Width      int32                    `json:"width"`
}

// PrintCycleStatements lays out every member's page in one run, which is what a
// society actually does at the end of a fortnight.
func (h *Handler) PrintCycleStatements(ctx context.Context, req *connect.Request[PrintCycleRequest]) (*connect.Response[PrintCycleResponse], error) {
	m := req.Msg
	o := printOptions(PrintStatementRequest{
		Width: m.Width, SocietyName: m.SocietyName, Labels: m.Labels,
	})
	pages, err := h.svc.PrintCycle(ctx, m.TenantID, m.CycleID, o)
	if err != nil {
		return nil, classify(err)
	}
	out := &PrintCycleResponse{
		Statements: make([]PrintStatementResponse, 0, len(pages)),
		Width:      int32(effectiveWidth(o)),
	}
	for _, p := range pages {
		out.Statements = append(out.Statements, PrintStatementResponse{
			ProducerRef: p.ProducerRef, Page: p.Page, Width: out.Width,
		})
	}
	return connect.NewResponse(out), nil
}

func effectiveWidth(o statement.Options) int {
	if o.Width == 0 {
		return statement.DefaultWidth
	}
	return o.Width
}

func printOptions(m PrintStatementRequest) statement.Options {
	o := statement.Options{Width: int(m.Width), SocietyName: m.SocietyName}
	o.Labels = statement.DefaultLabels()
	// Applied field by field so a society can override the words it cares about
	// without having to restate the twenty it does not. An unknown key is
	// ignored rather than refused: a caller sending "titel" gets the English
	// title, which is visible on the page, rather than a failed print run at
	// the end of a fortnight.
	for k, v := range m.Labels {
		if v == "" {
			continue
		}
		setLabel(&o.Labels, k, v)
	}
	return o
}

func setLabel(l *statement.Labels, key, value string) {
	switch key {
	case "title":
		l.Title = value
	case "member":
		l.Member = value
	case "society":
		l.Society = value
	case "period":
		l.Period = value
	case "cycle":
		l.Cycle = value
	case "date":
		l.Date = value
	case "shift":
		l.Shift = value
	case "quantity":
		l.Quantity = value
	case "rate":
		l.Rate = value
	case "amount":
		l.Amount = value
	case "morning":
		l.Morning = value
	case "evening":
		l.Evening = value
	case "total":
		l.Total = value
	case "gross":
		l.Gross = value
	case "less":
		l.Less = value
	case "net":
		l.Net = value
	case "carried_forward":
		l.CarriedFwd = value
	case "paid":
		l.Paid = value
	case "reference":
		l.Reference = value
	case "held":
		l.Held = value
	case "not_yet_paid":
		l.NotYetPaid = value
	case "no_milk":
		l.NoMilk = value
	}
}

type RaiseAdjustmentRequest struct {
	TenantID    string `json:"tenant_id"`
	CycleID     string `json:"cycle_id"`
	ProducerRef string `json:"producer_ref"`

	Currency    string `json:"currency"`
	AmountScale int32  `json:"amount_scale"`
	// Amount may be negative. A reading restated downwards means the producer
	// was overpaid and the money comes back, and refusing to record that would
	// leave half of what adjustments are for impossible.
	Amount string `json:"amount"`

	// AdjustsPayableID is the payment this corrects, where it corrects one.
	AdjustsPayableID string `json:"adjusts_payable_id,omitempty"`
	// Reason is required and goes on the producer's statement.
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

// RaiseAdjustment records money owed after a cycle was already paid.
func (h *Handler) RaiseAdjustment(ctx context.Context, req *connect.Request[RaiseAdjustmentRequest]) (*connect.Response[PayableResponse], error) {
	m := req.Msg
	amount, err := money.Parse(m.Amount, m.AmountScale, m.Currency)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("amount: "+err.Error()))
	}
	p, err := h.svc.RaiseAdjustment(ctx, &domain.ProducerPayable{
		TenantID: m.TenantID, CycleID: m.CycleID, ProducerRef: m.ProducerRef,
		Net: amount, AdjustsPayableID: m.AdjustsPayableID, Reason: m.Reason,
	}, m.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PayableResponse{Payable: fromPayable(p)}), nil
}

type ApprovePayableRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Actor    string `json:"actor"`
}

// ApprovePayable signs off one payable. ApproveCycle handles a whole gathered
// fortnight; this is for an adjustment raised against one that is finished.
func (h *Handler) ApprovePayable(ctx context.Context, req *connect.Request[ApprovePayableRequest]) (*connect.Response[PayableResponse], error) {
	p, err := h.svc.ApprovePayable(ctx, req.Msg.TenantID, req.Msg.ID, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PayableResponse{Payable: fromPayable(p)}), nil
}
