package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"
)

func on(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// weighbridge is specified as a fixed 5 kg whatever the load, which is how a
// weighbridge certificate reads.
func weighbridge(t *testing.T, node string) *Instrument {
	t.Helper()
	abs := qty(t, "5.000", quantity.Kilograms)
	return &Instrument{
		TenantID: "T", NodeID: node, Method: Weighbridge, Label: "WB-1",
		Absolute: &abs, CertificateRef: "CERT-WB-2026",
		CalibratedOn: on(2026, time.January, 1), ValidUntil: on(2027, time.January, 1),
	}
}

// flowmeter is specified as 2000 parts per million — two tenths of a per cent
// of the reading — which is how a flowmeter certificate reads.
func flowmeter(t *testing.T, node string) *Instrument {
	t.Helper()
	return &Instrument{
		TenantID: "T", NodeID: node, Method: Flowmeter, Label: "FM-1",
		RelativePPM: 2000, CertificateRef: "CERT-FM-2026",
		CalibratedOn: on(2026, time.January, 1), ValidUntil: on(2027, time.January, 1),
	}
}

func movedOn(id, from, to string, day time.Time, q quantity.Quantity, method Method) *Movement {
	return &Movement{
		ID: id, TenantID: "T", FromNodeID: from, ToNodeID: to,
		DispatchedAt: day, Dispatched: q, DispatchMethod: method,
		Status: Received,
	}
}

// closing is a second leg out of N2, so that N2 both receives and sends and the
// window has a node whose balance can be tested.
//
// Every test about one flow needs it: a lone movement N1 to N2 has no node with
// both an inflow and an outflow, so ProposeFlows correctly reports that there is
// nothing to balance and the test never reaches its subject.
func closing(t *testing.T, day time.Time) *Movement {
	t.Helper()
	return movedOn("M9", "N2", "N3", day, qty(t, "100.000", quantity.Litres), Flowmeter)
}

func instrumentsBy(list ...*Instrument) map[string]*Instrument {
	out := map[string]*Instrument{}
	for _, i := range list {
		out[i.NodeID+"|"+string(i.Method)] = i
	}
	return out
}

// A relative specification gives a larger uncertainty on a larger load, which
// is the whole reason the two shapes are kept apart.
//
//	5000.000 litres at 2000 ppm is 10.000 litres
//	1000.000 litres at 2000 ppm is  2.000 litres
func TestARelativeUncertaintyScalesWithTheReading(t *testing.T) {
	fm := flowmeter(t, "N1")
	for _, c := range []struct{ reading, want string }{
		{"5000.000", "10.000"},
		{"1000.000", "2.000"},
		{"12345.678", "24.691"},
	} {
		u, err := fm.Uncertainty(qty(t, c.reading, quantity.Litres), money.RoundHalfUp)
		if err != nil {
			t.Fatalf("%s: %v", c.reading, err)
		}
		if u.String() != c.want {
			t.Errorf("%s litres at 2000 ppm gives %s, want %s", c.reading, u, c.want)
		}
	}
}

// An absolute specification does not scale, which is equally the point: a
// weighbridge reads to the same few kilograms whether the tanker is full or
// nearly empty.
func TestAnAbsoluteUncertaintyDoesNotScale(t *testing.T) {
	wb := weighbridge(t, "N1")
	for _, reading := range []string{"5000.000", "100.000"} {
		u, err := wb.Uncertainty(qty(t, reading, quantity.Kilograms), money.RoundHalfUp)
		if err != nil {
			t.Fatalf("%s: %v", reading, err)
		}
		if u.String() != "5.000" {
			t.Errorf("%s kg gives an uncertainty of %s, want the certificate's 5.000", reading, u)
		}
	}
}

// An instrument specified in one unit against a reading in another is refused
// rather than applied. Five kilograms of uncertainty on a reading in litres is
// not five litres.
func TestAnInstrumentSpecifiedInAnotherUnitIsRefused(t *testing.T) {
	wb := weighbridge(t, "N1")
	_, err := wb.Uncertainty(qty(t, "5000.000", quantity.Litres), money.RoundHalfUp)
	if err == nil {
		t.Fatal("a kilogram uncertainty was applied to a reading in litres")
	}
	if !strings.Contains(err.Error(), "WB-1") {
		t.Errorf("the refusal does not name the instrument: %v", err)
	}
}

// The ordinary case: a period of movements becomes a weighted network.
func TestAPeriodOfMovementsBecomesAWeightedNetwork(t *testing.T) {
	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M2", "N2", "N3", day, qty(t, "4990.000", quantity.Litres), Flowmeter),
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N2")),
		money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 2 {
		t.Fatalf("%d flows, want 2", len(flows))
	}
	// Ordered, so the same period proposed twice is the same list.
	if flows[0].FlowID != "M1" || flows[1].FlowID != "M2" {
		t.Errorf("flows came back as %s then %s", flows[0].FlowID, flows[1].FlowID)
	}
	if flows[0].Unmeasured || flows[0].Uncertainty.String() != "10.000" {
		t.Errorf("the first flow reads unmeasured=%v uncertainty=%s",
			flows[0].Unmeasured, flows[0].Uncertainty)
	}
	if flows[1].Uncertainty.String() != "9.980" {
		t.Errorf("the second flow's uncertainty is %s, want 9.980", flows[1].Uncertainty)
	}
}

// The claim worth making loudly.
//
// An expired certificate does not make a reading wrong. It makes it unvouched
// for — and weighting a settlement by an instrument nobody has checked in three
// years is worse than solving for the leg, because the reconciler will pull the
// whole solution towards a number no one can defend.
func TestAnInstrumentOutOfCalibrationProducesAnUnmeasuredFlow(t *testing.T) {
	expired := flowmeter(t, "N1")
	expired.ValidUntil = on(2026, time.February, 1)

	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
			closing(t, day),
		},
		instrumentsBy(expired, flowmeter(t, "N2")), money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if !flows[0].Unmeasured {
		t.Fatalf("a reading taken a month after the certificate expired was weighted at %s",
			flows[0].Uncertainty)
	}
	for _, want := range []string{"FM-1", "CERT-FM-2026", "2026-02-01", "2026-03-01"} {
		if !strings.Contains(flows[0].UnmeasuredReason, want) {
			t.Errorf("the reason does not carry %q: %q", want, flows[0].UnmeasuredReason)
		}
	}
}

