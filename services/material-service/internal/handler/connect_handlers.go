package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"

	"github.com/ppusapati/gavya/services/material-service/internal/domain"
	"github.com/ppusapati/gavya/services/material-service/internal/repository"
	"github.com/ppusapati/gavya/services/material-service/internal/service"
)

const ServiceName = "material.v1.MaterialService"

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}
	route("RegisterNode", connectjson.Unary(h.RegisterNode))
	route("GetNode", connectjson.Unary(h.GetNode))
	route("ListNodes", connectjson.Unary(h.ListNodes))

	route("Dispatch", connectjson.Unary(h.Dispatch))
	route("Receive", connectjson.Unary(h.Receive))
	route("AbandonMovement", connectjson.Unary(h.AbandonMovement))
	route("GetMovement", connectjson.Unary(h.GetMovement))
	route("ListMovements", connectjson.Unary(h.ListMovements))

	route("RegisterInstrument", connectjson.Unary(h.RegisterInstrument))
	route("ListInstruments", connectjson.Unary(h.ListInstruments))
	route("ProposeFlows", connectjson.Unary(h.ProposeFlows))
}

// QuantityProto is an amount with its unit.
//
// The unit is not optional and there is no default. Litres is what most of this
// platform's milk is measured in, which makes it the assumption that would be
// wrong least often and hardest to find: a weighbridge reading taken as litres
// understates a consignment by about three per cent, and three per cent of a
// tanker looks like a plausible transit loss.
type QuantityProto struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

func (q QuantityProto) quantity() (quantity.Quantity, error) {
	return quantity.Parse(q.Value, quantity.Unit(q.Unit))
}

func fromQuantity(q *quantity.Quantity) *QuantityProto {
	if q == nil {
		return nil
	}
	return &QuantityProto{Value: q.String(), Unit: string(q.Unit)}
}

type DensityProto struct {
	// KgPerLitre is the conversion factor as a decimal literal.
	KgPerLitre string `json:"kg_per_litre"`
	Scale      int32  `json:"scale"`
	// AtCelsius is in tenths of a degree, so 4.0 degrees is 40. An integer
	// because a temperature with a decimal point in a JSON number is the same
	// float problem every quantity here avoids.
	AtCelsius int32 `json:"at_celsius"`
	// Source is LACTOMETER, ANALYSED or DECLARED.
	Source string `json:"source"`
}

