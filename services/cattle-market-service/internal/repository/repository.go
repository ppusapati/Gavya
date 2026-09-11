package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/cattle-market-service/internal/domain"
)

// moneyColumnScale is how many decimals the money columns hold.
//
// They are NUMERIC(18,4) so that one schema serves a yen deployment and a dinar
// one: four is the most any ISO 4217 currency has. It is not how many decimals
// any particular amount has — a rupee price stored there reads back as
// "85000.0000", and those trailing zeros are the column's padding.
const moneyColumnScale int32 = 4

// parseAmount turns a stored decimal literal into money at its currency's scale.
//
// A row whose currency is unreadable is an error rather than a zero: an amount
// with no currency is not an amount, and returning one as though it were is how
// a rupee figure ends up being read as dollars. money.ParseStored is what
// separates the column's padding from the amount's real precision.
func parseAmount(literal, code string) (money.Money, error) {
	normalised, err := currency.Normalise(code)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	scale, err := currency.Scale(normalised)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	return money.ParseStored(literal, moneyColumnScale, scale, normalised)
}

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such listing" from "the database
// is unreachable" — and is told to retry a call that will never succeed.
var ErrNotFound = errors.New("not found")

// ErrListingNotActive is a listing that has already been sold, withdrawn or
// expired. It is named so the caller learns the listing's state refused the
// request rather than that the service failed.
var ErrListingNotActive = errors.New("this listing is not open")

// Repository defines the data access interface for cattle-market-service.
type Repository interface {
	// PinTenantMoney fixes the currency this tenant records money in.
	PinTenantMoney(ctx context.Context, tenantID string, money Money) error
	// TenantMoney reports it.
	TenantMoney(ctx context.Context, tenantID string) (Money, error)

	CreateListing(ctx context.Context, l *domain.CattleListing) (*domain.CattleListing, error)
	GetListing(ctx context.Context, id, tenantID string) (*domain.CattleListing, error)
	ListActiveListings(ctx context.Context, tenantID string, limit, offset int) ([]*domain.CattleListing, error)
	UpdateListingStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.CattleListing, error)

	CreateBid(ctx context.Context, b *domain.CattleBid) (*domain.CattleBid, error)
	ListListingBids(ctx context.Context, listingID, tenantID string) ([]*domain.CattleBid, error)
	UpdateBidStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.CattleBid, error)

	GetSale(ctx context.Context, id, tenantID string) (*domain.CattleSale, error)
	// AcceptBidAndCloseListing accepts a bid and closes its listing together.
	AcceptBidAndCloseListing(ctx context.Context, bidID, tenantID, updatedBy string) (*domain.CattleBid, *domain.CattleListing, error)
	// RecordSaleAndTransfer writes a sale and the ownership it transfers
	// together, so an animal cannot be paid for without changing hands.
	RecordSaleAndTransfer(ctx context.Context, sale *domain.CattleSale, o *domain.CattleOwnership, price string, money Money) (*domain.CattleSale, *domain.CattleOwnership, error)
	ListCattleOwnership(ctx context.Context, cattleID, tenantID string) ([]*domain.CattleOwnership, error)
}

// Columns are listed once and named, because the scans below are positional:
// adding a column to a table would otherwise silently misalign every field
// after it in one query and not another.
const listingCols = `id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency,
       listing_type, status, expires_at, created_at, updated_at, created_by, updated_by, deleted_at`

const bidCols = `id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message,
       created_at, updated_at, created_by, updated_by, deleted_at`

const saleCols = `id, tenant_id, listing_id, seller_id, buyer_id, cattle_id, sale_price, currency,
       sale_date, transfer_date, status, created_at, updated_at, created_by, updated_by, deleted_at`

const ownershipCols = `id, tenant_id, cattle_id, owner_id, acquired_at, released_at, acquisition_type, sale_id,
       created_at, updated_at, created_by, updated_by`

// IDs supplies the identifier each audit entry carries.
type IDs interface{ New() string }

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

// New creates a new Repository backed by the given connection pool.
func New(db *pgxpool.Pool, ids IDs) Repository {
	return &repo{db: db, ids: ids}
}

const serviceName = "cattle-market-service"

// ─── CattleListing ────────────────────────────────────────────────────────────

