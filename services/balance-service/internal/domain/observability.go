package domain

import (
	"errors"
	"sort"
)

// Observability is what a window's measurement layout can and cannot tell you,
// before any milk is measured at all.
//
// It answers two questions a reconciled figure does not:
//
//   - Of the legs nobody measured, which can be worked out from the ones that
//     were? An unobservable leg is one the reconciler will report a number for
//     that is not determined by the data — any value on it can be absorbed by
//     the others without breaking a single node balance.
//
//   - Of the legs that were measured, which can be checked against the rest? A
//     measurement that cannot be cross-checked is one the reconciler will always
//     accept exactly as given. It can be wrong by any amount and nothing in the
//     window will notice, which makes "the window reconciled" a much weaker
//     statement than it sounds.
//
// This is a property of where the instruments are, not of what they read, so it
// can be computed before a route is ever run — which is the useful time to find
// out that the tanker leg is the only one anybody can verify.
type Observability struct {
	// Observable are unmeasured flow ids the node balances determine uniquely.
	Observable []string
	// Unobservable are unmeasured flow ids they do not. The reconciler will
	// still print a value; it is one of infinitely many that fit.
	Unobservable []string

	// Redundant are measured flow ids that can be computed from the others, so
	// a gross error on them can be detected.
	Redundant []string
	// JustDetermined are measured flow ids that cannot. Nothing in the window
	// disagrees with them however wrong they are.
	JustDetermined []string
}

// FullyRedundant reports whether every measurement in the window can be checked.
func (o *Observability) FullyRedundant() bool { return len(o.JustDetermined) == 0 }

// FullyObservable reports whether every unmeasured leg is determined.
func (o *Observability) FullyObservable() bool { return len(o.Unobservable) == 0 }

var ErrNoFlowsToClassify = errors.New("there are no flows to classify")

// edge is one flow as an undirected connection, which is all the structure
// matters. A node balance is an equation whichever way the milk runs; reversing
// a flow changes the sign of a term and not whether the term is there.
type edge struct {
	id       string
	a, b     string
	measured bool
}

// Classify works out which legs are determined and which measurements can be
// checked.
//
// The method is graph-theoretic rather than algebraic, and exactly equivalent
// for a network of flows. The node-balance equations are the incidence matrix of
// the graph, and its null space is spanned by the cycles — so:
//
//   - An unmeasured leg is determined exactly when it lies on no cycle of the
//     unmeasured subgraph. A cycle of unmeasured legs can carry any circulation
//     at all without changing a single node's balance, so nothing in the data
//     picks a value for the legs on it.
//
//   - A measured leg can be checked exactly when it lies on a cycle of the graph
//     with the unmeasured legs contracted away. That cycle is the second path by
//     which its value can be reached, and having two is what makes disagreement
//     detectable.
//
// Doing it this way rather than by elimination means the answer is exact with no
// arithmetic at all: there is no tolerance to choose, and no near-singular
// matrix to decide about.
func Classify(flows []FlowMeasurement) (*Observability, error) {
	if len(flows) == 0 {
		return nil, ErrNoFlowsToClassify
	}
	if err := ValidateFlows(flows); err != nil {
		return nil, err
	}

	edges := make([]edge, 0, len(flows))
	for _, f := range flows {
		edges = append(edges, edge{
			id: f.FlowID, a: f.From.ID, b: f.To.ID, measured: !f.Unmeasured,
		})
	}

	// The unmeasured subgraph. A leg in it is determined exactly when it is a
	// bridge: removing a bridge disconnects its ends, which is another way of
	// saying it is on no cycle.
	var unmeasured []edge
	for _, e := range edges {
		if !e.measured {
			unmeasured = append(unmeasured, e)
		}
	}
	unmeasuredBridges := bridges(unmeasured)

	// Contract the unmeasured legs: two nodes joined by one are the same node
	// as far as a measured leg's checkability goes, because milk can pass
	// between them without anybody measuring it.
	contract := newUnionFind()
	for _, e := range unmeasured {
		contract.union(e.a, e.b)
	}
	var measured []edge
	for _, e := range edges {
		if e.measured {
			measured = append(measured, edge{
				id: e.id, a: contract.find(e.a), b: contract.find(e.b), measured: true,
			})
		}
	}
	measuredBridges := bridges(measured)

	out := &Observability{}
	for _, e := range edges {
		if e.measured {
			if measuredBridges[e.id] {
				out.JustDetermined = append(out.JustDetermined, e.id)
			} else {
				out.Redundant = append(out.Redundant, e.id)
			}
			continue
		}
		if unmeasuredBridges[e.id] {
			out.Observable = append(out.Observable, e.id)
		} else {
			out.Unobservable = append(out.Unobservable, e.id)
		}
	}

	// Sorted, so the same window classified twice reads the same. A caller
	// diffing two runs of the same layout depends on it, and the underlying
	// traversal order does not.
	sort.Strings(out.Observable)
	sort.Strings(out.Unobservable)
	sort.Strings(out.Redundant)
	sort.Strings(out.JustDetermined)
	return out, nil
}

