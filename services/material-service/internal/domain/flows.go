package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"
)

// Instrument is what measures at a node, and how well it is known.
//
// The uncertainty is a property of this instrument rather than of its method. A
// dipstick is not a number; a particular society's dipstick on a particular
// tanker, calibrated on a particular day, is.
type Instrument struct {
	ID       string
	TenantID string

	NodeID string
	Method Method
	Label  string

	// Exactly one of these. An instrument's specification is written one of two
	// ways and the difference matters across its range: a weighbridge is a fixed
	// number of kilograms whatever the load, a flowmeter is a proportion of the
	// reading.
	RelativePPM int64
	Absolute    *quantity.Quantity

	// Where the figure came from. An uncertainty with no certificate behind it
	// is a number somebody remembered, and this one weights money.
	CertificateRef string
	CalibratedOn   time.Time
	ValidUntil     time.Time
}

var (
	ErrNoUncertainty    = errors.New("an instrument must state its standard uncertainty, either as parts per million of the reading or as a fixed quantity")
	ErrTwoUncertainties = errors.New("an instrument states one uncertainty, not both a relative and an absolute one")
	ErrNoCertificate    = errors.New("an instrument's uncertainty must say which certificate it came from; a figure with no provenance is one somebody remembered, and this one weights a settlement")
	ErrNoExpiry         = errors.New("a calibration must say when it stops being current; one that never expires is an instrument nobody will ever re-check")
)

func (i *Instrument) Validate() error {
	switch {
	case i.TenantID == "":
		return errors.New("tenant_id is required")
	case i.NodeID == "":
		return errors.New("an instrument must say which node it is at")
	case !ValidMethod(i.Method):
		return ErrNoMethod
	case i.Label == "":
		return errors.New("an instrument must carry the label its society knows it by, or a " +
			"certificate cannot be matched to it")
	case i.CertificateRef == "":
		return ErrNoCertificate
	case i.CalibratedOn.IsZero():
		return errors.New("an instrument must say when it was calibrated")
	case i.ValidUntil.IsZero():
		return ErrNoExpiry
	case !i.ValidUntil.After(i.CalibratedOn):
		return errors.New("a calibration cannot expire before it was taken")
	}
	hasRelative, hasAbsolute := i.RelativePPM > 0, i.Absolute != nil && i.Absolute.Value() > 0
	switch {
	case hasRelative && hasAbsolute:
		return ErrTwoUncertainties
	case !hasRelative && !hasAbsolute:
		return ErrNoUncertainty
	}
	if hasAbsolute && !quantity.ValidUnit(i.Absolute.Unit) {
		return quantity.ErrNoUnit
	}
	return nil
}

// InCalibrationOn says whether this instrument's certificate was current on a
// day.
//
// Asked about the day the milk was measured, not about today. A reconciliation
// re-run next year has to weight each reading by what the instrument was known
// to on the morning it took it.
func (i *Instrument) InCalibrationOn(day time.Time) bool {
	d := day.UTC().Truncate(24 * time.Hour)
	return !d.Before(i.CalibratedOn.UTC().Truncate(24*time.Hour)) &&
		d.Before(i.ValidUntil.UTC().Truncate(24*time.Hour))
}

// Uncertainty is this instrument's standard uncertainty on a particular reading.
//
// A relative specification is applied to the reading, so the same instrument
// gives a larger uncertainty on a larger load — which is the whole reason the
// two shapes are kept apart.
func (i *Instrument) Uncertainty(reading quantity.Quantity, mode money.RoundingMode) (quantity.Quantity, error) {
	if i.Absolute != nil && i.Absolute.Value() > 0 {
		if i.Absolute.Unit != reading.Unit {
			return quantity.Quantity{}, fmt.Errorf(
				"%w: instrument %s is specified in %s and the reading is %s",
				quantity.ErrUnitMismatch, i.Label, i.Absolute.Unit, reading.Unit)
		}
		return *i.Absolute, nil
	}
	if mode == "" {
		return quantity.Quantity{}, errors.New("a relative uncertainty is a multiplication and " +
			"multiplication rounds, so the rounding mode has to be stated")
	}
	// parts per million, so a rate of RelativePPM at scale 6.
	rate := money.Rate{Numerator: i.RelativePPM, Scale: 6}
	out, _, err := money.MulRate(reading.Amount, rate, mode)
	if err != nil {
		return quantity.Quantity{}, err
	}
	return quantity.Quantity{Amount: out, Unit: reading.Unit}, nil
}

