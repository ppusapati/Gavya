// Package domain holds what material flow is about: the places milk is held,
// and the movements between them measured at both ends.
//
// The arithmetic on quantities is in libs/integrity/quantity rather than here,
// for the same reason pricing lives in libs/integrity/ratecard: balance-service
// reconciles the same movements this service records, and two implementations
// of a litre-to-kilogram conversion would eventually disagree by a litre. Every
// disagreement between them would be reported as a transit loss, which is a
// number somebody gets asked about.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"
)

// NodeKind is what a place in the milk network is.
type NodeKind string

const (
	CollectionCentre NodeKind = "COLLECTION_CENTRE"
	BulkCooler       NodeKind = "BULK_COOLER"
	ChillingUnit     NodeKind = "CHILLING_UNIT"
	Tanker           NodeKind = "TANKER"
	Plant            NodeKind = "PLANT"
)

func ValidNodeKind(k NodeKind) bool {
	switch k {
	case CollectionCentre, BulkCooler, ChillingUnit, Tanker, Plant:
		return true
	}
	return false
}

// IsVessel says whether a node can only be in one place at a time.
//
// A tanker can. A plant receives from several coolers at once and a cooler
// loads several tankers in a morning, so neither is a vessel in this sense.
func (k NodeKind) IsVessel() bool { return k == Tanker }

// Node is a place milk is held or passes through.
type Node struct {
	ID       string
	TenantID string

	// Code is the society's own name for it: a route code, a registration
	// plate, a chilling centre number. What somebody says on the telephone.
	Code string
	Name string
	Kind NodeKind

	// Capacity is what it holds when full. Optional: a collection centre has
	// none worth recording, a tanker does.
	Capacity *quantity.Quantity

	Active bool

	CreatedAt time.Time
	CreatedBy string
}

// Method is how a quantity was measured.
//
// Kept because balance-service weights a reconciliation by measurement
// uncertainty, and a dipstick, a flowmeter and a weighbridge are not the same
// evidence. A movement that does not say how it was measured cannot be weighted
// and would be given somebody's guess.
type Method string

const (
	Dip         Method = "DIP"
	Flowmeter   Method = "FLOWMETER"
	Weighbridge Method = "WEIGHBRIDGE"
	// DeclaredMethod is a figure with no instrument behind it. Permitted and
	// labelled, because societies do work this way and anybody reading the
	// number afterwards should see that is what it is.
	DeclaredMethod Method = "DECLARED"
)

func ValidMethod(m Method) bool {
	switch m {
	case Dip, Flowmeter, Weighbridge, DeclaredMethod:
		return true
	}
	return false
}

type MovementStatus string

const (
	InTransit MovementStatus = "IN_TRANSIT"
	Received  MovementStatus = "RECEIVED"
	// Abandoned is a movement that never arrived: a breakdown, a load
	// transferred to another vehicle, milk rejected at the gate. Kept rather
	// than deleted, because the milk left somewhere and that is a fact about a
	// cooler's morning.
	Abandoned MovementStatus = "ABANDONED"
)

// Movement is one physical transfer, measured twice.
type Movement struct {
	ID       string
	TenantID string

	FromNodeID string
	ToNodeID   string

	DispatchedAt   time.Time
	Dispatched     quantity.Quantity
	DispatchMethod Method
	DispatchedBy   string

	// The receipt. Zero until the milk arrives.
	ReceivedAt    *time.Time
	Received      *quantity.Quantity
	ReceiptMethod Method
	ReceivedBy    string

	// Holdup is what stayed behind in the sending vessel: the film on a
	// tanker's walls, the heel a cooler cannot pump out.
	//
	// Recorded beside the dispatch rather than deducted from it, because they
	// are different claims. "4960 left" and "5000 left, 40 stayed" balance the
	// same and mean different things, and only the second can be checked
	// against the vessel.
	Holdup *quantity.Quantity

	// Density is what was used to compare two ends measured in different units.
	Density *quantity.Density

	// Variance is dispatched less received, in the unit the movement started
	// in. Nil when the two ends could not be compared.
	Variance *quantity.Quantity
	// VarianceUnavailableReason says why there is none. An empty column and
	// "the two ends were measured in different units and nobody supplied a
	// density" look identical in a report, and only one is a thing somebody can
	// go and fix.
	VarianceUnavailableReason string

	Status          MovementStatus
	AbandonedReason string

	CreatedAt time.Time
	CreatedBy string
}

var (
	ErrNoNodes        = errors.New("a movement must say where the milk went from and to")
	ErrSameNode       = errors.New("a movement from a node to itself moves nothing")
	ErrNoQuantity     = errors.New("a dispatch of nothing is not a movement")
	ErrNoMethod       = errors.New("a measurement must say how it was taken: DIP, FLOWMETER, WEIGHBRIDGE or DECLARED")
	ErrArrivedBefore  = errors.New("milk does not arrive before it leaves")
	ErrAlreadyClosed  = errors.New("this movement has already been received or abandoned")
	ErrNoReason       = errors.New("abandoning a movement must say why: a tanker that never arrived is worth a sentence")
	ErrNodeNotAVessel = errors.New("that node is not a vessel")
)

