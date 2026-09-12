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
	case errors.Is(err, repository.ErrCurrencyMismatch):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, repository.ErrNotPayable):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, repository.ErrNotDraft):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateInvoiceRequest struct {
	TenantID      string `json:"tenant_id"`
	CustomerID    string `json:"customer_id"`
	ReferenceID   string `json:"reference_id"`
	ReferenceType string `json:"reference_type"`
	// Currency is required. There is no default: an invoice whose currency was
	// assumed would be an invoice nobody can be asked to pay.
	Currency string `json:"currency"`
	// TaxInclusive says whether the line prices already contain the tax.
	TaxInclusive bool      `json:"tax_inclusive"`
	IssuedAt     time.Time `json:"issued_at"`
	Notes        string    `json:"notes"`
	CreatedBy    string    `json:"created_by"`
}

type AddInvoiceItemRequest struct {
	TenantID    string `json:"tenant_id"`
	InvoiceID   string `json:"invoice_id"`
	Description string `json:"description"`
	// Quantity counts litres or kilos and stays a JSON number: its column is
	// NUMERIC(10,3) and the boundary is guarded by libs/integrity/exact.
	Quantity float64 `json:"quantity"`
	// UnitPrice is a decimal literal — "42.50", not 42.5 — because a JSON number
	// is a float64 by the time Go has read it, and on an invoice this is part of
	// what a customer is asked to pay.
	UnitPrice string  `json:"unit_price"`
	TaxRate   float64 `json:"tax_rate"`
	CreatedBy string  `json:"created_by"`
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
	TenantID  string `json:"tenant_id"`
	InvoiceID string `json:"invoice_id"`
	// A decimal literal. There is no currency field: a payment is in its
	// invoice's currency, and the repository refuses one that is not.
	Amount        string    `json:"amount"`
	PaymentMethod string    `json:"payment_method"`
	ReferenceNo   string    `json:"reference_no"`
	PaidAt        time.Time `json:"paid_at"`
	Notes         string    `json:"notes"`
	CreatedBy     string    `json:"created_by"`
}