// Flow is one movement shaped for the reconciler.
//
// It carries either a measured quantity with its uncertainty, or nothing and a
// reason. Nothing is not a failure: balance-service solves for an unmeasured
// flow rather than adjusting it, which is the right answer for a reading whose
// instrument nobody can vouch for.
type Flow struct {
	FlowID     string
	FromNodeID string
	ToNodeID   string

	Measured    quantity.Quantity
	Uncertainty quantity.Quantity

	// Unmeasured means the reconciler should solve for this leg instead of
	// weighting it.
	Unmeasured bool
	// UnmeasuredReason says why, in a sentence. A flow the reconciler solved for
	// and a flow nobody measured look identical in its output, and only one of
	// them is a thing somebody can go and fix.
	UnmeasuredReason string

	// MovementID is the trail back, so a nominated gross error reaches a
	// consignment and an instrument rather than a row id.
	MovementID string
}

// ErrNothingToBalance is a period with no node whose balance can be tested.
//
// Every movement crosses the window's edge: a set of deliveries with no node
// that both receives and sends. There is nothing wrong with the data; the window
// is simply not a network, and saying so is better than handing the reconciler
// something it will refuse for a reason that sounds like a fault.
type ErrNothingToBalance struct {
	Movements   int
	Unconnected []string
}

func (e *ErrNothingToBalance) Error() string {
	return fmt.Sprintf("no node in this period both receives and sends, so there is no balance "+
		"to test: %d movements, and every one of them crosses the edge of the window",
		e.Movements)
}

// ErrMixedUnits is a window whose movements are not all in one unit.
type ErrMixedUnits struct {
	Units []string
}

func (e *ErrMixedUnits) Error() string {
	return fmt.Sprintf("this period's movements are measured in %v; a mass balance adds them "+
		"together, so they have to be in one unit before it can", e.Units)
}

// Boundary is balance-service's name for outside the network being balanced.
// Milk crossing it is neither an inflow nor an outflow of any node whose balance
// is being tested.
const Boundary = ""

// interiorNodes are the nodes this window can actually test.
//
// A node with both an inflow and an outflow among the window's movements has a
// balance worth checking: what went in should come out. A node that only sends,
// or only receives, has its other side outside the window — producers delivering
// into a cooler, a plant taking milk into processing — and testing it would
// report its entire throughput as an imbalance.
//
// Derived rather than declared, because it is derivable and a declared list is
// one somebody has to keep right. It is also the honest answer: whether a node
// can be balanced is a fact about the data in the window, not a preference.
func interiorNodes(movements []*Movement) map[string]bool {
	sends, receives := map[string]bool{}, map[string]bool{}
	for _, m := range movements {
		if m.Status != Received {
			continue
		}
		sends[m.FromNodeID] = true
		receives[m.ToNodeID] = true
	}
	out := map[string]bool{}
	for id := range sends {
		if receives[id] {
			out[id] = true
		}
	}
	return out
}

