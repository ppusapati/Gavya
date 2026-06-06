package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/services/feed-service/internal/domain"
	"github.com/ppusapati/gavya/services/feed-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/feed.v1.FeedService/CreateFeedType", h.CreateFeedType)
	mux.HandleFunc("/feed.v1.FeedService/ListFeedTypes", h.ListFeedTypes)
	mux.HandleFunc("/feed.v1.FeedService/CreateNutritionPlan", h.CreateNutritionPlan)
	mux.HandleFunc("/feed.v1.FeedService/GetNutritionPlan", h.GetNutritionPlan)
	mux.HandleFunc("/feed.v1.FeedService/RecordFeedConsumption", h.RecordFeedConsumption)
	mux.HandleFunc("/feed.v1.FeedService/GetFeedConsumptionReport", h.GetFeedConsumptionReport)
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

type CreateFeedTypeRequest struct {
	TenantID       string `json:"tenant_id"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	Unit           string `json:"unit"`
	NutritionalInfo string `json:"nutritional_info"`
	CreatedBy      string `json:"created_by"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type CreateNutritionPlanRequest struct {
	TenantID        string     `json:"tenant_id"`
	CattleID        string     `json:"cattle_id"`
	FeedTypeID      string     `json:"feed_type_id"`
	DailyQuantityKg float64    `json:"daily_quantity_kg"`
	StartDate       time.Time  `json:"start_date"`
	EndDate         *time.Time `json:"end_date"`
	Notes           string     `json:"notes"`
	CreatedBy       string     `json:"created_by"`
}

type GetNutritionPlanRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type RecordFeedConsumptionRequest struct {
	TenantID   string    `json:"tenant_id"`
	CattleID   string    `json:"cattle_id"`
	FeedTypeID string    `json:"feed_type_id"`
	QuantityKg float64   `json:"quantity_kg"`
	FedAt      time.Time `json:"fed_at"`
	FedBy      string    `json:"fed_by"`
	CreatedBy  string    `json:"created_by"`
}

type GetFeedConsumptionReportRequest struct {
	TenantID string    `json:"tenant_id"`
	CattleID string    `json:"cattle_id"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

func (h *Handler) CreateFeedType(w http.ResponseWriter, r *http.Request) {
	var req CreateFeedTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f := &domain.FeedType{
		TenantID:       req.TenantID,
		Name:           req.Name,
		Category:       req.Category,
		Unit:           req.Unit,
		NutritionalInfo: req.NutritionalInfo,
		CreatedBy:      req.CreatedBy,
	}
	result, err := h.svc.CreateFeedType(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListFeedTypes(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListFeedTypes(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateNutritionPlan(w http.ResponseWriter, r *http.Request) {
	var req CreateNutritionPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := &domain.NutritionPlan{
		TenantID:        req.TenantID,
		CattleID:        req.CattleID,
		FeedTypeID:      req.FeedTypeID,
		DailyQuantityKg: req.DailyQuantityKg,
		StartDate:       req.StartDate,
		EndDate:         req.EndDate,
		Notes:           req.Notes,
		CreatedBy:       req.CreatedBy,
	}
	result, err := h.svc.CreateNutritionPlan(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetNutritionPlan(w http.ResponseWriter, r *http.Request) {
	var req GetNutritionPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetNutritionPlan(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RecordFeedConsumption(w http.ResponseWriter, r *http.Request) {
	var req RecordFeedConsumptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c := &domain.FeedConsumption{
		TenantID:   req.TenantID,
		CattleID:   req.CattleID,
		FeedTypeID: req.FeedTypeID,
		QuantityKg: req.QuantityKg,
		FedAt:      req.FedAt,
		FedBy:      req.FedBy,
		CreatedBy:  req.CreatedBy,
	}
	result, err := h.svc.RecordFeedConsumption(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetFeedConsumptionReport(w http.ResponseWriter, r *http.Request) {
	var req GetFeedConsumptionReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetFeedConsumptionReport(r.Context(), req.TenantID, req.CattleID, req.From, req.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
