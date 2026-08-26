package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"
)

func qty(t *testing.T, s string, u quantity.Unit) quantity.Quantity {
	t.Helper()
	v, err := quantity.Parse(s, u)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func density(t *testing.T) *quantity.Density {
	t.Helper()
	r, err := money.ParseRate("1.0300", 4)
	if err != nil {
		t.Fatal(err)
	}
	return &quantity.Density{KgPerLitre: r, AtCelsius: 40, Source: quantity.Lactometer}
}

// The ordinary journey: a tanker leaves a cooler with 5000 litres by dip and
// the plant dips 4900. A hundred litres is the number a plant manager already
// knows and hates.
func TestAMovementMeasuredInOneUnitAtBothEndsGivesAnExactVariance(t *testing.T) {
	v, why := Reconcile(qty(t, "5000.000", quantity.Litres), &Receipt{
		Quantity: qty(t, "4900.000", quantity.Litres), Method: Dip,
	})
	if v == nil {
		t.Fatalf("no variance: %s", why)
	}
	if v.String() != "100.000" || v.Unit != quantity.Litres {
		t.Errorf("variance %s, want 100.000 litres", v.Describe())
	}
}

// The failure the whole design is arranged around.
//
// A cooler dips litres and a weighbridge weighs kilograms. Subtracting one from
// the other reports about three per cent of every consignment as missing, which
// is the density of milk and looks exactly like theft.
func TestLitresDispatchedAgainstKilogramsReceivedIsRefusedNotSubtracted(t *testing.T) {
	v, why := Reconcile(qty(t, "5000.000", quantity.Litres), &Receipt{
		Quantity: qty(t, "5090.000", quantity.Kilograms), Method: Weighbridge,
	})
	if v != nil {
		t.Fatalf("a variance of %s was reported across units", v.Describe())
	}
	if !strings.Contains(why, "density") {
		t.Errorf("the reason does not say a density is missing: %q", why)
	}
	// The reason has to name both figures: somebody looking at a consignment
	// needs to know which two numbers are the problem.
	if !strings.Contains(why, "5000.000") || !strings.Contains(why, "5090.000") {
		t.Errorf("the reason does not carry the two quantities: %q", why)
	}
}

// With a density the same consignment reconciles, in the unit it started in.
//
//	5090 kg at 1.0300 is 4941.748 litres
//	5000 dispatched less that is 58.252 litres
func TestWithADensityTheConsignmentReconcilesInTheDispatchUnit(t *testing.T) {
	v, why := Reconcile(qty(t, "5000.000", quantity.Litres), &Receipt{
		Quantity: qty(t, "5090.000", quantity.Kilograms), Method: Weighbridge,
		Density: density(t), Rounding: money.RoundHalfUp,
	})
	if v == nil {
		t.Fatalf("no variance: %s", why)
	}
	if v.Unit != quantity.Litres {
		t.Errorf("the variance is in %s; it should be in the unit the movement started in", v.Unit)
	}
	if v.String() != "58.252" {
		t.Errorf("variance %s, want 58.252 litres", v)
	}
}

// Holdup is deliberately not a parameter of Reconcile.
//
// A tanker that left with 5000, held 40 back on its walls and delivered 4900
// has a variance of 100 and a holdup of 40. Those are two conversations: the 40
// is in a vessel somebody can look inside, and the 60 is not anywhere. Netting
// them would report 60 and lose the fact that 40 is accounted for.
//
// There was a test here asserting that. It built a holdup, passed it nowhere —
// Reconcile has no such parameter — and could not fail. The claim is real and
// this was not a test of it; it is covered where holdup actually flows, in
// TestTheHoldupAndTheVarianceAreSeparateFigures over the running services.

// More arriving than left is real — a mis-dip, a tanker that was not empty when
// it loaded — and has to be reportable rather than refused.
func TestMoreArrivingThanLeftIsReported(t *testing.T) {
	v, why := Reconcile(qty(t, "5000.000", quantity.Litres), &Receipt{
		Quantity: qty(t, "5010.000", quantity.Litres), Method: Dip,
	})
	if v == nil {
		t.Fatalf("no variance: %s", why)
	}
	if v.String() != "-10.000" {
		t.Errorf("variance %s, want -10.000", v)
	}
}

// A density supplied with no rounding mode is refused at validation, because
// converting rounds and which way is a decision about a consignment.
func TestADensityWithNoRoundingModeIsRefused(t *testing.T) {
	r := &Receipt{
		TenantID: "T", MovementID: "M", At: nowish(), Method: Weighbridge,
		Quantity: qty(t, "5090.000", quantity.Kilograms),
		Density:  density(t), Actor: "u",
	}
	if err := r.Validate(); err == nil {
		t.Error("a density was accepted with no rounding mode")
	}
	r.Rounding = money.RoundHalfUp
	if err := r.Validate(); err != nil {
		t.Errorf("a complete receipt was refused: %v", err)
	}
}

// A dispatch from a node to itself moves nothing.
func TestADispatchMustGoSomewhere(t *testing.T) {
	d := &Dispatch{
		TenantID: "T", FromNodeID: "N1", ToNodeID: "N1", At: nowish(),
		Quantity: qty(t, "100.000", quantity.Litres), Method: Dip, Actor: "u",
	}
	if err := d.Validate(); err != ErrSameNode {
		t.Errorf("a movement from a node to itself gave %v", err)
	}
}

// A measurement with no method cannot be weighted by the reconciler, and would
// be given somebody's guess instead.
func TestAMeasurementMustSayHowItWasTaken(t *testing.T) {
	d := &Dispatch{
		TenantID: "T", FromNodeID: "N1", ToNodeID: "N2", At: nowish(),
		Quantity: qty(t, "100.000", quantity.Litres), Actor: "u",
	}
	if err := d.Validate(); err != ErrNoMethod {
		t.Errorf("a dispatch with no method gave %v", err)
	}
	d.Method = "GUESS"
	if err := d.Validate(); err != ErrNoMethod {
		t.Errorf("an unknown method gave %v", err)
	}
}

// A node has to carry the code its society calls it by. A member of staff asked
// about BMC-04 cannot look up an identifier they have never seen.
func TestANodeMustCarryTheCodeItsSocietyUses(t *testing.T) {
	n := &Node{TenantID: "T", Name: "Kothapalli cooler", Kind: BulkCooler, CreatedBy: "u"}
	if err := n.Validate(); err == nil {
		t.Error("a node was accepted with no code")
	}
	n.Code = "BMC-04"
	if err := n.Validate(); err != nil {
		t.Errorf("a complete node was refused: %v", err)
	}
}

// Only a tanker is a vessel that can be in one place at a time. A plant
// receives from several coolers at once.
func TestOnlyAVesselCanBeInOnlyOnePlace(t *testing.T) {
	if !Tanker.IsVessel() {
		t.Error("a tanker is not treated as a vessel")
	}
	for _, k := range []NodeKind{CollectionCentre, BulkCooler, ChillingUnit, Plant} {
		if k.IsVessel() {
			t.Errorf("%s is treated as a vessel, so a second delivery to one would be refused", k)
		}
	}
}

func nowish() time.Time { return time.Date(2026, 3, 1, 5, 0, 0, 0, time.UTC) }
