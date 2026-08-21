//! Wire types for `gavya.ml.v1.DivergenceService`.
//!
//! These mirror `libs/integrity/mlclient/divergence.go` field for field.
//! Changing a `serde` name here is a breaking protocol change.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize)]
pub struct ExplainDivergenceRequest {
    pub tenant_id: String,
    pub divergence_id: String,
    #[serde(default)]
    pub currency: String,
    /// External minus shadow, in currency minor units.
    pub delta_minor_units: i64,
    #[serde(default)]
    pub features: BTreeMap<String, f64>,
    #[serde(default)]
    pub labels: BTreeMap<String, String>,
    #[serde(default)]
    pub peer_history: Vec<DivergencePrecedent>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DivergencePrecedent {
    pub divergence_id: String,
    pub classification: String,
    pub delta_minor_units: i64,
    #[serde(default)]
    pub features: BTreeMap<String, f64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DivergenceHypothesis {
    pub classification: String,
    pub confidence: f64,
    pub rationale: String,
    pub supporting_fields: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub precedent_ids: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct ExplainDivergenceResponse {
    pub model_version: String,
    pub hypotheses: Vec<DivergenceHypothesis>,
    /// True when no hypothesis cleared the confidence floor. The divergence
    /// then stays UNEXPLAINED and goes to a human.
    pub abstained: bool,
}
