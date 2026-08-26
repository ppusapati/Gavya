package domain

import (
	"sort"
	"strings"
	"testing"
)

// flow builds one leg. An empty node id is the system boundary.
func flow(id, from, to string, measured bool) FlowMeasurement {
	f := FlowMeasurement{
		FlowID: id,
		From:   BalanceNode{ID: from},
		To:     BalanceNode{ID: to},
	}
	if from != Boundary {
		f.From.Kind = NodeTanker
	}
	if to != Boundary {
		f.To.Kind = NodeTanker
	}
	if measured {
		f.Measured = "100.000"
		f.StandardUncertainty = "1.000"
	} else {
		f.Unmeasured = true
		f.Measured = "0.000"
	}
	return f
}

func classify(t *testing.T, flows ...FlowMeasurement) *Observability {
	t.Helper()
	o, err := Classify(flows)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	return o
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s = [%s], want [%s]", what, strings.Join(g, ","), strings.Join(w, ","))
	}
}

// The morning material-service proposes: milk in from producers, through a
// tanker, out to the plant. Both legs measured.
//
// The tanker is the only node in the window, so its balance is one equation over
// two measurements. Each can be computed from the other, so both are checkable —
// which is exactly what makes a ten-litre discrepancy detectable at all.
func TestTwoMeasuredLegsThroughOneNodeCheckEachOther(t *testing.T) {
	o := classify(t,
		flow("in", Boundary, "TANKER", true),
		flow("out", "TANKER", Boundary, true),
	)
	same(t, "redundant", o.Redundant, []string{"in", "out"})
	same(t, "just determined", o.JustDetermined, nil)
	if !o.FullyRedundant() {
		t.Error("a window where both legs check each other does not report as fully redundant")
	}
}

// One leg measured, one not.
//
// The unmeasured leg is determined — it is whatever makes the node balance — but
// the measured one now has nothing to disagree with. It can be wrong by any
// amount and the window will still reconcile perfectly, which is the thing a
// plant manager most needs to be told and the thing a reconciled figure never
// says.
func TestAMeasurementWithNothingToCheckItAgainstIsReportedAsSuch(t *testing.T) {
	o := classify(t,
		flow("in", Boundary, "TANKER", true),
		flow("out", "TANKER", Boundary, false),
	)
	same(t, "observable", o.Observable, []string{"out"})
	same(t, "unobservable", o.Unobservable, nil)
	same(t, "redundant", o.Redundant, nil)
	same(t, "just determined", o.JustDetermined, []string{"in"})

	if o.FullyRedundant() {
		t.Error("a window with one unchecked measurement reports as fully redundant")
	}
	if !o.FullyObservable() {
		t.Error("the unmeasured leg is determined by the balance and reports as unobservable")
	}
}

// Two unmeasured legs out of the same node cannot both be determined.
//
// Milk arrives at a chilling unit and leaves by two routes, neither metered. The
// node balance says the two add up to the inflow, and nothing says how it splits.
// Any pair that sums correctly fits, so the reconciler will print a split that is
// one of infinitely many.
func TestASplitWithNeitherBranchMeteredIsNotDetermined(t *testing.T) {
	o := classify(t,
		flow("in", Boundary, "CHILLER", true),
		flow("branch-a", "CHILLER", "PLANT", false),
		flow("branch-b", "CHILLER", "PLANT", false),
		// The plant has to send milk on, or it is a node that receives forever
		// and its balance can never close. An earlier version of this test left
		// it out and was asking about a network that cannot exist.
		flow("out", "PLANT", Boundary, true),
	)
	same(t, "unobservable", o.Unobservable, []string{"branch-a", "branch-b"})
	same(t, "observable", o.Observable, nil)
	if o.FullyObservable() {
		t.Error("an unmetered split reports as fully observable")
	}
	// The two ends still check each other: whatever the split does internally,
	// what goes in has to come out.
	same(t, "redundant", o.Redundant, []string{"in", "out"})
}