// ErrTankerBusy names a vehicle that is already carrying something.
type ErrTankerBusy struct {
	NodeCode string
	Other    string
}

func (e *ErrTankerBusy) Error() string {
	return fmt.Sprintf("tanker %s is already carrying movement %s; one of the two names the "+
		"wrong vehicle", e.NodeCode, e.Other)
}

func (n *Node) Validate() error {
	switch {
	case n.TenantID == "":
		return errors.New("tenant_id is required")
	case n.Code == "":
		return errors.New("a node must have the code its society calls it by; a member of staff " +
			"asked about BMC-04 cannot look up an identifier they have never seen")
	case n.Name == "":
		return errors.New("a node must have a name")
	case !ValidNodeKind(n.Kind):
		return errors.New("a node must say what it is: COLLECTION_CENTRE, BULK_COOLER, " +
			"CHILLING_UNIT, TANKER or PLANT")
	case n.CreatedBy == "":
		return errors.New("actor is required")
	}
	if n.Capacity != nil {
		if !quantity.ValidUnit(n.Capacity.Unit) {
			return quantity.ErrNoUnit
		}
		if n.Capacity.Value() <= 0 {
			return errors.New("a capacity of nothing is not a capacity; leave it unset instead")
		}
	}
	return nil
}

// Dispatch is milk leaving a node.
type Dispatch struct {
	TenantID   string
	FromNodeID string
	ToNodeID   string

	At       time.Time
	Quantity quantity.Quantity
	Method   Method

	Holdup *quantity.Quantity

	Actor string
}

func (d *Dispatch) Validate() error {
	switch {
	case d.TenantID == "":
		return errors.New("tenant_id is required")
	case d.FromNodeID == "" || d.ToNodeID == "":
		return ErrNoNodes
	case d.FromNodeID == d.ToNodeID:
		return ErrSameNode
	case d.At.IsZero():
		return errors.New("a dispatch must say when the milk left")
	case !quantity.ValidUnit(d.Quantity.Unit):
		return quantity.ErrNoUnit
	case d.Quantity.Value() <= 0:
		return ErrNoQuantity
	case !ValidMethod(d.Method):
		return ErrNoMethod
	case d.Actor == "":
		return errors.New("actor is required")
	}
	if d.Holdup != nil {
		if !quantity.ValidUnit(d.Holdup.Unit) {
			return quantity.ErrNoUnit
		}
		if d.Holdup.Value() < 0 {
			return errors.New("a negative holdup is milk the vessel owes")
		}
	}
	return nil
}

// Receipt is milk arriving.
type Receipt struct {
	TenantID   string
	MovementID string

	At       time.Time
	Quantity quantity.Quantity
	Method   Method

	// Density is required only when the two ends were measured in different
	// units. Supplied here rather than looked up, because it belongs to this
	// consignment: it was read off this milk, at this temperature, at this dock.
	Density *quantity.Density
	// Rounding says which way the conversion goes, and is required with a
	// density for the same reason the density itself is.
	Rounding money.RoundingMode

	Actor string
}

func (r *Receipt) Validate() error {
	switch {
	case r.TenantID == "":
		return errors.New("tenant_id is required")
	case r.MovementID == "":
		return errors.New("a receipt must say which movement arrived")
	case r.At.IsZero():
		return errors.New("a receipt must say when the milk arrived")
	case !quantity.ValidUnit(r.Quantity.Unit):
		return quantity.ErrNoUnit
	case r.Quantity.Value() < 0:
		return errors.New("a negative quantity arrived is not a thing that can be measured")
	case !ValidMethod(r.Method):
		return ErrNoMethod
	case r.Actor == "":
		return errors.New("actor is required")
	}
	if r.Density != nil {
		if err := r.Density.Validate(); err != nil {
			return err
		}
		if r.Rounding == "" {
			return errors.New("a density converts, and converting rounds, so the rounding mode " +
				"has to be stated rather than assumed")
		}
	}
	return nil
}

// Reconcile works out what a movement came to.
//
// Returns the variance in the unit the movement started in, or nothing and a
// sentence saying why. The sentence is the point: an empty variance column and
// a variance that could not be computed look identical in a report, and only
// one of them is a thing somebody can go and fix.
//
// The holdup is not deducted. A tanker that left with 5000 litres, held 40 back
// on its walls and delivered 4900 has a variance of 100 and a holdup of 40, and
// those are two separate conversations: the 40 is in a vessel somebody can look
// inside, and the 60 is not anywhere.
func Reconcile(dispatched quantity.Quantity, r *Receipt) (*quantity.Quantity, string) {
	from, to, err := quantity.Comparable(dispatched, r.Quantity, r.Density, r.Rounding)
	if err != nil {
		return nil, err.Error()
	}
	variance, err := quantity.Sub(from, to)
	if err != nil {
		return nil, err.Error()
	}
	return &variance, ""
}
