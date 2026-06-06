package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
	"github.com/ppusapati/gavya/services/inventory-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/inventory.v1.InventoryService/CreateWarehouse", h.CreateWarehouse)
	mux.HandleFunc("/inventory.v1.InventoryService/GetWarehouse", h.GetWarehouse)
	mux.HandleFunc("/inventory.v1.InventoryService/ListWarehouses", h.ListWarehouses)
	mux.HandleFunc("/inventory.v1.InventoryService/AdjustStock", h.AdjustStock)
	mux.HandleFunc("/inventory.v1.InventoryService/ListStockMovements", h.ListStockMovements)
	mux.HandleFunc("/inventory.v1.InventoryService/CreateBatch", h.CreateBatch)
	mux.HandleFunc("/inventory.v1.InventoryService/GetBatch", h.GetBatch)
	mux.HandleFunc("/inventory.v1.InventoryService/ListExpiringBatches", h.ListExpiringBatches)
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

type CreateWarehouseRequest struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Address   string `json:"address"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type AdjustStockRequest struct {
	TenantID      string    `json:"tenant_id"`
	WarehouseID   string    `json:"warehouse_id"`
	SKUID         string    `json:"sku_id"`
	MovementType  string    `json:"movement_type"`
	Quantity      float64   `json:"quantity"`
	ReferenceID   string    `json:"reference_id"`
	ReferenceType string    `json:"reference_type"`
	Notes         string    `json:"notes"`
	MovedAt       time.Time `json:"moved_at"`
	MovedBy       string    `json:"moved_by"`
	CreatedBy     string    `json:"created_by"`
}

type ListStockMovementsRequest struct {
	TenantID    string `json:"tenant_id"`
	WarehouseID string `json:"warehouse_id"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

type CreateBatchRequest struct {
	TenantID       string     `json:"tenant_id"`
	WarehouseID    string     `json:"warehouse_id"`
	SKUID          string     `json:"sku_id"`
	BatchNumber    string     `json:"batch_number"`
	Quantity       float64    `json:"quantity"`
	ManufacturedAt *time.Time `json:"manufactured_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Status         string     `json:"status"`
	CreatedBy      string     `json:"created_by"`
}

func (h *Handler) CreateWarehouse(w http.ResponseWriter, r *http.Request) {
	var req CreateWarehouseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wh := &domain.Warehouse{
		TenantID:  req.TenantID,
		Name:      req.Name,
		Code:      req.Code,
		Address:   req.Address,
		ManagerID: req.ManagerID,
		Status:    req.Status,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.CreateWarehouse(r.Context(), wh)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetWarehouse(w http.ResponseWriter, r *http.Request) {
	var req IDTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetWarehouse(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListWarehouses(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListWarehouses(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AdjustStock(w http.ResponseWriter, r *http.Request) {
	var req AdjustStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m := &domain.StockMovement{
		TenantID:      req.TenantID,
		WarehouseID:   req.WarehouseID,
		SKUID:         req.SKUID,
		MovementType:  req.MovementType,
		Quantity:      req.Quantity,
		ReferenceID:   req.ReferenceID,
		ReferenceType: req.ReferenceType,
		Notes:         req.Notes,
		MovedAt:       req.MovedAt,
		MovedBy:       req.MovedBy,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.AdjustStock(r.Context(), m, req.CreatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListStockMovements(w http.ResponseWriter, r *http.Request) {
	var req ListStockMovementsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListStockMovements(r.Context(), req.TenantID, req.WarehouseID, req.Limit, req.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	var req CreateBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b := &domain.Batch{
		TenantID:       req.TenantID,
		WarehouseID:    req.WarehouseID,
		SKUID:          req.SKUID,
		BatchNumber:    req.BatchNumber,
		Quantity:       req.Quantity,
		ManufacturedAt: req.ManufacturedAt,
		ExpiresAt:      req.ExpiresAt,
		Status:         req.Status,
		CreatedBy:      req.CreatedBy,
	}
	result, err := h.svc.CreateBatch(r.Context(), b)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetBatch(w http.ResponseWriter, r *http.Request) {
	var req IDTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetBatch(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListExpiringBatches(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListExpiringBatches(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
