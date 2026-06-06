package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/audit-service/internal/service"
	"p9e.in/samavaya/packages/p9log"
)

type Handler struct {
	svc *service.Service
	log *p9log.Helper
}

func New(svc *service.Service, log *p9log.Helper) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/audit.v1.AuditService/CreateAuditLog", h.createAuditLog)
	mux.HandleFunc("/audit.v1.AuditService/GetAuditLog", h.getAuditLog)
	mux.HandleFunc("/audit.v1.AuditService/ListAuditLogs", h.listAuditLogs)
	mux.HandleFunc("/audit.v1.AuditService/ListAuditLogsByResource", h.listAuditLogsByResource)
	mux.HandleFunc("/audit.v1.AuditService/ListAuditLogsByActor", h.listAuditLogsByActor)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) createAuditLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID     string `json:"tenant_id"`
		ActorID      string `json:"actor_id"`
		ActorType    string `json:"actor_type"`
		Action       string `json:"action"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		OldValue     string `json:"old_value"`
		NewValue     string `json:"new_value"`
		IPAddress    string `json:"ip_address"`
		UserAgent    string `json:"user_agent"`
		ServiceName  string `json:"service_name"`
		TraceID      string `json:"trace_id"`
		CreatedBy    string `json:"created_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log, err := h.svc.CreateAuditLog(r.Context(), req.TenantID, req.ActorID, req.ActorType, req.Action,
		req.ResourceType, req.ResourceID, req.OldValue, req.NewValue, req.IPAddress,
		req.UserAgent, req.ServiceName, req.TraceID, req.CreatedBy)
	if err != nil {
		h.log.Errorf("CreateAuditLog: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(log)
}

func (h *Handler) getAuditLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a, err := h.svc.GetAuditLog(r.Context(), req.ID, req.TenantID)
	if err != nil {
		h.log.Errorf("GetAuditLog: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

func (h *Handler) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logs, err := h.svc.ListAuditLogs(r.Context(), req.TenantID)
	if err != nil {
		h.log.Errorf("ListAuditLogs: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (h *Handler) listAuditLogsByResource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID     string `json:"tenant_id"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logs, err := h.svc.ListAuditLogsByResource(r.Context(), req.TenantID, req.ResourceType, req.ResourceID)
	if err != nil {
		h.log.Errorf("ListAuditLogsByResource: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (h *Handler) listAuditLogsByActor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
		ActorID  string `json:"actor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logs, err := h.svc.ListAuditLogsByActor(r.Context(), req.TenantID, req.ActorID)
	if err != nil {
		h.log.Errorf("ListAuditLogsByActor: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