func (r *repo) CreateListing(ctx context.Context, l *domain.CattleListing) (*domain.CattleListing, error) {
	const q = `
INSERT INTO cattle_listings
  (id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency,
   listing_type, status, expires_at, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8,$9,$10,$11,$12,$13)
RETURNING id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency,
          listing_type, status, expires_at, created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q,
		l.ID, l.TenantID, l.CattleID, l.SellerID, l.Title, l.Description,
		l.AskingPrice.String(), l.AskingPrice.Currency, l.ListingType, l.Status, l.ExpiresAt,
		l.CreatedBy, l.UpdatedBy,
	)
	return scanListing(row)
}

func (r *repo) GetListing(ctx context.Context, id, tenantID string) (*domain.CattleListing, error) {
	const q = `
SELECT id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency,
       listing_type, status, expires_at, created_at, updated_at, created_by, updated_by, deleted_at
FROM cattle_listings
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id, tenantID)
	return scanListing(row)
}

func (r *repo) ListActiveListings(ctx context.Context, tenantID string, limit, offset int) ([]*domain.CattleListing, error) {
	const q = `
SELECT id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency,
       listing_type, status, expires_at, created_at, updated_at, created_by, updated_by, deleted_at
FROM cattle_listings
WHERE tenant_id = $1 AND status = 'active' AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list active listings: %w", err)
	}
	defer rows.Close()

	var result []*domain.CattleListing
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

func (r *repo) UpdateListingStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.CattleListing, error) {
	const q = `
UPDATE cattle_listings
SET status = $3, updated_by = $4, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
RETURNING id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency,
          listing_type, status, expires_at, created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q, id, tenantID, status, updatedBy)
	return scanListing(row)
}

// ─── CattleBid ────────────────────────────────────────────────────────────────

func (r *repo) CreateBid(ctx context.Context, b *domain.CattleBid) (*domain.CattleBid, error) {
	const q = `
INSERT INTO cattle_bids
  (id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8,$9,$10)
RETURNING id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message,
          created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q,
		b.ID, b.TenantID, b.ListingID, b.BidderID, b.BidAmount.String(), b.BidAmount.Currency,
		b.Status, b.Message, b.CreatedBy, b.UpdatedBy,
	)
	return scanBid(row)
}

func (r *repo) ListListingBids(ctx context.Context, listingID, tenantID string) ([]*domain.CattleBid, error) {
	const q = `
SELECT id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message,
       created_at, updated_at, created_by, updated_by, deleted_at
FROM cattle_bids
WHERE listing_id = $1 AND tenant_id = $2 AND deleted_at IS NULL
ORDER BY bid_amount DESC`

	rows, err := r.db.Query(ctx, q, listingID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list listing bids: %w", err)
	}
	defer rows.Close()

	var result []*domain.CattleBid
	for rows.Next() {
		b, err := scanBid(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (r *repo) UpdateBidStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.CattleBid, error) {
	const q = `
UPDATE cattle_bids
SET status = $3, updated_by = $4, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
RETURNING id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message,
          created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q, id, tenantID, status, updatedBy)
	return scanBid(row)
}

// ─── CattleSale ───────────────────────────────────────────────────────────────

func (r *repo) CreateSale(ctx context.Context, s *domain.CattleSale) (*domain.CattleSale, error) {
	const q = `
INSERT INTO cattle_sales
  (id, tenant_id, listing_id, seller_id, buyer_id, cattle_id, sale_price, currency, sale_date, status, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8,$9,$10,$11,$12)
RETURNING id, tenant_id, listing_id, seller_id, buyer_id, cattle_id, sale_price, currency,
          sale_date, transfer_date, status, created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q,
		s.ID, s.TenantID, s.ListingID, s.SellerID, s.BuyerID, s.CattleID,
		s.SalePrice.String(), s.SalePrice.Currency, s.SaleDate, s.Status, s.CreatedBy, s.UpdatedBy,
	)
	return scanSale(row)
}

func (r *repo) GetSale(ctx context.Context, id, tenantID string) (*domain.CattleSale, error) {
	const q = `
SELECT id, tenant_id, listing_id, seller_id, buyer_id, cattle_id, sale_price, currency,
       sale_date, transfer_date, status, created_at, updated_at, created_by, updated_by, deleted_at
FROM cattle_sales
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id, tenantID)
	return scanSale(row)
}

// ─── CattleOwnership ──────────────────────────────────────────────────────────

