package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
	"github.com/ppusapati/gavya/services/breeding-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/breeding.v1.BreedingService/CreateBreedingCycle", h.CreateBreedingCycle)
	mux.HandleFunc("/breeding.v1.BreedingService/RecordInsemination", h.RecordInsemination)
	mux.HandleFunc("/breeding.v1.BreedingService/ConfirmPregnancy", h.ConfirmPregnancy)
	mux.HandleFunc("/breeding.v1.BreedingService/RecordCalving", h.RecordCalving)
	mux.HandleFunc("/breeding.v1.BreedingService/GetBreedingHistory", h.GetBreedingHistory)
	mux.HandleFunc("/breeding.v1.BreedingService/ListActivePregnancies", h.ListActivePregnancies)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Request/Response types

type CreateBreedingCycleRequest struct {
	TenantID  string    `json:"tenant_id"`
	CattleID  string    `json:"cattle_id"`
	HeatDate  time.Time `json:"heat_date"`
	Status    string    `json:"status"`
	Notes     string    `json:"notes"`
	CreatedBy string    `json:"created_by"`
}

type RecordInseminationRequest struct {
	TenantID      string    `json:"tenant_id"`
	CycleID       string    `json:"cycle_id"`
	CattleID      string    `json:"cattle_id"`
	BullID        *string   `json:"bull_id"`
	SemenBatchID  *string   `json:"semen_batch_id"`
	InseminatedAt time.Time `json:"inseminated_at"`
	Method        string    `json:"method"`
	CreatedBy     string    `json:"created_by"`
}

type ConfirmPregnancyRequest struct {
	TenantID             string    `json:"tenant_id"`
	CattleID             string    `json:"cattle_id"`
	InseminationID       string    `json:"insemination_id"`
	ConfirmedAt          time.Time `json:"confirmed_at"`
	ExpectedCalvingDate  time.Time `json:"expected_calving_date"`
	CreatedBy            string    `json:"created_by"`
}

type RecordCalvingRequest struct {
	TenantID      string   `json:"tenant_id"`
	PregnancyID   string   `json:"pregnancy_id"`
	CattleID      string   `json:"cattle_id"`
	CalfID        *string  `json:"calf_id"`
	CalfGender    string   `json:"calf_gender"`
	CalfWeight    float64  `json:"calf_weight"`
	Complications string   `json:"complications"`
	Status        string   `json:"status"`
	CreatedBy     string   `json:"created_by"`
}

type GetBreedingHistoryRequest struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}

type ListActivePregnanciesRequest struct {
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

func (h *Handler) CreateBreedingCycle(w http.ResponseWriter, r *http.Request) {
	var req CreateBreedingCycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b := &domain.BreedingCycle{
		TenantID:  req.TenantID,
		CattleID:  req.CattleID,
		HeatDate:  req.HeatDate,
		Status:    req.Status,
		Notes:     req.Notes,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.CreateBreedingCycle(r.Context(), b)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RecordInsemination(w http.ResponseWriter, r *http.Request) {
	var req RecordInseminationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ins := &domain.Insemination{
		TenantID:      req.TenantID,
		CycleID:       req.CycleID,
		CattleID:      req.CattleID,
		BullID:        req.BullID,
		SemenBatchID:  req.SemenBatchID,
		InseminatedAt: req.InseminatedAt,
		Method:        req.Method,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.RecordInsemination(r.Context(), ins)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ConfirmPregnancy(w http.ResponseWriter, r *http.Request) {
	var req ConfirmPregnancyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := &domain.Pregnancy{
		TenantID:            req.TenantID,
		CattleID:            req.CattleID,
		InseminationID:      req.InseminationID,
		ConfirmedAt:         req.ConfirmedAt,
		ExpectedCalvingDate: req.ExpectedCalvingDate,
		CreatedBy:           req.CreatedBy,
	}
	result, err := h.svc.ConfirmPregnancy(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RecordCalving(w http.ResponseWriter, r *http.Request) {
	var req RecordCalvingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c := &domain.CalvingRecord{
		TenantID:      req.TenantID,
		PregnancyID:   req.PregnancyID,
		CattleID:      req.CattleID,
		CalfID:        req.CalfID,
		CalfGender:    req.CalfGender,
		CalfWeight:    req.CalfWeight,
		Complications: req.Complications,
		Status:        req.Status,
		CreatedBy:     req.CreatedBy,
	}
	result, err := h.svc.RecordCalving(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetBreedingHistory(w http.ResponseWriter, r *http.Request) {
	var req GetBreedingHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetBreedingHistory(r.Context(), req.TenantID, req.CattleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListActivePregnancies(w http.ResponseWriter, r *http.Request) {
	var req ListActivePregnanciesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListActivePregnancies(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