// The same instrument on a day it was in calibration is weighted normally, so
// the test above is about the date and not about the instrument.
func TestTheSameInstrumentIsWeightedOnADayItWasCurrent(t *testing.T) {
	expired := flowmeter(t, "N1")
	expired.ValidUntil = on(2026, time.February, 1)

	day := on(2026, time.January, 15)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
			closing(t, day),
		},
		instrumentsBy(expired, flowmeter(t, "N2")), money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if flows[0].Unmeasured {
		t.Errorf("a reading taken inside the certificate's period was not weighted: %s",
			flows[0].UnmeasuredReason)
	}
}

// Calibration is judged on the day the milk was measured, not on today. A
// reconciliation re-run next year has to weight each reading by what the
// instrument was known to on the morning it took it.
func TestCalibrationIsJudgedOnTheDayTheMilkWasMeasured(t *testing.T) {
	fm := flowmeter(t, "N1")
	if !fm.InCalibrationOn(on(2026, time.June, 1)) {
		t.Error("a day inside the certificate's period reads as out of calibration")
	}
	if fm.InCalibrationOn(on(2025, time.December, 31)) {
		t.Error("the day before the calibration was taken reads as in calibration")
	}
	if fm.InCalibrationOn(on(2027, time.January, 1)) {
		t.Error("the day the certificate expires reads as still current")
	}
	if !fm.InCalibrationOn(on(2026, time.December, 31)) {
		t.Error("the last day of the certificate reads as expired")
	}
}

// A node with no registered instrument for the method it measured by produces
// an unmeasured flow, and says so. It is not an error: a society that has not
// got round to recording its dipsticks can still reconcile, with those legs
// solved for.
func TestAnUnregisteredInstrumentProducesAnUnmeasuredFlowWithAReason(t *testing.T) {
	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Dip),
			closing(t, day),
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N2")), money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if !flows[0].Unmeasured {
		t.Fatal("a reading from an unregistered instrument was weighted")
	}
	if !strings.Contains(flows[0].UnmeasuredReason, "DIP") {
		t.Errorf("the reason does not say which measurement is unregistered: %q",
			flows[0].UnmeasuredReason)
	}
}