func (d *DensityProto) density() (*quantity.Density, error) {
	if d == nil {
		return nil, nil
	}
	r, err := money.ParseRate(d.KgPerLitre, d.Scale)
	if err != nil {
		return nil, errors.New("kg_per_litre: " + err.Error())
	}
	out := quantity.Density{
		KgPerLitre: r, AtCelsius: d.AtCelsius,
		Source: quantity.DensitySource(d.Source),
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return &out, nil
}

func fromDensity(d *quantity.Density) *DensityProto {
	if d == nil {
		return nil
	}
	return &DensityProto{
		KgPerLitre: d.KgPerLitre.String(), Scale: d.KgPerLitre.Scale,
		AtCelsius: d.AtCelsius, Source: string(d.Source),
	}
}

// ---------------------------------------------------------------------------
// Nodes
// ---------------------------------------------------------------------------

type NodeProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`

	Capacity *QuantityProto `json:"capacity,omitempty"`
	Active   bool           `json:"active"`
}

type RegisterNodeRequest struct {
	TenantID string         `json:"tenant_id"`
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Capacity *QuantityProto `json:"capacity,omitempty"`
	Actor    string         `json:"actor"`
}

type NodeResponse struct {
	Node *NodeProto `json:"node"`
}

func (h *Handler) RegisterNode(ctx context.Context, req *connect.Request[RegisterNodeRequest]) (*connect.Response[NodeResponse], error) {
	m := req.Msg
	n := &domain.Node{
		TenantID: m.TenantID, Code: m.Code, Name: m.Name,
		Kind: domain.NodeKind(m.Kind), CreatedBy: m.Actor,
	}
	if m.Capacity != nil {
		q, err := m.Capacity.quantity()
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("capacity: "+err.Error()))
		}
		n.Capacity = &q
	}
	saved, err := h.svc.RegisterNode(ctx, n)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NodeResponse{Node: fromNode(saved)}), nil
}

type GetNodeRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

func (h *Handler) GetNode(ctx context.Context, req *connect.Request[GetNodeRequest]) (*connect.Response[NodeResponse], error) {
	n, err := h.svc.GetNode(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NodeResponse{Node: fromNode(n)}), nil
}

type ListNodesRequest struct {
	TenantID string `json:"tenant_id"`
	Kind     string `json:"kind,omitempty"`
}

type ListNodesResponse struct {
	Nodes []*NodeProto `json:"nodes"`
}

func (h *Handler) ListNodes(ctx context.Context, req *connect.Request[ListNodesRequest]) (*connect.Response[ListNodesResponse], error) {
	list, err := h.svc.ListNodes(ctx, req.Msg.TenantID, domain.NodeKind(req.Msg.Kind))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*NodeProto, 0, len(list))
	for _, n := range list {
		out = append(out, fromNode(n))
	}
	return connect.NewResponse(&ListNodesResponse{Nodes: out}), nil
}

// ---------------------------------------------------------------------------
// Movements
// ---------------------------------------------------------------------------

type MovementProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`

	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`

	DispatchedAt   string        `json:"dispatched_at"`
	Dispatched     QuantityProto `json:"dispatched"`
	DispatchMethod string        `json:"dispatch_method"`
	DispatchedBy   string        `json:"dispatched_by"`

	ReceivedAt    string         `json:"received_at,omitempty"`
	Received      *QuantityProto `json:"received,omitempty"`
	ReceiptMethod string         `json:"receipt_method,omitempty"`
	ReceivedBy    string         `json:"received_by,omitempty"`

	// Holdup is what stayed in the sending vessel. Reported beside the variance
	// rather than folded into it: the holdup is in a vessel somebody can look
	// inside, and the variance is not anywhere.
	Holdup  *QuantityProto `json:"holdup,omitempty"`
	Density *DensityProto  `json:"density,omitempty"`

	Variance                  *QuantityProto `json:"variance,omitempty"`
	VarianceUnavailableReason string         `json:"variance_unavailable_reason,omitempty"`

	Status          string `json:"status"`
	AbandonedReason string `json:"abandoned_reason,omitempty"`
}

type DispatchRequest struct {
	TenantID   string `json:"tenant_id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`

	At       string        `json:"at"`
	Quantity QuantityProto `json:"quantity"`
	Method   string        `json:"method"`

	Holdup *QuantityProto `json:"holdup,omitempty"`
	Actor  string         `json:"actor"`
}

type MovementResponse struct {
	Movement *MovementProto `json:"movement"`
}

func (h *Handler) Dispatch(ctx context.Context, req *connect.Request[DispatchRequest]) (*connect.Response[MovementResponse], error) {
	m := req.Msg
	at, err := parseTime(m.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	q, err := m.Quantity.quantity()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("quantity: "+err.Error()))
	}
	in := domain.Dispatch{
		TenantID: m.TenantID, FromNodeID: m.FromNodeID, ToNodeID: m.ToNodeID,
		At: at, Quantity: q, Method: domain.Method(m.Method), Actor: m.Actor,
	}
	if m.Holdup != nil {
		hq, err := m.Holdup.quantity()
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("holdup: "+err.Error()))
		}
		in.Holdup = &hq
	}

	saved, err := h.svc.Dispatch(ctx, in)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&MovementResponse{Movement: fromMovement(saved)}), nil
}

type ReceiveRequest struct {
	TenantID   string `json:"tenant_id"`
	MovementID string `json:"movement_id"`

	At       string        `json:"at"`
	Quantity QuantityProto `json:"quantity"`
	Method   string        `json:"method"`

	// Density is needed only when the two ends were measured in different
	// units. Supplied per consignment rather than looked up: it was read off
	// this milk, at this temperature, at this dock.
	Density *DensityProto `json:"density,omitempty"`
	// Rounding is required with a density, because converting rounds.
	Rounding string `json:"rounding,omitempty"`

	Actor string `json:"actor"`
}

