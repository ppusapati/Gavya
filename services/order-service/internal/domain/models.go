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

type Order struct {
	ID          string
	TenantID    string
	CustomerID  string
	OrderNumber string
	Status      string // draft/confirmed/processing/shipped/delivered/cancelled/returned
	// The totals are exact and each carries its own currency. They were float64
	// read out of NUMERIC(18,4) columns, which carry four decimals faithfully
	// only below about 10^11. See libs/integrity/money.ParseStored.
	SubTotal    money.Money
	TaxAmount   money.Money
	TotalAmount money.Money
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
	ID        string
	TenantID  string
	OrderID   string
	SKUID     string
	ProductID string
	// Quantity counts litres or kilos, not money. It is exact at the column's
	// three decimals, the way the price beside it is exact at its currency's.
	Quantity   exact.Fixed
	UnitPrice  money.Money
	TotalPrice money.Money
	// TaxRate is a percentage, per line. A catalogue that mixes exempt and rated
	// goods cannot be taxed at one rate, and a dairy catalogue mixes them. It is
	// a rate rather than an amount, held to three decimals by its own column.
	TaxRate   exact.Fixed
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
	SubTotal      money.Money
	TaxAmount     money.Money
	TotalAmount   money.Money
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
	RefundAmount money.Money
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

// Return statuses. These were a comment on the Return type and nothing else for
// as long as the type existed; naming them is the first half of making them
// mean something.
const (
	ReturnRequested = "requested"
	ReturnApproved  = "approved"
	ReturnRejected  = "rejected"
	ReturnCompleted = "completed"
)

// returnMoves is where a return may go from where it is.
//
// Written as the whole map rather than as a series of checks, because what is
// interesting about a state machine is the transitions it does not have, and
// those are invisible in a chain of if statements. Read it as: a request is
// decided once, a rejection is final, an approval is carried out, and a
// completed refund is not undone by editing it — that would need a second
// movement of money, which is a decision nobody here has made.
var returnMoves = map[string][]string{
	ReturnRequested: {ReturnApproved, ReturnRejected},
	ReturnApproved:  {ReturnCompleted},
	ReturnRejected:  {},
	ReturnCompleted: {},
}

// ReturnMayMove reports whether a return in state from may become to.
func ReturnMayMove(from, to string) bool {
	for _, allowed := range returnMoves[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// ValidReturnStatus reports whether a status is one this service writes.
func ValidReturnStatus(s string) bool {
	_, ok := returnMoves[s]
	return ok
}
