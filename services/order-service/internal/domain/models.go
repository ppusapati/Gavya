package domain

import "time"

type Order struct {
	ID          string
	TenantID    string
	CustomerID  string
	OrderNumber string
	Status      string // draft/confirmed/processing/shipped/delivered/cancelled/returned
	SubTotal    float64
	TaxAmount   float64
	TotalAmount float64
	Currency    string
	// TaxInclusive says whether the line prices already contain the tax. Europe,
	// the UK and Indian retail generally quote inclusive; the United States
	// quotes exclusive. Getting it backwards mis-charges every line.
	TaxInclusive    bool
	ShippingAddress string
	Notes           string
	OrderedAt       time.Time
	DeliveredAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CreatedBy       string
	UpdatedBy       string
	DeletedAt       *time.Time
}

type OrderItem struct {
	ID         string
	TenantID   string
	OrderID    string
	SKUID      string
	ProductID  string
	Quantity   float64
	UnitPrice  float64
	TotalPrice float64
	// TaxRate is a percentage, per line. A catalogue that mixes exempt and rated
	// goods cannot be taxed at one rate, and a dairy catalogue mixes them.
	TaxRate   float64
	Status    string // pending/confirmed/shipped/delivered/returned
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
}

type Invoice struct {
	ID            string
	TenantID      string
	OrderID       string
	InvoiceNumber string
	Status        string // draft/sent/paid/overdue/cancelled
	SubTotal      float64
	TaxAmount     float64
	TotalAmount   float64
	Currency      string
	IssuedAt      time.Time
	DueAt         time.Time
	PaidAt        *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	UpdatedBy     string
	DeletedAt     *time.Time
}

type Return struct {
	ID           string
	TenantID     string
	OrderID      string
	Reason       string
	Status       string // requested/approved/rejected/completed
	RefundAmount float64
	Currency     string
	RequestedAt  time.Time
	ProcessedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    string
	UpdatedBy    string
	DeletedAt    *time.Time
}

// Order statuses, named so a comparison against a literal string cannot drift
// from what the service writes.
const (
	OrderDraft     = "draft"
	OrderConfirmed = "confirmed"
	OrderCancelled = "cancelled"
)
