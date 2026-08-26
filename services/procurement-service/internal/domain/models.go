// Package domain holds what procurement is about: rate cards, and the
// collections priced through them.
//
// The pricing arithmetic itself is in libs/integrity/ratecard rather than here,
// because the platform recomputing an incumbent's settlement has to reach the
// same paisa as the platform pricing natively. Two implementations would
// disagree, and every disagreement would be reported as a fault in the
// incumbent — which is the one claim this product cannot afford to get wrong.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/libs/integrity/ratecard"
)

// Shift is which of the day's two collections this is.
//
// A closed set rather than free text. "M", "AM", "Morning" and "1" all mean the
// morning somewhere, and a platform that accepted all of them would hold a
// producer's day in four places.
type Shift string

const (
	Morning Shift = "MORNING"
	Evening Shift = "EVENING"
)

func ValidShift(s Shift) bool { return s == Morning || s == Evening }

// Collection is one delivery, before it has been priced.
type Collection struct {
	TenantID    string
	ProducerRef string
	SocietyCode string
	CollectedOn time.Time
	Shift       Shift

	Quantity ratecard.Point
	Unit     ratecard.Basis

	Fat ratecard.Point
	SNF ratecard.Point

	// FatKg and SNFKg are the component weights a formula card prices. Derived
	// where a society records percentages, supplied where it weighs directly.
	FatKg ratecard.Point
	SNFKg ratecard.Point

	Origin origin.Origin
	// Where an imported collection came from, kept so a producer's payment can
	// be traced back to the line of the file it arrived on.
	SourceSystemID string
	ImportBatchID  string
	SourceRecordID string

	Actor string
}

// PricedCollection is a delivery and what it came to.
type PricedCollection struct {
	ID       string
	TenantID string

	ProducerRef string
	SocietyCode string
	CollectedOn time.Time
	Shift       Shift

	Quantity ratecard.Point
	Unit     ratecard.Basis
	Fat      ratecard.Point
	SNF      ratecard.Point

	RateCardID string
	Rate       money.Rate
	Amount     money.Money

	// Explanation is how the number was reached, in words. Kept rather than
	// reconstructed, because the card it was computed from may since have been
	// replaced and a producer disputing a payment is owed the reasoning that was
	// used at the time.
	Explanation string

	Origin         origin.Origin
	SourceSystemID string
	ImportBatchID  string
	SourceRecordID string

	CreatedAt time.Time
	CreatedBy string
}

var (
	ErrNoProducer = errors.New("a collection with no producer cannot be paid to anybody")
	ErrNoQuantity = errors.New("a collection with no quantity records no milk")
	ErrNoShift    = errors.New("a collection must say which of the day's two deliveries it is")
	ErrNoCard     = errors.New("no rate card was in force when this milk was collected")
)

// ErrNoCardInForce names the moment nothing priced.
type ErrNoCardInForce struct {
	TenantID string
	At       time.Time
}

func (e *ErrNoCardInForce) Error() string {
	return fmt.Sprintf("no rate card was in force on %s; milk collected then cannot be priced "+
		"until somebody says what it was worth", e.At.Format("2006-01-02"))
}

func (e *ErrNoCardInForce) Unwrap() error { return ErrNoCard }

// Validate refuses a collection that cannot be priced or paid.
//
// Each of these produces a row that looks like a collection, joins like a
// collection, and cannot be settled — which is worse than no row at all, because
// the totals it changes are believed.
func (c *Collection) Validate() error {
	switch {
	case c.TenantID == "":
		return errors.New("tenant_id is required")
	case c.ProducerRef == "":
		return ErrNoProducer
	case c.CollectedOn.IsZero():
		return errors.New("a collection must say when it was collected, or it cannot be settled")
	case !ValidShift(c.Shift):
		return ErrNoShift
	case c.Quantity.Value <= 0:
		return ErrNoQuantity
	case c.Unit != ratecard.PerLitre && c.Unit != ratecard.PerKg:
		return errors.New("a collection must say whether its quantity is litres or kilograms; " +
			"the two differ by about three per cent")
	case c.Actor == "":
		return errors.New("actor is required")
	}
	return nil
}

// Day is the date a collection belongs to, with the time discarded.
//
// A collection belongs to a day and a shift, not to an instant. Keeping the time
// would mean the same morning's milk sorted differently depending on when the
// operator got to the keyboard, and a producer's fortnight would depend on that.
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
