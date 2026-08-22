package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
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
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateOrder", connectjson.Unary(h.CreateOrder))
	route("GetOrder", connectjson.Unary(h.GetOrder))
	route("AddOrderItem", connectjson.Unary(h.AddOrderItem))
	route("ConfirmOrder", connectjson.Unary(h.ConfirmOrder))
	route("CancelOrder", connectjson.Unary(h.CancelOrder))
	route("GenerateInvoice", connectjson.Unary(h.GenerateInvoice))
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
	case errors.Is(err, repository.ErrNotDraft):
		// The request was well formed; the order's state refused it. Retrying
		// changes nothing until the order changes.
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateOrderRequest struct {
	TenantID        string    `json:"tenant_id"`
	CustomerID      string    `json:"customer_id"`
	Currency        string    `json:"currency"`
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
	TenantID  string  `json:"tenant_id"`
	OrderID   string  `json:"order_id"`
	SKUID     string  `json:"sku_id"`
	ProductID string  `json:"product_id"`
	Quantity  float64 `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
	CreatedBy string  `json:"created_by"`
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

type OrderResponse struct {
	Order *domain.Order `json:"order"`
}

type OrderItemResponse struct {
	Item *domain.OrderItem `json:"item"`
	// Order is returned with the item because adding a line changes the order's
	// totals, and a caller that fetched them separately could be handed figures
	// a concurrent line had already moved on from.
	Order *domain.Order `json:"order,omitempty"`
}

type InvoiceResponse struct {
	Invoice *domain.Invoice `json:"invoice"`
}

func (h *Handler) CreateOrder(ctx context.Context, req *connect.Request[CreateOrderRequest]) (*connect.Response[OrderResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateOrder(ctx, &domain.Order{
		TenantID:        m.TenantID,
		CustomerID:      m.CustomerID,
		Currency:        m.Currency,
		ShippingAddress: m.ShippingAddress,
		Notes:           m.Notes,
		OrderedAt:       m.OrderedAt,
		CreatedBy:       m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: out}), nil
}

func (h *Handler) GetOrder(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[OrderResponse], error) {
	out, err := h.svc.GetOrder(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: out}), nil
}

func (h *Handler) AddOrderItem(ctx context.Context, req *connect.Request[AddOrderItemRequest]) (*connect.Response[OrderItemResponse], error) {
	m := req.Msg
	out, err := h.svc.AddOrderItem(ctx, &domain.OrderItem{
		TenantID:  m.TenantID,
		OrderID:   m.OrderID,
		SKUID:     m.SKUID,
		ProductID: m.ProductID,
		Quantity:  m.Quantity,
		UnitPrice: m.UnitPrice,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderItemResponse{Item: out.Item, Order: out.Order}), nil
}

func (h *Handler) ConfirmOrder(ctx context.Context, req *connect.Request[OrderActionRequest]) (*connect.Response[OrderResponse], error) {
	m := req.Msg
	out, err := h.svc.ConfirmOrder(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: out}), nil
}

func (h *Handler) CancelOrder(ctx context.Context, req *connect.Request[OrderActionRequest]) (*connect.Response[OrderResponse], error) {
	m := req.Msg
	out, err := h.svc.CancelOrder(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&OrderResponse{Order: out}), nil
}

func (h *Handler) GenerateInvoice(ctx context.Context, req *connect.Request[GenerateInvoiceRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.GenerateInvoice(ctx, m.OrderID, m.TenantID, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: out}), nil
}