// bridges returns the ids of edges whose removal disconnects their ends.
//
// Tarjan's algorithm, iterative so a long chain of coolers cannot exhaust the
// stack, and keyed on the edge rather than on the parent node — which is what
// makes it correct in the presence of two legs between the same pair. Two
// parallel legs are each other's second path, so neither is a bridge, and a
// version that skipped "the edge back to my parent" by node would wrongly call
// them both bridges.
//
// A self-loop counts as a bridge here, and the reason is worth stating because
// the graph-theoretic instinct says the opposite.
//
// A self-loop arises when contracting the unmeasured legs merges both ends of a
// measured one — milk that could have gone round the unmeasured way instead.
// Its contribution to the merged node's balance is plus x minus x, which is
// zero: the equation says nothing about it at all. So it is exactly as
// unconstrained as a leg with no second path, and calling it "on a cycle"
// because it closes on itself would report an unchecked measurement as checked.
//
// Worked through: a cooler measured on one branch of an unmetered split, where
// the other branch rejoins. Eliminating the unmeasured branch cancels the
// measured one out of every equation, leaving only inflow equals outflow. The
// branch measurement can be wrong by any amount and the window still closes.
func bridges(edges []edge) map[string]bool {
	if len(edges) == 0 {
		return map[string]bool{}
	}

	type half struct {
		to   string
		edge int
	}
	adj := map[string][]half{}
	isBridge := make([]bool, len(edges))
	for i, e := range edges {
		if e.a == e.b {
			isBridge[i] = true
			continue
		}
		adj[e.a] = append(adj[e.a], half{to: e.b, edge: i})
		adj[e.b] = append(adj[e.b], half{to: e.a, edge: i})
	}

	disc := map[string]int{}
	low := map[string]int{}
	timer := 0

	type frame struct {
		node     string
		viaEdge  int
		childIdx int
	}

	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	for _, root := range nodes {
		if _, seen := disc[root]; seen {
			continue
		}
		stack := []frame{{node: root, viaEdge: -1}}
		timer++
		disc[root], low[root] = timer, timer

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.childIdx < len(adj[top.node]) {
				h := adj[top.node][top.childIdx]
				top.childIdx++
				if h.edge == top.viaEdge {
					// The edge we arrived by. Skipped once, by edge and not by
					// node, so a parallel leg between the same pair is still
					// traversed and correctly makes neither a bridge.
					continue
				}
				if d, seen := disc[h.to]; seen {
					if d < low[top.node] {
						low[top.node] = d
					}
					continue
				}
				timer++
				disc[h.to], low[h.to] = timer, timer
				stack = append(stack, frame{node: h.to, viaEdge: h.edge})
				continue
			}

			// Done with this node: fold its low-point into its parent.
			done := *top
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				continue
			}
			parent := &stack[len(stack)-1]
			if low[done.node] < low[parent.node] {
				low[parent.node] = low[done.node]
			}
			if low[done.node] > disc[parent.node] {
				isBridge[done.viaEdge] = true
			}
		}
	}

	out := map[string]bool{}
	for i, e := range edges {
		if isBridge[i] {
			out[e.id] = true
		}
	}
	return out
}

type unionFind struct{ parent map[string]string }

func newUnionFind() *unionFind { return &unionFind{parent: map[string]string{}} }

func (u *unionFind) find(x string) string {
	p, seen := u.parent[x]
	if !seen {
		u.parent[x] = x
		return x
	}
	if p == x {
		return x
	}
	root := u.find(p)
	u.parent[x] = root
	return root
}

func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}
