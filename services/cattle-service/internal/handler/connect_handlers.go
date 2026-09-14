package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/cattle-service/internal/domain"
	"github.com/ppusapati/gavya/services/cattle-service/internal/repository"
	"github.com/ppusapati/gavya/services/cattle-service/internal/service"
)

// classify maps a failure onto the code that describes it.
//
// Every procedure here used to choose its code by which procedure it was rather
// than by what had happened. CreateCattle and CreateBreed reported everything as
// a bad request, so an unreachable database looked like a typo. GetCattle
// reported everything as not-found, so during an outage a caller was told their
// animal does not exist — the one answer they will act on by going to look for
// it. ListCattle, ListBreeds and DeleteCattle reported everything as internal,
// so a missing tenant_id looked worth retrying.
//
// The repository had nothing to match on either: every read wrapped
// pgx.ErrNoRows in a formatted string. It has a named ErrNotFound now, and the
// service marks its caller mistakes, so this can ask what went wrong instead of
// where.
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

// ─── Request / Response types ─────────────────────────────────────────────────
// These mirror the proto messages. Replace with buf-generated types after buf generate.

type CreateCattleRequest struct {
	TenantID  string `json:"tenant_id"`
	TagNumber string `json:"tag_number"`
	Name      string `json:"name"`
	BreedID   string `json:"breed_id"`
	Gender    string `json:"gender"`
	// Weight is read from the digits that were sent, whether as a JSON string
	// or a bare number, and goes back out as a decimal literal.
	Weight    exact.Fixed `json:"weight"`
	CreatedBy string      `json:"created_by"`
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
	ID        string      `json:"id"`
	TenantID  string      `json:"tenant_id"`
	Status    string      `json:"status"`
	Weight    exact.Fixed `json:"weight"`
	UpdatedBy string      `json:"updated_by"`
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
	ID        string      `json:"id"`
	TenantID  string      `json:"tenant_id"`
	TagNumber string      `json:"tag_number"`
	Name      string      `json:"name"`
	BreedID   string      `json:"breed_id"`
	Gender    string      `json:"gender"`
	Status    string      `json:"status"`
	Weight    exact.Fixed `json:"weight"`
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

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "cattle.v1.CattleService"

// Register mounts all routes on the given mux.
func (h *Handler) Register(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateCattle", connectjson.Unary(h.CreateCattle))
	route("GetCattle", connectjson.Unary(h.GetCattle))
	route("ListCattle", connectjson.Unary(h.ListCattle))
	route("UpdateCattle", connectjson.Unary(h.UpdateCattle))
	route("DeleteCattle", connectjson.Unary(h.DeleteCattle))
	route("CreateBreed", connectjson.Unary(h.CreateBreed))
	route("ListBreeds", connectjson.Unary(h.ListBreeds))
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
		return nil, classify(err)
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
		return nil, classify(err)
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
		return nil, classify(err)
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
		return nil, classify(err)
	}
	return connect.NewResponse(&UpdateCattleResponse{Cattle: cattleToProto(updated)}), nil
}

func (h *Handler) DeleteCattle(
	ctx context.Context,
	req *connect.Request[DeleteCattleRequest],
) (*connect.Response[DeleteCattleResponse], error) {
	msg := req.Msg
	if err := h.svc.DeleteCattle(ctx, msg.ID, msg.TenantID, msg.DeletedBy); err != nil {
		return nil, classify(err)
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
		return nil, classify(err)
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
		return nil, classify(err)
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
