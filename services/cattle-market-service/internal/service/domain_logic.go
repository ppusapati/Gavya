package service

import (
	"context"
	"fmt"
	"time"

	ulidpkg "p9e.in/samavaya/packages/ULID"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/cattle-market-service/internal/domain"
)

// ─── CattleListing ────────────────────────────────────────────────────────────

// CreateListing validates input, assigns a ULID, and persists a new listing.
func (s *Service) CreateListing(ctx context.Context, l *domain.CattleListing) (*domain.CattleListing, error) {
	// asking_price is stored as NUMERIC(12,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(l.AskingPrice, 2, 12); err != nil {
		return nil, exact.Field("asking_price", err)
	}
	if l.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if l.CattleID == "" {
		return nil, fmt.Errorf("cattle_id is required")
	}
	if l.SellerID == "" {
		return nil, fmt.Errorf("seller_id is required")
	}
	if l.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if l.AskingPrice <= 0 {
		return nil, fmt.Errorf("asking_price must be greater than zero")
	}
	if l.ListingType != "fixed" && l.ListingType != "auction" && l.ListingType != "negotiable" {
		return nil, fmt.Errorf("listing_type must be fixed, auction, or negotiable")
	}
	if l.CreatedBy == "" {
		return nil, fmt.Errorf("created_by is required")
	}

	l.ID = ulidpkg.New().String()
	if l.Status == "" {
		l.Status = "active"
	}
	if l.Currency == "" {
		l.Currency = "INR"
	}
	l.UpdatedBy = l.CreatedBy
	l.CreatedAt = time.Now()
	l.UpdatedAt = l.CreatedAt

	result, err := s.repo.CreateListing(ctx, l)
	if err != nil {
		s.log.Errorf("CreateListing: %v", err)
		return nil, fmt.Errorf("create listing: %w", err)
	}
	return result, nil
}

// GetListing retrieves a single listing by ID and tenant.
func (s *Service) GetListing(ctx context.Context, id, tenantID string) (*domain.CattleListing, error) {
	if id == "" || tenantID == "" {
		return nil, fmt.Errorf("id and tenant_id are required")
	}
	l, err := s.repo.GetListing(ctx, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get listing: %w", err)
	}
	return l, nil
}

// ListActiveListings returns paginated active listings for a tenant.
func (s *Service) ListActiveListings(ctx context.Context, tenantID string, limit, offset int) ([]*domain.CattleListing, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListActiveListings(ctx, tenantID, limit, offset)
}

// ─── CattleBid ────────────────────────────────────────────────────────────────

// PlaceBid validates the listing is active, validates bid amount, and persists a bid.
func (s *Service) PlaceBid(ctx context.Context, b *domain.CattleBid) (*domain.CattleBid, error) {
	// bid_amount is stored as NUMERIC(12,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(b.BidAmount, 2, 12); err != nil {
		return nil, exact.Field("bid_amount", err)
	}
	if b.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if b.ListingID == "" {
		return nil, fmt.Errorf("listing_id is required")
	}
	if b.BidderID == "" {
		return nil, fmt.Errorf("bidder_id is required")
	}
	if b.BidAmount <= 0 {
		return nil, fmt.Errorf("bid_amount must be greater than zero")
	}
	if b.CreatedBy == "" {
		return nil, fmt.Errorf("created_by is required")
	}

	listing, err := s.repo.GetListing(ctx, b.ListingID, b.TenantID)
	if err != nil {
		return nil, fmt.Errorf("listing not found: %w", err)
	}
	if listing.Status != "active" {
		return nil, fmt.Errorf("listing is not active (status=%s)", listing.Status)
	}

	b.ID = ulidpkg.New().String()
	if b.Status == "" {
		b.Status = "pending"
	}
	if b.Currency == "" {
		b.Currency = "INR"
	}
	b.UpdatedBy = b.CreatedBy
	b.CreatedAt = time.Now()
	b.UpdatedAt = b.CreatedAt

	result, err := s.repo.CreateBid(ctx, b)
	if err != nil {
		s.log.Errorf("PlaceBid: %v", err)
		return nil, fmt.Errorf("place bid: %w", err)
	}
	return result, nil
}

// AcceptBid sets bid status to accepted and marks the listing as sold.
func (s *Service) AcceptBid(ctx context.Context, bidID, tenantID, updatedBy string) (*domain.CattleBid, error) {
	if bidID == "" || tenantID == "" || updatedBy == "" {
		return nil, fmt.Errorf("bid_id, tenant_id, and updated_by are required")
	}

	bid, err := s.repo.UpdateBidStatus(ctx, bidID, "accepted", updatedBy)
	if err != nil {
		s.log.Errorf("AcceptBid UpdateBidStatus: %v", err)
		return nil, fmt.Errorf("accept bid: %w", err)
	}

	if _, err := s.repo.UpdateListingStatus(ctx, bid.ListingID, tenantID, "sold", updatedBy); err != nil {
		s.log.Errorf("AcceptBid UpdateListingStatus: %v", err)
		return nil, fmt.Errorf("update listing to sold: %w", err)
	}

	return bid, nil
}