// Metering one branch of the same split determines the other.
//
// The same network, one instrument. This is the test the previous one exists for:
// it shows the classification is about where the instruments are and not about
// the shape of the pipework.
func TestMeteringOneBranchOfASplitDeterminesTheOther(t *testing.T) {
	o := classify(t,
		flow("in", Boundary, "CHILLER", true),
		flow("branch-a", "CHILLER", "PLANT", true),
		flow("branch-b", "CHILLER", "PLANT", false),
		flow("out", "PLANT", Boundary, true),
	)
	same(t, "observable", o.Observable, []string{"branch-b"})
	same(t, "unobservable", o.Unobservable, nil)

	// The branch measurement is determined and still cannot be checked, which
	// is the instructive part and was not what this test first expected.
	//
	// Work it through: the chiller says in = a + b and the plant says a + b =
	// out. Eliminating the unmeasured b leaves in = out, and a has cancelled out
	// of every equation. The branch meter can read anything at all and the
	// window closes perfectly.
	same(t, "redundant", o.Redundant, []string{"in", "out"})
	same(t, "just determined", o.JustDetermined, []string{"branch-a"})
}

// A chain of coolers with only the ends measured.
//
// Every interior leg is determined — the milk has nowhere else to go — but the
// two measurements at the ends check each other, because the chain of unmeasured
// legs contracts to a single node between them.
func TestALongChainWithOnlyTheEndsMeasured(t *testing.T) {
	o := classify(t,
		flow("in", Boundary, "N1", true),
		flow("a", "N1", "N2", false),
		flow("b", "N2", "N3", false),
		flow("c", "N3", "N4", false),
		flow("out", "N4", Boundary, true),
	)
	same(t, "observable", o.Observable, []string{"a", "b", "c"})
	same(t, "redundant", o.Redundant, []string{"in", "out"})
	same(t, "just determined", o.JustDetermined, nil)
}

// A leg between two nodes that milk can already pass between unmeasured cannot
// be checked by anything.
//
// The measured leg runs alongside an unmeasured one between the same pair. The
// unmeasured leg absorbs any error in the measurement without a single node
// balance noticing — so a window like this reconciles whatever the instrument
// reads.
func TestAMeasurementBypassedByAnUnmeasuredLegIsUnchecked(t *testing.T) {
	o := classify(t,
		flow("in", Boundary, "N1", true),
		flow("metered", "N1", "N2", true),
		flow("bypass", "N1", "N2", false),
		flow("out", "N2", Boundary, true),
	)
	// The bypass is the only unmeasured leg, so it is determined: it is whatever
	// makes the two nodes balance.
	same(t, "observable", o.Observable, []string{"bypass"})

	// The metered leg cannot be checked by anything. Contracting the bypass
	// merges N1 and N2 and turns it into a loop on one node, whose contribution
	// to that node's balance is plus x minus x — nothing.
	//
	// This assertion was the wrong way round when it was written, and the
	// comment beside it said the right thing: the bypass absorbs any error in
	// the meter without a single balance noticing. A network like this
	// reconciles whatever the instrument reads.
	if !contains(o.JustDetermined, "metered") {
		t.Errorf("the metered leg reads as checkable; the unmeasured bypass beside it absorbs "+
			"any error it makes, so nothing in this window disagrees with it: %v", o)
	}
	same(t, "redundant", o.Redundant, []string{"in", "out"})
}

// Two legs in parallel, both measured.
//
// Each is the other's second path, so both can be checked. A version of the
// bridge search that skipped "the edge back to my parent" by node rather than by
// edge would call both of them bridges and report neither as checkable.
func TestTwoParallelMeasurementsCheckEachOther(t *testing.T) {
	o := classify(t,
		flow("left", "N1", "N2", true),
		flow("right", "N1", "N2", true),
	)
	same(t, "redundant", o.Redundant, []string{"left", "right"})
	same(t, "just determined", o.JustDetermined, nil)
}

