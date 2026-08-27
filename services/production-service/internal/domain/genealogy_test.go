package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"
)

// graph is a genealogy written down as edges, so a test can state a shape and
// then ask what a walk makes of it.
type graph struct {
	edges []Edge
	// calls records how many times the loader was asked for a level, which is
	// how the tests check that a walk is breadth-first rather than one query
	// per batch.
	calls int
	// asked records the batch sets the walk asked about, in order.
	asked [][]string
}

func newGraph(pairs ...[2]string) *graph {
	g := &graph{}
	for _, p := range pairs {
		q, err := quantity.New(1000, quantity.Litres)
		if err != nil {
			panic(err)
		}
		g.edges = append(g.edges, Edge{InputBatchID: p[0], OutputBatchID: p[1], Consumed: q})
	}
	return g
}

// next is the loader. It returns every edge touching the given batches on
// either side — deliberately more than the walk needs, because a real SQL
// loader written with `input_batch_id = ANY($1) OR output_batch_id = ANY($1)`
// would do the same, and the walk has to be right anyway.
func (g *graph) next(ids []string) ([]Edge, error) {
	g.calls++
	g.asked = append(g.asked, append([]string(nil), ids...))
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []Edge
	for _, e := range g.edges {
		if want[e.InputBatchID] || want[e.OutputBatchID] {
			out = append(out, e)
		}
	}
	return out, nil
}

func wide() Limit { return Limit{MaxDepth: 100, MaxBatches: 1000} }

