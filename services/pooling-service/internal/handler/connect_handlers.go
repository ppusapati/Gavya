package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/pooling-service/internal/domain"
	"github.com/ppusapati/gavya/services/pooling-service/internal/repository"
	"github.com/ppusapati/gavya/services/pooling-service/internal/service"
)

type CreatePoolRequest struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Unit        string `json:"unit,omitempty"`
	Currency    string `json:"currency"`
	// AmountScale is the number of decimal places every amount in the pool is
	// held at, so a decimal string on the wire has one unambiguous reading.
	AmountScale   int32  `json:"amount_scale"`
	RateCardID    string `json:"rate_card_id,omitempty"`
	PolicyVersion string `json:"policy_version,omitempty"`
	Actor         string `json:"actor"`
}
type CreatePoolResponse struct {
	Pool *PoolProto `json:"pool"`
}

type PoolProto struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	Name          string `json:"name"`
	PeriodStart   string `json:"period_start"`
	PeriodEnd     string `json:"period_end"`
	Unit          string `json:"unit"`
	Currency      string `json:"currency"`
	AmountScale   int32  `json:"amount_scale"`
	Status        string `json:"status"`
	RateCardID    string `json:"rate_card_id,omitempty"`
	PolicyVersion string `json:"policy_version,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type GetPoolRequest struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}
type GetPoolResponse struct {
	Pool *PoolProto `json:"pool"`
}

type ListPoolsRequest struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Limit    int32  `json:"limit,omitempty"`
	Offset   int32  `json:"offset,omitempty"`
}
type ListPoolsResponse struct {
	Pools []*PoolProto `json:"pools"`
}

type AddProducerMilkRequest struct {
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id"`
	ProducerRef string `json:"producer_ref"`
	// Quantity and Components are decimal literals at three decimals.
	Quantity   string            `json:"quantity"`
	Components map[string]string `json:"components,omitempty"`
	SlotRefs   []string          `json:"slot_refs,omitempty"`

	OriginKind        string `json:"origin_kind,omitempty"`
	SourceSystemID    string `json:"source_system_id,omitempty"`
	ImportBatchID     string `json:"import_batch_id,omitempty"`
	SourceRecordID    string `json:"source_record_id,omitempty"`
	SourcePayloadHash string `json:"source_payload_hash,omitempty"`
	DerivationID      string `json:"derivation_id,omitempty"`

	Actor string `json:"actor"`
}
type AddProducerMilkResponse struct {
	ProducerMilk *ProducerMilkProto `json:"producer_milk"`
}

type ProducerMilkProto struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	PoolID      string            `json:"pool_id"`
	ProducerRef string            `json:"producer_ref"`
	Quantity    string            `json:"quantity"`
	Components  map[string]string `json:"components,omitempty"`
	SlotRefs    []string          `json:"slot_refs,omitempty"`
	OriginKind  string            `json:"origin_kind"`
	CreatedAt   string            `json:"created_at"`
}

type ListProducerMilkRequest struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}
type ListProducerMilkResponse struct {
	ProducerMilk []*ProducerMilkProto `json:"producer_milk"`
}

type RecordUtilisationRequest struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
	Class    string `json:"class"`
	Quantity string `json:"quantity"`
	// Price is a decimal literal per unit of quantity, read at PriceScale.
	Price      string `json:"price"`
	PriceScale int32  `json:"price_scale"`
	Actor      string `json:"actor"`
}
type RecordUtilisationResponse struct {
	Utilisation *UtilisationProto `json:"utilisation"`
}

type UtilisationProto struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	PoolID     string `json:"pool_id"`
	Class      string `json:"class"`
	Quantity   string `json:"quantity"`
	Price      string `json:"price"`
	PriceScale int32  `json:"price_scale"`
	CreatedAt  string `json:"created_at"`
}

type ListUtilisationsRequest struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}
type ListUtilisationsResponse struct {
	Utilisations []*UtilisationProto `json:"utilisations"`
}

type ComponentPriceProto struct {
	Component string `json:"component"`
	Price     string `json:"price"`
	Scale     int32  `json:"scale"`
}

