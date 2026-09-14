package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/order-service/internal/domain"
	"github.com/ppusapati/gavya/services/order-service/internal/repository"
	"github.com/ppusapati/gavya/services/order-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "order.v1.OrderService"

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

	route("CreateOrder", connectjson.Unary(h.CreateOrder))
	route("GetOrder", connectjson.Unary(h.GetOrder))
	route("AddOrderItem", connectjson.Unary(h.AddOrderItem))
	route("ConfirmOrder", connectjson.Unary(h.ConfirmOrder))
	route("CancelOrder", connectjson.Unary(h.CancelOrder))
	route("GenerateInvoice", connectjson.Unary(h.GenerateInvoice))

	// Returns. The table and domain.Return were here from the start with no
	// endpoint reaching them.
	route("RequestReturn", connectjson.Unary(h.RequestReturn))
	route("DecideReturn", connectjson.Unary(h.DecideReturn))
	route("GetReturn", connectjson.Unary(h.GetReturn))
	route("ListOrderReturns", connectjson.Unary(h.ListOrderReturns))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing order from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateOrderNumber):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, repository.ErrCurrencyMismatch):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, repository.ErrNotDraft),
		errors.Is(err, repository.ErrNotReturnable),
		errors.Is(err, repository.ErrBadTransition),
		errors.Is(err, repository.ErrRefundTooLarge):
		// The request was well formed; the state of the thing it names refused
		// it. Retrying changes nothing until that changes, which is what
		// FailedPrecondition tells a caller and Internal does not — and a
		// refund refused because the order was already fully refunded is
		// precisely something a person needs to read rather than retry.
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateOrderRequest struct {
	TenantID   string `json:"tenant_id"`
	CustomerID string `json:"customer_id"`
	// Currency is required. There is no default: an order priced in an assumed
	// currency is an order nobody can fulfil.
	Currency string `json:"currency"`
	// TaxInclusive says whether the line prices already contain the tax.
	TaxInclusive    bool      `json:"tax_inclusive"`
	ShippingAddress string    `json:"shipping_address"`
	Notes           string    `json:"notes"`
	OrderedAt       time.Time `json:"ordered_at"`
	CreatedBy       string    `json:"created_by"`
}

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type AddOrderItemRequest struct {
	TenantID  string `json:"tenant_id"`
	OrderID   string `json:"order_id"`
	SKUID     string `json:"sku_id"`
	ProductID string `json:"product_id"`
	// Quantity and TaxRate are read from the digits that were sent, whether as
	// a JSON string or a bare number, and go back out as decimal literals.
	Quantity exact.Fixed `json:"quantity"`
	// UnitPrice is a decimal literal — "42.50", not 42.5 — because a JSON number
	// is a float64 by the time Go has read it, and a price that has been through
	// a float is one nobody can prove was not changed on the way.
	UnitPrice string `json:"unit_price"`
	// TaxRate is a percentage for this line: 0 for an exempt good, 12 for one
	// rated at twelve per cent.
	TaxRate   exact.Fixed `json:"tax_rate"`
	CreatedBy string      `json:"created_by"`
}

type OrderActionRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type GenerateInvoiceRequest struct {
	OrderID   string `json:"order_id"`
	TenantID  string `json:"tenant_id"`
	CreatedBy string `json:"created_by"`
}

// OrderView is what an order looks like on the wire.
//
// The domain model used to be serialised directly, which sent its totals out as
// JSON numbers. This exists so they go out as decimal literals at the currency's
// scale, and so a change to the stored shape is not automatically a change to
// the published one.
type OrderView struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	CustomerID      string     `json:"customer_id"`
	OrderNumber     string     `json:"order_number"`
	Status          string     `json:"status"`
	SubTotal        string     `json:"sub_total"`
	TaxAmount       string     `json:"tax_amount"`
	TotalAmount     string     `json:"total_amount"`
	Currency        string     `json:"currency"`
	TaxInclusive    bool       `json:"tax_inclusive"`
	ShippingAddress string     `json:"shipping_address"`
	Notes           string     `json:"notes"`
	OrderedAt       time.Time  `json:"ordered_at"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatedBy       string     `json:"created_by"`
	UpdatedBy       string     `json:"updated_by"`
}

func viewOrder(o *domain.Order) *OrderView {
	if o == nil {
		return nil
	}
	return &OrderView{
		ID: o.ID, TenantID: o.TenantID, CustomerID: o.CustomerID,
		OrderNumber: o.OrderNumber, Status: o.Status,
		SubTotal: o.SubTotal.String(), TaxAmount: o.TaxAmount.String(),
		TotalAmount: o.TotalAmount.String(), Currency: o.TotalAmount.Currency,
		TaxInclusive: o.TaxInclusive, ShippingAddress: o.ShippingAddress, Notes: o.Notes,
		OrderedAt: o.OrderedAt, DeliveredAt: o.DeliveredAt,
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
		CreatedBy: o.CreatedBy, UpdatedBy: o.UpdatedBy,
	}
}

type OrderItemView struct {
	ID         string      `json:"id"`
	TenantID   string      `json:"tenant_id"`
	OrderID    string      `json:"order_id"`
	SKUID      string      `json:"sku_id"`
	ProductID  string      `json:"product_id"`
	Quantity   exact.Fixed `json:"quantity"`
	UnitPrice  string      `json:"unit_price"`
	TotalPrice string      `json:"total_price"`
	Currency   string      `json:"currency"`
	TaxRate    exact.Fixed `json:"tax_rate"`
	Status     string      `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	CreatedBy  string      `json:"created_by"`
	UpdatedBy  string      `json:"updated_by"`
}

