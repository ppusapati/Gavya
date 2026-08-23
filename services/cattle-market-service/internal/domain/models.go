package domain

import "time"

type CattleListing struct {
	ID          string
	TenantID    string
	CattleID    string
	SellerID    string
	Title       string
	Description string
	AskingPrice float64
	Currency    string
	ListingType string // fixed/auction/negotiable
	Status      string // active/sold/expired/cancelled
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	DeletedAt   *time.Time
}

type CattleBid struct {
	ID        string
	TenantID  string
	ListingID string
	BidderID  string
	BidAmount float64
	Currency  string
	Status    string // pending/accepted/rejected/withdrawn
	Message   string
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
}

type CattleSale struct {
	ID           string
	TenantID     string
	ListingID    string
	SellerID     string
	BuyerID      string
	CattleID     string
	SalePrice    float64
	Currency     string
	SaleDate     time.Time
	TransferDate *time.Time
	Status       string // pending/completed/cancelled
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    string
	UpdatedBy    string
	DeletedAt    *time.Time
}

type CattleOwnership struct {
	ID              string
	TenantID        string
	CattleID        string
	OwnerID         string
	AcquiredAt      time.Time
	ReleasedAt      *time.Time
	AcquisitionType string // purchase/birth/transfer
	SaleID          *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CreatedBy       string
	UpdatedBy       string
}

// Listing and bid states, named so a comparison against a literal cannot drift
// from what the service writes.
const (
	ListingActive    = "active"
	ListingSold      = "sold"
	ListingWithdrawn = "withdrawn"
	ListingExpired   = "expired"

	BidPending  = "pending"
	BidAccepted = "accepted"
	BidRejected = "rejected"
)