type ValuePoolRequest struct {
	TenantID        string                `json:"tenant_id"`
	PoolID          string                `json:"pool_id"`
	ComponentPrices []ComponentPriceProto `json:"component_prices"`
	Actor           string                `json:"actor"`
}
type ValuePoolResponse struct {
	Pool        *PoolProto        `json:"pool"`
	Valuation   *ValuationProto   `json:"valuation"`
	Allocations []AllocationProto `json:"allocations"`
}

type RoundingStepProto struct {
	Operation string `json:"operation"`
	Mode      string `json:"mode"`
	FromScale int32  `json:"from_scale"`
	ToScale   int32  `json:"to_scale"`
	Discarded int64  `json:"discarded"`
	Result    string `json:"result"`
}

type ValuationProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
	Currency string `json:"currency"`
	// AmountScale applies to every amount in this valuation and in its
	// allocations, so the decimal strings below have one reading.
	AmountScale int32 `json:"amount_scale"`

	ClassifiedValue        string `json:"classified_value"`
	ComponentValue         string `json:"component_value"`
	ProducerSettlementFund string `json:"producer_settlement_fund"`

	TotalQuantity   string `json:"total_quantity"`
	BlendPrice      string `json:"blend_price"`
	BlendPriceScale int32  `json:"blend_price_scale"`

	RoundingTrail []RoundingStepProto `json:"rounding_trail,omitempty"`
	ComputedAt    string              `json:"computed_at"`
}

type AllocationProto struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	PoolID         string `json:"pool_id"`
	ValuationID    string `json:"valuation_id"`
	ProducerRef    string `json:"producer_ref"`
	Currency       string `json:"currency"`
	AmountScale    int32  `json:"amount_scale"`
	ComponentValue string `json:"component_value"`
	FundShare      string `json:"fund_share"`
	Total          string `json:"total"`
	Weight         int64  `json:"weight"`
	CreatedAt      string `json:"created_at"`
}

type GetValuationRequest struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
}
type GetValuationResponse struct {
	Valuation *ValuationProto `json:"valuation"`
}

type ListAllocationsRequest struct {
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id"`
	ValuationID string `json:"valuation_id,omitempty"`
}
type ListAllocationsResponse struct {
	Allocations []AllocationProto `json:"allocations"`
}

type SettlePoolRequest struct {
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`
	Actor    string `json:"actor"`
}
type SettlePoolResponse struct {
	Pool   *PoolProto           `json:"pool"`
	Events []EconomicEventProto `json:"events"`
}

type EconomicEventProto struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	PoolID            string `json:"pool_id"`
	AllocationID      string `json:"allocation_id"`
	ProducerRef       string `json:"producer_ref"`
	Currency          string `json:"currency"`
	AmountScale       int32  `json:"amount_scale"`
	Amount            string `json:"amount"`
	Kind              string `json:"kind"`
	SupersedesEventID string `json:"supersedes_event_id,omitempty"`
	CreatedAt         string `json:"created_at"`
}

type ListEconomicEventsRequest struct {
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id,omitempty"`
	ProducerRef string `json:"producer_ref,omitempty"`
	Limit       int32  `json:"limit,omitempty"`
	Offset      int32  `json:"offset,omitempty"`
}
type ListEconomicEventsResponse struct {
	Events []EconomicEventProto `json:"events"`
}

type DeclareRetroactivityPolicyRequest struct {
	TenantID        string `json:"tenant_id"`
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	MaxLookbackDays int32  `json:"max_lookback_days"`

	Currency string `json:"currency"`
	// AmountScale reads MinimumAdjustment, which is a decimal literal.
	AmountScale       int32  `json:"amount_scale"`
	MinimumAdjustment string `json:"minimum_adjustment,omitempty"`

	EffectiveFrom string `json:"effective_from"`
	EffectiveTo   string `json:"effective_to,omitempty"`
	Actor         string `json:"actor"`
}
type DeclareRetroactivityPolicyResponse struct {
	Policy *RetroactivityPolicyProto `json:"policy"`
}

type RetroactivityPolicyProto struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	Name              string `json:"name"`
	Mode              string `json:"mode"`
	MaxLookbackDays   int32  `json:"max_lookback_days"`
	Currency          string `json:"currency"`
	AmountScale       int32  `json:"amount_scale"`
	MinimumAdjustment string `json:"minimum_adjustment"`
	EffectiveFrom     string `json:"effective_from"`
	EffectiveTo       string `json:"effective_to,omitempty"`
}

