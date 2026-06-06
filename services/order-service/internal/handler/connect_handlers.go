package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/services/order-service/internal/domain"
	"github.com/ppusapati/gavya/services/order-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/order.v1.OrderService/CreateOrder", h.CreateOrder)
	mux.HandleFunc("/order.v1.OrderService/GetOrder", h.GetOrder)
	mux.HandleFunc("/order.v1.OrderService/AddOrderItem", h.AddOrderItem)
	mux.HandleFunc("/order.v1.OrderService/ConfirmOrder", h.ConfirmOrder)
	mux.HandleFunc("/order.v1.OrderService/CancelOrder", h.CancelOrder)
	mux.HandleFunc("/order.v1.OrderService/GenerateInvoice", h.GenerateInvoice)
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

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	o := &domain.Order{
		TenantID:        req.TenantID,
		CustomerID:      req.CustomerID,
		Currency:        req.Currency,
		ShippingAddress: req.ShippingAddress,
		Notes:           req.Notes,
		OrderedAt:       req.OrderedAt,
		CreatedBy:       req.CreatedBy,
	}
	result, err := h.svc.CreateOrder(r.Context(), o)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	var req IDTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetOrder(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AddOrderItem(w http.ResponseWriter, r *http.Request) {
	var req AddOrderItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item := &domain.OrderItem{
		TenantID:  req.TenantID,
		OrderID:   req.OrderID,
		SKUID:     req.SKUID,
		ProductID: req.ProductID,
		Quantity:  req.Quantity,
		UnitPrice: req.UnitPrice,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.AddOrderItem(r.Context(), item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ConfirmOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ConfirmOrder(r.Context(), req.ID, req.TenantID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.CancelOrder(r.Context(), req.ID, req.TenantID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GenerateInvoice(w http.ResponseWriter, r *http.Request) {
	var req GenerateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GenerateInvoice(r.Context(), req.OrderID, req.TenantID, req.CreatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
