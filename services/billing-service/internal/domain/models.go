package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/money"
)

// The quantity and tax-rate columns: NUMERIC(10,3) and NUMERIC(6,3). A value
// arriving from the wire is checked against these and held at their scale.
const (
	QuantityScale     int32 = 3
	QuantityPrecision int32 = 10
	TaxRateScale      int32 = 3
	TaxRatePrecision  int32 = 6
)

// TaxRateCeiling is the first rate that is not a percentage.
var TaxRateCeiling = exact.MustFixed("1000", TaxRateScale)

type Invoice struct {
	ID            string
	TenantID      string
	CustomerID    string
	InvoiceNumber string
	ReferenceID   string
	ReferenceType string // order/subscription/manual
	Status        string // draft/sent/paid/overdue/cancelled/refunded
	// The totals are exact and each carries its own currency. They were float64
	// read out of NUMERIC(18,4) columns, which carry four decimals faithfully
	// only below about 10^11. On an invoice these are the figures a customer is
	// asked to pay. See libs/integrity/money.ParseStored.
	SubTotal    money.Money
	TaxAmount   money.Money
	TotalAmount money.Money
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
	// Quantity counts litres or kilos, not money. It is exact at the column's
	// three decimals, the way the price beside it is exact at its currency's.
	Quantity   exact.Fixed
	UnitPrice  money.Money
	TotalPrice money.Money
	// TaxRate is a percentage rather than an amount, held to three decimals by
	// its own column.
	TaxRate   exact.Fixed
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
}

type Payment struct {
	ID            string
	TenantID      string
	InvoiceID     string
	Amount        money.Money
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
