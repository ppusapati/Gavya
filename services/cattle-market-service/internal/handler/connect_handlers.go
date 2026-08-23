package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/cattle-market-service/internal/domain"
	"github.com/ppusapati/gavya/services/cattle-market-service/internal/repository"
	"github.com/ppusapati/gavya/services/cattle-market-service/internal/service"
)

// ── Request / Response types (mirror proto messages) ──────────────────────────

type CreateListingRequest struct {
	TenantID    string  `json:"tenant_id"`
	CattleID    string  `json:"cattle_id"`
	SellerID    string  `json:"seller_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	AskingPrice float64 `json:"asking_price"`
	Currency    string  `json:"currency"`
	ListingType string  `json:"listing_type"`
	CreatedBy   string  `json:"created_by"`
}
type CreateListingResponse struct {
	Listing *ListingProto `json:"listing"`
}
type GetListingRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
type GetListingResponse struct {
	Listing *ListingProto `json:"listing"`
}
type ListActiveRequest struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}
type ListActiveResponse struct {
	Listings []*ListingProto `json:"listings"`
}
type PlaceBidRequest struct {
	TenantID  string  `json:"tenant_id"`
	ListingID string  `json:"listing_id"`
	BidderID  string  `json:"bidder_id"`
	BidAmount float64 `json:"bid_amount"`
	Message   string  `json:"message"`
	CreatedBy string  `json:"created_by"`
}
type PlaceBidResponse struct {
	Bid *BidProto `json:"bid"`
}
type BidActionRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}
type BidActionResponse struct {
	Bid *BidProto `json:"bid"`
}
type RecordSaleRequest struct {
	TenantID  string  `json:"tenant_id"`
	ListingID string  `json:"listing_id"`
	SellerID  string  `json:"seller_id"`
	BuyerID   string  `json:"buyer_id"`
	CattleID  string  `json:"cattle_id"`
	SalePrice float64 `json:"sale_price"`
	CreatedBy string  `json:"created_by"`
}
type RecordSaleResponse struct {
	Sale *SaleProto `json:"sale"`
}
type OwnershipRequest struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}
type OwnershipResponse struct {
	History []*OwnershipProto `json:"history"`
}

type ListingProto struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	CattleID    string  `json:"cattle_id"`
	Title       string  `json:"title"`
	AskingPrice float64 `json:"asking_price"`
	ListingType string  `json:"listing_type"`
	Status      string  `json:"status"`
}
type BidProto struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	ListingID string  `json:"listing_id"`
	BidderID  string  `json:"bidder_id"`
	BidAmount float64 `json:"bid_amount"`
	Status    string  `json:"status"`
}
type SaleProto struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	ListingID string  `json:"listing_id"`
	SalePrice float64 `json:"sale_price"`
	Status    string  `json:"status"`
}
type OwnershipProto struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id"`
	CattleID        string `json:"cattle_id"`
	OwnerID         string `json:"owner_id"`
	AcquisitionType string `json:"acquisition_type"`
}

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "cattlemarket.v1.CattleMarketService"

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateListing", connectjson.Unary(h.CreateListing))
	route("GetListing", connectjson.Unary(h.GetListing))
	route("ListActiveListings", connectjson.Unary(h.ListActiveListings))
	route("PlaceBid", connectjson.Unary(h.PlaceBid))
	route("AcceptBid", connectjson.Unary(h.AcceptBid))
	route("RejectBid", connectjson.Unary(h.RejectBid))
	route("RecordSale", connectjson.Unary(h.RecordSale))
	route("GetOwnershipHistory", connectjson.Unary(h.GetOwnershipHistory))
}

// classify turns a service failure into the Connect code that describes it.
//
// Every failure used to arrive as Internal, which tells a client to retry —
// so a missing listing, a malformed request and an unreachable database were
// indistinguishable, and only one of them was worth trying again.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrListingNotActive):
		// The request was well formed and the listing's state refused it.
		// Retrying changes nothing until the listing does.
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, repository.ErrCurrencyMismatch):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

func (h *Handler) CreateListing(ctx context.Context, req *connect.Request[CreateListingRequest]) (*connect.Response[CreateListingResponse], error) {
	m := req.Msg
	l, err := h.svc.CreateListing(ctx, &domain.CattleListing{TenantID: m.TenantID, CattleID: m.CattleID, SellerID: m.SellerID, Title: m.Title, Description: m.Description, AskingPrice: m.AskingPrice, Currency: m.Currency, ListingType: m.ListingType, CreatedBy: m.CreatedBy})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CreateListingResponse{Listing: toListingProto(l)}), nil
}

