package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/tenant-service/internal/domain"
	"github.com/ppusapati/gavya/services/tenant-service/internal/repository"
	"github.com/ppusapati/gavya/services/tenant-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "tenant.v1.TenantService"

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateTenant", connectjson.Unary(h.CreateTenant))
	route("GetTenant", connectjson.Unary(h.GetTenant))
	route("ListTenants", connectjson.Unary(h.ListTenants))
	route("UpdateTenant", connectjson.Unary(h.UpdateTenant))
	route("SuspendTenant", connectjson.Unary(h.SuspendTenant))
	route("ActivateTenant", connectjson.Unary(h.ActivateTenant))
	route("UpsertTenantSetting", connectjson.Unary(h.UpsertTenantSetting))
	route("ListTenantSettings", connectjson.Unary(h.ListTenantSettings))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing tenant from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateSlug):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
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

type TenantResponse struct {
	Tenant *domain.Tenant `json:"tenant"`
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

type ListTenantsRequest struct{}

type ListTenantsResponse struct {
	Tenants []*domain.Tenant `json:"tenants"`
}

type UpsertTenantSettingRequest struct {
	TenantID  string `json:"tenant_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	DataType  string `json:"data_type"`
	CreatedBy string `json:"created_by"`
}

type TenantSettingResponse struct {
	Setting *domain.TenantSetting `json:"setting"`
}

type ListTenantSettingsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListTenantSettingsResponse struct {
	Settings []*domain.TenantSetting `json:"settings"`
}

func (h *Handler) CreateTenant(ctx context.Context, req *connect.Request[CreateTenantRequest]) (*connect.Response[TenantResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateTenant(ctx, &domain.Tenant{
		Name:         m.Name,
		Slug:         m.Slug,
		Plan:         m.Plan,
		ContactEmail: m.ContactEmail,
		ContactPhone: m.ContactPhone,
		Address:      m.Address,
		Country:      m.Country,
		Timezone:     m.Timezone,
		Currency:     m.Currency,
		MaxUsers:     m.MaxUsers,
		MaxCattle:    m.MaxCattle,
		CreatedBy:    m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TenantResponse{Tenant: out}), nil
}

func (h *Handler) GetTenant(ctx context.Context, req *connect.Request[IDRequest]) (*connect.Response[TenantResponse], error) {
	out, err := h.svc.GetTenant(ctx, req.Msg.ID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TenantResponse{Tenant: out}), nil
}

func (h *Handler) ListTenants(ctx context.Context, req *connect.Request[ListTenantsRequest]) (*connect.Response[ListTenantsResponse], error) {
	out, err := h.svc.ListTenants(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListTenantsResponse{Tenants: out}), nil
}

func (h *Handler) UpdateTenant(ctx context.Context, req *connect.Request[UpdateTenantRequest]) (*connect.Response[TenantResponse], error) {
	m := req.Msg
	out, err := h.svc.UpdateTenant(ctx, &domain.Tenant{
		ID:           m.ID,
		Name:         m.Name,
		ContactEmail: m.ContactEmail,
		ContactPhone: m.ContactPhone,
		Address:      m.Address,
		Country:      m.Country,
		Timezone:     m.Timezone,
		Currency:     m.Currency,
		MaxUsers:     m.MaxUsers,
		MaxCattle:    m.MaxCattle,
		UpdatedBy:    m.UpdatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TenantResponse{Tenant: out}), nil
}

func (h *Handler) SuspendTenant(ctx context.Context, req *connect.Request[TenantActionRequest]) (*connect.Response[TenantResponse], error) {
	out, err := h.svc.SuspendTenant(ctx, req.Msg.ID, req.Msg.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TenantResponse{Tenant: out}), nil
}

func (h *Handler) ActivateTenant(ctx context.Context, req *connect.Request[TenantActionRequest]) (*connect.Response[TenantResponse], error) {
	out, err := h.svc.ActivateTenant(ctx, req.Msg.ID, req.Msg.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TenantResponse{Tenant: out}), nil
}

func (h *Handler) UpsertTenantSetting(ctx context.Context, req *connect.Request[UpsertTenantSettingRequest]) (*connect.Response[TenantSettingResponse], error) {
	m := req.Msg
	out, err := h.svc.UpsertTenantSetting(ctx, &domain.TenantSetting{
		TenantID:  m.TenantID,
		Key:       m.Key,
		Value:     m.Value,
		DataType:  m.DataType,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&TenantSettingResponse{Setting: out}), nil
}

func (h *Handler) ListTenantSettings(ctx context.Context, req *connect.Request[ListTenantSettingsRequest]) (*connect.Response[ListTenantSettingsResponse], error) {
	out, err := h.svc.ListTenantSettings(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListTenantSettingsResponse{Settings: out}), nil
}
