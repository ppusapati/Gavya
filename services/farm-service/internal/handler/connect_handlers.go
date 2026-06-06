package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/farm-service/internal/domain"
	"github.com/ppusapati/gavya/services/farm-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/farm.v1.FarmService/CreateFarm", h.CreateFarm)
	mux.HandleFunc("/farm.v1.FarmService/GetFarm", h.GetFarm)
	mux.HandleFunc("/farm.v1.FarmService/ListFarms", h.ListFarms)
	mux.HandleFunc("/farm.v1.FarmService/UpdateFarm", h.UpdateFarm)
	mux.HandleFunc("/farm.v1.FarmService/CreateFarmSection", h.CreateFarmSection)
	mux.HandleFunc("/farm.v1.FarmService/ListFarmSections", h.ListFarmSections)
	mux.HandleFunc("/farm.v1.FarmService/UpdateFarmCapacity", h.UpdateFarmCapacity)
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

type CreateFarmRequest struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Address   string `json:"address"`
	City      string `json:"city"`
	State     string `json:"state"`
	Country   string `json:"country"`
	Capacity  int    `json:"capacity"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

type GetFarmRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListFarmsRequest struct {
	TenantID string `json:"tenant_id"`
}

type UpdateFarmRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	City      string `json:"city"`
	State     string `json:"state"`
	Country   string `json:"country"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
	UpdatedBy string `json:"updated_by"`
}

type CreateFarmSectionRequest struct {
	TenantID         string `json:"tenant_id"`
	FarmID           string `json:"farm_id"`
	Name             string `json:"name"`
	SectionType      string `json:"section_type"`
	Capacity         int    `json:"capacity"`
	CurrentOccupancy int    `json:"current_occupancy"`
	CreatedBy        string `json:"created_by"`
}

type ListFarmSectionsRequest struct {
	TenantID string `json:"tenant_id"`
	FarmID   string `json:"farm_id"`
}

type UpdateFarmCapacityRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Capacity  int    `json:"capacity"`
	UpdatedBy string `json:"updated_by"`
}

func (h *Handler) CreateFarm(w http.ResponseWriter, r *http.Request) {
	var req CreateFarmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f := &domain.Farm{
		TenantID:  req.TenantID,
		Name:      req.Name,
		Code:      req.Code,
		Address:   req.Address,
		City:      req.City,
		State:     req.State,
		Country:   req.Country,
		Capacity:  req.Capacity,
		ManagerID: req.ManagerID,
		Status:    req.Status,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.CreateFarm(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetFarm(w http.ResponseWriter, r *http.Request) {
	var req GetFarmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetFarm(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListFarms(w http.ResponseWriter, r *http.Request) {
	var req ListFarmsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListFarms(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateFarm(w http.ResponseWriter, r *http.Request) {
	var req UpdateFarmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f := &domain.Farm{
		ID:        req.ID,
		TenantID:  req.TenantID,
		Name:      req.Name,
		Address:   req.Address,
		City:      req.City,
		State:     req.State,
		Country:   req.Country,
		ManagerID: req.ManagerID,
		Status:    req.Status,
		UpdatedBy: req.UpdatedBy,
	}
	result, err := h.svc.UpdateFarm(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateFarmSection(w http.ResponseWriter, r *http.Request) {
	var req CreateFarmSectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sec := &domain.FarmSection{
		TenantID:         req.TenantID,
		FarmID:           req.FarmID,
		Name:             req.Name,
		SectionType:      req.SectionType,
		Capacity:         req.Capacity,
		CurrentOccupancy: req.CurrentOccupancy,
		CreatedBy:        req.CreatedBy,
	}
	result, err := h.svc.CreateFarmSection(r.Context(), sec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListFarmSections(w http.ResponseWriter, r *http.Request) {
	var req ListFarmSectionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListFarmSections(r.Context(), req.TenantID, req.FarmID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateFarmCapacity(w http.ResponseWriter, r *http.Request) {
	var req UpdateFarmCapacityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.UpdateFarmCapacity(r.Context(), req.ID, req.TenantID, req.Capacity, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
