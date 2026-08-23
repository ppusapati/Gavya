package domain

import "time"

type Invoice struct {
	ID            string
	TenantID      string
	CustomerID    string
	InvoiceNumber string
	ReferenceID   string
	ReferenceType string // order/subscription/manual
	Status        string // draft/sent/paid/overdue/cancelled/refunded
	SubTotal      float64
	TaxAmount     float64
	TotalAmount   float64
	Currency      string
	// TaxInclusive says whether the prices on this invoice already contain the
	// tax. Europe, the UK and Indian retail generally quote inclusive; the
	// United States quotes exclusive. Getting it backwards overcharges or
	// undercharges every line.
	TaxInclusive bool
	IssuedAt     time.Time
	DueAt        time.Time
	PaidAt       *time.Time
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    string
	UpdatedBy    string
	DeletedAt    *time.Time
}

type InvoiceItem struct {
	ID          string
	TenantID    string
	InvoiceID   string
	Description string
	Quantity    float64
	UnitPrice   float64
	TotalPrice  float64
	TaxRate     float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
}

type Payment struct {
	ID            string
	TenantID      string
	InvoiceID     string
	Amount        float64
	Currency      string
	PaymentMethod string // cash/bank_transfer/upi/cheque
	ReferenceNo   string
	PaidAt        time.Time
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	UpdatedBy     string
}

// Invoice statuses, named so a comparison cannot drift from what is written.
const (
	InvoiceDraft     = "draft"
	InvoiceSent      = "sent"
	InvoicePaid      = "paid"
	InvoiceCancelled = "cancelled"
)
