package mlclient

import "context"

const (
	ProcEstimateUncertainty = "gavya.ml.v1.UncertaintyService/EstimateUncertainty"
	ProcFitUncertaintyModel = "gavya.ml.v1.UncertaintyService/FitUncertaintyModel"
)

// UncertaintyComponent is one contribution to the combined standard uncertainty,
// in the sense of the GUM: Type A components are evaluated statistically from
// repeated observation, Type B from calibration certificates and instrument
// class limits.
type UncertaintyComponent struct {
	Name string `json:"name"`
	// Type is "A" or "B".
	Type string `json:"type"`
	// Distribution is "normal", "rectangular" or "triangular"; it fixes the
	// divisor used to convert a half-width limit into a standard uncertainty.
	Distribution      string  `json:"distribution"`
	Value             float64 `json:"value"`
	Sensitivity       float64 `json:"sensitivity"`
	StandardUncertain float64 `json:"standard_uncertainty"`
	// DegreesOfFreedom is nil for a Type B component, which is not estimated
	// from a finite sample and so carries infinite degrees of freedom. It is a
	// pointer because JSON cannot carry an infinity: it arrives as null, which
	// would decode into 0.0 and read as zero degrees of freedom.
	DegreesOfFreedom *float64 `json:"degrees_of_freedom"`
}

type EstimateUncertaintyRequest struct {
	TenantID string `json:"tenant_id"`
	// UncertaintyModelID selects the registered model to evaluate. Observations
	// store this id so an estimate can be recomputed identically later.
	UncertaintyModelID string `json:"uncertainty_model_id"`
	Quantity           string `json:"quantity_kind"`
	MeasuredValue      float64 `json:"measured_value"`
	Unit               string  `json:"unit"`
	// Inputs are model-specific covariates: ambient temperature, instrument
	// class, time since last verification, volume of the sample.
	Inputs map[string]float64 `json:"inputs,omitempty"`
	// CoverageProbability defaults to 0.95 when zero.
	CoverageProbability float64 `json:"coverage_probability,omitempty"`
}

type EstimateUncertaintyResponse struct {
	ModelVersion string `json:"model_version"`
	// StandardUncertainty is the combined standard uncertainty u_c.
	StandardUncertainty float64 `json:"standard_uncertainty"`
	// CoverageFactor is k, derived from the effective degrees of freedom via
	// the Welch-Satterthwaite formula.
	CoverageFactor float64 `json:"coverage_factor"`
	// ExpandedUncertainty is U = k * u_c.
	ExpandedUncertainty float64 `json:"expanded_uncertainty"`
	// EffectiveDegreesOfFreedom is nil when the budget is built entirely from
	// Type B components and so has infinite effective degrees of freedom.
	EffectiveDegreesOfFreedom *float64               `json:"effective_degrees_of_freedom"`
	CoverageProbability       float64                `json:"coverage_probability"`
	LowerBound                float64                `json:"lower_bound"`
	UpperBound                float64                `json:"upper_bound"`
	Components                []UncertaintyComponent `json:"components"`
}

type FitUncertaintyModelRequest struct {
	TenantID    string        `json:"tenant_id"`
	Quantity    string        `json:"quantity_kind"`
	InstrumentID string       `json:"instrument_id"`
	Replicates  []SeriesPoint `json:"replicates"`
}

type FitUncertaintyModelResponse struct {
	ModelVersion string `json:"model_version"`
	// TypeAStandardUncertainty is the experimental standard deviation of the
	// mean over the supplied replicates.
	TypeAStandardUncertainty float64 `json:"type_a_standard_uncertainty"`
	DegreesOfFreedom         float64 `json:"degrees_of_freedom"`
	ReplicateCount           int     `json:"replicate_count"`
	Sufficient               bool    `json:"sufficient"`
}

// UncertaintyClient evaluates measurement uncertainty models.
type UncertaintyClient struct{ c *Client }

func NewUncertaintyClient(cfg Config) *UncertaintyClient { return &UncertaintyClient{c: New(cfg)} }

func (u *UncertaintyClient) Estimate(ctx context.Context, in *EstimateUncertaintyRequest, opts CallOptions) (*EstimateUncertaintyResponse, error) {
	return Invoke[*EstimateUncertaintyRequest, EstimateUncertaintyResponse](ctx, u.c, ProcEstimateUncertainty, in, opts)
}

func (u *UncertaintyClient) FitModel(ctx context.Context, in *FitUncertaintyModelRequest, opts CallOptions) (*FitUncertaintyModelResponse, error) {
	return Invoke[*FitUncertaintyModelRequest, FitUncertaintyModelResponse](ctx, u.c, ProcFitUncertaintyModel, in, opts)
}

func (u *UncertaintyClient) Health(ctx context.Context) error { return u.c.Health(ctx) }
