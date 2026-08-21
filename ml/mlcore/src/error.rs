//! The error vocabulary shared with Go's `mlclient`.
//!
//! The JSON shape (`code`, `message`) and the HTTP status mapping are a
//! contract: `mlclient::Error::Retryable` keys off the status, so a validation
//! failure must never be reported with a 5xx or Go will retry it three times.

use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde::Serialize;

pub type MlResult<T> = Result<T, MlError>;

#[derive(Debug, thiserror::Error)]
pub enum MlError {
    /// The request was structurally valid JSON but semantically unusable.
    #[error("{0}")]
    InvalidArgument(String),

    /// The named model or resource is not registered for this tenant.
    #[error("{0}")]
    NotFound(String),

    /// The request carried no tenant, or a tenant that may not be served.
    #[error("{0}")]
    PermissionDenied(String),

    /// The input is well formed but too small or too degenerate to score.
    /// Distinct from `InvalidArgument`: the caller did nothing wrong, there is
    /// simply not enough evidence, and Go degrades to its deterministic path.
    #[error("{0}")]
    InsufficientEvidence(String),

    /// The numerical routine did not converge or hit a singular system.
    #[error("{0}")]
    Unprocessable(String),

    #[error("{0}")]
    Internal(String),
}

impl MlError {
    pub fn code(&self) -> &'static str {
        match self {
            Self::InvalidArgument(_) => "invalid_argument",
            Self::NotFound(_) => "not_found",
            Self::PermissionDenied(_) => "permission_denied",
            Self::InsufficientEvidence(_) => "insufficient_evidence",
            Self::Unprocessable(_) => "unprocessable",
            Self::Internal(_) => "internal",
        }
    }

    pub fn status(&self) -> StatusCode {
        match self {
            Self::InvalidArgument(_) => StatusCode::BAD_REQUEST,
            Self::NotFound(_) => StatusCode::NOT_FOUND,
            Self::PermissionDenied(_) => StatusCode::FORBIDDEN,
            Self::InsufficientEvidence(_) => StatusCode::UNPROCESSABLE_ENTITY,
            Self::Unprocessable(_) => StatusCode::UNPROCESSABLE_ENTITY,
            Self::Internal(_) => StatusCode::INTERNAL_SERVER_ERROR,
        }
    }

    pub fn invalid(msg: impl Into<String>) -> Self {
        Self::InvalidArgument(msg.into())
    }

    pub fn insufficient(msg: impl Into<String>) -> Self {
        Self::InsufficientEvidence(msg.into())
    }

    pub fn unprocessable(msg: impl Into<String>) -> Self {
        Self::Unprocessable(msg.into())
    }
}

#[derive(Serialize)]
struct ErrorBody<'a> {
    code: &'a str,
    message: String,
}

impl IntoResponse for MlError {
    fn into_response(self) -> Response {
        let status = self.status();
        if status.is_server_error() {
            tracing::error!(code = self.code(), error = %self, "ml procedure failed");
        } else {
            tracing::debug!(code = self.code(), error = %self, "ml procedure rejected");
        }
        let body = ErrorBody {
            code: self.code(),
            message: self.to_string(),
        };
        (status, Json(body)).into_response()
    }
}
