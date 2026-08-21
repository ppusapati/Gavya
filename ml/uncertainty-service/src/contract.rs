//! Wire types for `gavya.ml.v1.UncertaintyService`.
//!
//! These mirror `libs/integrity/mlclient/uncertainty.go` field for field.
//! Changing a `serde` name here is a breaking protocol change.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct SeriesPoint {
    pub observation_id: String,
    pub valid_at: String,
    pub value: f64,
    #[serde(default)]
    pub uncertainty: f64,
}

#[derive(Debug, Clone, Serialize)]
pub struct UncertaintyComponent {
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub distribution: String,
    pub value: f64,
    pub sensitivity: f64,
    pub standard_uncertainty: f64,
    // None means infinite: a Type B component evaluated from a certificate or
    // a class limit is not estimated from a finite sample. It cannot be an
    // f64::INFINITY, because serde_json writes a non-finite float as JSON null
    // and Go would decode that into 0.0 — zero degrees of freedom, the exact
    // opposite of the intended meaning.
    pub degrees_of_freedom: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct EstimateUncertaintyRequest {
    pub tenant_id: String,
    pub uncertainty_model_id: String,
    #[serde(default)]
    pub quantity_kind: String,
    pub measured_value: f64,
    #[serde(default)]
    pub unit: String,
    #[serde(default)]
    pub inputs: BTreeMap<String, f64>,
    #[serde(default)]
    pub coverage_probability: f64,
}

#[derive(Debug, Serialize)]
pub struct EstimateUncertaintyResponse {
    pub model_version: String,
    pub standard_uncertainty: f64,
    pub coverage_factor: f64,
    pub expanded_uncertainty: f64,
    // None means infinite, for the same reason as the component field above.
    pub effective_degrees_of_freedom: Option<f64>,
    pub coverage_probability: f64,
    pub lower_bound: f64,
    pub upper_bound: f64,
    pub components: Vec<UncertaintyComponent>,
}

#[derive(Debug, Deserialize)]
pub struct FitUncertaintyModelRequest {
    pub tenant_id: String,
    #[serde(default)]
    pub quantity_kind: String,
    #[serde(default)]
    pub instrument_id: String,
    #[serde(default)]
    pub replicates: Vec<SeriesPoint>,
}

#[derive(Debug, Serialize)]
pub struct FitUncertaintyModelResponse {
    pub model_version: String,
    pub type_a_standard_uncertainty: f64,
    pub degrees_of_freedom: f64,
    pub replicate_count: usize,
    pub sufficient: bool,
}
