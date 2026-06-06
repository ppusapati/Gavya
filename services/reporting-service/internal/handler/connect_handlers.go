package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/reporting-service/internal/service"
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
	mux.HandleFunc("/reporting.v1.ReportingService/RequestReport", h.requestReport)
	mux.HandleFunc("/reporting.v1.ReportingService/GetReport", h.getReport)
	mux.HandleFunc("/reporting.v1.ReportingService/ListReports", h.listReports)
	mux.HandleFunc("/reporting.v1.ReportingService/GetReportDownloadURL", h.getReportDownloadURL)
	mux.HandleFunc("/reporting.v1.ReportingService/CreateSchedule", h.createSchedule)
	mux.HandleFunc("/reporting.v1.ReportingService/ListSchedules", h.listSchedules)
	mux.HandleFunc("/reporting.v1.ReportingService/UpdateSchedule", h.updateSchedule)
	mux.HandleFunc("/reporting.v1.ReportingService/DeleteSchedule", h.deleteSchedule)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) requestReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID    string `json:"tenant_id"`
		Name        string `json:"name"`
		ReportType  string `json:"report_type"`
		Parameters  string `json:"parameters"`
		FileFormat  string `json:"file_format"`
		RequestedBy string `json:"requested_by"`
		CreatedBy   string `json:"created_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rep, err := h.svc.RequestReport(r.Context(), req.TenantID, req.Name, req.ReportType, req.Parameters, req.FileFormat, req.RequestedBy, req.CreatedBy)
	if err != nil {
		h.log.Errorf("RequestReport: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rep)
}

func (h *Handler) getReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rep, err := h.svc.GetReport(r.Context(), req.ID, req.TenantID)
	if err != nil {
		h.log.Errorf("GetReport: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rep)
}

func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reports, err := h.svc.ListReports(r.Context(), req.TenantID)
	if err != nil {
		h.log.Errorf("ListReports: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reports)
}

func (h *Handler) getReportDownloadURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	url, err := h.svc.GetReportDownloadURL(r.Context(), req.ID, req.TenantID)
	if err != nil {
		h.log.Errorf("GetReportDownloadURL: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": url})
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID   string `json:"tenant_id"`
		ReportType string `json:"report_type"`
		Schedule   string `json:"schedule"`
		Parameters string `json:"parameters"`
		CreatedBy  string `json:"created_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sched, err := h.svc.CreateSchedule(r.Context(), req.TenantID, req.ReportType, req.Schedule, req.Parameters, req.CreatedBy)
	if err != nil {
		h.log.Errorf("CreateSchedule: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sched)
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scheds, err := h.svc.ListSchedules(r.Context(), req.TenantID)
	if err != nil {
		h.log.Errorf("ListSchedules: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scheds)
}

func (h *Handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		TenantID  string `json:"tenant_id"`
		IsActive  bool   `json:"is_active"`
		UpdatedBy string `json:"updated_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sched, err := h.svc.UpdateSchedule(r.Context(), req.ID, req.TenantID, req.IsActive, req.UpdatedBy)
	if err != nil {
		h.log.Errorf("UpdateSchedule: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sched)
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		TenantID  string `json:"tenant_id"`
		UpdatedBy string `json:"updated_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.DeleteSchedule(r.Context(), req.ID, req.TenantID, req.UpdatedBy); err != nil {
		h.log.Errorf("DeleteSchedule: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
