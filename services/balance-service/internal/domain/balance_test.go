package domain

import (
	"errors"
	"fmt"
	"testing"
)

func measured(id, from, to, quantity, uncertainty string) FlowMeasurement {
	return FlowMeasurement{
		FlowID:              id,
		From:                node(from),
		To:                  node(to),
		Measured:            quantity,
		StandardUncertainty: uncertainty,
	}
}

func unmeasured(id, from, to string) FlowMeasurement {
	return FlowMeasurement{
		FlowID:     id,
		From:       node(from),
		To:         node(to),
		Measured:   "0.000",
		Unmeasured: true,
	}
}

// node gives every interior node the same kind; which kind it is only matters
// to the vocabulary check.
func node(id string) BalanceNode {
	if id == Boundary {
		return BalanceNode{}
	}
	return BalanceNode{ID: id, Kind: NodeChillingUnit}
}

// seriesNode is one inflow and one outflow at a chilling centre, two litres
// short at the outlet.
func seriesNode() []FlowMeasurement {
	return []FlowMeasurement{
		measured("in", Boundary, "cc-1", "100.000", "1.000"),
		measured("out", "cc-1", Boundary, "98.000", "1.000"),
	}
}

// splittingNode is one intake feeding two tankers, five litres unaccounted for.
func splittingNode() []FlowMeasurement {
	return []FlowMeasurement{
		measured("intake", Boundary, "cc-1", "100.000", "1.000"),
		measured("tanker-a", "cc-1", Boundary, "40.000", "1.000"),
		measured("tanker-b", "cc-1", Boundary, "55.000", "1.000"),
	}
}

