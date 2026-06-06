package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
	"github.com/ppusapati/gavya/services/billing-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/billing.v1.BillingService/CreateInvoice", h.CreateInvoice)
	mux.HandleFunc("/billing.v1.BillingService/AddInvoiceItem", h.AddInvoiceItem)
	mux.HandleFunc("/billing.v1.BillingService/SendInvoice", h.SendInvoice)
	mux.HandleFunc("/billing.v1.BillingService/RecordPayment", h.RecordPayment)
	mux.HandleFunc("/billing.v1.BillingService/VoidInvoice", h.VoidInvoice)
	mux.HandleFunc("/billing.v1.BillingService/GetOutstandingInvoices", h.GetOutstandingInvoices)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
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

func (h *Handler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	var req CreateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inv := &domain.Invoice{
		TenantID:      req.TenantID,
		CustomerID:    req.CustomerID,
		ReferenceID:   req.ReferenceID,
		ReferenceType: req.ReferenceType,
		Currency:      req.Currency,
		IssuedAt:      req.IssuedAt,
		Notes:         req.Notes,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.CreateInvoice(r.Context(), inv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AddInvoiceItem(w http.ResponseWriter, r *http.Request) {
	var req AddInvoiceItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item := &domain.InvoiceItem{
		TenantID:    req.TenantID,
		InvoiceID:   req.InvoiceID,
		Description: req.Description,
		Quantity:    req.Quantity,
		UnitPrice:   req.UnitPrice,
		TaxRate:     req.TaxRate,
		CreatedBy:   req.CreatedBy,
	}
	result, err := h.svc.AddInvoiceItem(r.Context(), item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) SendInvoice(w http.ResponseWriter, r *http.Request) {
	var req InvoiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.SendInvoice(r.Context(), req.ID, req.TenantID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RecordPayment(w http.ResponseWriter, r *http.Request) {
	var req RecordPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := &domain.Payment{
		TenantID:      req.TenantID,
		InvoiceID:     req.InvoiceID,
		Amount:        req.Amount,
		Currency:      req.Currency,
		PaymentMethod: req.PaymentMethod,
		ReferenceNo:   req.ReferenceNo,
		PaidAt:        req.PaidAt,
		Notes:         req.Notes,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.RecordPayment(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) VoidInvoice(w http.ResponseWriter, r *http.Request) {
	var req InvoiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.VoidInvoice(r.Context(), req.ID, req.TenantID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetOutstandingInvoices(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetOutstandingInvoices(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