func (r *repo) CreateOwnership(ctx context.Context, o *domain.CattleOwnership) (*domain.CattleOwnership, error) {
	const q = `
INSERT INTO cattle_ownership
  (id, tenant_id, cattle_id, owner_id, acquired_at, acquisition_type, sale_id, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, tenant_id, cattle_id, owner_id, acquired_at, released_at, acquisition_type, sale_id,
          created_at, updated_at, created_by, updated_by`

	row := r.db.QueryRow(ctx, q,
		o.ID, o.TenantID, o.CattleID, o.OwnerID, o.AcquiredAt,
		o.AcquisitionType, o.SaleID, o.CreatedBy, o.UpdatedBy,
	)
	return scanOwnership(row)
}

// ListCattleOwnership takes the cattle first and the tenant second, like
// GetListing, ListListingBids and GetSale above it. It used to be the one
// method here in the other order, and both layers above it called it as if it
// were not: the handler passed (tenant, cattle) to a service declaring
// (cattle, tenant), which passed (cattle, tenant) to this method declaring
// (tenant, cattle). Two transpositions, no cancellation — the query looked for
// a tenant whose id was an animal's. It matched nothing, ever, and the endpoint
// answered 200 with an empty history for every animal that had ever been sold.
func (r *repo) ListCattleOwnership(ctx context.Context, cattleID, tenantID string) ([]*domain.CattleOwnership, error) {
	const q = `
SELECT id, tenant_id, cattle_id, owner_id, acquired_at, released_at, acquisition_type, sale_id,
       created_at, updated_at, created_by, updated_by
FROM cattle_ownership
WHERE tenant_id = $1 AND cattle_id = $2
ORDER BY acquired_at DESC`

	rows, err := r.db.Query(ctx, q, tenantID, cattleID)
	if err != nil {
		return nil, fmt.Errorf("list cattle ownership: %w", err)
	}
	defer rows.Close()

	var result []*domain.CattleOwnership
	for rows.Next() {
		o, err := scanOwnership(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanListing(s scanner) (*domain.CattleListing, error) {
	l := &domain.CattleListing{}
	var price, code string
	err := s.Scan(
		&l.ID, &l.TenantID, &l.CattleID, &l.SellerID, &l.Title, &l.Description,
		&price, &code, &l.ListingType, &l.Status, &l.ExpiresAt,
		&l.CreatedAt, &l.UpdatedAt, &l.CreatedBy, &l.UpdatedBy, &l.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan listing: %w", err)
	}
	if l.AskingPrice, err = parseAmount(price, code); err != nil {
		return nil, fmt.Errorf("listing %s: %w", l.ID, err)
	}
	return l, nil
}

func scanBid(s scanner) (*domain.CattleBid, error) {
	b := &domain.CattleBid{}
	var amount, code string
	err := s.Scan(
		&b.ID, &b.TenantID, &b.ListingID, &b.BidderID, &amount, &code,
		&b.Status, &b.Message, &b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan bid: %w", err)
	}
	if b.BidAmount, err = parseAmount(amount, code); err != nil {
		return nil, fmt.Errorf("bid %s: %w", b.ID, err)
	}
	return b, nil
}

func scanSale(s scanner) (*domain.CattleSale, error) {
	sale := &domain.CattleSale{}
	var price, code string
	err := s.Scan(
		&sale.ID, &sale.TenantID, &sale.ListingID, &sale.SellerID, &sale.BuyerID, &sale.CattleID,
		&price, &code, &sale.SaleDate, &sale.TransferDate, &sale.Status,
		&sale.CreatedAt, &sale.UpdatedAt, &sale.CreatedBy, &sale.UpdatedBy, &sale.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan sale: %w", err)
	}
	if sale.SalePrice, err = parseAmount(price, code); err != nil {
		return nil, fmt.Errorf("sale %s: %w", sale.ID, err)
	}
	return sale, nil
}

func scanOwnership(s scanner) (*domain.CattleOwnership, error) {
	o := &domain.CattleOwnership{}
	err := s.Scan(
		&o.ID, &o.TenantID, &o.CattleID, &o.OwnerID, &o.AcquiredAt, &o.ReleasedAt,
		&o.AcquisitionType, &o.SaleID, &o.CreatedAt, &o.UpdatedAt, &o.CreatedBy, &o.UpdatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan ownership: %w", err)
	}
	return o, nil
}

// ensure time import is used
var _ = time.Now
