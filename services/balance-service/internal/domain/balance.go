package domain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// MaxFlowsPerWindow bounds the network a single window may describe. The
// constrained solve is dense, so an unbounded window is a way to occupy the
// model tier indefinitely.
const MaxFlowsPerWindow = 5000

// QuantityScale is the three decimals milk is measured to. Litres to the
// millilitre, kilograms to the gram.
const QuantityScale = 3

// quantityUnit is ISO 4217's "no currency". The money package is used here for
// its exact fixed-point arithmetic, not because litres are money.
const quantityUnit = "XXX"

var (
	ErrNoFlows                = errors.New("the window carries no flows to reconcile")
	ErrTooManyFlows           = errors.New("the window carries more flows than the reconciler accepts")
	ErrNoFlowID               = errors.New("a flow carries no flow id")
	ErrDuplicateFlowID        = errors.New("a flow id appears more than once in the window")
	ErrUncertaintyNotPositive = errors.New("a measured flow needs a positive standard uncertainty")
	ErrBoundaryToBoundary     = errors.New("a flow runs from the system boundary to the system boundary")
	ErrSelfLoop               = errors.New("a flow leaves and enters the same node")
	ErrUnknownNodeKind        = errors.New("node kind is not recognised")
	ErrBoundaryHasNoKind      = errors.New("the system boundary is not a node kind")

	// ErrNoInteriorNodes means every flow crosses the boundary, so there is no
	// point whose inflow must equal its outflow and nothing to reconcile.
	ErrNoInteriorNodes = errors.New("no interior node: every flow crosses the system boundary")

	ErrGrossErrorWithoutConvergence = errors.New("a run that did not converge may not nominate a gross error")
	ErrGrossErrorOnUnmeasuredFlow   = errors.New("an unmeasured flow has no measurement to fail")
)

// ParseQuantity reads a decimal literal at QuantityScale. It never rounds: a
// literal finer than the scale is refused rather than silently truncated.
func ParseQuantity(s string) (money.Money, error) { return money.Parse(s, QuantityScale, quantityUnit) }

func ZeroQuantity() money.Money { return money.Zero(QuantityScale, quantityUnit) }

// ValidateFlows checks a window is reconcilable before anything is asked of the
// model tier, so a malformed network fails here with a reason rather than as an
// opaque model error.
func ValidateFlows(flows []FlowMeasurement) error {
	if len(flows) == 0 {
		return ErrNoFlows
	}
	if len(flows) > MaxFlowsPerWindow {
		return fmt.Errorf("%w: %d flows, limit is %d", ErrTooManyFlows, len(flows), MaxFlowsPerWindow)
	}

	seen := make(map[string]bool, len(flows))
	for _, f := range flows {
		if f.FlowID == "" {
			return ErrNoFlowID
		}
		if seen[f.FlowID] {
			return fmt.Errorf("%w: %q", ErrDuplicateFlowID, f.FlowID)
		}
		seen[f.FlowID] = true

		if err := validateNode(f.From); err != nil {
			return fmt.Errorf("flow %q from_node: %w", f.FlowID, err)
		}
		if err := validateNode(f.To); err != nil {
			return fmt.Errorf("flow %q to_node: %w", f.FlowID, err)
		}
		if f.From.IsBoundary() && f.To.IsBoundary() {
			return fmt.Errorf("%w: %q", ErrBoundaryToBoundary, f.FlowID)
		}
		if f.From.ID == f.To.ID {
			return fmt.Errorf("%w: %q at %q", ErrSelfLoop, f.FlowID, f.From.ID)
		}

		if f.Unmeasured {
			continue
		}
		if _, err := ParseQuantity(f.Measured); err != nil {
			return fmt.Errorf("flow %q measured: %w", f.FlowID, err)
		}
		u, err := ParseQuantity(f.StandardUncertainty)
		if err != nil {
			return fmt.Errorf("flow %q standard uncertainty: %w", f.FlowID, err)
		}
		// A zero or negative uncertainty weights the stream infinitely and lets
		// one instrument dictate the whole solution.
		if u.Value <= 0 {
			return fmt.Errorf("%w: flow %q states %q", ErrUncertaintyNotPositive, f.FlowID, f.StandardUncertainty)
		}
	}
	return nil
}

