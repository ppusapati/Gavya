//! Wire types for `gavya.ml.v1.AnomalyService`.
//!
//! These mirror `libs/integrity/mlclient/anomaly.go` field for field. Changing
//! a `serde` name here is a breaking protocol change.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct SeriesPoint {
    pub observation_id: String,
    pub valid_at: String,
    pub value: f64,
    #[serde(default)]
    pub uncertainty: f64,
}

#[derive(Debug, Deserialize)]
pub struct ScoreCollectionSeriesRequest {
    pub tenant_id: String,
    pub subject_ref: String,
    #[serde(default)]
    pub quantity_kind: String,
    #[serde(default)]
    pub points: Vec<SeriesPoint>,
    #[serde(default)]
    pub sensitivity: f64,
}

#[derive(Debug, Serialize)]
pub struct AnomalyScore {
    pub observation_id: String,
    pub score: f64,
    pub flagged: bool,
    pub method: String,
    pub expected: f64,
    // None means unbounded: with no baseline there is no tolerance band. It
    // must not be encoded as an infinity, because serde_json writes a
    // non-finite float as JSON null and Go would decode that into 0.0 — an
    // infinitely tight band, the exact opposite of the intended meaning.
    pub lower_bound: Option<f64>,
    pub upper_bound: Option<f64>,
    pub explanation: String,
}

#[derive(Debug, Serialize)]
pub struct ScoreCollectionSeriesResponse {
    pub model_version: String,
    pub scores: Vec<AnomalyScore>,
    pub baseline_insufficient: bool,
}

#[derive(Debug, Deserialize)]
pub struct ScoreObservationRequest {
    pub tenant_id: String,
    pub subject_ref: String,
    #[serde(default)]
    pub quantity_kind: String,
    pub candidate: SeriesPoint,
    #[serde(default)]
    pub history: Vec<SeriesPoint>,
    #[serde(default)]
    pub sensitivity: f64,
}

#[derive(Debug, Serialize)]
pub struct ScoreObservationResponse {
    pub model_version: String,
    pub score: AnomalyScore,
    pub baseline_insufficient: bool,
}
