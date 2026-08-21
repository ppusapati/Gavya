//! gavya.ml.v1.AnomalyService
//!
//! Scores dairy collection measurements against their own history. Reached only
//! over HTTP by the Go observation service; holds no database of its own.

mod contract;
mod detector;

use axum::routing::post;
use axum::Router;
use mlcore::{procedure, serve, CallContext, Json, MlError, MlResult, ServiceConfig};

use contract::{
    ScoreCollectionSeriesRequest, ScoreCollectionSeriesResponse, ScoreObservationRequest,
    ScoreObservationResponse,
};

const SERVICE: &str = "gavya.ml.v1.AnomalyService";
const MODEL_VERSION: &str = "anomaly-robust-z-1.0.0";
const MAX_POINTS: usize = 10_000;

#[tokio::main]
async fn main() -> std::io::Result<()> {
    mlcore::serve::init_tracing("anomaly-service");

    let routes = Router::new()
        .route(procedure!("gavya.ml.v1.AnomalyService", "ScoreCollectionSeries"), post(score_series))
        .route(procedure!("gavya.ml.v1.AnomalyService", "ScoreObservation"), post(score_observation));

    serve(ServiceConfig::from_env("anomaly-service", MODEL_VERSION, 9101), routes).await
}

async fn score_series(
    ctx: CallContext,
    Json(req): Json<ScoreCollectionSeriesRequest>,
) -> MlResult<axum::Json<ScoreCollectionSeriesResponse>> {
    ctx.check_pin(MODEL_VERSION)?;
    validate_tenant(&ctx, &req.tenant_id)?;
    if req.subject_ref.trim().is_empty() {
        return Err(MlError::invalid("subject_ref is required"));
    }
    if req.points.len() > MAX_POINTS {
        return Err(MlError::invalid(format!(
            "series carries {} points, limit is {MAX_POINTS}",
            req.points.len()
        )));
    }
    reject_non_finite(req.points.iter().map(|p| (p.observation_id.as_str(), p.value)))?;

    let (scores, baseline_insufficient) = detector::score_series(&req.points, req.sensitivity);
    tracing::info!(
        service = SERVICE,
        tenant_id = %ctx.tenant_id,
        subject_ref = %req.subject_ref,
        quantity_kind = %req.quantity_kind,
        points = req.points.len(),
        flagged = scores.iter().filter(|s| s.flagged).count(),
        baseline_insufficient,
        "scored collection series"
    );

    Ok(axum::Json(ScoreCollectionSeriesResponse {
        model_version: MODEL_VERSION.to_string(),
        scores,
        baseline_insufficient,
    }))
}

async fn score_observation(
    ctx: CallContext,
    Json(req): Json<ScoreObservationRequest>,
) -> MlResult<axum::Json<ScoreObservationResponse>> {
    ctx.check_pin(MODEL_VERSION)?;
    validate_tenant(&ctx, &req.tenant_id)?;
    if req.subject_ref.trim().is_empty() {
        return Err(MlError::invalid("subject_ref is required"));
    }
    if req.history.len() > MAX_POINTS {
        return Err(MlError::invalid(format!(
            "history carries {} points, limit is {MAX_POINTS}",
            req.history.len()
        )));
    }
    reject_non_finite(
        std::iter::once((req.candidate.observation_id.as_str(), req.candidate.value))
            .chain(req.history.iter().map(|p| (p.observation_id.as_str(), p.value))),
    )?;

    let baseline: Vec<f64> = req.history.iter().map(|p| p.value).collect();
    let insufficient = baseline.len() < detector::MIN_BASELINE;

    let score = if insufficient {
        contract::AnomalyScore {
            observation_id: req.candidate.observation_id.clone(),
            score: 0.0,
            flagged: false,
            method: detector::METHOD_DEGENERATE.to_string(),
            expected: req.candidate.value,
            lower_bound: None,
            upper_bound: None,
            explanation: format!(
                "history has {} points; at least {} are required to establish a baseline",
                baseline.len(),
                detector::MIN_BASELINE
            ),
        }
    } else {
        detector::score_against(&req.candidate, &baseline, req.sensitivity)
    };

    tracing::info!(
        service = SERVICE,
        tenant_id = %ctx.tenant_id,
        subject_ref = %req.subject_ref,
        quantity_kind = %req.quantity_kind,
        history = baseline.len(),
        flagged = score.flagged,
        baseline_insufficient = insufficient,
        "scored observation"
    );

    Ok(axum::Json(ScoreObservationResponse {
        model_version: MODEL_VERSION.to_string(),
        score,
        baseline_insufficient: insufficient,
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
/// back as an unflagged score, so they are refused at the boundary.
fn reject_non_finite<'a>(values: impl Iterator<Item = (&'a str, f64)>) -> MlResult<()> {
    for (id, v) in values {
        if !v.is_finite() {
            return Err(MlError::invalid(format!(
                "observation {id} carries a non-finite value"
            )));
        }
    }
    Ok(())
}
