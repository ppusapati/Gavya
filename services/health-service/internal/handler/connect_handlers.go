package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/services/health-service/internal/domain"
	"github.com/ppusapati/gavya/services/health-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/health.v1.HealthService/RecordVaccination", h.RecordVaccination)
	mux.HandleFunc("/health.v1.HealthService/GetVaccinationHistory", h.GetVaccinationHistory)
	mux.HandleFunc("/health.v1.HealthService/RecordTreatment", h.RecordTreatment)
	mux.HandleFunc("/health.v1.HealthService/GetTreatmentHistory", h.GetTreatmentHistory)
	mux.HandleFunc("/health.v1.HealthService/ScheduleVetVisit", h.ScheduleVetVisit)
	mux.HandleFunc("/health.v1.HealthService/ListUpcomingVaccinations", h.ListUpcomingVaccinations)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type RecordVaccinationRequest struct {
	TenantID       string     `json:"tenant_id"`
	CattleID       string     `json:"cattle_id"`
	VaccineName    string     `json:"vaccine_name"`
	BatchNumber    string     `json:"batch_number"`
	AdministeredAt time.Time  `json:"administered_at"`
	NextDueDate    *time.Time `json:"next_due_date"`
	VeterinarianID string     `json:"veterinarian_id"`
	Dosage         string     `json:"dosage"`
	CreatedBy      string     `json:"created_by"`
}

type RecordTreatmentRequest struct {
	TenantID      string     `json:"tenant_id"`
	CattleID      string     `json:"cattle_id"`
	DiagnosisCode string     `json:"diagnosis_code"`
	Diagnosis     string     `json:"diagnosis"`
	MedicineName  string     `json:"medicine_name"`
	Dosage        string     `json:"dosage"`
	TreatedAt     time.Time  `json:"treated_at"`
	TreatedBy     string     `json:"treated_by"`
	FollowUpDate  *time.Time `json:"follow_up_date"`
	Status        string     `json:"status"`
	CreatedBy     string     `json:"created_by"`
}

type ScheduleVetVisitRequest struct {
	TenantID       string    `json:"tenant_id"`
	CattleID       string    `json:"cattle_id"`
	VeterinarianID string    `json:"veterinarian_id"`
	VisitDate      time.Time `json:"visit_date"`
	Purpose        string    `json:"purpose"`
	Notes          string    `json:"notes"`
	Cost           float64   `json:"cost"`
	CreatedBy      string    `json:"created_by"`
}

type HistoryRequest struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) RecordVaccination(w http.ResponseWriter, r *http.Request) {
	var req RecordVaccinationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v := &domain.Vaccination{
		TenantID:       req.TenantID,
		CattleID:       req.CattleID,
		VaccineName:    req.VaccineName,
		BatchNumber:    req.BatchNumber,
		AdministeredAt: req.AdministeredAt,
		NextDueDate:    req.NextDueDate,
		VeterinarianID: req.VeterinarianID,
		Dosage:         req.Dosage,
		CreatedBy:      req.CreatedBy,
	}
	result, err := h.svc.RecordVaccination(r.Context(), v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetVaccinationHistory(w http.ResponseWriter, r *http.Request) {
	var req HistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetVaccinationHistory(r.Context(), req.TenantID, req.CattleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RecordTreatment(w http.ResponseWriter, r *http.Request) {
	var req RecordTreatmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t := &domain.Treatment{
		TenantID:      req.TenantID,
		CattleID:      req.CattleID,
		DiagnosisCode: req.DiagnosisCode,
		Diagnosis:     req.Diagnosis,
		MedicineName:  req.MedicineName,
		Dosage:        req.Dosage,
		TreatedAt:     req.TreatedAt,
		TreatedBy:     req.TreatedBy,
		FollowUpDate:  req.FollowUpDate,
		Status:        req.Status,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.RecordTreatment(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetTreatmentHistory(w http.ResponseWriter, r *http.Request) {
	var req HistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetTreatmentHistory(r.Context(), req.TenantID, req.CattleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ScheduleVetVisit(w http.ResponseWriter, r *http.Request) {
	var req ScheduleVetVisitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v := &domain.VetVisit{
		TenantID:       req.TenantID,
		CattleID:       req.CattleID,
		VeterinarianID: req.VeterinarianID,
		VisitDate:      req.VisitDate,
		Purpose:        req.Purpose,
		Notes:          req.Notes,
		Cost:           req.Cost,
		CreatedBy:      req.CreatedBy,
	}
	result, err := h.svc.ScheduleVetVisit(r.Context(), v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListUpcomingVaccinations(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListUpcomingVaccinations(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