func (h *Handler) GetListing(ctx context.Context, req *connect.Request[GetListingRequest]) (*connect.Response[GetListingResponse], error) {
	l, err := h.svc.GetListing(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetListingResponse{Listing: toListingProto(l)}), nil
}

func (h *Handler) ListActiveListings(ctx context.Context, req *connect.Request[ListActiveRequest]) (*connect.Response[ListActiveResponse], error) {
	list, err := h.svc.ListActiveListings(ctx, req.Msg.TenantID, int(req.Msg.Limit), int(req.Msg.Offset))
	if err != nil {
		return nil, classify(err)
	}
	protos := make([]*ListingProto, 0, len(list))
	for _, l := range list {
		protos = append(protos, toListingProto(l))
	}
	return connect.NewResponse(&ListActiveResponse{Listings: protos}), nil
}

func (h *Handler) PlaceBid(ctx context.Context, req *connect.Request[PlaceBidRequest]) (*connect.Response[PlaceBidResponse], error) {
	m := req.Msg
	b, err := h.svc.PlaceBid(ctx, &domain.CattleBid{TenantID: m.TenantID, ListingID: m.ListingID, BidderID: m.BidderID, BidAmount: m.BidAmount, Message: m.Message, CreatedBy: m.CreatedBy})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&PlaceBidResponse{Bid: toBidProto(b)}), nil
}

func (h *Handler) AcceptBid(ctx context.Context, req *connect.Request[BidActionRequest]) (*connect.Response[BidActionResponse], error) {
	b, err := h.svc.AcceptBid(ctx, req.Msg.ID, req.Msg.TenantID, req.Msg.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BidActionResponse{Bid: toBidProto(b)}), nil
}

func (h *Handler) RejectBid(ctx context.Context, req *connect.Request[BidActionRequest]) (*connect.Response[BidActionResponse], error) {
	b, err := h.svc.RejectBid(ctx, req.Msg.ID, req.Msg.TenantID, req.Msg.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BidActionResponse{Bid: toBidProto(b)}), nil
}

func (h *Handler) RecordSale(ctx context.Context, req *connect.Request[RecordSaleRequest]) (*connect.Response[RecordSaleResponse], error) {
	m := req.Msg
	// The buyer becomes the new owner, which is what makes the sale a transfer
	// rather than only a payment.
	sale, _, err := h.svc.RecordSale(ctx, &domain.CattleSale{
		TenantID:  m.TenantID,
		ListingID: m.ListingID,
		SellerID:  m.SellerID,
		BuyerID:   m.BuyerID,
		CattleID:  m.CattleID,
		SalePrice: m.SalePrice,
		CreatedBy: m.CreatedBy,
	}, m.BuyerID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&RecordSaleResponse{Sale: toSaleProto(sale)}), nil
}

func (h *Handler) GetOwnershipHistory(ctx context.Context, req *connect.Request[OwnershipRequest]) (*connect.Response[OwnershipResponse], error) {
	list, err := h.svc.GetOwnershipHistory(ctx, req.Msg.TenantID, req.Msg.CattleID)
	if err != nil {
		return nil, classify(err)
	}
	protos := make([]*OwnershipProto, 0, len(list))
	for _, o := range list {
		protos = append(protos, toOwnershipProto(o))
	}
	return connect.NewResponse(&OwnershipResponse{History: protos}), nil
}

func toListingProto(l *domain.CattleListing) *ListingProto {
	return &ListingProto{ID: l.ID, TenantID: l.TenantID, CattleID: l.CattleID, Title: l.Title, AskingPrice: l.AskingPrice, ListingType: l.ListingType, Status: l.Status}
}
func toBidProto(b *domain.CattleBid) *BidProto {
	return &BidProto{ID: b.ID, TenantID: b.TenantID, ListingID: b.ListingID, BidderID: b.BidderID, BidAmount: b.BidAmount, Status: b.Status}
}
func toSaleProto(s *domain.CattleSale) *SaleProto {
	return &SaleProto{ID: s.ID, TenantID: s.TenantID, ListingID: s.ListingID, SalePrice: s.SalePrice, Status: s.Status}
}
func toOwnershipProto(o *domain.CattleOwnership) *OwnershipProto {
	return &OwnershipProto{ID: o.ID, TenantID: o.TenantID, CattleID: o.CattleID, OwnerID: o.OwnerID, AcquisitionType: o.AcquisitionType}
}