func (h *Handler) Receive(ctx context.Context, req *connect.Request[ReceiveRequest]) (*connect.Response[MovementResponse], error) {
	m := req.Msg
	at, err := parseTime(m.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	q, err := m.Quantity.quantity()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("quantity: "+err.Error()))
	}
	d, err := m.Density.density()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("density: "+err.Error()))
	}

	saved, err := h.svc.Receive(ctx, domain.Receipt{
		TenantID: m.TenantID, MovementID: m.MovementID,
		At: at, Quantity: q, Method: domain.Method(m.Method),
		Density: d, Rounding: money.RoundingMode(m.Rounding), Actor: m.Actor,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&MovementResponse{Movement: fromMovement(saved)}), nil
}

type AbandonMovementRequest struct {
	TenantID   string `json:"tenant_id"`
	MovementID string `json:"movement_id"`
	Reason     string `json:"reason"`
	Actor      string `json:"actor"`
}

func (h *Handler) AbandonMovement(ctx context.Context, req *connect.Request[AbandonMovementRequest]) (*connect.Response[MovementResponse], error) {
	m, err := h.svc.Abandon(ctx, req.Msg.TenantID, req.Msg.MovementID, req.Msg.Reason, req.Msg.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&MovementResponse{Movement: fromMovement(m)}), nil
}

type GetMovementRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

func (h *Handler) GetMovement(ctx context.Context, req *connect.Request[GetMovementRequest]) (*connect.Response[MovementResponse], error) {
	m, err := h.svc.GetMovement(ctx, req.Msg.TenantID, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&MovementResponse{Movement: fromMovement(m)}), nil
}

type ListMovementsRequest struct {
	TenantID string `json:"tenant_id"`
	// NodeID matches at either end. Asked about a tanker, somebody wants both
	// what it loaded and what it delivered.
	NodeID string `json:"node_id,omitempty"`
	From   string `json:"from"`
	To     string `json:"to,omitempty"`
	Limit  int32  `json:"limit,omitempty"`
}

type ListMovementsResponse struct {
	Movements []*MovementProto `json:"movements"`
	// Unreconciled is how many of them arrived without a variance being
	// computable. A caller totalling transit loss over a period needs to know
	// how much of the period is missing from that total, or the figure reads as
	// complete when it is not.
	Unreconciled int32 `json:"unreconciled"`
}

