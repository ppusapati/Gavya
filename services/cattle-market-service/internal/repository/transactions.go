package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/cattle-market-service/internal/domain"
)

// AcceptBidAndCloseListing accepts a bid and closes the listing it was against,
// in one transaction.
//
// These were two separate calls. When the second failed, the bid stood accepted
// and the listing stayed open — so it could take further bids, or be sold again
// to somebody else, while a buyer had already been told their offer was
// accepted. The caller saw an error and had no way to undo the half that had
// succeeded.
//
// The listing is locked and its state checked under that lock, so two buyers
// whose bids are accepted at the same moment cannot both find it open.
func (r *repo) AcceptBidAndCloseListing(ctx context.Context, bidID, tenantID, updatedBy string) (*domain.CattleBid, *domain.CattleListing, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// The bid is read within the tenant, which is what stops one tenant
	// accepting another's bid by guessing an identifier.
	var listingID string
	if err := tx.QueryRow(ctx,
		`SELECT listing_id FROM cattle_bids
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		bidID, tenantID).Scan(&listingID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock bid: %w", err)
	}

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM cattle_listings
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		listingID, tenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock listing: %w", err)
	}
	if status != domain.ListingActive {
		return nil, nil, fmt.Errorf("%w: it is %s", ErrListingNotActive, status)
	}

	bid, err := scanBid(tx.QueryRow(ctx,
		`UPDATE cattle_bids SET status=$3, updated_by=$4, updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		 RETURNING `+bidCols,
		bidID, tenantID, domain.BidAccepted, updatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("accept bid: %w", err)
	}

	listing, err := scanListing(tx.QueryRow(ctx,
		`UPDATE cattle_listings SET status=$3, updated_by=$4, updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		 RETURNING `+listingCols,
		listingID, tenantID, domain.ListingSold, updatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("close listing: %w", err)
	}

	// Accepting a bid closes a listing and settles a price. The listing's prior
	// status is already read above, under the lock, so recording it costs
	// nothing and answers the question somebody asks when a seller disputes
	// which bid was taken.
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "accept_bid", ResourceType: "cattle_listing", ResourceID: listingID,
		Before: map[string]any{"status": status},
		After: map[string]any{
			"status": domain.ListingSold, "accepted_bid_id": bidID,
			"bid_amount": bid.BidAmount, "currency": bid.Currency,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return bid, listing, nil
}

// RecordSaleAndTransfer writes a sale and the ownership it transfers, in one
// transaction.
//
// These were two separate calls, and the consequence of the second failing was
// the worst state this service can hold: a sale recorded, the money accounted
// for, and the animal still belonging to the seller. The caller was handed an
// error after the sale row had already committed, so retrying created a second
// sale of the same animal.
//
// The listing is locked and checked here too. Nothing previously stopped the
// same listing being sold twice — RecordSale did not look at its status at all.
func (r *repo) RecordSaleAndTransfer(
	ctx context.Context,
	sale *domain.CattleSale,
	o *domain.CattleOwnership,
	price string,
	money Money,
) (*domain.CattleSale, *domain.CattleOwnership, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM cattle_listings
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		sale.ListingID, sale.TenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock listing: %w", err)
	}
	// A listing that has already been sold cannot be sold again. Without this an
	// animal could be recorded as bought by two people, each with a sale row and
	// an ownership transfer.
	if status == domain.ListingSold {
		return nil, nil, fmt.Errorf("%w: it has already been sold", ErrListingNotActive)
	}

	if err := pinCurrencyTx(ctx, tx, sale.TenantID, money.Code, money.Scale); err != nil {
		return nil, nil, err
	}

	created, err := scanSale(tx.QueryRow(ctx,
		`INSERT INTO cattle_sales (id,tenant_id,listing_id,seller_id,buyer_id,cattle_id,sale_price,currency,sale_date,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8,$9,$10,$11,$12)
		 RETURNING `+saleCols,
		sale.ID, sale.TenantID, sale.ListingID, sale.SellerID, sale.BuyerID, sale.CattleID,
		price, sale.Currency, sale.SaleDate, sale.Status, sale.CreatedBy, sale.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("record sale: %w", err)
	}

	o.SaleID = &created.ID
	ownership, err := scanOwnership(tx.QueryRow(ctx,
		`INSERT INTO cattle_ownership (id,tenant_id,cattle_id,owner_id,acquired_at,acquisition_type,sale_id,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 RETURNING `+ownershipCols,
		o.ID, o.TenantID, o.CattleID, o.OwnerID, o.AcquiredAt, o.AcquisitionType,
		o.SaleID, o.CreatedBy, o.UpdatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("transfer ownership: %w", err)
	}

	// The listing closes with the sale, so the same animal cannot be sold again
	// through it.
	if _, err := tx.Exec(ctx,
		`UPDATE cattle_listings SET status=$3, updated_by=$4, updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		sale.ListingID, sale.TenantID, domain.ListingSold, sale.UpdatedBy); err != nil {
		return nil, nil, fmt.Errorf("close listing: %w", err)
	}

	// The sale and the ownership row are inserts and are their own record. What
	// is overwritten is the listing's status, so that is what is recorded here,
	// along with the price the animal changed hands at.
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "record_sale", ResourceType: "cattle_listing", ResourceID: sale.ListingID,
		Before: map[string]any{"status": status},
		After: map[string]any{
			"status": domain.ListingSold, "sale_id": created.ID,
			"sale_price": price, "currency": sale.Currency,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return created, ownership, nil
}