// A movement still in transit is left out.
//
// A tanker that has not arrived has milk in it. A window that counted the
// dispatch without the receipt would report the whole load as missing from the
// node it left, and a plant manager would go looking for five thousand litres
// that are on a road.
func TestMilkStillOnTheRoadIsNotInTheWindow(t *testing.T) {
	inTransit := movedOn("M2", "N1", "N2", on(2026, time.March, 1),
		qty(t, "5000.000", quantity.Litres), Flowmeter)
	inTransit.Status = InTransit
	abandoned := movedOn("M3", "N1", "N2", on(2026, time.March, 1),
		qty(t, "3000.000", quantity.Litres), Flowmeter)
	abandoned.Status = Abandoned

	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "1000.000", quantity.Litres), Flowmeter),
			closing(t, day),
			inTransit, abandoned,
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N2")), money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 2 {
		t.Fatalf("%d flows in the window; only the two delivered ones belong", len(flows))
	}
	for _, f := range flows {
		if f.FlowID == "M2" || f.FlowID == "M3" {
			t.Errorf("%s is still on the road or was abandoned, and it is in the window", f.FlowID)
		}
	}
}

// A window whose movements are not all in one unit is refused. A mass balance
// adds them together, and litres plus kilograms is not a quantity.
func TestAWindowInTwoUnitsIsRefused(t *testing.T) {
	_, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", on(2026, time.March, 1),
				qty(t, "5000.000", quantity.Litres), Flowmeter),
			movedOn("M2", "N2", "N3", on(2026, time.March, 1),
				qty(t, "5150.000", quantity.Kilograms), Weighbridge),
		},
		instrumentsBy(flowmeter(t, "N1"), weighbridge(t, "N2")), money.RoundHalfUp)
	if err == nil {
		t.Fatal("a window mixing litres and kilograms was proposed to the reconciler")
	}
	var mixed *ErrMixedUnits
	if !as(err, &mixed) {
		t.Fatalf("the refusal is not about the units: %v", err)
	}
	if len(mixed.Units) != 2 {
		t.Errorf("the refusal names %v", mixed.Units)
	}
}

// An instrument so precise that its uncertainty rounds to nothing on a small
// reading produces an unmeasured flow rather than a measured one claiming to be
// exact.
//
// balance-service refuses a measured flow with a non-positive uncertainty, and
// it is right to: a reading claimed to be exact pulls the whole solution onto
// itself and every other leg absorbs the difference.
func TestAReadingWhoseUncertaintyRoundsToNothingIsSolvedForInstead(t *testing.T) {
	precise := flowmeter(t, "N1")
	precise.RelativePPM = 1 // one part per million

	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "0.100", quantity.Litres), Flowmeter),
			closing(t, day),
		},
		instrumentsBy(precise, flowmeter(t, "N2")), money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if !flows[0].Unmeasured {
		t.Fatalf("a reading with an uncertainty of %s was offered as measured", flows[0].Uncertainty)
	}
	if !strings.Contains(flows[0].UnmeasuredReason, "exact") {
		t.Errorf("the reason does not explain why: %q", flows[0].UnmeasuredReason)
	}
}

// An instrument with no uncertainty, or with both kinds, is refused. There is no
// default: a typical figure for the method would put a number nobody measured
// into the weighting of a settlement.
func TestAnInstrumentStatesExactlyOneUncertainty(t *testing.T) {
	base := func() *Instrument {
		return &Instrument{
			TenantID: "T", NodeID: "N1", Method: Flowmeter, Label: "FM-1",
			CertificateRef: "CERT", CalibratedOn: on(2026, time.January, 1),
			ValidUntil: on(2027, time.January, 1),
		}
	}
	if err := base().Validate(); err != ErrNoUncertainty {
		t.Errorf("an instrument with no uncertainty gave %v", err)
	}

	both := base()
	both.RelativePPM = 2000
	abs := qty(t, "5.000", quantity.Litres)
	both.Absolute = &abs
	if err := both.Validate(); err != ErrTwoUncertainties {
		t.Errorf("an instrument with two uncertainties gave %v", err)
	}
}

// An uncertainty with no certificate behind it is a number somebody remembered,
// and this one weights money.
func TestAnUncertaintyMustSayWhereItCameFrom(t *testing.T) {
	i := flowmeter(t, "N1")
	i.CertificateRef = ""
	if err := i.Validate(); err != ErrNoCertificate {
		t.Errorf("an instrument with no certificate gave %v", err)
	}

	i = flowmeter(t, "N1")
	i.ValidUntil = time.Time{}
	if err := i.Validate(); err != ErrNoExpiry {
		t.Errorf("a calibration that never expires gave %v", err)
	}
}