// ProposeFlows shapes a period's movements for the reconciler.
//
// One flow per movement, measured where the sending node's instrument was in
// calibration on the day and unmeasured where it was not.
//
// An endpoint the window cannot test becomes the boundary. A cooler that only
// sends in this window is receiving from producers, which is outside the
// network; left as an interior node it would report its whole morning as
// missing, and the one real imbalance would be lost among figures the size of a
// tanker.
//
// The dispatch is the reading offered. It is what the sending node says left,
// and the receiving node's disagreement with it is already answered
// movement-by-movement as the variance. The reconciler is being asked a
// different question — which readings are inconsistent with the network as a
// whole — and giving it both ends of one stream as two flows would have the
// network carry the same milk twice.
//
// Movements still in transit are left out. A tanker that has not arrived has
// milk in it, and a window that counted the dispatch without the receipt would
// report the whole load as missing from the node it left.
func ProposeFlows(movements []*Movement, instruments map[string]*Instrument, mode money.RoundingMode) ([]Flow, error) {
	var out []Flow
	var outside []string
	units := map[string]bool{}
	interior := interiorNodes(movements)

	for _, m := range movements {
		if m.Status != Received {
			continue
		}
		units[string(m.Dispatched.Unit)] = true

		f := Flow{
			FlowID:     m.ID,
			FromNodeID: m.FromNodeID,
			ToNodeID:   m.ToNodeID,
			Measured:   m.Dispatched,
			MovementID: m.ID,
		}
		if !interior[m.FromNodeID] {
			f.FromNodeID = Boundary
		}
		if !interior[m.ToNodeID] {
			f.ToNodeID = Boundary
		}
		if f.FromNodeID == Boundary && f.ToNodeID == Boundary {
			// This movement touches no node the window can test — a delivery on
			// a route nothing else in the period connects to. Offering it would
			// have balance-service refuse the whole window for a flow that runs
			// boundary to boundary, taking the legs that could have been
			// reconciled down with it.
			outside = append(outside, m.ID)
			continue
		}

		key := m.FromNodeID + "|" + string(m.DispatchMethod)
		inst, known := instruments[key]
		switch {
		case !known:
			f.Unmeasured = true
			f.UnmeasuredReason = fmt.Sprintf(
				"no instrument is registered for %s measurement at the node this left, so how "+
					"well the reading is known is not recorded anywhere", m.DispatchMethod)

		case !inst.InCalibrationOn(m.DispatchedAt):
			// The claim worth making loudly. An expired certificate does not make
			// the reading wrong; it makes it unvouched for, and weighting a
			// settlement by an instrument nobody has checked is worse than
			// solving for the leg.
			f.Unmeasured = true
			f.UnmeasuredReason = fmt.Sprintf(
				"instrument %s (certificate %s) was out of calibration on %s: it expired on %s",
				inst.Label, inst.CertificateRef,
				m.DispatchedAt.UTC().Format("2006-01-02"),
				inst.ValidUntil.UTC().Format("2006-01-02"))

		default:
			u, err := inst.Uncertainty(m.Dispatched, mode)
			if err != nil {
				return nil, fmt.Errorf("movement %s: %w", m.ID, err)
			}
			if u.Value() <= 0 {
				// balance-service refuses a measured flow with a
				// non-positive uncertainty, and it is right to: a reading
				// claimed to be exact pulls the whole solution onto itself.
				f.Unmeasured = true
				f.UnmeasuredReason = fmt.Sprintf(
					"instrument %s rounds to no uncertainty at all on a reading of %s, and a "+
						"reading claimed to be exact would pull the whole reconciliation onto itself",
					inst.Label, m.Dispatched.Describe())
			} else {
				f.Uncertainty = u
			}
		}
		out = append(out, f)
	}

	if len(out) == 0 {
		return nil, &ErrNothingToBalance{Movements: len(movements), Unconnected: outside}
	}
	if len(units) > 1 {
		names := make([]string, 0, len(units))
		for u := range units {
			names = append(names, u)
		}
		sort.Strings(names)
		return nil, &ErrMixedUnits{Units: names}
	}

	// A fixed order, so the same period proposed twice is the same list. The
	// reconciler's answer is keyed by flow id and does not depend on order, but
	// a caller diffing two runs of the same window does.
	sort.Slice(out, func(i, j int) bool { return out[i].FlowID < out[j].FlowID })
	return out, nil
}
