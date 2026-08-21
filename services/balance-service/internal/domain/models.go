package domain

import "time"

// NodeKind names what a point in the milk network is.
type NodeKind string

const (
	NodeCollectionCentre NodeKind = "COLLECTION_CENTRE"
	NodeTanker           NodeKind = "TANKER"
	NodeChillingUnit     NodeKind = "CHILLING_UNIT"
	NodePlant            NodeKind = "PLANT"
)

// Boundary is the node id of the system boundary: an external source the milk
// came from, or an external sink it went to. A flow naming it on one side
// crosses out of the network being balanced and is therefore never itself a
// point whose inflow must equal its outflow.
const Boundary = ""

// BalanceNode is one end of a flow.
type BalanceNode struct {
	ID string `json:"id"`
	// Kind is empty for the boundary, which is not a place milk is held.
	Kind NodeKind `json:"kind,omitempty"`
}

func (n BalanceNode) IsBoundary() bool { return n.ID == Boundary }

// Unit is what a window's quantities are measured in. Litres and kilograms do
// not balance against each other, so the unit belongs to the window rather than
// to the individual measurement.
type Unit string

const (
	UnitLitres Unit = "LITRES"
	UnitKG     Unit = "KG"
)

// WindowStatus tracks how far a period has got towards being closed.
type WindowStatus string

const (
	// WindowOpen still admits flows.
	WindowOpen WindowStatus = "OPEN"
	// WindowReconciled has at least one run against it.
	WindowReconciled WindowStatus = "RECONCILED"
	// WindowAccepted has a run a human took as the period's close.
	WindowAccepted WindowStatus = "ACCEPTED"
)

// BalanceWindow is one period being closed for one route.
type BalanceWindow struct {
	ID          string       `json:"id"`
	TenantID    string       `json:"tenant_id"`
	RouteRef    string       `json:"route_ref"`
	PeriodStart time.Time    `json:"period_start"`
	PeriodEnd   time.Time    `json:"period_end"`
	Unit        Unit         `json:"unit"`
	Status      WindowStatus `json:"status"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

// FlowMeasurement is one measured stream in a window: a producer's delivery
// into a chilling centre, a tanker's dispatch, a plant's intake.
//
// Quantities are decimal literals at QuantityScale, never floats: a settlement
// argued from these numbers has to reproduce them exactly.
type FlowMeasurement struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`
	// FlowID names the stream within its window and is what the reconciler's
	// answer is keyed by.
	FlowID string      `json:"flow_id"`
	From   BalanceNode `json:"from_node"`
	To     BalanceNode `json:"to_node"`

	Measured string `json:"measured"`
	// StandardUncertainty weights the reconciliation: a stream measured by a
	// verified instrument pulls the solution harder than an estimated one.
	StandardUncertainty string `json:"standard_uncertainty,omitempty"`
	// Unmeasured flows are solved for rather than adjusted, so they state no
	// quantity and carry no uncertainty of their own.
	Unmeasured bool `json:"unmeasured"`
	// ObservationRef links the measurement back to the observation it was read
	// from, so a nominated gross error can be traced to an instrument.
	ObservationRef string `json:"observation_ref,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// ReconciledFlow is what one run concluded about one flow.
type ReconciledFlow struct {
	RunID      string `json:"run_id"`
	TenantID   string `json:"tenant_id"`
	FlowID     string `json:"flow_id"`
	Measured   string `json:"measured"`
	Reconciled string `json:"reconciled"`
	Adjustment string `json:"adjustment"`
	// TestStatistic is how many of its own standard uncertainties the stream had
	// to move. It is a ratio, not a quantity, so it is not fixed-point.
	TestStatistic float64 `json:"test_statistic"`
	GrossError    bool    `json:"gross_error"`
	Unmeasured    bool    `json:"unmeasured"`
}

// ReconciliationRun is one attempt to close a window.
type ReconciliationRun struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	WindowID string `json:"window_id"`

	// Converged is false when the network was under-determined or when no model
	// answered. The adjustments are then indicative only.
	Converged bool `json:"converged"`
	// ResidualBefore is the total node imbalance the platform computed itself,
	// so it is present even when the reconciler is not.
	ResidualBefore string `json:"residual_before"`
	// ResidualAfter is nil when no model answered: there is no reconciled state
	// to measure a residual against, and reporting the before value again would
	// read as a window that closed.
	ResidualAfter       *string `json:"residual_after,omitempty"`
	ModelVersion        string  `json:"model_version,omitempty"`
	GrossErrorThreshold float64 `json:"gross_error_threshold,omitempty"`
	// Reason states why a run carries no model answer.
	Reason string `json:"reason,omitempty"`

	Flows []ReconciledFlow `json:"flows"`

	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	AcceptedBy string     `json:"accepted_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

func (r *ReconciliationRun) IsAccepted() bool { return r.AcceptedAt != nil }
