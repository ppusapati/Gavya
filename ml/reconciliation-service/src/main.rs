//! gavya.ml.v1.ReconciliationService
//!
//! Closes mass balances over a milk flow network and nominates the streams
//! whose adjustment is too large to be measurement noise. Reached only over
//! HTTP by the Go integrity service; holds no database of its own.

mod balance;
mod contract;

use axum::routing::post;
use axum::Router;
use mlcore::{procedure, serve, CallContext, Json, MlError, MlResult, ServiceConfig};

use contract::{ReconcileMassBalanceRequest, ReconcileMassBalanceResponse};

const SERVICE: &str = "gavya.ml.v1.ReconciliationService";
const MODEL_VERSION: &str = "reconciliation-wls-1.0.0";

#[tokio::main]
async fn main() -> std::io::Result<()> {
    mlcore::serve::init_tracing("reconciliation-service");

    let routes = Router::new().route(
        procedure!("gavya.ml.v1.ReconciliationService", "ReconcileMassBalance"),
        post(reconcile_mass_balance),
    );

    serve(
        ServiceConfig::from_env("reconciliation-service", MODEL_VERSION, 9103),
        routes,
    )
    .await
}

async fn reconcile_mass_balance(
    ctx: CallContext,
    Json(req): Json<ReconcileMassBalanceRequest>,
) -> MlResult<axum::Json<ReconcileMassBalanceResponse>> {
    ctx.check_pin(MODEL_VERSION)?;
    validate_tenant(&ctx, &req.tenant_id)?;
    reject_non_finite(req.flows.iter().map(|f| (f.flow_id.as_str(), f.measured)))?;

    let result = balance::reconcile(&req.flows, req.gross_error_threshold)?;
    tracing::info!(
        service = SERVICE,
        tenant_id = %ctx.tenant_id,
        balance_window = %req.balance_window,
        flows = req.flows.len(),
        converged = result.converged,
        suspects = result.suspect_flow_ids.len(),
        residual_before = result.residual_before,
        residual_after = result.residual_after,
        "reconciled mass balance"
    );

    Ok(axum::Json(ReconcileMassBalanceResponse {
        model_version: MODEL_VERSION.to_string(),
        flows: result.flows,
        converged: result.converged,
        residual_before: result.residual_before,
        residual_after: result.residual_after,
        suspect_flow_ids: result.suspect_flow_ids,
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

/// NaN and infinity would propagate silently through the solve and come back as
/// a balanced-looking network, so they are refused at the boundary.
fn reject_non_finite<'a>(values: impl Iterator<Item = (&'a str, f64)>) -> MlResult<()> {
    for (id, v) in values {
        if !v.is_finite() {
            return Err(MlError::invalid(format!(
                "flow {id} carries a non-finite value"
            )));
        }
    }
    Ok(())
}
