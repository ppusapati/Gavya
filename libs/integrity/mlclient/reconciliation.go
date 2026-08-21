package mlclient

import "context"

const ProcReconcileMassBalance = "gavya.ml.v1.ReconciliationService/ReconcileMassBalance"

// FlowMeasurement is one measured stream in a mass-balance network: a farm's
// delivery into a chilling centre, a tanker's dispatch, a plant's intake.
type FlowMeasurement struct {
	FlowID string `json:"flow_id"`
	// FromNode and ToNode are node ids; the empty string is the system boundary.
	FromNode string  `json:"from_node"`
	ToNode   string  `json:"to_node"`
	Measured float64 `json:"measured"`
	// StandardUncertainty weights the reconciliation. Streams measured by a
	// verified instrument pull the solution harder than estimated ones.
	StandardUncertainty float64 `json:"standard_uncertainty"`
	// Unmeasured flows are solved for rather than adjusted.
	Unmeasured bool `json:"unmeasured"`
}

type ReconcileMassBalanceRequest struct {
	TenantID string `json:"tenant_id"`
	// BalanceWindow identifies the period being closed, for traceability.
	BalanceWindow string            `json:"balance_window"`
	Flows         []FlowMeasurement `json:"flows"`
	// GrossErrorThreshold is the measurement-test statistic above which a flow
	// is reported as carrying a gross error. Zero means the service default.
	GrossErrorThreshold float64 `json:"gross_error_threshold,omitempty"`
}

type ReconciledFlow struct {
	FlowID     string  `json:"flow_id"`
	Measured   float64 `json:"measured"`
	Reconciled float64 `json:"reconciled"`
	Adjustment float64 `json:"adjustment"`
	// TestStatistic is the normalised adjustment used for the gross-error test.
	TestStatistic float64 `json:"test_statistic"`
	GrossError    bool    `json:"gross_error"`
	Unmeasured    bool    `json:"unmeasured"`
}

type ReconcileMassBalanceResponse struct {
	ModelVersion string           `json:"model_version"`
	Flows        []ReconciledFlow `json:"flows"`
	// Converged is false when the network is under-determined; adjustments must
	// then be treated as indicative only.
	Converged bool `json:"converged"`
	// ResidualBefore and ResidualAfter are the total node imbalances.
	ResidualBefore float64 `json:"residual_before"`
	ResidualAfter  float64 `json:"residual_after"`
	// SuspectFlowIDs is the ordered gross-error candidate list, worst first.
	SuspectFlowIDs []string `json:"suspect_flow_ids"`
}

// ReconciliationClient closes mass balances and nominates gross errors.
type ReconciliationClient struct{ c *Client }

func NewReconciliationClient(cfg Config) *ReconciliationClient {
	return &ReconciliationClient{c: New(cfg)}
}

func (r *ReconciliationClient) Reconcile(ctx context.Context, in *ReconcileMassBalanceRequest, opts CallOptions) (*ReconcileMassBalanceResponse, error) {
	return Invoke[*ReconcileMassBalanceRequest, ReconcileMassBalanceResponse](ctx, r.c, ProcReconcileMassBalance, in, opts)
}

func (r *ReconciliationClient) Health(ctx context.Context) error { return r.c.Health(ctx) }