// InvoiceView is what an invoice looks like on the wire.
//
// The domain model used to be serialised directly, which sent its totals out as
// JSON numbers. On an invoice those are the figures a customer is asked to pay,
// so they go out as decimal literals at the currency's scale.
type InvoiceView struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	CustomerID    string     `json:"customer_id"`
	InvoiceNumber string     `json:"invoice_number"`
	ReferenceID   string     `json:"reference_id,omitempty"`
	ReferenceType string     `json:"reference_type,omitempty"`
	Status        string     `json:"status"`
	SubTotal      string     `json:"sub_total"`
	TaxAmount     string     `json:"tax_amount"`
	TotalAmount   string     `json:"total_amount"`
	Currency      string     `json:"currency"`
	TaxInclusive  bool       `json:"tax_inclusive"`
	IssuedAt      time.Time  `json:"issued_at"`
	DueAt         time.Time  `json:"due_at"`
	PaidAt        *time.Time `json:"paid_at,omitempty"`
	Notes         string     `json:"notes,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
}

func viewInvoice(i *domain.Invoice) *InvoiceView {
	if i == nil {
		return nil
	}
	return &InvoiceView{
		ID: i.ID, TenantID: i.TenantID, CustomerID: i.CustomerID,
		InvoiceNumber: i.InvoiceNumber, ReferenceID: i.ReferenceID,
		ReferenceType: i.ReferenceType, Status: i.Status,
		SubTotal: i.SubTotal.String(), TaxAmount: i.TaxAmount.String(),
		TotalAmount: i.TotalAmount.String(), Currency: i.TotalAmount.Currency,
		TaxInclusive: i.TaxInclusive, IssuedAt: i.IssuedAt, DueAt: i.DueAt,
		PaidAt: i.PaidAt, Notes: i.Notes,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
		CreatedBy: i.CreatedBy, UpdatedBy: i.UpdatedBy,
	}
}

func viewInvoices(in []*domain.Invoice) []*InvoiceView {
	out := make([]*InvoiceView, 0, len(in))
	for _, i := range in {
		out = append(out, viewInvoice(i))
	}
	return out
}

type InvoiceItemView struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	InvoiceID   string    `json:"invoice_id"`
	Description string    `json:"description"`
	Quantity    float64   `json:"quantity"`
	UnitPrice   string    `json:"unit_price"`
	TotalPrice  string    `json:"total_price"`
	Currency    string    `json:"currency"`
	TaxRate     float64   `json:"tax_rate"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`
}

func viewInvoiceItem(i *domain.InvoiceItem) *InvoiceItemView {
	if i == nil {
		return nil
	}
	return &InvoiceItemView{
		ID: i.ID, TenantID: i.TenantID, InvoiceID: i.InvoiceID,
		Description: i.Description, Quantity: i.Quantity,
		UnitPrice: i.UnitPrice.String(), TotalPrice: i.TotalPrice.String(),
		Currency: i.TotalPrice.Currency, TaxRate: i.TaxRate,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
		CreatedBy: i.CreatedBy, UpdatedBy: i.UpdatedBy,
	}
}

type PaymentView struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	InvoiceID     string    `json:"invoice_id"`
	Amount        string    `json:"amount"`
	Currency      string    `json:"currency"`
	PaymentMethod string    `json:"payment_method"`
	ReferenceNo   string    `json:"reference_no,omitempty"`
	PaidAt        time.Time `json:"paid_at"`
	Notes         string    `json:"notes,omitempty"`
	CreatedBy     string    `json:"created_by"`
	UpdatedBy     string    `json:"updated_by"`
}

func viewPayment(p *domain.Payment) *PaymentView {
	if p == nil {
		return nil
	}
	return &PaymentView{
		ID: p.ID, TenantID: p.TenantID, InvoiceID: p.InvoiceID,
		Amount: p.Amount.String(), Currency: p.Amount.Currency,
		PaymentMethod: p.PaymentMethod, ReferenceNo: p.ReferenceNo,
		PaidAt: p.PaidAt, Notes: p.Notes,
		CreatedBy: p.CreatedBy, UpdatedBy: p.UpdatedBy,
	}
}

type InvoiceResponse struct {
	Invoice *InvoiceView `json:"invoice"`
}

type InvoiceItemResponse struct {
	Item *InvoiceItemView `json:"item"`
	// Invoice is returned with the line because adding one changes the totals,
	// and a caller that fetched them separately could be handed figures a
	// concurrent line had already moved on from.
	Invoice *InvoiceView `json:"invoice,omitempty"`
}

type PaymentResponse struct {
	Payment *PaymentView `json:"payment"`
	// Invoice is returned with the payment, so a caller learns from the same
	// reply whether that payment settled it.
	Invoice *InvoiceView `json:"invoice,omitempty"`
}

type ListInvoicesResponse struct {
	Invoices []*InvoiceView `json:"invoices"`
}

func (h *Handler) CreateInvoice(ctx context.Context, req *connect.Request[CreateInvoiceRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateInvoice(ctx, &domain.Invoice{
		TenantID:      m.TenantID,
		CustomerID:    m.CustomerID,
		ReferenceID:   m.ReferenceID,
		ReferenceType: m.ReferenceType,
		TaxInclusive:  m.TaxInclusive,
		IssuedAt:      m.IssuedAt,
		Notes:         m.Notes,
		CreatedBy:     m.CreatedBy,
	}, m.Currency)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: viewInvoice(out)}), nil
}

func (h *Handler) AddInvoiceItem(ctx context.Context, req *connect.Request[AddInvoiceItemRequest]) (*connect.Response[InvoiceItemResponse], error) {
	m := req.Msg
	out, err := h.svc.AddInvoiceItem(ctx, &domain.InvoiceItem{
		TenantID:    m.TenantID,
		InvoiceID:   m.InvoiceID,
		Description: m.Description,
		Quantity:    m.Quantity,
		TaxRate:     m.TaxRate,
		CreatedBy:   m.CreatedBy,
	}, m.UnitPrice)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceItemResponse{Item: viewInvoiceItem(out.Item), Invoice: viewInvoice(out.Invoice)}), nil
}

func (h *Handler) SendInvoice(ctx context.Context, req *connect.Request[InvoiceActionRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.SendInvoice(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: viewInvoice(out)}), nil
}

func (h *Handler) RecordPayment(ctx context.Context, req *connect.Request[RecordPaymentRequest]) (*connect.Response[PaymentResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordPayment(ctx, &domain.Payment{
		TenantID:      m.TenantID,
		InvoiceID:     m.InvoiceID,
		PaymentMethod: m.PaymentMethod,
		ReferenceNo:   m.ReferenceNo,
		PaidAt:        m.PaidAt,
		Notes:         m.Notes,
		CreatedBy:     m.CreatedBy,
	}, m.Amount)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PaymentResponse{Payment: viewPayment(out.Payment), Invoice: viewInvoice(out.Invoice)}), nil
}

func (h *Handler) VoidInvoice(ctx context.Context, req *connect.Request[InvoiceActionRequest]) (*connect.Response[InvoiceResponse], error) {
	m := req.Msg
	out, err := h.svc.VoidInvoice(ctx, m.ID, m.TenantID, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&InvoiceResponse{Invoice: viewInvoice(out)}), nil
}

func (h *Handler) GetOutstandingInvoices(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListInvoicesResponse], error) {
	out, err := h.svc.GetOutstandingInvoices(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListInvoicesResponse{Invoices: viewInvoices(out)}), nil
}
