package mlclient

import "context"

const (
	ProcScoreCollectionSeries = "gavya.ml.v1.AnomalyService/ScoreCollectionSeries"
	ProcScoreObservation      = "gavya.ml.v1.AnomalyService/ScoreObservation"
)

// SeriesPoint is one historical measurement for a single subject.
type SeriesPoint struct {
	ObservationID string  `json:"observation_id"`
	ValidAt       string  `json:"valid_at"`
	Value         float64 `json:"value"`
	// Uncertainty, when known, lets the scorer widen the tolerance band for
	// instruments that are simply less precise instead of flagging them.
	Uncertainty float64 `json:"uncertainty,omitempty"`
}

type ScoreCollectionSeriesRequest struct {
	TenantID string `json:"tenant_id"`
	// SubjectRef is a typed reference such as "cattle:01H..." or "route:01H...".
	SubjectRef string        `json:"subject_ref"`
	Quantity   string        `json:"quantity_kind"`
	Points     []SeriesPoint `json:"points"`
	// Sensitivity is the robust z-score threshold above which a point is
	// flagged. Zero means the service default.
	Sensitivity float64 `json:"sensitivity,omitempty"`
}

type AnomalyScore struct {
	ObservationID string  `json:"observation_id"`
	Score         float64 `json:"score"`
	Flagged       bool    `json:"flagged"`
	Method        string  `json:"method"`
	Expected      float64 `json:"expected"`
	// LowerBound and UpperBound are nil when no baseline could be established,
	// meaning the band is unbounded. They are pointers rather than plain floats
	// because JSON cannot carry an infinity: it arrives as null, which would
	// decode into 0.0 and read as an infinitely tight band.
	LowerBound  *float64 `json:"lower_bound"`
	UpperBound  *float64 `json:"upper_bound"`
	Explanation string   `json:"explanation"`
}

// Bounded reports whether the score carries a usable tolerance band.
func (a AnomalyScore) Bounded() bool { return a.LowerBound != nil && a.UpperBound != nil }

type ScoreCollectionSeriesResponse struct {
	ModelVersion string         `json:"model_version"`
	Scores       []AnomalyScore `json:"scores"`
	// BaselineInsufficient is set when the series was too short to establish a
	// baseline. Callers must not treat unflagged points as validated.
	BaselineInsufficient bool `json:"baseline_insufficient"`
}

type ScoreObservationRequest struct {
	TenantID    string        `json:"tenant_id"`
	SubjectRef  string        `json:"subject_ref"`
	Quantity    string        `json:"quantity_kind"`
	Candidate   SeriesPoint   `json:"candidate"`
	History     []SeriesPoint `json:"history"`
	Sensitivity float64       `json:"sensitivity,omitempty"`
}

type ScoreObservationResponse struct {
	ModelVersion         string       `json:"model_version"`
	Score                AnomalyScore `json:"score"`
	BaselineInsufficient bool         `json:"baseline_insufficient"`
}

// AnomalyClient scores collection measurements against their own history.
type AnomalyClient struct{ c *Client }

func NewAnomalyClient(cfg Config) *AnomalyClient { return &AnomalyClient{c: New(cfg)} }

func (a *AnomalyClient) ScoreSeries(ctx context.Context, in *ScoreCollectionSeriesRequest, opts CallOptions) (*ScoreCollectionSeriesResponse, error) {
	return Invoke[*ScoreCollectionSeriesRequest, ScoreCollectionSeriesResponse](ctx, a.c, ProcScoreCollectionSeries, in, opts)
}

func (a *AnomalyClient) ScoreObservation(ctx context.Context, in *ScoreObservationRequest, opts CallOptions) (*ScoreObservationResponse, error) {
	return Invoke[*ScoreObservationRequest, ScoreObservationResponse](ctx, a.c, ProcScoreObservation, in, opts)
}

func (a *AnomalyClient) Health(ctx context.Context) error { return a.c.Health(ctx) }