// as is errors.As without importing errors into every assertion.
func as[T error](err error, target *T) bool {
	for err != nil {
		if v, ok := err.(T); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// A node the window cannot test becomes the boundary.
//
// A cooler that only sends in this window is receiving from producers, which is
// outside the network. Left as an interior node it reports its whole morning as
// an imbalance, and the one real discrepancy — ten litres in a tanker — is lost
// among figures the size of a tanker.
func TestANodeTheWindowCannotTestBecomesTheBoundary(t *testing.T) {
	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
			movedOn("M2", "N2", "N3", day, qty(t, "4990.000", quantity.Litres), Flowmeter),
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N2")), money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}

	// N2 receives and sends, so it is the one node this window can test.
	if flows[0].FromNodeID != Boundary {
		t.Errorf("the cooler is %q; it only sends in this window, so its other side is outside it",
			flows[0].FromNodeID)
	}
	if flows[0].ToNodeID != "N2" {
		t.Errorf("the first flow arrives at %q, want the tanker", flows[0].ToNodeID)
	}
	if flows[1].FromNodeID != "N2" {
		t.Errorf("the second flow leaves %q, want the tanker", flows[1].FromNodeID)
	}
	if flows[1].ToNodeID != Boundary {
		t.Errorf("the plant is %q; it only receives in this window", flows[1].ToNodeID)
	}
}

// A node in the middle of a longer chain is testable at both ends.
func TestANodeThatBothReceivesAndSendsKeepsItsIdentity(t *testing.T) {
	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
			movedOn("M2", "N2", "N3", day, qty(t, "4990.000", quantity.Litres), Flowmeter),
			movedOn("M3", "N3", "N4", day, qty(t, "4900.000", quantity.Litres), Flowmeter),
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N2"), flowmeter(t, "N3")),
		money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	// N2 and N3 both receive and send.
	if flows[1].FromNodeID != "N2" || flows[1].ToNodeID != "N3" {
		t.Errorf("the middle leg runs %q to %q, and both ends are testable",
			flows[1].FromNodeID, flows[1].ToNodeID)
	}
}

// A period where nothing both receives and sends is not a network, and saying so
// is better than handing the reconciler something it will refuse for a reason
// that sounds like a fault.
func TestAPeriodWithNoTestableNodeSaysSo(t *testing.T) {
	day := on(2026, time.March, 1)
	_, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
			movedOn("M2", "N3", "N4", day, qty(t, "3000.000", quantity.Litres), Flowmeter),
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N3")), money.RoundHalfUp)
	if err == nil {
		t.Fatal("a period of unconnected deliveries was proposed as a network")
	}
	var nothing *ErrNothingToBalance
	if !as(err, &nothing) {
		t.Fatalf("the refusal is not about there being nothing to balance: %v", err)
	}
	if nothing.Movements != 2 {
		t.Errorf("the refusal counts %d movements", nothing.Movements)
	}
}

// A delivery on a route nothing else in the period connects to is left out
// rather than taking the rest of the window down with it.
//
// balance-service refuses a flow that runs boundary to boundary, and it refuses
// the whole window when it finds one — so offering an unconnected movement
// would lose the legs that could have been reconciled.
func TestAnUnconnectedDeliveryIsLeftOutRatherThanBreakingTheWindow(t *testing.T) {
	day := on(2026, time.March, 1)
	flows, err := ProposeFlows(
		[]*Movement{
			movedOn("M1", "N1", "N2", day, qty(t, "5000.000", quantity.Litres), Flowmeter),
			movedOn("M2", "N2", "N3", day, qty(t, "4990.000", quantity.Litres), Flowmeter),
			// A delivery between two nodes nothing else touches.
			movedOn("M3", "N8", "N9", day, qty(t, "1000.000", quantity.Litres), Flowmeter),
		},
		instrumentsBy(flowmeter(t, "N1"), flowmeter(t, "N2"), flowmeter(t, "N8")),
		money.RoundHalfUp)
	if err != nil {
		t.Fatalf("an unconnected delivery took the whole window down: %v", err)
	}
	if len(flows) != 2 {
		t.Fatalf("%d flows, want the two legs that touch the tanker", len(flows))
	}
	for _, f := range flows {
		if f.FlowID == "M3" {
			t.Error("the unconnected delivery was offered as a flow, and balance-service " +
				"would refuse the whole window for it")
		}
	}
}