func validateNode(n BalanceNode) error {
	if n.IsBoundary() {
		if n.Kind != "" {
			return fmt.Errorf("%w: %q", ErrBoundaryHasNoKind, n.Kind)
		}
		return nil
	}
	switch n.Kind {
	case NodeCollectionCentre, NodeTanker, NodeChillingUnit, NodePlant:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownNodeKind, n.Kind)
	}
}

// InteriorNodes lists the nodes that are not the system boundary, in a stable
// order.
func InteriorNodes(flows []FlowMeasurement) ([]string, error) {
	seen := make(map[string]bool, len(flows))
	out := make([]string, 0, len(flows))
	for _, f := range flows {
		for _, n := range []BalanceNode{f.From, f.To} {
			if n.IsBoundary() || seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			out = append(out, n.ID)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoInteriorNodes
	}
	sort.Strings(out)
	return out, nil
}

// NodeResidual is one interior node's imbalance.
type NodeResidual struct {
	NodeID string `json:"node_id"`
	// Imbalance is inflow minus outflow: positive means more milk was measured
	// arriving than leaving.
	Imbalance money.Money `json:"imbalance"`
}

// ResidualByNode computes each interior node's imbalance from the measured
// values alone.
//
// This exists so the platform can say a window does not close, and by how much,
// without the model tier. An investigation into a missing 47 litres must not
// depend on a reconciler being reachable.
func ResidualByNode(flows []FlowMeasurement) ([]NodeResidual, error) {
	nodes, err := InteriorNodes(flows)
	if err != nil {
		return nil, err
	}

	byNode := make(map[string]money.Money, len(nodes))
	for _, n := range nodes {
		byNode[n] = ZeroQuantity()
	}

	for _, f := range flows {
		// An unmeasured flow states no quantity: its value is an output of the
		// solve, so it cannot contribute to the imbalance being solved for.
		if f.Unmeasured {
			continue
		}
		v, err := ParseQuantity(f.Measured)
		if err != nil {
			return nil, fmt.Errorf("flow %q measured: %w", f.FlowID, err)
		}
		if !f.To.IsBoundary() {
			if byNode[f.To.ID], err = money.Add(byNode[f.To.ID], v); err != nil {
				return nil, fmt.Errorf("node %q inflow: %w", f.To.ID, err)
			}
		}
		if !f.From.IsBoundary() {
			if byNode[f.From.ID], err = money.Sub(byNode[f.From.ID], v); err != nil {
				return nil, fmt.Errorf("node %q outflow: %w", f.From.ID, err)
			}
		}
	}

	out := make([]NodeResidual, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, NodeResidual{NodeID: n, Imbalance: byNode[n]})
	}
	return out, nil
}

// TotalResidual sums the node imbalances by magnitude, so a network where one
// node is short by 5 and another long by 5 reports 10 rather than closing.
func TotalResidual(residuals []NodeResidual) (money.Money, error) {
	total := ZeroQuantity()
	var err error
	for _, r := range residuals {
		if total, err = money.Add(total, r.Imbalance.Abs()); err != nil {
			return money.Money{}, fmt.Errorf("node %q: %w", r.NodeID, err)
		}
	}
	return total, nil
}

// ValidateRun checks a run before it is stored.
//
// A run that did not converge is kept rather than discarded — an
// under-determined network is a real operational state and the imbalance is
// still evidence — but its adjustments are indicative, so it must not nominate
// a culprit. Naming a leg as carrying a gross error on the strength of an
// arithmetic that never closed would put an accusation behind nothing.
func ValidateRun(run *ReconciliationRun) error {
	for _, f := range run.Flows {
		if !f.GrossError {
			continue
		}
		if !run.Converged {
			return fmt.Errorf("%w: flow %q", ErrGrossErrorWithoutConvergence, f.FlowID)
		}
		if f.Unmeasured {
			return fmt.Errorf("%w: flow %q", ErrGrossErrorOnUnmeasuredFlow, f.FlowID)
		}
	}
	return nil
}
