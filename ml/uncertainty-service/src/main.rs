//! gavya.ml.v1.UncertaintyService
//!
//! Evaluates measurement uncertainty budgets for dairy collection quantities
//! following the GUM. Reached only over HTTP by the Go observation service;
//! holds no database of its own.

mod contract;
mod gum;

use axum::routing::post;
use axum::Router;
use mlcore::{procedure, serve, CallContext, Json, MlError, MlResult, ServiceConfig};

use contract::{
    EstimateUncertaintyRequest, EstimateUncertaintyResponse, FitUncertaintyModelRequest,
    FitUncertaintyModelResponse,
};

const SERVICE: &str = "gavya.ml.v1.UncertaintyService";
const MODEL_VERSION: &str = "uncertainty-gum-1.0.0";
const MAX_REPLICATES: usize = 10_000;

#[tokio::main]
async fn main() -> std::io::Result<()> {
    mlcore::serve::init_tracing("uncertainty-service");

    let routes = Router::new()
        .route(procedure!("gavya.ml.v1.UncertaintyService", "EstimateUncertainty"), post(estimate_uncertainty))
        .route(procedure!("gavya.ml.v1.UncertaintyService", "FitUncertaintyModel"), post(fit_uncertainty_model));

    serve(ServiceConfig::from_env("uncertainty-service", MODEL_VERSION, 9102), routes).await
}

async fn estimate_uncertainty(
    ctx: CallContext,
    Json(req): Json<EstimateUncertaintyRequest>,
) -> MlResult<axum::Json<EstimateUncertaintyResponse>> {
    ctx.check_pin(MODEL_VERSION)?;
    validate_tenant(&ctx, &req.tenant_id)?;
    if req.uncertainty_model_id.trim().is_empty() {
        return Err(MlError::invalid("uncertainty_model_id is required"));
    }
    reject_non_finite(
        std::iter::once(("measured_value", req.measured_value))
            .chain(req.inputs.iter().map(|(k, v)| (k.as_str(), *v))),
    )?;

    let budget = gum::estimate(&req)?;
    tracing::info!(
        service = SERVICE,
        tenant_id = %ctx.tenant_id,
        uncertainty_model_id = %req.uncertainty_model_id,
        quantity_kind = budget.quantity_kind,
        unit = %req.unit,
        components = budget.components.len(),
        combined = budget.combined,
        coverage_factor = budget.coverage_factor,
        effective_dof = budget.effective_dof,
        "estimated measurement uncertainty"
    );

    Ok(axum::Json(EstimateUncertaintyResponse {
        model_version: MODEL_VERSION.to_string(),
        standard_uncertainty: budget.combined,
        coverage_factor: budget.coverage_factor,
        expanded_uncertainty: budget.expanded,
        effective_degrees_of_freedom: budget.effective_dof.is_finite().then_some(budget.effective_dof),
        coverage_probability: budget.coverage_probability,
        lower_bound: budget.lower_bound,
        upper_bound: budget.upper_bound,
        components: budget.components,
    }))
}

async fn fit_uncertainty_model(
    ctx: CallContext,
    Json(req): Json<FitUncertaintyModelRequest>,
) -> MlResult<axum::Json<FitUncertaintyModelResponse>> {
    ctx.check_pin(MODEL_VERSION)?;
    validate_tenant(&ctx, &req.tenant_id)?;
    if req.instrument_id.trim().is_empty() {
        return Err(MlError::invalid("instrument_id is required"));
    }
    if req.replicates.len() > MAX_REPLICATES {
        return Err(MlError::invalid(format!(
            "fit carries {} replicates, limit is {MAX_REPLICATES}",
            req.replicates.len()
        )));
    }
    reject_non_finite(
        req.replicates
            .iter()
            .map(|p| (p.observation_id.as_str(), p.value)),
    )?;

    let fit = gum::fit_type_a(&req.replicates)?;
    tracing::info!(
        service = SERVICE,
        tenant_id = %ctx.tenant_id,
        instrument_id = %req.instrument_id,
        quantity_kind = %req.quantity_kind,
        replicate_count = fit.replicate_count,
        type_a_standard_uncertainty = fit.standard_uncertainty,
        sufficient = fit.sufficient,
        "fitted type A uncertainty"
    );

    Ok(axum::Json(FitUncertaintyModelResponse {
        model_version: MODEL_VERSION.to_string(),
        type_a_standard_uncertainty: fit.standard_uncertainty,
        degrees_of_freedom: fit.degrees_of_freedom,
        replicate_count: fit.replicate_count,
        sufficient: fit.sufficient,
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

/// NaN and infinity would propagate silently through every statistic and come
/// back as an unbounded interval, so they are refused at the boundary.
fn reject_non_finite<'a>(values: impl Iterator<Item = (&'a str, f64)>) -> MlResult<()> {
    for (id, v) in values {
        if !v.is_finite() {
            return Err(MlError::invalid(format!("{id} carries a non-finite value")));
        }
    }
    Ok(())
}
