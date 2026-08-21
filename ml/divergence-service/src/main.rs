//! gavya.ml.v1.DivergenceService
//!
//! Nominates a cause for a settlement divergence that the deterministic
//! classifier in Go returned as UNEXPLAINED. The authoritative classification
//! stays in Go; what this service returns is a hypothesis stored alongside it,
//! never a replacement for it, and never a decision about money.

mod contract;
mod explain;

use axum::routing::post;
use axum::Router;
use mlcore::{procedure, serve, CallContext, Json, MlError, MlResult, ServiceConfig};

use contract::{ExplainDivergenceRequest, ExplainDivergenceResponse};

const SERVICE: &str = "gavya.ml.v1.DivergenceService";
const MODEL_VERSION: &str = "divergence-knn-evidence-1.0.0";

#[tokio::main]
async fn main() -> std::io::Result<()> {
    mlcore::serve::init_tracing("divergence-service");

    let routes = Router::new().route(
        procedure!("gavya.ml.v1.DivergenceService", "ExplainDivergence"),
        post(explain_divergence),
    );

    serve(
        ServiceConfig::from_env("divergence-service", MODEL_VERSION, 9104),
        routes,
    )
    .await
}

async fn explain_divergence(
    ctx: CallContext,
    Json(req): Json<ExplainDivergenceRequest>,
) -> MlResult<axum::Json<ExplainDivergenceResponse>> {
    ctx.check_pin(MODEL_VERSION)?;
    validate_tenant(&ctx, &req.tenant_id)?;

    let outcome = explain::explain(&req)?;
    tracing::info!(
        service = SERVICE,
        tenant_id = %ctx.tenant_id,
        divergence_id = %req.divergence_id,
        currency = %req.currency,
        delta_minor_units = req.delta_minor_units,
        precedents = req.peer_history.len(),
        labels = req.labels.len(),
        hypotheses = outcome.hypotheses.len(),
        abstained = outcome.abstained,
        top_classification = outcome
            .hypotheses
            .first()
            .map(|h| h.classification.as_str())
            .unwrap_or(""),
        "explained settlement divergence"
    );

    Ok(axum::Json(ExplainDivergenceResponse {
        model_version: MODEL_VERSION.to_string(),
        hypotheses: outcome.hypotheses,
        abstained: outcome.abstained,
    }))
}

/// The tenant in the body must match the tenant in the header. A mismatch means
/// the caller's context and payload disagree, which is never safe to guess at.
fn validate_tenant(ctx: &CallContext, body_tenant: &str) -> MlResult<()> {
    if !body_tenant.is_empty() && body_tenant != ctx.tenant_id {
        return Err(MlError::PermissionDenied(
            "tenant_id in body does not match the X-Tenant-ID header".to_string(),
        ));
    }
    Ok(())
}