func (h *Handler) ListMovements(ctx context.Context, req *connect.Request[ListMovementsRequest]) (*connect.Response[ListMovementsResponse], error) {
	m := req.Msg
	from, err := parseTime(m.From, "from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var to time.Time
	if m.To != "" {
		if to, err = parseTime(m.To, "to"); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	list, err := h.svc.ListMovements(ctx, m.TenantID, m.NodeID, from, to, int(m.Limit))
	if err != nil {
		return nil, classify(err)
	}
	out := &ListMovementsResponse{Movements: make([]*MovementProto, 0, len(list))}
	for _, mv := range list {
		out.Movements = append(out.Movements, fromMovement(mv))
		if mv.Status == domain.Received && mv.Variance == nil {
			out.Unreconciled++
		}
	}
	return connect.NewResponse(out), nil
}

// ---------------------------------------------------------------------------

func fromNode(n *domain.Node) *NodeProto {
	return &NodeProto{
		ID: n.ID, TenantID: n.TenantID, Code: n.Code, Name: n.Name,
		Kind: string(n.Kind), Capacity: fromQuantity(n.Capacity), Active: n.Active,
	}
}

func fromMovement(m *domain.Movement) *MovementProto {
	p := &MovementProto{
		ID: m.ID, TenantID: m.TenantID,
		FromNodeID: m.FromNodeID, ToNodeID: m.ToNodeID,
		DispatchedAt:   m.DispatchedAt.UTC().Format(time.RFC3339),
		Dispatched:     *fromQuantity(&m.Dispatched),
		DispatchMethod: string(m.DispatchMethod), DispatchedBy: m.DispatchedBy,
		Received:      fromQuantity(m.Received),
		ReceiptMethod: string(m.ReceiptMethod), ReceivedBy: m.ReceivedBy,
		Holdup: fromQuantity(m.Holdup), Density: fromDensity(m.Density),
		Variance:                  fromQuantity(m.Variance),
		VarianceUnavailableReason: m.VarianceUnavailableReason,
		Status:                    string(m.Status), AbandonedReason: m.AbandonedReason,
	}
	if m.ReceivedAt != nil {
		p.ReceivedAt = m.ReceivedAt.UTC().Format(time.RFC3339)
	}
	return p
}

func parseTime(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New(field + " is required")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New(field + ": " + s + " is neither a date nor a timestamp")
}

// classify maps a failure onto the code that describes it.
func classify(err error) error {
	var busy *domain.ErrTankerBusy
	var mixed *domain.ErrMixedUnits
	if errors.As(err, &mixed) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateCode),
		errors.Is(err, repository.ErrTankerBusy),
		errors.As(err, &busy):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, domain.ErrAlreadyClosed),
		errors.Is(err, repository.ErrReceivedIsFinal),
		errors.Is(err, domain.ErrArrivedBefore):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrNoUncertainty), errors.Is(err, domain.ErrTwoUncertainties),
		errors.Is(err, domain.ErrNoCertificate), errors.Is(err, domain.ErrNoExpiry):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, domain.ErrNoNodes), errors.Is(err, domain.ErrSameNode),
		errors.Is(err, domain.ErrNoQuantity), errors.Is(err, domain.ErrNoMethod),
		errors.Is(err, domain.ErrNoReason),
		errors.Is(err, quantity.ErrNoUnit), errors.Is(err, quantity.ErrUnitMismatch),
		errors.Is(err, quantity.ErrNoDensity), errors.Is(err, quantity.ErrBadDensity):
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// ---------------------------------------------------------------------------
// Instruments and flows
// ---------------------------------------------------------------------------

type InstrumentProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
	Method   string `json:"method"`
	Label    string `json:"label"`

	// Exactly one of these. RelativePPM is parts per million of the reading,
	// which is how a flowmeter certificate reads; Absolute is a fixed quantity,
	// which is how a weighbridge certificate reads. An instrument specified the
	// wrong way round is wrong at one end of its range.
	RelativePPM int64          `json:"relative_ppm,omitempty"`
	Absolute    *QuantityProto `json:"absolute,omitempty"`

	CertificateRef string `json:"certificate_ref"`
	CalibratedOn   string `json:"calibrated_on"`
	ValidUntil     string `json:"valid_until"`
}

type RegisterInstrumentRequest struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
	Method   string `json:"method"`
	Label    string `json:"label"`

	RelativePPM int64          `json:"relative_ppm,omitempty"`
	Absolute    *QuantityProto `json:"absolute,omitempty"`

	CertificateRef string `json:"certificate_ref"`
	CalibratedOn   string `json:"calibrated_on"`
	ValidUntil     string `json:"valid_until"`
	Actor          string `json:"actor"`
}

type InstrumentResponse struct {
	Instrument *InstrumentProto `json:"instrument"`
}

func (h *Handler) RegisterInstrument(ctx context.Context, req *connect.Request[RegisterInstrumentRequest]) (*connect.Response[InstrumentResponse], error) {
	m := req.Msg
	calibrated, err := parseTime(m.CalibratedOn, "calibrated_on")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	until, err := parseTime(m.ValidUntil, "valid_until")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	i := &domain.Instrument{
		TenantID: m.TenantID, NodeID: m.NodeID, Method: domain.Method(m.Method),
		Label: m.Label, RelativePPM: m.RelativePPM,
		CertificateRef: m.CertificateRef, CalibratedOn: calibrated, ValidUntil: until,
	}
	if m.Absolute != nil {
		q, err := m.Absolute.quantity()
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("absolute: "+err.Error()))
		}
		i.Absolute = &q
	}
	saved, err := h.svc.RegisterInstrument(ctx, i, m.Actor)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InstrumentResponse{Instrument: fromInstrument(saved)}), nil
}

type ListInstrumentsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListInstrumentsResponse struct {
	Instruments []*InstrumentProto `json:"instruments"`
	// Expired is how many of them were out of calibration as at AsOf. A society
	// reconciling with instruments nobody has re-certified should be told, and a
	// count of zero read from an empty list is not the same as a count of zero
	// read from a list.
	Expired int32  `json:"expired"`
	AsOf    string `json:"as_of"`
}

func (h *Handler) ListInstruments(ctx context.Context, req *connect.Request[ListInstrumentsRequest]) (*connect.Response[ListInstrumentsResponse], error) {
	list, err := h.svc.ListInstruments(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	now := time.Now().UTC()
	out := &ListInstrumentsResponse{
		Instruments: make([]*InstrumentProto, 0, len(list)),
		AsOf:        now.Format("2006-01-02"),
	}
	for _, i := range list {
		out.Instruments = append(out.Instruments, fromInstrument(i))
		if !i.InCalibrationOn(now) {
			out.Expired++
		}
	}
	return connect.NewResponse(out), nil
}

type FlowProto struct {
	FlowID     string `json:"flow_id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`

	Measured    QuantityProto  `json:"measured"`
	Uncertainty *QuantityProto `json:"standard_uncertainty,omitempty"`

	Unmeasured       bool   `json:"unmeasured"`
	UnmeasuredReason string `json:"unmeasured_reason,omitempty"`

	MovementID string `json:"movement_id"`
}

type ProposeFlowsRequest struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to"`
	// Rounding applies to relative uncertainties, which are a multiplication. No
	// default: it is a small effect on one flow and it decides which of two
	// nearly-equal legs the reconciler blames when a window does not close.
	Rounding string `json:"rounding"`
}

type ProposeFlowsResponse struct {
	Flows []*FlowProto `json:"flows"`
	// Unmeasured is how many legs the reconciler will have to solve for rather
	// than weight. A window mostly made of these is one whose answer is mostly
	// inference, and the caller should know that before reading the result.
	Unmeasured int32 `json:"unmeasured"`
}

// ProposeFlows shapes a period's movements for balance-service.
func (h *Handler) ProposeFlows(ctx context.Context, req *connect.Request[ProposeFlowsRequest]) (*connect.Response[ProposeFlowsResponse], error) {
	m := req.Msg
	from, err := parseTime(m.From, "from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	to, err := parseTime(m.To, "to")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	flows, err := h.svc.ProposeFlows(ctx, m.TenantID, from, to, money.RoundingMode(m.Rounding))
	if err != nil {
		return nil, classify(err)
	}
	out := &ProposeFlowsResponse{Flows: make([]*FlowProto, 0, len(flows))}
	for _, f := range flows {
		p := &FlowProto{
			FlowID: f.FlowID, FromNodeID: f.FromNodeID, ToNodeID: f.ToNodeID,
			Measured:   *fromQuantity(&f.Measured),
			Unmeasured: f.Unmeasured, UnmeasuredReason: f.UnmeasuredReason,
			MovementID: f.MovementID,
		}
		if !f.Unmeasured {
			u := f.Uncertainty
			p.Uncertainty = fromQuantity(&u)
		} else {
			out.Unmeasured++
		}
		out.Flows = append(out.Flows, p)
	}
	return connect.NewResponse(out), nil
}

func fromInstrument(i *domain.Instrument) *InstrumentProto {
	return &InstrumentProto{
		ID: i.ID, TenantID: i.TenantID, NodeID: i.NodeID,
		Method: string(i.Method), Label: i.Label,
		RelativePPM: i.RelativePPM, Absolute: fromQuantity(i.Absolute),
		CertificateRef: i.CertificateRef,
		CalibratedOn:   i.CalibratedOn.UTC().Format("2006-01-02"),
		ValidUntil:     i.ValidUntil.UTC().Format("2006-01-02"),
	}
}
