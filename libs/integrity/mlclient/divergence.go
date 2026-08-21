package mlclient

import "context"

const ProcExplainDivergence = "gavya.ml.v1.DivergenceService/ExplainDivergence"

// ExplainDivergenceRequest asks the ML tier to nominate a cause for a
// settlement divergence.
//
// This is only ever called after the deterministic classifier in Go has already
// run and returned UNEXPLAINED. The authoritative classification stays in Go;
// the ML answer is stored as a hypothesis alongside it and never overwrites it.
type ExplainDivergenceRequest struct {
	TenantID     string `json:"tenant_id"`
	DivergenceID string `json:"divergence_id"`
	Currency     string `json:"currency"`
	// DeltaMinorUnits is external minus shadow, in currency minor units.
	DeltaMinorUnits int64 `json:"delta_minor_units"`
	// Features are the deterministic differences already computed by the Go
	// classifier: per-input deltas, applied rate ids, rounding residuals,
	// recovery amounts, policy version ids.
	Features map[string]float64 `json:"features"`
	// Labels are the categorical counterparts: policy versions, rate card ids.
	Labels map[string]string `json:"labels,omitempty"`
	// PeerHistory lets the model recognise a pattern already resolved elsewhere.
	PeerHistory []DivergencePrecedent `json:"peer_history,omitempty"`
}

// DivergencePrecedent is a previously adjudicated divergence used as evidence.
type DivergencePrecedent struct {
	DivergenceID    string             `json:"divergence_id"`
	Classification  string             `json:"classification"`
	DeltaMinorUnits int64              `json:"delta_minor_units"`
	Features        map[string]float64 `json:"features"`
}

type DivergenceHypothesis struct {
	// Classification mirrors the deterministic vocabulary: ROUNDING_DIFFERENCE,
	// INPUT_DIFFERENCE, POLICY_DIFFERENCE, RECOVERY_DIFFERENCE.
	Classification string  `json:"classification"`
	Confidence     float64 `json:"confidence"`
	// Rationale names the features that drove the hypothesis, so a reviewer can
	// check it rather than trust it.
	Rationale        string   `json:"rationale"`
	SupportingFields []string `json:"supporting_fields"`
	PrecedentIDs     []string `json:"precedent_ids,omitempty"`
}

type ExplainDivergenceResponse struct {
	ModelVersion string                 `json:"model_version"`
	Hypotheses   []DivergenceHypothesis `json:"hypotheses"`
	// Abstained is true when no hypothesis clears the confidence floor. The
	// divergence then stays UNEXPLAINED and goes to a human.
	Abstained bool `json:"abstained"`
}

// DivergenceClient proposes explanations for divergences the deterministic
// classifier could not attribute.
type DivergenceClient struct{ c *Client }

func NewDivergenceClient(cfg Config) *DivergenceClient { return &DivergenceClient{c: New(cfg)} }

func (d *DivergenceClient) Explain(ctx context.Context, in *ExplainDivergenceRequest, opts CallOptions) (*ExplainDivergenceResponse, error) {
	return Invoke[*ExplainDivergenceRequest, ExplainDivergenceResponse](ctx, d.c, ProcExplainDivergence, in, opts)
}

func (d *DivergenceClient) Health(ctx context.Context) error { return d.c.Health(ctx) }
