package service

import (
	"context"
	"fmt"
	"time"

	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/cattle-market-service/internal/domain"
	"github.com/ppusapati/gavya/services/cattle-market-service/internal/repository"
)

// ─── CattleListing ────────────────────────────────────────────────────────────

// CreateListing validates input, assigns a ULID, and persists a new listing.
func (s *Service) CreateListing(ctx context.Context, l *domain.CattleListing) (*domain.CattleListing, error) {
	if l.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if l.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if l.SellerID == "" {
		return nil, invalid("seller_id is required")
	}
	if l.Title == "" {
		return nil, invalid("title is required")
	}
	if l.AskingPrice <= 0 {
		return nil, invalid("asking_price must be greater than zero")
	}
	if l.ListingType != "fixed" && l.ListingType != "auction" && l.ListingType != "negotiable" {
		return nil, invalid("listing_type must be fixed, auction, or negotiable")
	}
	if l.CreatedBy == "" {
		return nil, invalid("created_by is required")
	}

	// The currency is stated, not assumed. There is no default: a price silently
	// recorded in rupees outside India is a price nobody can act on. The first
	// amount a tenant records fixes the currency it records in.
	code, err := currency.Normalise(l.Currency)
	if err != nil {
		return nil, invalid("currency: %s", err)
	}
	scale, err := currency.Scale(code)
	if err != nil {
		return nil, invalid("currency: %s", err)
	}
	if err := s.repo.PinTenantMoney(ctx, l.TenantID, repository.Money{Code: code, Scale: scale}); err != nil {
		return nil, err
	}
	l.Currency = code
	// Amounts are held to that currency's own precision: a yen price has no
	// decimals, a dinar price has three.
	if _, err := exact.NonNegativeDecimal(l.AskingPrice, scale, 18); err != nil {
		return nil, invalid("%s", exact.Field("asking_price", err))
	}

	l.ID = ulidpkg.New().String()
	if l.Status == "" {
		l.Status = "active"
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
		return nil, invalid("id and tenant_id are required")
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
		return nil, invalid("tenant_id is required")
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
	if b.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if b.ListingID == "" {
		return nil, invalid("listing_id is required")
	}
	if b.BidderID == "" {
		return nil, invalid("bidder_id is required")
	}
	if b.BidAmount <= 0 {
		return nil, invalid("bid_amount must be greater than zero")
	}
	if b.CreatedBy == "" {
		return nil, invalid("created_by is required")
	}

	listing, err := s.repo.GetListing(ctx, b.ListingID, b.TenantID)
	if err != nil {
		return nil, fmt.Errorf("listing not found: %w", err)
	}
	if listing.Status != "active" {
		return nil, invalid("listing is not active (status=%s)", listing.Status)
	}

	// The currency is stated, not assumed. There is no default: a price silently
	// recorded in rupees outside India is a price nobody can act on. The first
	// amount a tenant records fixes the currency it records in.
	code, err := currency.Normalise(b.Currency)
	if err != nil {
		return nil, invalid("currency: %s", err)
	}
	scale, err := currency.Scale(code)
	if err != nil {
		return nil, invalid("currency: %s", err)
	}
	if err := s.repo.PinTenantMoney(ctx, b.TenantID, repository.Money{Code: code, Scale: scale}); err != nil {
		return nil, err
	}
	b.Currency = code
	// A bid in a different currency from the listing cannot be compared against
	// the asking price, and ranking it against other bids would be ranking two
	// different kinds of money.
	if listing.Currency != code {
		return nil, invalid("this listing is priced in %s; a bid in %s cannot be compared with it",
			listing.Currency, code)
	}
	// Amounts are held to that currency's own precision: a yen price has no
	// decimals, a dinar price has three.
	if _, err := exact.NonNegativeDecimal(b.BidAmount, scale, 18); err != nil {
		return nil, invalid("%s", exact.Field("bid_amount", err))
	}

	b.ID = ulidpkg.New().String()
	if b.Status == "" {
		b.Status = "pending"
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
		return nil, invalid("bid_id, tenant_id, and updated_by are required")
	}

	// Accepting a bid and closing its listing happen together. When they were
	// two calls and the second failed, a buyer had been told their offer was
	// accepted while the listing stayed open for somebody else to buy.
	bid, _, err := s.repo.AcceptBidAndCloseListing(ctx, bidID, tenantID, updatedBy)
	if err != nil {
		return nil, err
	}
	return bid, nil
}

// RejectBid sets bid status to rejected.
//
// The tenant is a parameter because the query is scoped by it. Without that,
// knowing a bid identifier was enough to reject another tenant's bid.
func (s *Service) RejectBid(ctx context.Context, bidID, tenantID, updatedBy string) (*domain.CattleBid, error) {
	if bidID == "" || tenantID == "" || updatedBy == "" {
		return nil, invalid("bid_id, tenant_id and updated_by are required")
	}
	bid, err := s.repo.UpdateBidStatus(ctx, bidID, tenantID, domain.BidRejected, updatedBy)
	if err != nil {
		s.log.Errorf("RejectBid: %v", err)
		return nil, fmt.Errorf("reject bid: %w", err)
	}
	return bid, nil
}

// ListListingBids returns all bids for a listing.
//
// Scoped by tenant: bid amounts are commercially sensitive, and without the
// scope any caller could read every offer made on any listing anywhere on the
// platform.
func (s *Service) ListListingBids(ctx context.Context, listingID, tenantID string) ([]*domain.CattleBid, error) {
	if listingID == "" || tenantID == "" {
		return nil, invalid("listing_id and tenant_id are required")
	}
	return s.repo.ListListingBids(ctx, listingID, tenantID)
}

// ─── CattleSale ───────────────────────────────────────────────────────────────

// RecordSale validates the listing exists, creates a sale record, and creates an ownership transfer.
func (s *Service) RecordSale(ctx context.Context, sale *domain.CattleSale, newOwnerID string) (*domain.CattleSale, *domain.CattleOwnership, error) {
	if sale.TenantID == "" {
		return nil, nil, invalid("tenant_id is required")
	}
	if sale.ListingID == "" {
		return nil, nil, invalid("listing_id is required")
	}
	if sale.BuyerID == "" {
		return nil, nil, invalid("buyer_id is required")
	}
	if sale.SalePrice <= 0 {
		return nil, nil, invalid("sale_price must be greater than zero")
	}
	if sale.CreatedBy == "" {
		return nil, nil, invalid("created_by is required")
	}

	listing, err := s.repo.GetListing(ctx, sale.ListingID, sale.TenantID)
	if err != nil {
		return nil, nil, err
	}
	if listing.Status == domain.ListingSold {
		return nil, nil, fmt.Errorf("%w: it has already been sold", repository.ErrListingNotActive)
	}

	// The currency is stated, not assumed. There is no default: a price silently
	// recorded in rupees outside India is a price nobody can act on. The first
	// amount a tenant records fixes the currency it records in.
	code, err := currency.Normalise(sale.Currency)
	if err != nil {
		return nil, nil, invalid("currency: %s", err)
	}
	scale, err := currency.Scale(code)
	if err != nil {
		return nil, nil, invalid("currency: %s", err)
	}
	if err := s.repo.PinTenantMoney(ctx, sale.TenantID, repository.Money{Code: code, Scale: scale}); err != nil {
		return nil, nil, err
	}
	sale.Currency = code
	// Amounts are held to that currency's own precision: a yen price has no
	// decimals, a dinar price has three.
	priceLiteral, err := exact.NonNegativeDecimal(sale.SalePrice, scale, 18)
	if err != nil {
		return nil, nil, invalid("%s", exact.Field("sale_price", err))
	}

	sale.ID = ulidpkg.New().String()
	sale.CattleID = listing.CattleID
	sale.SellerID = listing.SellerID
	if sale.Status == "" {
		sale.Status = "pending"
	}
	sale.SaleDate = time.Now()
	sale.UpdatedBy = sale.CreatedBy
	sale.CreatedAt = time.Now()
	sale.UpdatedAt = sale.CreatedAt

	ownership := &domain.CattleOwnership{
		ID:              ulidpkg.New().String(),
		TenantID:        sale.TenantID,
		CattleID:        sale.CattleID,
		OwnerID:         newOwnerID,
		AcquiredAt:      sale.SaleDate,
		AcquisitionType: "purchase",
		CreatedBy:       sale.CreatedBy,
		UpdatedBy:       sale.CreatedBy,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// The sale and the transfer of ownership commit together. When they were two
	// calls and the second failed, the money was accounted for and the animal
	// still belonged to the seller — and retrying sold it twice.
	return s.repo.RecordSaleAndTransfer(ctx, sale, ownership, priceLiteral,
		repository.Money{Code: code, Scale: scale})
}

// GetSale retrieves a single sale by ID and tenant.
func (s *Service) GetSale(ctx context.Context, id, tenantID string) (*domain.CattleSale, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetSale(ctx, id, tenantID)
}

// ─── CattleOwnership ──────────────────────────────────────────────────────────

// GetOwnershipHistory returns the ownership history for a cattle.
func (s *Service) GetOwnershipHistory(ctx context.Context, cattleID, tenantID string) ([]*domain.CattleOwnership, error) {
	if cattleID == "" || tenantID == "" {
		return nil, invalid("cattle_id and tenant_id are required")
	}
	return s.repo.ListCattleOwnership(ctx, tenantID, cattleID)
}