func viewOrderItem(i *domain.OrderItem) *OrderItemView {
	if i == nil {
		return nil
	}
	return &OrderItemView{
		ID: i.ID, TenantID: i.TenantID, OrderID: i.OrderID, SKUID: i.SKUID,
		ProductID: i.ProductID, Quantity: i.Quantity,
		UnitPrice: i.UnitPrice.String(), TotalPrice: i.TotalPrice.String(),
		Currency: i.TotalPrice.Currency, TaxRate: i.TaxRate, Status: i.Status,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
		CreatedBy: i.CreatedBy, UpdatedBy: i.UpdatedBy,
	}
}

type InvoiceView struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	OrderID       string     `json:"order_id"`
	InvoiceNumber string     `json:"invoice_number"`
	Status        string     `json:"status"`
	SubTotal      string     `json:"sub_total"`
	TaxAmount     string     `json:"tax_amount"`
	TotalAmount   string     `json:"total_amount"`
	Currency      string     `json:"currency"`
	IssuedAt      time.Time  `json:"issued_at"`
	DueAt         time.Time  `json:"due_at"`
	PaidAt        *time.Time `json:"paid_at,omitempty"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
}

func viewInvoice(i *domain.Invoice) *InvoiceView {
	if i == nil {
		return nil
	}
	return &InvoiceView{
		ID: i.ID, TenantID: i.TenantID, OrderID: i.OrderID,
		InvoiceNumber: i.InvoiceNumber, Status: i.Status,
		SubTotal: i.SubTotal.String(), TaxAmount: i.TaxAmount.String(),
		TotalAmount: i.TotalAmount.String(), Currency: i.TotalAmount.Currency,
		IssuedAt: i.IssuedAt, DueAt: i.DueAt, PaidAt: i.PaidAt,
		CreatedBy: i.CreatedBy, UpdatedBy: i.UpdatedBy,
	}
}

type OrderResponse struct {
	Order *OrderView `json:"order"`
}

type OrderItemResponse struct {
	Item *OrderItemView `json:"item"`
	// Order is returned with the item because adding a line changes the order's
	// totals, and a caller that fetched them separately could be handed figures
	// a concurrent line had already moved on from.
	Order *OrderView `json:"order,omitempty"`
}

type InvoiceResponse struct {
	Invoice *InvoiceView `json:"invoice"`
}

func (h *Handler) CreateOrder(ctx context.Context, req *connect.Request[CreateOrderRequest]) (*connect.Response[OrderResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateOrder(ctx, &domain.Order{
		TenantID:        m.TenantID,
		CustomerID:      m.CustomerID,
		TaxInclusive:    m.TaxInclusive,
		ShippingAddress: m.ShippingAddress,
		Notes:           m.Notes,
		OrderedAt:       m.OrderedAt,
		CreatedBy:       m.CreatedBy,
	}, m.Currency)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: viewOrder(out)}), nil
}

func (h *Handler) GetOrder(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[OrderResponse], error) {
	out, err := h.svc.GetOrder(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: viewOrder(out)}), nil
}

func (h *Handler) AddOrderItem(ctx context.Context, req *connect.Request[AddOrderItemRequest]) (*connect.Response[OrderItemResponse], error) {
	m := req.Msg
	out, err := h.svc.AddOrderItem(ctx, &domain.OrderItem{
		TenantID:  m.TenantID,
		OrderID:   m.OrderID,
		SKUID:     m.SKUID,
		ProductID: m.ProductID,
		Quantity:  m.Quantity,
		TaxRate:   m.TaxRate,
		CreatedBy: m.CreatedBy,
	}, m.UnitPrice)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderItemResponse{Item: viewOrderItem(out.Item), Order: viewOrder(out.Order)}), nil
}

func (h *Handler) ConfirmOrder(ctx context.Context, req *connect.Request[OrderActionRequest]) (*connect.Response[OrderResponse], error) {
	m := req.Msg
	out, err := h.svc.ConfirmOrder(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: viewOrder(out)}), nil
}

func (h *Handler) CancelOrder(ctx context.Context, req *connect.Request[OrderActionRequest]) (*connect.Response[OrderResponse], error) {
	m := req.Msg
	out, err := h.svc.CancelOrder(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: viewOrder(out)}), nil
}

func (h *Handler) GenerateInvoice(ctx context.Context, req *connect.Request[GenerateInvoiceRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.GenerateInvoice(ctx, m.OrderID, m.TenantID, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: viewInvoice(out)}), nil
}

// ---------------------------------------------------------------------------
// Returns
// ---------------------------------------------------------------------------

type RequestReturnRequest struct {
	TenantID string `json:"tenant_id"`
	OrderID  string `json:"order_id"`
	// Reason is required. A refund with no stated reason is a payment nobody
	// can account for afterwards.
	Reason string `json:"reason"`
	// RefundAmount is a decimal literal, in the tenant's currency, for the same
	// reason every other money field here is one.
	RefundAmount string `json:"refund_amount"`
	CreatedBy    string `json:"created_by"`
}

type DecideReturnRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	// Status is approved, rejected or completed. Which of those a return may
	// move to depends on where it is, and the service says so rather than the
	// caller assuming.
	Status    string `json:"status"`
	UpdatedBy string `json:"updated_by"`
}

type ListOrderReturnsRequest struct {
	OrderID  string `json:"order_id"`
	TenantID string `json:"tenant_id"`
}

// ReturnView is what a return looks like on the wire.
type ReturnView struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	OrderID      string     `json:"order_id"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	RefundAmount string     `json:"refund_amount"`
	Currency     string     `json:"currency"`
	RequestedAt  time.Time  `json:"requested_at"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty"`
	CreatedBy    string     `json:"created_by"`
	UpdatedBy    string     `json:"updated_by"`
}

