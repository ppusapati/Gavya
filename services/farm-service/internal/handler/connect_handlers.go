package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/farm-service/internal/domain"
	"github.com/ppusapati/gavya/services/farm-service/internal/repository"
	"github.com/ppusapati/gavya/services/farm-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "farm.v1.FarmService"

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

	route("CreateFarm", connectjson.Unary(h.CreateFarm))
	route("GetFarm", connectjson.Unary(h.GetFarm))
	route("ListFarms", connectjson.Unary(h.ListFarms))
	route("UpdateFarm", connectjson.Unary(h.UpdateFarm))
	route("CreateFarmSection", connectjson.Unary(h.CreateFarmSection))
	route("ListFarmSections", connectjson.Unary(h.ListFarmSections))
	route("UpdateFarmCapacity", connectjson.Unary(h.UpdateFarmCapacity))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing farm from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateCode):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
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

type FarmResponse struct {
	Farm *domain.Farm `json:"farm"`
}

type GetFarmRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListFarmsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListFarmsResponse struct {
	Farms []*domain.Farm `json:"farms"`
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

type FarmSectionResponse struct {
	Section *domain.FarmSection `json:"section"`
}

type ListFarmSectionsRequest struct {
	TenantID string `json:"tenant_id"`
	FarmID   string `json:"farm_id"`
}

type ListFarmSectionsResponse struct {
	Sections []*domain.FarmSection `json:"sections"`
}

type UpdateFarmCapacityRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Capacity  int    `json:"capacity"`
	UpdatedBy string `json:"updated_by"`
}

func (h *Handler) CreateFarm(ctx context.Context, req *connect.Request[CreateFarmRequest]) (*connect.Response[FarmResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateFarm(ctx, &domain.Farm{
		TenantID:  m.TenantID,
		Name:      m.Name,
		Code:      m.Code,
		Address:   m.Address,
		City:      m.City,
		State:     m.State,
		Country:   m.Country,
		Capacity:  m.Capacity,
		ManagerID: m.ManagerID,
		Status:    m.Status,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FarmResponse{Farm: out}), nil
}

func (h *Handler) GetFarm(ctx context.Context, req *connect.Request[GetFarmRequest]) (*connect.Response[FarmResponse], error) {
	out, err := h.svc.GetFarm(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FarmResponse{Farm: out}), nil
}

func (h *Handler) ListFarms(ctx context.Context, req *connect.Request[ListFarmsRequest]) (*connect.Response[ListFarmsResponse], error) {
	out, err := h.svc.ListFarms(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListFarmsResponse{Farms: out}), nil
}

func (h *Handler) UpdateFarm(ctx context.Context, req *connect.Request[UpdateFarmRequest]) (*connect.Response[FarmResponse], error) {
	m := req.Msg
	out, err := h.svc.UpdateFarm(ctx, &domain.Farm{
		ID:        m.ID,
		TenantID:  m.TenantID,
		Name:      m.Name,
		Address:   m.Address,
		City:      m.City,
		State:     m.State,
		Country:   m.Country,
		ManagerID: m.ManagerID,
		Status:    m.Status,
		UpdatedBy: m.UpdatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FarmResponse{Farm: out}), nil
}

func (h *Handler) CreateFarmSection(ctx context.Context, req *connect.Request[CreateFarmSectionRequest]) (*connect.Response[FarmSectionResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateFarmSection(ctx, &domain.FarmSection{
		TenantID:         m.TenantID,
		FarmID:           m.FarmID,
		Name:             m.Name,
		SectionType:      m.SectionType,
		Capacity:         m.Capacity,
		CurrentOccupancy: m.CurrentOccupancy,
		CreatedBy:        m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FarmSectionResponse{Section: out}), nil
}

func (h *Handler) ListFarmSections(ctx context.Context, req *connect.Request[ListFarmSectionsRequest]) (*connect.Response[ListFarmSectionsResponse], error) {
	out, err := h.svc.ListFarmSections(ctx, req.Msg.TenantID, req.Msg.FarmID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListFarmSectionsResponse{Sections: out}), nil
}

func (h *Handler) UpdateFarmCapacity(ctx context.Context, req *connect.Request[UpdateFarmCapacityRequest]) (*connect.Response[FarmResponse], error) {
	m := req.Msg
	out, err := h.svc.UpdateFarmCapacity(ctx, m.ID, m.TenantID, m.Capacity, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FarmResponse{Farm: out}), nil
}
