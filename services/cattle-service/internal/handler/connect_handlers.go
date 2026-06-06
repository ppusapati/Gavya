package handler

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/services/cattle-service/internal/domain"
	"github.com/ppusapati/gavya/services/cattle-service/internal/service"
)

// ─── Request / Response types ─────────────────────────────────────────────────
// These mirror the proto messages. Replace with buf-generated types after buf generate.

type CreateCattleRequest struct {
	TenantID  string  `json:"tenant_id"`
	TagNumber string  `json:"tag_number"`
	Name      string  `json:"name"`
	BreedID   string  `json:"breed_id"`
	Gender    string  `json:"gender"`
	Weight    float64 `json:"weight"`
	CreatedBy string  `json:"created_by"`
}

type CreateCattleResponse struct {
	Cattle *CattleProto `json:"cattle"`
}

type GetCattleRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type GetCattleResponse struct {
	Cattle *CattleProto `json:"cattle"`
}

type ListCattleRequest struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type ListCattleResponse struct {
	Cattle []*CattleProto `json:"cattle"`
	Total  int32          `json:"total"`
}

type UpdateCattleRequest struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Status    string  `json:"status"`
	Weight    float64 `json:"weight"`
	UpdatedBy string  `json:"updated_by"`
}

type UpdateCattleResponse struct {
	Cattle *CattleProto `json:"cattle"`
}

type DeleteCattleRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	DeletedBy string `json:"deleted_by"`
}

type DeleteCattleResponse struct {
	Success bool `json:"success"`
}

type CreateBreedRequest struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
}

type CreateBreedResponse struct {
	Breed *BreedProto `json:"breed"`
}

type ListBreedsRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListBreedsResponse struct {
	Breeds []*BreedProto `json:"breeds"`
}

type CattleProto struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	TagNumber string  `json:"tag_number"`
	Name      string  `json:"name"`
	BreedID   string  `json:"breed_id"`
	Gender    string  `json:"gender"`
	Status    string  `json:"status"`
	Weight    float64 `json:"weight"`
}

type BreedProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
}

// ─── Handler ──────────────────────────────────────────────────────────────────

// Handler dispatches ConnectRPC requests to the service layer.
type Handler struct {
	svc *service.Service
}

// New creates a new Handler wrapping the given service.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts all routes on the given mux.
// After running `buf generate`, replace with the generated registration call:
//
//	mux.Handle(cattlev1connect.NewCattleServiceHandler(h))
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// ConnectRPC route registration placeholder — replace after buf generate:
	// path, handler := cattlev1connect.NewCattleServiceHandler(h)
	// mux.Handle(path, handler)
}

// ─── RPC implementations ──────────────────────────────────────────────────────

func (h *Handler) CreateCattle(
	ctx context.Context,
	req *connect.Request[CreateCattleRequest],
) (*connect.Response[CreateCattleResponse], error) {
	msg := req.Msg
	c := &domain.Cattle{
		TenantID:  msg.TenantID,
		TagNumber: msg.TagNumber,
		Name:      msg.Name,
		BreedID:   msg.BreedID,
		Gender:    msg.Gender,
		Weight:    msg.Weight,
		CreatedBy: msg.CreatedBy,
	}
	created, err := h.svc.CreateCattle(ctx, c)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&CreateCattleResponse{Cattle: cattleToProto(created)}), nil
}

func (h *Handler) GetCattle(
	ctx context.Context,
	req *connect.Request[GetCattleRequest],
) (*connect.Response[GetCattleResponse], error) {
	msg := req.Msg
	c, err := h.svc.GetCattle(ctx, msg.ID, msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&GetCattleResponse{Cattle: cattleToProto(c)}), nil
}

func (h *Handler) ListCattle(
	ctx context.Context,
	req *connect.Request[ListCattleRequest],
) (*connect.Response[ListCattleResponse], error) {
	msg := req.Msg
	list, err := h.svc.ListCattle(ctx, msg.TenantID, msg.Status, int(msg.Limit), int(msg.Offset))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	protos := make([]*CattleProto, 0, len(list))
	for _, c := range list {
		protos = append(protos, cattleToProto(c))
	}
	return connect.NewResponse(&ListCattleResponse{Cattle: protos, Total: int32(len(protos))}), nil
}

func (h *Handler) UpdateCattle(
	ctx context.Context,
	req *connect.Request[UpdateCattleRequest],
) (*connect.Response[UpdateCattleResponse], error) {
	msg := req.Msg
	c := &domain.Cattle{
		ID:        msg.ID,
		TenantID:  msg.TenantID,
		Status:    msg.Status,
		Weight:    msg.Weight,
		UpdatedBy: msg.UpdatedBy,
	}
	updated, err := h.svc.UpdateCattle(ctx, c)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&UpdateCattleResponse{Cattle: cattleToProto(updated)}), nil
}

func (h *Handler) DeleteCattle(
	ctx context.Context,
	req *connect.Request[DeleteCattleRequest],
) (*connect.Response[DeleteCattleResponse], error) {
	msg := req.Msg
	if err := h.svc.DeleteCattle(ctx, msg.ID, msg.TenantID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&DeleteCattleResponse{Success: true}), nil
}

func (h *Handler) CreateBreed(
	ctx context.Context,
	req *connect.Request[CreateBreedRequest],
) (*connect.Response[CreateBreedResponse], error) {
	msg := req.Msg
	b := &domain.Breed{
		TenantID:    msg.TenantID,
		Name:        msg.Name,
		Origin:      msg.Origin,
		Description: msg.Description,
		CreatedBy:   msg.CreatedBy,
	}
	created, err := h.svc.CreateBreed(ctx, b)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&CreateBreedResponse{Breed: breedToProto(created)}), nil
}

func (h *Handler) ListBreeds(
	ctx context.Context,
	req *connect.Request[ListBreedsRequest],
) (*connect.Response[ListBreedsResponse], error) {
	msg := req.Msg
	list, err := h.svc.ListBreeds(ctx, msg.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	protos := make([]*BreedProto, 0, len(list))
	for _, b := range list {
		protos = append(protos, breedToProto(b))
	}
	return connect.NewResponse(&ListBreedsResponse{Breeds: protos}), nil
}

// ─── Mapping helpers ──────────────────────────────────────────────────────────

func cattleToProto(c *domain.Cattle) *CattleProto {
	return &CattleProto{
		ID:        c.ID,
		TenantID:  c.TenantID,
		TagNumber: c.TagNumber,
		Name:      c.Name,
		BreedID:   c.BreedID,
		Gender:    c.Gender,
		Status:    c.Status,
		Weight:    c.Weight,
	}
}

func breedToProto(b *domain.Breed) *BreedProto {
	return &BreedProto{
		ID:          b.ID,
		TenantID:    b.TenantID,
		Name:        b.Name,
		Origin:      b.Origin,
		Description: b.Description,
	}
}