// A window of unconnected deliveries: nothing checks anything.
func TestUnconnectedLegsCheckNothing(t *testing.T) {
	o := classify(t,
		flow("one", "N1", "N2", true),
		flow("two", "N3", "N4", true),
	)
	same(t, "just determined", o.JustDetermined, []string{"one", "two"})
	same(t, "redundant", o.Redundant, nil)
}

// The classification does not depend on which way the milk runs.
//
// A node balance is an equation whichever direction a flow is written in;
// reversing one changes the sign of a term and not whether the term is there.
func TestTheAnswerDoesNotDependOnWhichWayTheMilkIsWrittenAsRunning(t *testing.T) {
	forwards := classify(t,
		flow("in", Boundary, "TANKER", true),
		flow("out", "TANKER", Boundary, false),
	)
	backwards := classify(t,
		flow("in", "TANKER", Boundary, true),
		flow("out", Boundary, "TANKER", false),
	)
	same(t, "observable", backwards.Observable, forwards.Observable)
	same(t, "just determined", backwards.JustDetermined, forwards.JustDetermined)
}

// The same window classified twice reads the same, so a caller diffing two runs
// of one layout is looking at a real difference.
func TestTheSameLayoutClassifiesTheSameEveryTime(t *testing.T) {
	build := func() []FlowMeasurement {
		return []FlowMeasurement{
			flow("z", Boundary, "N1", true),
			flow("a", "N1", "N2", false),
			flow("m", "N2", "N3", false),
			flow("b", "N3", Boundary, true),
			flow("c", "N1", "N3", true),
		}
	}
	// Compared in the order they came back, not as sets.
	//
	// The helper used everywhere else sorts both sides before comparing, which
	// is right for asserting membership and useless here: it normalises away
	// exactly the property this test exists for. An earlier version used it and
	// passed with the sorting removed from the code entirely.
	inOrder := func(o *Observability) string {
		return strings.Join(o.Observable, ",") + "|" +
			strings.Join(o.Unobservable, ",") + "|" +
			strings.Join(o.Redundant, ",") + "|" +
			strings.Join(o.JustDetermined, ",")
	}

	first := inOrder(classify(t, build()...))
	for i := 0; i < 20; i++ {
		if again := inOrder(classify(t, build()...)); again != first {
			t.Fatalf("classification %d differs from the first:\n  first %s\n  again %s",
				i+2, first, again)
		}
	}
	// And it is actually sorted, so the order is a property of the answer rather
	// than of whichever traversal happened to run.
	if !sort.StringsAreSorted(classify(t, build()...).Redundant) {
		t.Error("the redundant legs come back in traversal order, so two runs of the same " +
			"layout can read differently")
	}
}

// Every leg is accounted for exactly once. A classification that quietly dropped
// one would report a window as fully redundant while a measurement nobody can
// check sat outside the answer.
func TestEveryLegIsClassifiedExactlyOnce(t *testing.T) {
	flows := []FlowMeasurement{
		flow("in", Boundary, "N1", true),
		flow("a", "N1", "N2", false),
		flow("b", "N1", "N2", false),
		flow("c", "N2", "N3", true),
		flow("out", "N3", Boundary, true),
	}
	o := classify(t, flows...)

	seen := map[string]int{}
	for _, group := range [][]string{o.Observable, o.Unobservable, o.Redundant, o.JustDetermined} {
		for _, id := range group {
			seen[id]++
		}
	}
	for _, f := range flows {
		if seen[f.FlowID] != 1 {
			t.Errorf("%s appears %d times in the classification, want exactly once",
				f.FlowID, seen[f.FlowID])
		}
	}
	if len(seen) != len(flows) {
		t.Errorf("%d legs classified for %d flows", len(seen), len(flows))
	}
}

// A window with no flows is refused rather than reported as perfectly
// observable, which is what an empty answer would read as.
func TestAnEmptyWindowIsRefusedRatherThanReportedAsPerfect(t *testing.T) {
	if _, err := Classify(nil); err != ErrNoFlowsToClassify {
		t.Errorf("classifying nothing gave %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