func TestValidateFlows(t *testing.T) {
	tooMany := make([]FlowMeasurement, MaxFlowsPerWindow+1)
	for i := range tooMany {
		tooMany[i] = measured(fmt.Sprintf("f%d", i), Boundary, "cc-1", "1.000", "1.000")
	}

	cases := []struct {
		name  string
		flows []FlowMeasurement
		// wantErr is the sentinel expected; wantAnyErr covers failures that are
		// wrapped parse errors rather than a sentinel of this package.
		wantErr    error
		wantAnyErr bool
	}{
		{name: "series node", flows: seriesNode()},
		{name: "splitting node", flows: splittingNode()},
		{
			name: "an unmeasured flow needs no uncertainty",
			flows: []FlowMeasurement{
				measured("intake", Boundary, "cc-1", "100.000", "1.000"),
				unmeasured("loss", "cc-1", Boundary),
			},
		},
		{name: "no flows", flows: nil, wantErr: ErrNoFlows},
		{name: "empty slice", flows: []FlowMeasurement{}, wantErr: ErrNoFlows},
		{name: "more flows than the reconciler accepts", flows: tooMany, wantErr: ErrTooManyFlows},
		{
			name: "duplicate flow id",
			flows: []FlowMeasurement{
				measured("in", Boundary, "cc-1", "100.000", "1.000"),
				measured("in", "cc-1", Boundary, "98.000", "1.000"),
			},
			wantErr: ErrDuplicateFlowID,
		},
		{
			name:    "flow with no id",
			flows:   []FlowMeasurement{measured("", Boundary, "cc-1", "100.000", "1.000")},
			wantErr: ErrNoFlowID,
		},
		{
			name:    "zero uncertainty on a measured flow",
			flows:   []FlowMeasurement{measured("in", Boundary, "cc-1", "100.000", "0.000")},
			wantErr: ErrUncertaintyNotPositive,
		},
		{
			name:    "negative uncertainty on a measured flow",
			flows:   []FlowMeasurement{measured("in", Boundary, "cc-1", "100.000", "-1.000")},
			wantErr: ErrUncertaintyNotPositive,
		},
		{
			name:       "missing uncertainty on a measured flow",
			flows:      []FlowMeasurement{measured("in", Boundary, "cc-1", "100.000", "")},
			wantAnyErr: true,
		},
		{
			name:       "measured value finer than the schema's scale",
			flows:      []FlowMeasurement{measured("in", Boundary, "cc-1", "100.0001", "1.000")},
			wantAnyErr: true,
		},
		{
			name:    "boundary to boundary",
			flows:   []FlowMeasurement{measured("nowhere", Boundary, Boundary, "100.000", "1.000")},
			wantErr: ErrBoundaryToBoundary,
		},
		{
			name:    "self loop",
			flows:   []FlowMeasurement{measured("recirculation", "cc-1", "cc-1", "100.000", "1.000")},
			wantErr: ErrSelfLoop,
		},
		{
			name: "unrecognised node kind",
			flows: []FlowMeasurement{{
				FlowID:              "in",
				To:                  BalanceNode{ID: "cc-1", Kind: "WAREHOUSE"},
				Measured:            "100.000",
				StandardUncertainty: "1.000",
			}},
			wantErr: ErrUnknownNodeKind,
		},
		{
			name: "interior node with no kind",
			flows: []FlowMeasurement{{
				FlowID:              "in",
				To:                  BalanceNode{ID: "cc-1"},
				Measured:            "100.000",
				StandardUncertainty: "1.000",
			}},
			wantErr: ErrUnknownNodeKind,
		},
		{
			name: "the boundary claims a kind",
			flows: []FlowMeasurement{{
				FlowID:              "in",
				From:                BalanceNode{ID: Boundary, Kind: NodePlant},
				To:                  BalanceNode{ID: "cc-1", Kind: NodeChillingUnit},
				Measured:            "100.000",
				StandardUncertainty: "1.000",
			}},
			wantErr: ErrBoundaryHasNoKind,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateFlows(c.flows)
			switch {
			case c.wantAnyErr:
				if err == nil {
					t.Fatal("the flows were accepted but carry an unusable quantity")
				}
			case c.wantErr == nil:
				if err != nil {
					t.Fatalf("valid flows were rejected: %v", err)
				}
			case !errors.Is(err, c.wantErr):
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}

// A quantity finer than the schema's scale is refused rather than truncated:
// discarding the third decimal silently would move milk nobody could account
// for.
func TestParseQuantityKeepsTheThirdDecimalAndRefusesAFourth(t *testing.T) {
	q, err := ParseQuantity("1234.567")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Value != 1234567 {
		t.Errorf("value = %d, want 1234567", q.Value)
	}
	if q.String() != "1234.567" {
		t.Errorf("round trip = %s, want 1234.567", q.String())
	}
	if _, err := ParseQuantity("1234.5678"); err == nil {
		t.Error("a literal with four decimals was accepted and would have been truncated")
	}
}

func TestInteriorNodes(t *testing.T) {
	cases := []struct {
		name    string
		flows   []FlowMeasurement
		want    []string
		wantErr error
	}{
		{"series node", seriesNode(), []string{"cc-1"}, nil},
		{"splitting node names its node once", splittingNode(), []string{"cc-1"}, nil},
		{
			"a chain names every node, sorted",
			[]FlowMeasurement{
				measured("haul", "cc-1", "plant-1", "130.000", "1.000"),
				measured("collect", Boundary, "cc-1", "100.000", "1.000"),
				measured("intake", "plant-1", Boundary, "100.000", "1.000"),
			},
			[]string{"cc-1", "plant-1"},
			nil,
		},
		{
			"every flow crosses the boundary",
			[]FlowMeasurement{
				{FlowID: "a", Measured: "10.000", StandardUncertainty: "1.000"},
			},
			nil,
			ErrNoInteriorNodes,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := InteriorNodes(c.flows)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("got %v, want %v", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("interior nodes: %v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestResidualByNode(t *testing.T) {
	cases := []struct {
		name  string
		flows []FlowMeasurement
		// want maps node id to the expected imbalance as a decimal literal.
		want      map[string]string
		wantTotal string
	}{
		{
			"a series node is short by the difference",
			seriesNode(),
			map[string]string{"cc-1": "2.000"},
			"2.000",
		},
		{
			"a splitting node counts both outflows",
			splittingNode(),
			map[string]string{"cc-1": "5.000"},
			"5.000",
		},
		{
			"a chain attributes the imbalance to each end of the bad leg",
			[]FlowMeasurement{
				measured("collect", Boundary, "cc-1", "100.000", "1.000"),
				measured("haul", "cc-1", "plant-1", "130.000", "1.000"),
				measured("intake", "plant-1", Boundary, "100.000", "1.000"),
			},
			map[string]string{"cc-1": "-30.000", "plant-1": "30.000"},
			"60.000",
		},
		{
			"a balanced node has no residual",
			[]FlowMeasurement{
				measured("in", Boundary, "cc-1", "100.000", "1.000"),
				measured("out", "cc-1", Boundary, "100.000", "1.000"),
			},
			map[string]string{"cc-1": "0.000"},
			"0.000",
		},
		{
			"the third decimal survives",
			[]FlowMeasurement{
				measured("in", Boundary, "cc-1", "100.125", "1.000"),
				measured("out", "cc-1", Boundary, "100.124", "1.000"),
			},
			map[string]string{"cc-1": "0.001"},
			"0.001",
		},
		{
			"an unmeasured flow contributes nothing",
			[]FlowMeasurement{
				measured("intake", Boundary, "cc-1", "100.000", "1.000"),
				unmeasured("loss", "cc-1", Boundary),
			},
			map[string]string{"cc-1": "100.000"},
			"100.000",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResidualByNode(c.flows)
			if err != nil {
				t.Fatalf("residual: %v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %d nodes, want %d: %+v", len(got), len(c.want), got)
			}
			for _, r := range got {
				want, ok := c.want[r.NodeID]
				if !ok {
					t.Fatalf("unexpected node %q in the residual", r.NodeID)
				}
				if r.Imbalance.String() != want {
					t.Errorf("node %s imbalance = %s, want %s", r.NodeID, r.Imbalance.String(), want)
				}
			}

			total, err := TotalResidual(got)
			if err != nil {
				t.Fatalf("total residual: %v", err)
			}
			if total.String() != c.wantTotal {
				t.Errorf("total residual = %s, want %s", total.String(), c.wantTotal)
			}
		})
	}
}

// Offsetting imbalances do not cancel: a node short by five and another long by
// five is a network with ten litres in the wrong place, not a closed one.
func TestTotalResidualSumsMagnitudes(t *testing.T) {
	residuals, err := ResidualByNode([]FlowMeasurement{
		measured("collect", Boundary, "cc-1", "100.000", "1.000"),
		measured("haul", "cc-1", "plant-1", "105.000", "1.000"),
		measured("intake", "plant-1", Boundary, "100.000", "1.000"),
	})
	if err != nil {
		t.Fatalf("residual: %v", err)
	}
	total, err := TotalResidual(residuals)
	if err != nil {
		t.Fatalf("total: %v", err)
	}
	if total.String() != "10.000" {
		t.Errorf("total = %s, want 10.000; offsetting imbalances must not cancel", total.String())
	}
}

func TestResidualByNodeRejectsAMalformedQuantity(t *testing.T) {
	_, err := ResidualByNode([]FlowMeasurement{
		measured("in", Boundary, "cc-1", "one hundred", "1.000"),
	})
	if err == nil {
		t.Fatal("a non-numeric measured value produced a residual")
	}
}

func TestValidateRun(t *testing.T) {
	cases := []struct {
		name    string
		run     ReconciliationRun
		wantErr error
	}{
		{
			"a converged run may nominate a gross error",
			ReconciliationRun{
				Converged: true,
				Flows:     []ReconciledFlow{{FlowID: "haul", TestStatistic: 12.5, GrossError: true}},
			},
			nil,
		},
		{
			"a run that did not converge is kept when it accuses nobody",
			ReconciliationRun{
				Converged: false,
				Flows:     []ReconciledFlow{{FlowID: "out"}, {FlowID: "back"}},
			},
			nil,
		},
		{
			"a run that did not converge may not accuse",
			ReconciliationRun{
				Converged: false,
				Flows:     []ReconciledFlow{{FlowID: "out", GrossError: true}},
			},
			ErrGrossErrorWithoutConvergence,
		},
		{
			"an unmeasured flow cannot carry a gross error",
			ReconciliationRun{
				Converged: true,
				Flows:     []ReconciledFlow{{FlowID: "loss", Unmeasured: true, GrossError: true}},
			},
			ErrGrossErrorOnUnmeasuredFlow,
		},
		{
			"a run with no flows at all",
			ReconciliationRun{Converged: false},
			nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateRun(&c.run)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("a storable run was rejected: %v", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}
