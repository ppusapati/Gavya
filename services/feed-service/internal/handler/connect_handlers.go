package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/feed-service/internal/domain"
	"github.com/ppusapati/gavya/services/feed-service/internal/repository"
	"github.com/ppusapati/gavya/services/feed-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "feed.v1.FeedService"

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateFeedType", connectjson.Unary(h.CreateFeedType))
	route("ListFeedTypes", connectjson.Unary(h.ListFeedTypes))
	route("CreateNutritionPlan", connectjson.Unary(h.CreateNutritionPlan))
	route("GetNutritionPlan", connectjson.Unary(h.GetNutritionPlan))
	route("RecordFeedConsumption", connectjson.Unary(h.RecordFeedConsumption))
	route("GetFeedConsumptionReport", connectjson.Unary(h.GetFeedConsumptionReport))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing plan from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateFeedTypeRequest struct {
	TenantID        string `json:"tenant_id"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	Unit            string `json:"unit"`
	NutritionalInfo string `json:"nutritional_info"`
	CreatedBy       string `json:"created_by"`
}

type FeedTypeResponse struct {
	FeedType *domain.FeedType `json:"feed_type"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListFeedTypesResponse struct {
	FeedTypes []*domain.FeedType `json:"feed_types"`
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

type NutritionPlanResponse struct {
	NutritionPlan *domain.NutritionPlan `json:"nutrition_plan"`
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

type FeedConsumptionResponse struct {
	Consumption *domain.FeedConsumption `json:"consumption"`
}

type GetFeedConsumptionReportRequest struct {
	TenantID string    `json:"tenant_id"`
	CattleID string    `json:"cattle_id"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

type GetFeedConsumptionReportResponse struct {
	Entries []*domain.FeedConsumptionReport `json:"entries"`
}

func (h *Handler) CreateFeedType(ctx context.Context, req *connect.Request[CreateFeedTypeRequest]) (*connect.Response[FeedTypeResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateFeedType(ctx, &domain.FeedType{
		TenantID:        m.TenantID,
		Name:            m.Name,
		Category:        m.Category,
		Unit:            m.Unit,
		NutritionalInfo: m.NutritionalInfo,
		CreatedBy:       m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FeedTypeResponse{FeedType: out}), nil
}

func (h *Handler) ListFeedTypes(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListFeedTypesResponse], error) {
	out, err := h.svc.ListFeedTypes(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListFeedTypesResponse{FeedTypes: out}), nil
}

func (h *Handler) CreateNutritionPlan(ctx context.Context, req *connect.Request[CreateNutritionPlanRequest]) (*connect.Response[NutritionPlanResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateNutritionPlan(ctx, &domain.NutritionPlan{
		TenantID:        m.TenantID,
		CattleID:        m.CattleID,
		FeedTypeID:      m.FeedTypeID,
		DailyQuantityKg: m.DailyQuantityKg,
		StartDate:       m.StartDate,
		EndDate:         m.EndDate,
		Notes:           m.Notes,
		CreatedBy:       m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NutritionPlanResponse{NutritionPlan: out}), nil
}

func (h *Handler) GetNutritionPlan(ctx context.Context, req *connect.Request[GetNutritionPlanRequest]) (*connect.Response[NutritionPlanResponse], error) {
	out, err := h.svc.GetNutritionPlan(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&NutritionPlanResponse{NutritionPlan: out}), nil
}

func (h *Handler) RecordFeedConsumption(ctx context.Context, req *connect.Request[RecordFeedConsumptionRequest]) (*connect.Response[FeedConsumptionResponse], error) {
	m := req.Msg
	out, err := h.svc.RecordFeedConsumption(ctx, &domain.FeedConsumption{
		TenantID:   m.TenantID,
		CattleID:   m.CattleID,
		FeedTypeID: m.FeedTypeID,
		QuantityKg: m.QuantityKg,
		FedAt:      m.FedAt,
		FedBy:      m.FedBy,
		CreatedBy:  m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FeedConsumptionResponse{Consumption: out}), nil
}

func (h *Handler) GetFeedConsumptionReport(ctx context.Context, req *connect.Request[GetFeedConsumptionReportRequest]) (*connect.Response[GetFeedConsumptionReportResponse], error) {
	m := req.Msg
	out, err := h.svc.GetFeedConsumptionReport(ctx, m.TenantID, m.CattleID, m.From, m.To)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetFeedConsumptionReportResponse{Entries: out}), nil
}