// RejectBid sets bid status to rejected.
func (s *Service) RejectBid(ctx context.Context, bidID, updatedBy string) (*domain.CattleBid, error) {
	if bidID == "" || updatedBy == "" {
		return nil, fmt.Errorf("bid_id and updated_by are required")
	}
	bid, err := s.repo.UpdateBidStatus(ctx, bidID, "rejected", updatedBy)
	if err != nil {
		s.log.Errorf("RejectBid: %v", err)
		return nil, fmt.Errorf("reject bid: %w", err)
	}
	return bid, nil
}

// ListListingBids returns all bids for a listing.
func (s *Service) ListListingBids(ctx context.Context, listingID string) ([]*domain.CattleBid, error) {
	if listingID == "" {
		return nil, fmt.Errorf("listing_id is required")
	}
	return s.repo.ListListingBids(ctx, listingID)
}

// ─── CattleSale ───────────────────────────────────────────────────────────────

// RecordSale validates the listing exists, creates a sale record, and creates an ownership transfer.
func (s *Service) RecordSale(ctx context.Context, sale *domain.CattleSale, newOwnerID string) (*domain.CattleSale, *domain.CattleOwnership, error) {
	// sale_price is stored as NUMERIC(12,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(sale.SalePrice, 2, 12); err != nil {
		return nil, nil, exact.Field("sale_price", err)
	}
	if sale.TenantID == "" {
		return nil, nil, fmt.Errorf("tenant_id is required")
	}
	if sale.ListingID == "" {
		return nil, nil, fmt.Errorf("listing_id is required")
	}
	if sale.BuyerID == "" {
		return nil, nil, fmt.Errorf("buyer_id is required")
	}
	if sale.SalePrice <= 0 {
		return nil, nil, fmt.Errorf("sale_price must be greater than zero")
	}
	if sale.CreatedBy == "" {
		return nil, nil, fmt.Errorf("created_by is required")
	}

	listing, err := s.repo.GetListing(ctx, sale.ListingID, sale.TenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("listing not found: %w", err)
	}

	sale.ID = ulidpkg.New().String()
	sale.CattleID = listing.CattleID
	sale.SellerID = listing.SellerID
	if sale.Status == "" {
		sale.Status = "pending"
	}
	if sale.Currency == "" {
		sale.Currency = "INR"
	}
	sale.SaleDate = time.Now()
	sale.UpdatedBy = sale.CreatedBy
	sale.CreatedAt = time.Now()
	sale.UpdatedAt = sale.CreatedAt

	createdSale, err := s.repo.CreateSale(ctx, sale)
	if err != nil {
		s.log.Errorf("RecordSale CreateSale: %v", err)
		return nil, nil, fmt.Errorf("create sale: %w", err)
	}

	ownership := &domain.CattleOwnership{
		ID:              ulidpkg.New().String(),
		TenantID:        sale.TenantID,
		CattleID:        sale.CattleID,
		OwnerID:         newOwnerID,
		AcquiredAt:      sale.SaleDate,
		AcquisitionType: "purchase",
		SaleID:          &createdSale.ID,
		CreatedBy:       sale.CreatedBy,
		UpdatedBy:       sale.CreatedBy,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	createdOwnership, err := s.repo.CreateOwnership(ctx, ownership)
	if err != nil {
		s.log.Errorf("RecordSale CreateOwnership: %v", err)
		return nil, nil, fmt.Errorf("create ownership: %w", err)
	}

	return createdSale, createdOwnership, nil
}

// GetSale retrieves a single sale by ID and tenant.
func (s *Service) GetSale(ctx context.Context, id, tenantID string) (*domain.CattleSale, error) {
	if id == "" || tenantID == "" {
		return nil, fmt.Errorf("id and tenant_id are required")
	}
	return s.repo.GetSale(ctx, id, tenantID)
}

// ─── CattleOwnership ──────────────────────────────────────────────────────────

// GetOwnershipHistory returns the ownership history for a cattle.
func (s *Service) GetOwnershipHistory(ctx context.Context, cattleID, tenantID string) ([]*domain.CattleOwnership, error) {
	if cattleID == "" || tenantID == "" {
		return nil, fmt.Errorf("cattle_id and tenant_id are required")
	}
	return s.repo.ListCattleOwnership(ctx, tenantID, cattleID)
}
