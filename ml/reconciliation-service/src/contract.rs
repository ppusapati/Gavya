//! Wire types for `gavya.ml.v1.ReconciliationService`.
//!
//! These mirror `libs/integrity/mlclient/reconciliation.go` field for field.
//! Changing a `serde` name here is a breaking protocol change.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct FlowMeasurement {
    pub flow_id: String,
    /// Node ids; the empty string is the system boundary.
    #[serde(default)]
    pub from_node: String,
    #[serde(default)]
    pub to_node: String,
    #[serde(default)]
    pub measured: f64,
    #[serde(default)]
    pub standard_uncertainty: f64,
    #[serde(default)]
    pub unmeasured: bool,
}

#[derive(Debug, Deserialize)]
pub struct ReconcileMassBalanceRequest {
    pub tenant_id: String,
    #[serde(default)]
    pub balance_window: String,
    #[serde(default)]
    pub flows: Vec<FlowMeasurement>,
    #[serde(default)]
    pub gross_error_threshold: f64,
}

#[derive(Debug, Serialize)]
pub struct ReconciledFlow {
    pub flow_id: String,
    pub measured: f64,
    pub reconciled: f64,
    pub adjustment: f64,
    pub test_statistic: f64,
    pub gross_error: bool,
    pub unmeasured: bool,
}

#[derive(Debug, Serialize)]
pub struct ReconcileMassBalanceResponse {
    pub model_version: String,
    pub flows: Vec<ReconciledFlow>,
    pub converged: bool,
    pub residual_before: f64,
    pub residual_after: f64,
    pub suspect_flow_ids: Vec<String>,
}
