package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/cattle-market-service/internal/domain"
)

// Two things here can only be shown against a real database: that a query is
// scoped to its tenant, and that two writes commit together or not at all.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "cattle_market_repo_test"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	tpl := os.Getenv("TEST_DATABASE_DSN")
	if tpl == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, fmt.Sprintf(tpl, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, testDatabase).Scan(&exists); err != nil {
		t.Fatalf("look for %s: %v", testDatabase, err)
	}
	if !exists {
		if _, err := admin.Exec(ctx, `CREATE DATABASE "`+testDatabase+`"`); err != nil {
			t.Fatalf("create %s: %v", testDatabase, err)
		}
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf(tpl, testDatabase))
	if err != nil {
		t.Fatalf("connect to %s: %v", testDatabase, err)
	}
	t.Cleanup(pool.Close)

	schema, err := os.ReadFile(filepath.Join("..", "db", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return pool
}

var idSeq atomic.Int64

func newTestID(prefix string) string {
	body := fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano(), idSeq.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + "0000000000000000000000000"[:26-len(body)]
}

var rupees = Money{Code: "INR", Scale: 2}

type market struct {
	repo   Repository
	tenant string
}

func newMarket(t *testing.T) *market {
	t.Helper()
	return &market{repo: New(testPool(t)), tenant: newTestID("tnt")}
}

func (m *market) listing(t *testing.T) *domain.CattleListing {
	t.Helper()
	actor := newTestID("usr")
	l, err := m.repo.CreateListing(context.Background(), &domain.CattleListing{
		ID:          newTestID("lst"),
		TenantID:    m.tenant,
		CattleID:    newTestID("cow"),
		SellerID:    newTestID("slr"),
		Title:       "Murrah buffalo, third lactation",
		AskingPrice: 65000,
		Currency:    "INR",
		ListingType: "fixed",
		Status:      domain.ListingActive,
		CreatedBy:   actor,
		UpdatedBy:   actor,
	})
	if err != nil {
		t.Fatalf("CreateListing: %v", err)
	}
	return l
}

func (m *market) bid(t *testing.T, listingID string) *domain.CattleBid {
	t.Helper()
	actor := newTestID("usr")
	b, err := m.repo.CreateBid(context.Background(), &domain.CattleBid{
		ID:        newTestID("bid"),
		TenantID:  m.tenant,
		ListingID: listingID,
		BidderID:  newTestID("byr"),
		BidAmount: 60000,
		Currency:  "INR",
		Status:    domain.BidPending,
		CreatedBy: actor,
		UpdatedBy: actor,
	})
	if err != nil {
		t.Fatalf("CreateBid: %v", err)
	}
	return b
}

func (m *market) sale(t *testing.T, listingID, cattleID, sellerID string) (*domain.CattleSale, *domain.CattleOwnership) {
	t.Helper()
	actor := newTestID("usr")
	now := time.Now()
	return &domain.CattleSale{
			ID:        newTestID("sal"),
			TenantID:  m.tenant,
			ListingID: listingID,
			SellerID:  sellerID,
			BuyerID:   newTestID("byr"),
			CattleID:  cattleID,
			SalePrice: 62000,
			Currency:  "INR",
			SaleDate:  now,
			Status:    "pending",
			CreatedBy: actor,
			UpdatedBy: actor,
		}, &domain.CattleOwnership{
			ID:              newTestID("own"),
			TenantID:        m.tenant,
			CattleID:        cattleID,
			OwnerID:         newTestID("byr"),
			AcquiredAt:      now,
			AcquisitionType: "purchase",
			CreatedBy:       actor,
			UpdatedBy:       actor,
		}
}

/* ---- tenant isolation ---- */

// The defect: UpdateBidStatus constrained by bid id alone, so knowing an
// identifier was enough to accept or reject another tenant's bid.
func TestOneTenantCannotChangeAnothersBid(t *testing.T) {
	seller := newMarket(t)
	listing := seller.listing(t)
	bid := seller.bid(t, listing.ID)

	intruder := newTestID("tnt")
	_, err := seller.repo.UpdateBidStatus(context.Background(), bid.ID, intruder, domain.BidRejected, newTestID("usr"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound — another tenant reached this bid", err)
	}

	after, err := seller.repo.ListListingBids(context.Background(), listing.ID, seller.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Status != domain.BidPending {
		t.Errorf("the bid is now %s; another tenant changed it", after[0].Status)
	}
}

// Bid amounts are commercially sensitive. Unscoped, any caller could read every
// offer made on any listing on the platform.
func TestOneTenantCannotReadAnothersBids(t *testing.T) {
	seller := newMarket(t)
	listing := seller.listing(t)
	seller.bid(t, listing.ID)

	intruder := newTestID("tnt")
	got, err := seller.repo.ListListingBids(context.Background(), listing.ID, intruder)
	if err != nil {
		t.Fatalf("ListListingBids: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("another tenant read %d bids", len(got))
	}
}

func TestAcceptingAnotherTenantsBidIsNotFound(t *testing.T) {
	seller := newMarket(t)
	listing := seller.listing(t)
	bid := seller.bid(t, listing.ID)

	intruder := newTestID("tnt")
	_, _, err := seller.repo.AcceptBidAndCloseListing(context.Background(), bid.ID, intruder, newTestID("usr"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

/* ---- a missing record is named ---- */

func TestAMissingListingIsNotFoundRatherThanAnOpaqueFailure(t *testing.T) {
	m := newMarket(t)

	if _, err := m.repo.GetListing(context.Background(), newTestID("gone"), m.tenant); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := m.repo.GetSale(context.Background(), newTestID("gone"), m.tenant); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

/* ---- accepting a bid ---- */

func TestAcceptingABidClosesItsListing(t *testing.T) {
	m := newMarket(t)
	listing := m.listing(t)
	bid := m.bid(t, listing.ID)

	gotBid, gotListing, err := m.repo.AcceptBidAndCloseListing(context.Background(), bid.ID, m.tenant, newTestID("usr"))
	if err != nil {
		t.Fatalf("AcceptBidAndCloseListing: %v", err)
	}
	if gotBid.Status != domain.BidAccepted {
		t.Errorf("bid = %s, want accepted", gotBid.Status)
	}
	if gotListing.Status != domain.ListingSold {
		t.Errorf("listing = %s, want sold", gotListing.Status)
	}
}

// The defect: the bid and the listing were two calls. A buyer could be told
// their offer was accepted while the listing stayed open for somebody else.
func TestASecondBidCannotBeAcceptedOnASoldListing(t *testing.T) {
	m := newMarket(t)
	listing := m.listing(t)
	first := m.bid(t, listing.ID)
	second := m.bid(t, listing.ID)

	if _, _, err := m.repo.AcceptBidAndCloseListing(context.Background(), first.ID, m.tenant, newTestID("usr")); err != nil {
		t.Fatal(err)
	}

	_, _, err := m.repo.AcceptBidAndCloseListing(context.Background(), second.ID, m.tenant, newTestID("usr"))
	if !errors.Is(err, ErrListingNotActive) {
		t.Fatalf("err = %v, want ErrListingNotActive", err)
	}

	bids, err := m.repo.ListListingBids(context.Background(), listing.ID, m.tenant)
	if err != nil {
		t.Fatal(err)
	}
	var accepted int
	for _, b := range bids {
		if b.Status == domain.BidAccepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("%d bids are accepted on one listing", accepted)
	}
}

/* ---- recording a sale ---- */

func TestASaleTransfersOwnershipAndClosesTheListing(t *testing.T) {
	m := newMarket(t)
	listing := m.listing(t)
	sale, ownership := m.sale(t, listing.ID, listing.CattleID, listing.SellerID)

	gotSale, gotOwnership, err := m.repo.RecordSaleAndTransfer(
		context.Background(), sale, ownership, "62000.00", rupees)
	if err != nil {
		t.Fatalf("RecordSaleAndTransfer: %v", err)
	}
	if gotOwnership.SaleID == nil || *gotOwnership.SaleID != gotSale.ID {
		t.Error("the ownership record does not point at the sale that caused it")
	}

	after, err := m.repo.GetListing(context.Background(), listing.ID, m.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.ListingSold {
		t.Errorf("listing = %s after a sale, want sold", after.Status)
	}
}

// The worst state this service can hold: money accounted for, animal still
// belonging to the seller. Nothing previously stopped the same listing being
// sold twice — RecordSale did not look at its status at all.
func TestTheSameListingCannotBeSoldTwice(t *testing.T) {
	m := newMarket(t)
	listing := m.listing(t)

	first, firstOwn := m.sale(t, listing.ID, listing.CattleID, listing.SellerID)
	if _, _, err := m.repo.RecordSaleAndTransfer(context.Background(), first, firstOwn, "62000.00", rupees); err != nil {
		t.Fatal(err)
	}

	second, secondOwn := m.sale(t, listing.ID, listing.CattleID, listing.SellerID)
	_, _, err := m.repo.RecordSaleAndTransfer(context.Background(), second, secondOwn, "70000.00", rupees)
	if !errors.Is(err, ErrListingNotActive) {
		t.Fatalf("err = %v, want ErrListingNotActive — the animal was sold to two buyers", err)
	}

	owners, err := m.repo.ListCattleOwnership(context.Background(), m.tenant, listing.CattleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 1 {
		t.Errorf("%d ownership records for one animal sold once", len(owners))
	}
}

// A failure writing the transfer must take the sale with it. Otherwise the
// money stands recorded against an animal that never changed hands, and a
// retry sells it a second time.
func TestASaleThatCannotTransferOwnershipIsNotRecorded(t *testing.T) {
	m := newMarket(t)
	listing := m.listing(t)
	sale, ownership := m.sale(t, listing.ID, listing.CattleID, listing.SellerID)

	// An ownership id longer than the column holds fails the insert, after the
	// sale row has already been written inside the transaction.
	ownership.ID = "this-identifier-is-far-too-long-for-the-column-it-goes-in"

	if _, _, err := m.repo.RecordSaleAndTransfer(context.Background(), sale, ownership, "62000.00", rupees); err == nil {
		t.Fatal("the sale was accepted with an unwritable ownership record")
	}

	if _, err := m.repo.GetSale(context.Background(), sale.ID, m.tenant); !errors.Is(err, ErrNotFound) {
		t.Errorf("the sale stands although ownership never transferred (err = %v)", err)
	}
	after, err := m.repo.GetListing(context.Background(), listing.ID, m.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.ListingActive {
		t.Errorf("listing = %s, want it left open by a failed sale", after.Status)
	}
}

// A tenant records money in one currency, and the sale path pins it inside the
// same transaction as the sale.
func TestASaleInASecondCurrencyIsRefused(t *testing.T) {
	m := newMarket(t)
	listing := m.listing(t)
	sale, ownership := m.sale(t, listing.ID, listing.CattleID, listing.SellerID)
	if _, _, err := m.repo.RecordSaleAndTransfer(context.Background(), sale, ownership, "62000.00", rupees); err != nil {
		t.Fatal(err)
	}

	other := m.listing(t)
	s2, o2 := m.sale(t, other.ID, other.CattleID, other.SellerID)
	s2.Currency = "USD"
	_, _, err := m.repo.RecordSaleAndTransfer(context.Background(), s2, o2, "800.00", Money{Code: "USD", Scale: 2})
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}
