package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
	"github.com/ppusapati/gavya/services/billing-service/internal/repository"
	"github.com/ppusapati/gavya/services/billing-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "billing.v1.BillingService"

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

	route("CreateInvoice", connectjson.Unary(h.CreateInvoice))
	route("AddInvoiceItem", connectjson.Unary(h.AddInvoiceItem))
	route("SendInvoice", connectjson.Unary(h.SendInvoice))
	route("RecordPayment", connectjson.Unary(h.RecordPayment))
	route("VoidInvoice", connectjson.Unary(h.VoidInvoice))
	route("GetOutstandingInvoices", connectjson.Unary(h.GetOutstandingInvoices))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing invoice from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateInvoiceNumber):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateInvoiceRequest struct {
	TenantID      string    `json:"tenant_id"`
	CustomerID    string    `json:"customer_id"`
	ReferenceID   string    `json:"reference_id"`
	ReferenceType string    `json:"reference_type"`
	Currency      string    `json:"currency"`
	IssuedAt      time.Time `json:"issued_at"`
	Notes         string    `json:"notes"`
	CreatedBy     string    `json:"created_by"`
}

type AddInvoiceItemRequest struct {
	TenantID    string  `json:"tenant_id"`
	InvoiceID   string  `json:"invoice_id"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TaxRate     float64 `json:"tax_rate"`
	CreatedBy   string  `json:"created_by"`
}

type InvoiceActionRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type RecordPaymentRequest struct {
	TenantID      string    `json:"tenant_id"`
	InvoiceID     string    `json:"invoice_id"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	PaymentMethod string    `json:"payment_method"`
	ReferenceNo   string    `json:"reference_no"`
	PaidAt        time.Time `json:"paid_at"`
	Notes         string    `json:"notes"`
	CreatedBy     string    `json:"created_by"`
}

type InvoiceResponse struct {
	Invoice *domain.Invoice `json:"invoice"`
}

type InvoiceItemResponse struct {
	Item *domain.InvoiceItem `json:"item"`
}

type PaymentResponse struct {
	Payment *domain.Payment `json:"payment"`
}

type ListInvoicesResponse struct {
	Invoices []*domain.Invoice `json:"invoices"`
}

func (h *Handler) CreateInvoice(ctx context.Context, req *connect.Request[CreateInvoiceRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateInvoice(ctx, &domain.Invoice{
		TenantID:      m.TenantID,
		CustomerID:    m.CustomerID,
		ReferenceID:   m.ReferenceID,
		ReferenceType: m.ReferenceType,
		Currency:      m.Currency,
		IssuedAt:      m.IssuedAt,
		Notes:         m.Notes,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: out}), nil
}

func (h *Handler) AddInvoiceItem(ctx context.Context, req *connect.Request[AddInvoiceItemRequest]) (*connect.Response[InvoiceItemResponse], error) {
	m := req.Msg
	out, err := h.svc.AddInvoiceItem(ctx, &domain.InvoiceItem{
		TenantID:    m.TenantID,
		InvoiceID:   m.InvoiceID,
		Description: m.Description,
		Quantity:    m.Quantity,
		UnitPrice:   m.UnitPrice,
		TaxRate:     m.TaxRate,
		CreatedBy:   m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceItemResponse{Item: out}), nil
}

func (h *Handler) SendInvoice(ctx context.Context, req *connect.Request[InvoiceActionRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.SendInvoice(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: out}), nil
}

func (h *Handler) RecordPayment(ctx context.Context, req *connect.Request[RecordPaymentRequest]) (*connect.Response[PaymentResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordPayment(ctx, &domain.Payment{
		TenantID:      m.TenantID,
		InvoiceID:     m.InvoiceID,
		Amount:        m.Amount,
		Currency:      m.Currency,
		PaymentMethod: m.PaymentMethod,
		ReferenceNo:   m.ReferenceNo,
		PaidAt:        m.PaidAt,
		Notes:         m.Notes,
		CreatedBy:     m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PaymentResponse{Payment: out}), nil
}

func (h *Handler) VoidInvoice(ctx context.Context, req *connect.Request[InvoiceActionRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.VoidInvoice(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: out}), nil
}

func (h *Handler) GetOutstandingInvoices(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListInvoicesResponse], error) {
	out, err := h.svc.GetOutstandingInvoices(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListInvoicesResponse{Invoices: out}), nil
}