func reachedString(t *Trace) string {
	parts := make([]string, 0, len(t.Reached))
	for _, r := range t.Reached {
		parts = append(parts, fmt.Sprintf("%s@%d", r.BatchID, r.Depth))
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Which way the walk goes
// ---------------------------------------------------------------------------

// A chain milk → curd → paneer → carton. Forward from the milk finds the
// carton; backward from the carton finds the milk. If direction were ignored,
// one of these two would come back empty and the other would still pass, which
// is why both are asserted here rather than one.
func TestWalkFollowsTheDirectionItIsGiven(t *testing.T) {
	g := newGraph(
		[2]string{"milk", "curd"},
		[2]string{"curd", "paneer"},
		[2]string{"paneer", "carton"},
	)

	fwd, err := Walk("milk", Forward, g.next, wide())
	if err != nil {
		t.Fatalf("forward walk: %v", err)
	}
	if got := reachedString(fwd); got != "curd@1 paneer@2 carton@3" {
		t.Errorf("forward from milk reached %q, want the chain down to the carton", got)
	}

	back, err := Walk("carton", Backward, g.next, wide())
	if err != nil {
		t.Fatalf("backward walk: %v", err)
	}
	if got := reachedString(back); got != "paneer@1 curd@2 milk@3" {
		t.Errorf("backward from carton reached %q, want the chain back to the milk", got)
	}
}

// The walk must not wander upstream while going downstream. A silo that took
// two tankers, one of them contaminated: the recall must name what the silo
// became, not the other producer's milk that also went in. Naming the innocent
// tanker would have a society recalling milk that was never in the carton.
func TestForwardWalkDoesNotWanderUpstream(t *testing.T) {
	g := newGraph(
		[2]string{"bad_tanker", "silo"},
		[2]string{"good_tanker", "silo"},
		[2]string{"silo", "butter"},
	)
	tr, err := Walk("bad_tanker", Forward, g.next, wide())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := reachedString(tr); got != "silo@1 butter@2" {
		t.Errorf("forward from the bad tanker reached %q; the other tanker's milk is not "+
			"downstream of it and recalling it would be recalling the wrong milk", got)
	}
}

// A batch reachable by two routes is named once, at the shorter one. A silo
// feeding two lines that meet again in a blend is the ordinary case, and naming
// the blend twice would have a clerk pulling the same pallet off the shelf
// twice and wondering which count was right.
func TestDiamondNamesABatchOnceAtItsShortestDepth(t *testing.T) {
	g := newGraph(
		[2]string{"silo", "creamA"},
		[2]string{"silo", "creamB"},
		[2]string{"creamA", "blend"},
		[2]string{"creamB", "blend"},
		[2]string{"silo", "blend"}, // the silo also goes in direct: depth 1, not 2
	)
	tr, err := Walk("silo", Forward, g.next, wide())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := reachedString(tr); got != "blend@1 creamA@1 creamB@1" {
		t.Errorf("reached %q; the blend is one hop from the silo by the direct line and must be "+
			"named once at that depth", got)
	}
}

// The origin is not in its own answer. A recall list that contains the thing
// being recalled reads as though a batch was made from itself.
func TestWalkDoesNotNameItsOwnOrigin(t *testing.T) {
	g := newGraph([2]string{"a", "b"}, [2]string{"b", "c"})
	tr, err := Walk("a", Forward, g.next, wide())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, r := range tr.Reached {
		if r.BatchID == "a" {
			t.Fatalf("the walk named its own origin: %s", reachedString(tr))
		}
	}
}

// A loop in the data does not hang the recall.
//
// The database refuses cycles, so this cannot arise through the service. It can
// arise through a restore, a migration or a backfill, and a recall is not the
// moment to discover that the genealogy has a knot in it.
func TestWalkTerminatesOnALoopTheDatabaseWouldHaveRefused(t *testing.T) {
	g := newGraph([2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"c", "a"})
	done := make(chan *Trace, 1)
	go func() {
		tr, err := Walk("a", Forward, g.next, wide())
		if err != nil {
			t.Errorf("walk: %v", err)
		}
		done <- tr
	}()
	select {
	case tr := <-done:
		if got := reachedString(tr); got != "b@1 c@2" {
			t.Errorf("reached %q, want each batch in the loop once", got)
		}
		if !tr.Complete {
			t.Error("the walk finished the whole loop and should say so")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the walk did not terminate on a cyclic genealogy")
	}
}

// One query per level, not one per batch. A silo that took forty tankers is one
// question to the database.
func TestWalkAsksOneQuestionPerLevel(t *testing.T) {
	g := newGraph(
		[2]string{"t1", "silo"}, [2]string{"t2", "silo"}, [2]string{"t3", "silo"},
		[2]string{"silo", "cream"}, [2]string{"silo", "skim"},
		[2]string{"cream", "butter"}, [2]string{"skim", "powder"},
	)
	tr, err := Walk("silo", Forward, g.next, wide())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := reachedString(tr); got != "cream@1 skim@1 butter@2 powder@2" {
		t.Fatalf("reached %q", got)
	}
	// Level 1 from the silo, level 2 from {cream, skim}, level 3 from
	// {butter, powder} which finds nothing. Three calls for four batches.
	if g.calls != 3 {
		t.Errorf("the walk made %d queries for a two-level genealogy; it should ask once per "+
			"level, not once per batch: asked %v", g.calls, g.asked)
	}
	if len(g.asked) > 1 && len(g.asked[1]) != 2 {
		t.Errorf("the second query asked about %v; both of the silo's children belong in one "+
			"question", g.asked[1])
	}
}

// A loader that over-fetches must not change the answer.
//
// The shortcut every implementation reaches for eventually is to load the
// tenant's whole edge table once and serve every level from memory. It is a
// reasonable thing to do in a small plant. The walk has to survive it: if it
// trusted the loader to return only the edges it asked about, that shortcut
// would put every batch in the plant at depth one, and a recall on a tanker
// would name the entire year's production.
func TestWalkIsNotFooledByALoaderThatReturnsEverything(t *testing.T) {
	g := newGraph(
		[2]string{"milk", "curd"},
		[2]string{"curd", "paneer"},
		[2]string{"paneer", "carton"},
		// A second, entirely unrelated line running the same day.
		[2]string{"otherMilk", "otherCream"},
		[2]string{"otherCream", "otherButter"},
	)
	everything := func(ids []string) ([]Edge, error) { return g.edges, nil }

	tr, err := Walk("milk", Forward, everything, wide())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := reachedString(tr); got != "curd@1 paneer@2 carton@3" {
		t.Errorf("reached %q; the other line is not downstream of this milk and the depths on "+
			"this one are what a recall reads to know how far the contamination travelled", got)
	}
}

// ---------------------------------------------------------------------------
// Stopping, and saying so
// ---------------------------------------------------------------------------

// The assertion this package exists for: a walk that stopped early does not
// look like one that finished.
func TestADepthBoundedWalkSaysItIsIncompleteAndNamesWhereItStopped(t *testing.T) {
	g := newGraph(
		[2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"c", "d"}, [2]string{"d", "e"},
	)
	tr, err := Walk("a", Forward, g.next, Limit{MaxDepth: 2, MaxBatches: 1000})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := reachedString(tr); got != "b@1 c@2" {
		t.Errorf("reached %q, want two levels", got)
	}
	if tr.Complete {
		t.Fatal("the walk stopped two hops short of the end of the chain and reported itself " +
			"complete; a recall list that stops early and does not say so is acted on as though " +
			"it were the whole answer")
	}
	if len(tr.Frontier) != 1 || tr.Frontier[0] != "c" {
		t.Errorf("frontier is %v; it must name c, which is where somebody continuing the recall "+
			"by hand has to start", tr.Frontier)
	}
	if tr.StoppedBecause == "" {
		t.Error("an incomplete walk must say in words why it stopped")
	}
}

// The batch bound is the harder case: the walk stops partway through a level,
// so some batches one step out were never even named. The frontier has to send
// somebody back to the batches whose edges were half-read, not only to the ones
// that were named.
func TestABatchBoundedWalkPointsBackAtTheHalfReadLevel(t *testing.T) {
	// One silo, five children. A bound of two stops in the middle of level 1.
	g := newGraph(
		[2]string{"silo", "c1"}, [2]string{"silo", "c2"}, [2]string{"silo", "c3"},
		[2]string{"silo", "c4"}, [2]string{"silo", "c5"},
	)
	tr, err := Walk("silo", Forward, g.next, Limit{MaxDepth: 100, MaxBatches: 2})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(tr.Reached) != 2 {
		t.Fatalf("reached %q, want exactly the two it was allowed", reachedString(tr))
	}
	if tr.Complete {
		t.Fatal("the walk named two of five children and reported itself complete")
	}
	found := false
	for _, f := range tr.Frontier {
		if f == "silo" {
			found = true
		}
	}
	if !found {
		t.Errorf("frontier is %v; it must include the silo, because three of its children were "+
			"never named at all and restarting from the two that were would never find them",
			tr.Frontier)
	}
}

// A walk that finishes says so, and carries no frontier. Without this the
// incompleteness tests above would pass against a Walk that reported every
// answer as incomplete — which would be safe and useless.
func TestACompleteWalkSaysSoAndHasNoFrontier(t *testing.T) {
	g := newGraph([2]string{"a", "b"}, [2]string{"b", "c"})
	tr, err := Walk("a", Forward, g.next, wide())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !tr.Complete {
		t.Errorf("a walk that reached the end of the genealogy reported itself incomplete: %s",
			tr.StoppedBecause)
	}
	if len(tr.Frontier) != 0 {
		t.Errorf("a complete walk carries a frontier of %v", tr.Frontier)
	}
	if tr.StoppedBecause != "" {
		t.Errorf("a complete walk says it stopped because %q", tr.StoppedBecause)
	}
}

// A walk that stops exactly at its depth bound reports itself incomplete even
// when, as it happens, nothing lay beyond.
//
// This looks like an off-by-one and is not. The chain here is two deep, the
// bound is two, and the walk reaches the end of it — but it reaches the end
// without ever asking what is below c, so it does not know that c is a leaf.
// Reporting complete would be asserting something it never checked, which is
// the one thing this package must not do. The cost is a second query with a
// larger bound; the alternative is a recall list that claims to be the whole
// answer on the strength of a coincidence.
func TestStoppingAtTheDepthBoundIsIncompleteEvenWhenNothingLayBeyond(t *testing.T) {
	g := newGraph([2]string{"a", "b"}, [2]string{"b", "c"})
	tr, err := Walk("a", Forward, g.next, Limit{MaxDepth: 2, MaxBatches: 1000})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := reachedString(tr); got != "b@1 c@2" {
		t.Fatalf("reached %q, want the whole chain", got)
	}
	if tr.Complete {
		t.Error("the walk stopped at its bound without looking past c and called the answer " +
			"complete")
	}
	if len(tr.Frontier) != 1 || tr.Frontier[0] != "c" {
		t.Errorf("frontier is %v, want the batch it stopped at", tr.Frontier)
	}

	// One more hop of allowance and the walk finds out for itself.
	tr, err = Walk("a", Forward, g.next, Limit{MaxDepth: 3, MaxBatches: 1000})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !tr.Complete {
		t.Errorf("with a bound past the end of the chain the walk still reported itself "+
			"incomplete: %s", tr.StoppedBecause)
	}
	if got := reachedString(tr); got != "b@1 c@2" {
		t.Errorf("the larger bound changed the answer to %q; it should only change the "+
			"confidence in it", got)
	}
}

// A bound is required. Left unset it would be the platform quietly deciding how
// much of a recall gets answered.
func TestAWalkWithoutABoundIsRefused(t *testing.T) {
	g := newGraph([2]string{"a", "b"})
	for _, lim := range []Limit{{}, {MaxDepth: 5}, {MaxBatches: 5}, {MaxDepth: -1, MaxBatches: 5}} {
		if _, err := Walk("a", Forward, g.next, lim); !errors.Is(err, ErrNoLimit) {
			t.Errorf("Walk with limit %+v returned %v, want a refusal", lim, err)
		}
	}
}

func TestAWalkWithoutADirectionIsRefused(t *testing.T) {
	g := newGraph([2]string{"a", "b"})
	if _, err := Walk("a", Direction("SIDEWAYS"), g.next, wide()); err == nil {
		t.Error("a walk with no direction was accepted; it would silently pick one")
	}
	if _, err := Walk("", Forward, g.next, wide()); err == nil {
		t.Error("a walk with no origin was accepted")
	}
}

// A loader that fails must not produce a short answer that looks complete.
func TestALoaderFailureIsNotAnEmptyRecall(t *testing.T) {
	boom := func(ids []string) ([]Edge, error) { return nil, errors.New("connection reset") }
	if _, err := Walk("a", Forward, boom, wide()); err == nil {
		t.Fatal("a walk whose database call failed returned a trace; an empty recall list that " +
			"came from a dropped connection is indistinguishable from a batch that went nowhere")
	}
}

// ---------------------------------------------------------------------------
// Yield
// ---------------------------------------------------------------------------

func kg(v int64) quantity.Quantity {
	q, err := quantity.New(v, quantity.Kilograms)
	if err != nil {
		panic(err)
	}
	return q
}

func litres(v int64) quantity.Quantity {
	q, err := quantity.New(v, quantity.Litres)
	if err != nil {
		panic(err)
	}
	return q
}

func batchOf(out quantity.Quantity) *Batch {
	return &Batch{ID: "b", Code: "PAN-1", Produced: out}
}

func consumed(qs ...quantity.Quantity) []Input {
	var in []Input
	for i, q := range qs {
		in = append(in, Input{ID: fmt.Sprintf("i%d", i), OutputBatchID: "b",
			InputBatchID: fmt.Sprintf("s%d", i), Consumed: q})
	}
	return in
}

// One kilogram of paneer from six kilograms of milk.
func TestObservedYieldIsTheRatioActuallyMeasured(t *testing.T) {
	y, err := ComputeYield(batchOf(kg(1000)), consumed(kg(6000)), nil, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.ObservedPPM == nil {
		t.Fatalf("no observed yield: %s", y.UnavailableReason)
	}
	if *y.ObservedPPM != 166666 {
		t.Errorf("observed yield %d ppm, want 166666 — one kilogram from six", *y.ObservedPPM)
	}
	if got := PercentString(*y.ObservedPPM); got != "16.6666%" {
		t.Errorf("rendered as %q", got)
	}
}

// The inputs are totalled, not taken one at a time. A vat filled from three
// tankers yields against all three.
func TestYieldTotalsEveryInput(t *testing.T) {
	y, err := ComputeYield(batchOf(kg(1000)), consumed(kg(2000), kg(2000), kg(2000)),
		nil, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.ObservedPPM == nil || *y.ObservedPPM != 166666 {
		t.Errorf("observed %v against three tankers totalling six kilograms; want 166666 ppm, "+
			"which is what a yield against only the first would not give", y.ObservedPPM)
	}
	if y.Consumed.Value() != 6000 {
		t.Errorf("totalled %s of input, want 6.000", y.Consumed)
	}
}

// Where no expectation was declared there is no variance, and the report says
// none was declared rather than showing a variance against nothing.
func TestNoDeclaredExpectationIsReportedAsSuch(t *testing.T) {
	y, err := ComputeYield(batchOf(kg(1000)), consumed(kg(6000)), nil, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if !y.NoExpectationDeclared {
		t.Error("a batch with no declared yield did not report that none was declared")
	}
	if y.VariancePPM != nil {
		t.Errorf("variance of %d against an expectation nobody stated; a number somebody is "+
			"asked to explain and cannot", *y.VariancePPM)
	}
}

func TestVarianceIsObservedMinusExpected(t *testing.T) {
	expected := int64(180000) // the plant expected 18%
	y, err := ComputeYield(batchOf(kg(1000)), consumed(kg(6000)), &expected, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.NoExpectationDeclared {
		t.Error("an expectation was declared and the yield says it was not")
	}
	if y.VariancePPM == nil {
		t.Fatal("no variance against a declared expectation")
	}
	// 166666 observed against 180000 expected: the vat came up short.
	if *y.VariancePPM != -13334 {
		t.Errorf("variance %d ppm, want -13334; the sign says which way the plant was wrong and "+
			"reversing it turns a shortfall into a windfall", *y.VariancePPM)
	}
}

// Litres in, kilograms out, and no density: the yield is not computed.
//
// This is the case that happens in every plant, every day: milk is measured in
// litres and paneer is weighed. Assuming the 1.03 that everyone quotes would
// put a three per cent error into every yield in the plant, and it would look
// like a process problem rather than an arithmetic one.
func TestYieldRefusesToInventADensity(t *testing.T) {
	y, err := ComputeYield(batchOf(kg(1000)), consumed(litres(6000)), nil, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.ObservedPPM != nil {
		t.Fatalf("a yield of %d ppm was computed from litres against kilograms with no density "+
			"supplied", *y.ObservedPPM)
	}
	if y.UnavailableReason == "" {
		t.Error("no yield and no reason; a blank cell in a variance report is read as a zero")
	}
	if !strings.Contains(y.UnavailableReason, "density") {
		t.Errorf("reason %q does not say a density is what is missing", y.UnavailableReason)
	}
}

// With a density supplied, the conversion happens and the yield is real.
func TestYieldConvertsWhenADensityIsSupplied(t *testing.T) {
	rate, err := money.NewRate(1030, 3)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}
	d := quantity.Density{KgPerLitre: rate, AtCelsius: 200, Source: quantity.Lactometer}
	y, err := ComputeYield(batchOf(kg(1000)), consumed(litres(6000)), nil, &d, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.ObservedPPM == nil {
		t.Fatalf("no yield with a density supplied: %s", y.UnavailableReason)
	}
	// 6.000 L at 1.030 kg/L is 6.180 kg, so 1.000 kg out is 161812 ppm — and
	// not the 166666 the same numbers give if the density is ignored.
	if *y.ObservedPPM != 161812 {
		t.Errorf("observed %d ppm; want 161812. 166666 would mean the density was not applied",
			*y.ObservedPPM)
	}
	if y.Consumed.Value() != 6180 {
		t.Errorf("totalled %s after conversion, want 6.180", y.Consumed)
	}
}

// A batch with nothing recorded going into it has no yield, and says why. This
// is the state every batch passes through between being created and having its
// inputs recorded, and a zero yield in that window would be a plant-wide alarm
// every time somebody typed slowly.
func TestABatchWithNoInputsHasNoYieldRatherThanZero(t *testing.T) {
	y, err := ComputeYield(batchOf(kg(1000)), nil, nil, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.ObservedPPM != nil {
		t.Errorf("a yield of %d ppm from no inputs at all", *y.ObservedPPM)
	}
	if y.UnavailableReason == "" {
		t.Fatal("no inputs, no yield, and no explanation")
	}
	if !strings.Contains(y.UnavailableReason, "no inputs") {
		t.Errorf("reason %q; a batch nobody has recorded inputs for yet needs different advice "+
			"from one whose inputs total nothing — the first is somebody still typing and the "+
			"second is a figure that is wrong", y.UnavailableReason)
	}

	// The other way to have nothing to divide by: input lines that are there
	// and add up to zero. That is a different problem and says so.
	zeroed := []Input{{ID: "i0", OutputBatchID: "b", InputBatchID: "s0", Consumed: kg(0)}}
	y, err = ComputeYield(batchOf(kg(1000)), zeroed, nil, nil, money.RoundHalfUp)
	if err != nil {
		t.Fatalf("yield: %v", err)
	}
	if y.ObservedPPM != nil {
		t.Errorf("a yield of %d ppm against inputs totalling nothing", *y.ObservedPPM)
	}
	if strings.Contains(y.UnavailableReason, "no inputs") {
		t.Errorf("reason %q; there are inputs, they total nothing, and telling somebody to go "+
			"and record the inputs sends them looking for lines that already exist",
			y.UnavailableReason)
	}
}

func TestPercentStringCarriesTheSign(t *testing.T) {
	for _, c := range []struct {
		ppm  int64
		want string
	}{
		{166666, "16.6666%"},
		{-13334, "-1.3334%"},
		{1000000, "100.0000%"},
		{0, "0.0000%"},
		{5, "0.0005%"},
	} {
		if got := PercentString(c.ppm); got != c.want {
			t.Errorf("PercentString(%d) = %q, want %q", c.ppm, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// What a batch may say about itself
// ---------------------------------------------------------------------------

func goodBatch() *Batch {
	return &Batch{
		TenantID: "t", Code: "SILO-1", Kind: Intermediate, ProductRef: "CREAM",
		Produced: kg(1000), ProducedAt: time.Now(), ProducedBy: "op",
		Status: Open, CreatedBy: "u",
	}
}

func TestRawMilkMustSayWhereItCameFrom(t *testing.T) {
	b := goodBatch()
	b.Kind = Raw
	if err := b.Validate(); !errors.Is(err, ErrRawNeedsSource) {
		t.Errorf("a raw batch with no source was accepted (%v); the genealogy would stop at the "+
			"plant gate, which is where a recall needs to keep going", err)
	}
	b.SourceKind = FromMovement
	if err := b.Validate(); !errors.Is(err, ErrRawNeedsSource) {
		t.Errorf("a raw batch naming a kind and no reference was accepted: %v", err)
	}
	b.SourceRef = "mv1"
	if err := b.Validate(); err != nil {
		t.Errorf("a raw batch that says where it came from was refused: %v", err)
	}
}

func TestOnlyRawMilkNamesAnOutsideSource(t *testing.T) {
	b := goodBatch()
	b.SourceKind, b.SourceRef = FromMovement, "mv1"
	if err := b.Validate(); !errors.Is(err, ErrSourceOnMade) {
		t.Errorf("a batch made from other batches also named an outside source (%v); it now has "+
			"two accounts of where it came from and nothing reconciles them", err)
	}
}

func TestAHeldBatchMustSayWhy(t *testing.T) {
	for _, s := range []Status{Quarantined, Recalled, Disposed} {
		b := goodBatch()
		b.Status = s
		if err := b.Validate(); !errors.Is(err, ErrHoldNeedsReason) {
			t.Errorf("%s with no reason was accepted: %v", s, err)
		}
		b.StatusReason = "antibiotic screen"
		if err := b.Validate(); err != nil {
			t.Errorf("%s with a reason was refused: %v", s, err)
		}
	}
	for _, s := range []Status{Open, Released} {
		b := goodBatch()
		b.Status = s
		if err := b.Validate(); err != nil {
			t.Errorf("%s with no reason was refused (%v); ordinary batches do not need one", s, err)
		}
	}
}

func TestHeldIsTheSetThatStopsTheLotMoving(t *testing.T) {
	for _, s := range []Status{Quarantined, Recalled, Disposed} {
		if !s.Held() {
			t.Errorf("%s does not count as held, so the lot keeps moving through the plant", s)
		}
	}
	for _, s := range []Status{Open, Released} {
		if s.Held() {
			t.Errorf("%s counts as held, so ordinary production is refused", s)
		}
	}
}

func TestABatchOfNothingIsRefused(t *testing.T) {
	b := goodBatch()
	b.Produced = kg(0)
	if err := b.Validate(); !errors.Is(err, ErrNotPositive) {
		t.Errorf("a batch of nothing was accepted: %v", err)
	}
	b.Produced = quantity.Quantity{Amount: kg(1000).Amount}
	if err := b.Validate(); !errors.Is(err, quantity.ErrNoUnit) {
		t.Errorf("a batch with no unit was accepted (%v); litres and kilograms differ by three "+
			"per cent and nothing here would say which was meant", err)
	}
}

func TestABatchIsNotAnInputToItself(t *testing.T) {
	in := &Input{TenantID: "t", OutputBatchID: "b", InputBatchID: "b",
		Consumed: kg(10), CreatedBy: "u"}
	if err := in.Validate(); !errors.Is(err, ErrConsumeSelf) {
		t.Errorf("a batch consuming itself was accepted: %v", err)
	}
}
