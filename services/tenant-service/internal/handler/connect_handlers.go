package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/tenant-service/internal/domain"
	"github.com/ppusapati/gavya/services/tenant-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/tenant.v1.TenantService/CreateTenant", h.CreateTenant)
	mux.HandleFunc("/tenant.v1.TenantService/GetTenant", h.GetTenant)
	mux.HandleFunc("/tenant.v1.TenantService/ListTenants", h.ListTenants)
	mux.HandleFunc("/tenant.v1.TenantService/UpdateTenant", h.UpdateTenant)
	mux.HandleFunc("/tenant.v1.TenantService/SuspendTenant", h.SuspendTenant)
	mux.HandleFunc("/tenant.v1.TenantService/ActivateTenant", h.ActivateTenant)
	mux.HandleFunc("/tenant.v1.TenantService/UpsertTenantSetting", h.UpsertTenantSetting)
	mux.HandleFunc("/tenant.v1.TenantService/ListTenantSettings", h.ListTenantSettings)
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

type CreateTenantRequest struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Plan         string `json:"plan"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	Address      string `json:"address"`
	Country      string `json:"country"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	MaxUsers     int    `json:"max_users"`
	MaxCattle    int    `json:"max_cattle"`
	CreatedBy    string `json:"created_by"`
}

type IDRequest struct {
	ID string `json:"id"`
}

type TenantActionRequest struct {
	ID        string `json:"id"`
	UpdatedBy string `json:"updated_by"`
}

type UpdateTenantRequest struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	Address      string `json:"address"`
	Country      string `json:"country"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	MaxUsers     int    `json:"max_users"`
	MaxCattle    int    `json:"max_cattle"`
	UpdatedBy    string `json:"updated_by"`
}

type UpsertTenantSettingRequest struct {
	TenantID  string `json:"tenant_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	DataType  string `json:"data_type"`
	CreatedBy string `json:"created_by"`
}

type ListTenantSettingsRequest struct {
	TenantID string `json:"tenant_id"`
}

func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t := &domain.Tenant{
		Name:         req.Name,
		Slug:         req.Slug,
		Plan:         req.Plan,
		ContactEmail: req.ContactEmail,
		ContactPhone: req.ContactPhone,
		Address:      req.Address,
		Country:      req.Country,
		Timezone:     req.Timezone,
		Currency:     req.Currency,
		MaxUsers:     req.MaxUsers,
		MaxCattle:    req.MaxCattle,
		CreatedBy:    req.CreatedBy,
	}
	result, err := h.svc.CreateTenant(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	var req IDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetTenant(r.Context(), req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListTenants(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	var req UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t := &domain.Tenant{
		ID:           req.ID,
		Name:         req.Name,
		ContactEmail: req.ContactEmail,
		ContactPhone: req.ContactPhone,
		Address:      req.Address,
		Country:      req.Country,
		Timezone:     req.Timezone,
		Currency:     req.Currency,
		MaxUsers:     req.MaxUsers,
		MaxCattle:    req.MaxCattle,
		UpdatedBy:    req.UpdatedBy,
	}
	result, err := h.svc.UpdateTenant(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) SuspendTenant(w http.ResponseWriter, r *http.Request) {
	var req TenantActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.SuspendTenant(r.Context(), req.ID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ActivateTenant(w http.ResponseWriter, r *http.Request) {
	var req TenantActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ActivateTenant(r.Context(), req.ID, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpsertTenantSetting(w http.ResponseWriter, r *http.Request) {
	var req UpsertTenantSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s := &domain.TenantSetting{
		TenantID:  req.TenantID,
		Key:       req.Key,
		Value:     req.Value,
		DataType:  req.DataType,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.UpsertTenantSetting(r.Context(), s)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListTenantSettings(w http.ResponseWriter, r *http.Request) {
	var req ListTenantSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListTenantSettings(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