func viewReturn(r *domain.Return) *ReturnView {
	if r == nil {
		return nil
	}
	return &ReturnView{
		ID: r.ID, TenantID: r.TenantID, OrderID: r.OrderID,
		Reason: r.Reason, Status: r.Status,
		RefundAmount: r.RefundAmount.String(), Currency: r.RefundAmount.Currency,
		RequestedAt: r.RequestedAt, ProcessedAt: r.ProcessedAt,
		CreatedBy: r.CreatedBy, UpdatedBy: r.UpdatedBy,
	}
}

type ReturnResponse struct {
	Return *ReturnView `json:"return"`
}

type ListReturnsResponse struct {
	Returns []*ReturnView `json:"returns"`
}

func (h *Handler) RequestReturn(ctx context.Context, req *connect.Request[RequestReturnRequest]) (*connect.Response[ReturnResponse], error) {
	m := req.Msg
	out, err := h.svc.RequestReturn(ctx, &domain.Return{
		TenantID:  m.TenantID,
		OrderID:   m.OrderID,
		Reason:    m.Reason,
		CreatedBy: m.CreatedBy,
	}, m.RefundAmount)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ReturnResponse{Return: viewReturn(out)}), nil
}

func (h *Handler) DecideReturn(ctx context.Context, req *connect.Request[DecideReturnRequest]) (*connect.Response[ReturnResponse], error) {
	m := req.Msg
	out, err := h.svc.DecideReturn(ctx, m.ID, m.TenantID, m.Status, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ReturnResponse{Return: viewReturn(out)}), nil
}

func (h *Handler) GetReturn(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[ReturnResponse], error) {
	out, err := h.svc.GetReturn(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ReturnResponse{Return: viewReturn(out)}), nil
}

func (h *Handler) ListOrderReturns(ctx context.Context, req *connect.Request[ListOrderReturnsRequest]) (*connect.Response[ListReturnsResponse], error) {
	list, err := h.svc.ListOrderReturns(ctx, req.Msg.OrderID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]*ReturnView, 0, len(list))
	for _, r := range list {
		out = append(out, viewReturn(r))
	}
	return connect.NewResponse(&ListReturnsResponse{Returns: out}), nil
}