type GetEffectiveRetroactivityPolicyRequest struct {
	TenantID string `json:"tenant_id"`
	At       string `json:"at,omitempty"`
}
type GetEffectiveRetroactivityPolicyResponse struct {
	Policy *RetroactivityPolicyProto `json:"policy"`
}

type ApplyCorrectionRequest struct {
	TenantID        string                `json:"tenant_id"`
	PoolID          string                `json:"pool_id"`
	ComponentPrices []ComponentPriceProto `json:"component_prices"`
	At              string                `json:"at,omitempty"`
	Actor           string                `json:"actor"`
}
type ApplyCorrectionResponse struct {
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason"`
	EventKind string `json:"event_kind,omitempty"`
	// Adjustment is the change in the pool's value the correction produces,
	// reported whether or not the policy permitted it to be applied.
	Adjustment  string               `json:"adjustment"`
	Pool        *PoolProto           `json:"pool"`
	Valuation   *ValuationProto      `json:"valuation,omitempty"`
	Allocations []AllocationProto    `json:"allocations,omitempty"`
	Events      []EconomicEventProto `json:"events,omitempty"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func (h *Handler) CreatePool(ctx context.Context, req *connect.Request[CreatePoolRequest]) (*connect.Response[CreatePoolResponse], error) {
	m := req.Msg

	periodStart, err := parseTime(m.PeriodStart, "period_start")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	periodEnd, err := parseTime(m.PeriodEnd, "period_end")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.CreatePool(ctx, service.CreatePoolInput{
		TenantID:      m.TenantID,
		Name:          m.Name,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		Unit:          m.Unit,
		Currency:      m.Currency,
		Scale:         m.AmountScale,
		RateCardID:    m.RateCardID,
		PolicyVersion: m.PolicyVersion,
		Actor:         m.Actor,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&CreatePoolResponse{Pool: toPoolProto(out)}), nil
}

func (h *Handler) GetPool(ctx context.Context, req *connect.Request[GetPoolRequest]) (*connect.Response[GetPoolResponse], error) {
	out, err := h.svc.GetPool(ctx, req.Msg.TenantID, req.Msg.PoolID)
	if err != nil {
		return nil, statusError(err)
	}
	return connect.NewResponse(&GetPoolResponse{Pool: toPoolProto(out)}), nil
}

func (h *Handler) ListPools(ctx context.Context, req *connect.Request[ListPoolsRequest]) (*connect.Response[ListPoolsResponse], error) {
	m := req.Msg

	from, err := parseTime(m.From, "from")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	to, err := parseTime(m.To, "to")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	list, err := h.svc.ListPools(ctx, m.TenantID, from, to, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*PoolProto, 0, len(list))
	for _, p := range list {
		out = append(out, toPoolProto(p))
	}
	return connect.NewResponse(&ListPoolsResponse{Pools: out}), nil
}

func (h *Handler) AddProducerMilk(ctx context.Context, req *connect.Request[AddProducerMilkRequest]) (*connect.Response[AddProducerMilkResponse], error) {
	m := req.Msg

	components := make(map[domain.ComponentKind]string, len(m.Components))
	for k, v := range m.Components {
		components[domain.ComponentKind(k)] = v
	}

	var o origin.Origin
	if m.OriginKind != "" {
		kind, err := origin.ParseKind(m.OriginKind)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		o = origin.Origin{
			Kind:              kind,
			SourceSystemID:    m.SourceSystemID,
			ImportBatchID:     m.ImportBatchID,
			SourceRecordID:    m.SourceRecordID,
			SourcePayloadHash: m.SourcePayloadHash,
			DerivationID:      m.DerivationID,
		}
	}

	out, err := h.svc.AddProducerMilk(ctx, service.AddProducerMilkInput{
		TenantID:    m.TenantID,
		PoolID:      m.PoolID,
		ProducerRef: m.ProducerRef,
		Quantity:    m.Quantity,
		Components:  components,
		SlotRefs:    m.SlotRefs,
		Origin:      o,
		Actor:       m.Actor,
	})
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateProducer) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, statusError(err)
	}
	return connect.NewResponse(&AddProducerMilkResponse{ProducerMilk: toProducerMilkProto(out)}), nil
}

func (h *Handler) ListProducerMilk(ctx context.Context, req *connect.Request[ListProducerMilkRequest]) (*connect.Response[ListProducerMilkResponse], error) {
	list, err := h.svc.ListProducerMilk(ctx, req.Msg.TenantID, req.Msg.PoolID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*ProducerMilkProto, 0, len(list))
	for i := range list {
		out = append(out, toProducerMilkProto(&list[i]))
	}
	return connect.NewResponse(&ListProducerMilkResponse{ProducerMilk: out}), nil
}

func (h *Handler) RecordUtilisation(ctx context.Context, req *connect.Request[RecordUtilisationRequest]) (*connect.Response[RecordUtilisationResponse], error) {
	m := req.Msg

	price, err := money.ParseRate(m.Price, m.PriceScale)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.RecordUtilisation(ctx, service.RecordUtilisationInput{
		TenantID: m.TenantID,
		PoolID:   m.PoolID,
		Class:    domain.UtilisationClass(m.Class),
		Quantity: m.Quantity,
		Price:    price,
		Actor:    m.Actor,
	})
	if err != nil {
		return nil, statusError(err)
	}
	return connect.NewResponse(&RecordUtilisationResponse{Utilisation: toUtilisationProto(out)}), nil
}

func (h *Handler) ListUtilisations(ctx context.Context, req *connect.Request[ListUtilisationsRequest]) (*connect.Response[ListUtilisationsResponse], error) {
	list, err := h.svc.ListUtilisations(ctx, req.Msg.TenantID, req.Msg.PoolID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*UtilisationProto, 0, len(list))
	for i := range list {
		out = append(out, toUtilisationProto(&list[i]))
	}
	return connect.NewResponse(&ListUtilisationsResponse{Utilisations: out}), nil
}

func (h *Handler) ValuePool(ctx context.Context, req *connect.Request[ValuePoolRequest]) (*connect.Response[ValuePoolResponse], error) {
	m := req.Msg

	prices, err := toComponentPrices(m.ComponentPrices)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.ValuePool(ctx, service.ValuePoolInput{
		TenantID:        m.TenantID,
		PoolID:          m.PoolID,
		ComponentPrices: prices,
		Actor:           m.Actor,
	})
	if err != nil {
		return nil, statusError(err)
	}
	return connect.NewResponse(&ValuePoolResponse{
		Pool:        toPoolProto(out.Pool),
		Valuation:   toValuationProto(out.Valuation),
		Allocations: toAllocationProtos(out.Allocations),
	}), nil
}

func (h *Handler) GetValuation(ctx context.Context, req *connect.Request[GetValuationRequest]) (*connect.Response[GetValuationResponse], error) {
	out, err := h.svc.GetValuation(ctx, req.Msg.TenantID, req.Msg.PoolID)
	if err != nil {
		return nil, statusError(err)
	}
	return connect.NewResponse(&GetValuationResponse{Valuation: toValuationProto(out)}), nil
}

func (h *Handler) ListAllocations(ctx context.Context, req *connect.Request[ListAllocationsRequest]) (*connect.Response[ListAllocationsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListAllocations(ctx, m.TenantID, m.PoolID, m.ValuationID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&ListAllocationsResponse{Allocations: toAllocationProtos(list)}), nil
}

func (h *Handler) SettlePool(ctx context.Context, req *connect.Request[SettlePoolRequest]) (*connect.Response[SettlePoolResponse], error) {
	m := req.Msg
	out, err := h.svc.SettlePool(ctx, m.TenantID, m.PoolID, m.Actor)
	if err != nil {
		return nil, statusError(err)
	}
	return connect.NewResponse(&SettlePoolResponse{
		Pool:   toPoolProto(out.Pool),
		Events: toEventProtos(out.Events),
	}), nil
}

func (h *Handler) ListEconomicEvents(ctx context.Context, req *connect.Request[ListEconomicEventsRequest]) (*connect.Response[ListEconomicEventsResponse], error) {
	m := req.Msg
	list, err := h.svc.ListEconomicEvents(ctx, m.TenantID, m.PoolID, m.ProducerRef, int(m.Limit), int(m.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&ListEconomicEventsResponse{Events: toEventProtos(list)}), nil
}

func (h *Handler) DeclareRetroactivityPolicy(ctx context.Context, req *connect.Request[DeclareRetroactivityPolicyRequest]) (*connect.Response[DeclareRetroactivityPolicyResponse], error) {
	m := req.Msg

	minimum := money.Zero(m.AmountScale, m.Currency)
	if m.MinimumAdjustment != "" {
		parsed, err := money.Parse(m.MinimumAdjustment, m.AmountScale, m.Currency)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		minimum = parsed
	}

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

	out, err := h.svc.DeclareRetroactivityPolicy(ctx, service.DeclareRetroactivityPolicyInput{
		TenantID:          m.TenantID,
		Name:              m.Name,
		Mode:              domain.RetroactivityMode(m.Mode),
		MaxLookbackDays:   m.MaxLookbackDays,
		MinimumAdjustment: minimum,
		EffectiveFrom:     effectiveFrom,
		EffectiveTo:       effectiveTo,
		Actor:             m.Actor,
	})
	if err != nil {
		if errors.Is(err, repository.ErrOverlappingPolicy) {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&DeclareRetroactivityPolicyResponse{Policy: toRetroPolicyProto(out)}), nil
}

func (h *Handler) GetEffectiveRetroactivityPolicy(ctx context.Context, req *connect.Request[GetEffectiveRetroactivityPolicyRequest]) (*connect.Response[GetEffectiveRetroactivityPolicyResponse], error) {
	at, err := parseTime(req.Msg.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out, err := h.svc.GetEffectiveRetroactivityPolicy(ctx, req.Msg.TenantID, at)
	if err != nil {
		return nil, statusError(err)
	}
	return connect.NewResponse(&GetEffectiveRetroactivityPolicyResponse{Policy: toRetroPolicyProto(out)}), nil
}

// ApplyCorrection re-values a settled pool under the tenant's retroactivity
// policy. An escalation comes back as FailedPrecondition rather than a
// successful response with an empty outcome: a caller that ignored the outcome
// field would otherwise record the correction as handled.
func (h *Handler) ApplyCorrection(ctx context.Context, req *connect.Request[ApplyCorrectionRequest]) (*connect.Response[ApplyCorrectionResponse], error) {
	m := req.Msg

	prices, err := toComponentPrices(m.ComponentPrices)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	at, err := parseTime(m.At, "at")
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out, err := h.svc.ApplyCorrection(ctx, service.ApplyCorrectionInput{
		TenantID:        m.TenantID,
		PoolID:          m.PoolID,
		ComponentPrices: prices,
		At:              at,
		Actor:           m.Actor,
	})
	if err != nil {
		if errors.Is(err, service.ErrEscalated) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, statusError(err)
	}

	return connect.NewResponse(&ApplyCorrectionResponse{
		Outcome:     string(out.Decision.Outcome),
		Reason:      out.Decision.Reason,
		EventKind:   string(out.Decision.EventKind),
		Adjustment:  out.Adjustment.String(),
		Pool:        toPoolProto(out.Pool),
		Valuation:   toValuationProto(out.Valuation),
		Allocations: toAllocationProtos(out.Allocations),
		Events:      toEventProtos(out.Events),
	}), nil
}

func toComponentPrices(in []ComponentPriceProto) ([]domain.ComponentPrice, error) {
	out := make([]domain.ComponentPrice, 0, len(in))
	for _, c := range in {
		rate, err := money.ParseRate(c.Price, c.Scale)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.ComponentPrice{
			Component: domain.ComponentKind(c.Component),
			Price:     rate,
		})
	}
	return out, nil
}

func toPoolProto(p *domain.Pool) *PoolProto {
	if p == nil {
		return nil
	}
	return &PoolProto{
		ID:            p.ID,
		TenantID:      p.TenantID,
		Name:          p.Name,
		PeriodStart:   p.PeriodStart.Format(time.RFC3339),
		PeriodEnd:     p.PeriodEnd.Format(time.RFC3339),
		Unit:          p.Unit,
		Currency:      p.Currency,
		AmountScale:   p.Scale,
		Status:        string(p.Status),
		RateCardID:    p.RateCardID,
		PolicyVersion: p.PolicyVersion,
		CreatedAt:     p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     p.UpdatedAt.Format(time.RFC3339),
	}
}

func toProducerMilkProto(m *domain.ProducerMilk) *ProducerMilkProto {
	if m == nil {
		return nil
	}
	components := make(map[string]string, len(m.Components))
	for k, v := range m.Components {
		components[string(k)] = v
	}
	return &ProducerMilkProto{
		ID:          m.ID,
		TenantID:    m.TenantID,
		PoolID:      m.PoolID,
		ProducerRef: m.ProducerRef,
		Quantity:    m.Quantity,
		Components:  components,
		SlotRefs:    m.SlotRefs,
		OriginKind:  string(m.Origin.Kind),
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
	}
}

func toUtilisationProto(u *domain.ClassifiedUtilisation) *UtilisationProto {
	if u == nil {
		return nil
	}
	return &UtilisationProto{
		ID:         u.ID,
		TenantID:   u.TenantID,
		PoolID:     u.PoolID,
		Class:      string(u.Class),
		Quantity:   u.Quantity,
		Price:      u.Price.String(),
		PriceScale: u.Price.Scale,
		CreatedAt:  u.CreatedAt.Format(time.RFC3339),
	}
}

func toValuationProto(v *domain.PoolValuation) *ValuationProto {
	if v == nil {
		return nil
	}
	trail := make([]RoundingStepProto, 0, len(v.RoundingTrail))
	for _, s := range v.RoundingTrail {
		trail = append(trail, RoundingStepProto{
			Operation: s.Operation,
			Mode:      string(s.Mode),
			FromScale: s.FromScale,
			ToScale:   s.ToScale,
			Discarded: s.Discarded,
			Result:    s.Result.String(),
		})
	}
	return &ValuationProto{
		ID:                     v.ID,
		TenantID:               v.TenantID,
		PoolID:                 v.PoolID,
		Currency:               v.ClassifiedValue.Currency,
		AmountScale:            v.ClassifiedValue.Scale,
		ClassifiedValue:        v.ClassifiedValue.String(),
		ComponentValue:         v.ComponentValue.String(),
		ProducerSettlementFund: v.ProducerSettlementFund.String(),
		TotalQuantity:          v.TotalQuantity,
		BlendPrice:             v.BlendPrice.String(),
		BlendPriceScale:        v.BlendPrice.Scale,
		RoundingTrail:          trail,
		ComputedAt:             v.ComputedAt.Format(time.RFC3339),
	}
}

func toAllocationProtos(list []domain.Allocation) []AllocationProto {
	out := make([]AllocationProto, 0, len(list))
	for _, a := range list {
		out = append(out, AllocationProto{
			ID:             a.ID,
			TenantID:       a.TenantID,
			PoolID:         a.PoolID,
			ValuationID:    a.ValuationID,
			ProducerRef:    a.ProducerRef,
			Currency:       a.Total.Currency,
			AmountScale:    a.Total.Scale,
			ComponentValue: a.ComponentValue.String(),
			FundShare:      a.FundShare.String(),
			Total:          a.Total.String(),
			Weight:         a.Weight,
			CreatedAt:      a.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func toEventProtos(list []domain.ProducerEconomicEvent) []EconomicEventProto {
	out := make([]EconomicEventProto, 0, len(list))
	for _, e := range list {
		out = append(out, EconomicEventProto{
			ID:                e.ID,
			TenantID:          e.TenantID,
			PoolID:            e.PoolID,
			AllocationID:      e.AllocationID,
			ProducerRef:       e.ProducerRef,
			Currency:          e.Amount.Currency,
			AmountScale:       e.Amount.Scale,
			Amount:            e.Amount.String(),
			Kind:              string(e.Kind),
			SupersedesEventID: e.SupersedesEventID,
			CreatedAt:         e.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func toRetroPolicyProto(p *domain.RecoveryRetroactivityPolicy) *RetroactivityPolicyProto {
	if p == nil {
		return nil
	}
	out := &RetroactivityPolicyProto{
		ID:                p.ID,
		TenantID:          p.TenantID,
		Name:              p.Name,
		Mode:              string(p.Mode),
		MaxLookbackDays:   p.MaxLookbackDays,
		Currency:          p.MinimumAdjustment.Currency,
		AmountScale:       p.MinimumAdjustment.Scale,
		MinimumAdjustment: p.MinimumAdjustment.String(),
		EffectiveFrom:     p.EffectiveFrom.Format(time.RFC3339),
	}
	if p.EffectiveTo != nil {
		out.EffectiveTo = p.EffectiveTo.Format(time.RFC3339)
	}
	return out
}

func statusError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInvalidArgument, err)
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
