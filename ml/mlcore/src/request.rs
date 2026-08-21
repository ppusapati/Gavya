//! Request-side plumbing: correlation headers and a JSON extractor whose
//! rejection speaks the same error vocabulary as everything else.

use axum::extract::rejection::JsonRejection;
use axum::extract::FromRequest;
use axum::http::request::Parts;
use axum::http::HeaderMap;

use crate::error::MlError;

pub const HEADER_TENANT_ID: &str = "x-tenant-id";
pub const HEADER_REQUEST_ID: &str = "x-request-id";
pub const HEADER_MODEL_VERSION: &str = "x-model-version";

/// Correlation data lifted from the request headers.
///
/// `tenant_id` is required. The ML tier holds no per-tenant data of its own,
/// but every score it emits is attributed to a tenant in the caller's audit
/// trail, so an unattributed call is refused rather than silently accepted.
#[derive(Debug, Clone)]
pub struct CallContext {
    pub tenant_id: String,
    pub request_id: Option<String>,
    /// Set when the caller pins a model version to make a score reproducible.
    pub model_pin: Option<String>,
}

impl CallContext {
    pub fn from_headers(headers: &HeaderMap) -> Result<Self, MlError> {
        let tenant_id = headers
            .get(HEADER_TENANT_ID)
            .and_then(|v| v.to_str().ok())
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                MlError::PermissionDenied(format!("missing {HEADER_TENANT_ID} header"))
            })?
            .to_string();

        Ok(Self {
            tenant_id,
            request_id: header_opt(headers, HEADER_REQUEST_ID),
            model_pin: header_opt(headers, HEADER_MODEL_VERSION),
        })
    }

    /// Rejects a pin the service cannot honour. Returning a stale score under a
    /// pin the service does not implement would corrupt a replay.
    pub fn check_pin(&self, served_version: &str) -> Result<(), MlError> {
        match &self.model_pin {
            Some(pin) if pin != served_version => Err(MlError::NotFound(format!(
                "model version {pin} is not served by this instance (serving {served_version})"
            ))),
            _ => Ok(()),
        }
    }
}

fn header_opt(headers: &HeaderMap, name: &str) -> Option<String> {
    headers
        .get(name)
        .and_then(|v| v.to_str().ok())
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(str::to_string)
}

impl<S: Send + Sync> axum::extract::FromRequestParts<S> for CallContext {
    type Rejection = MlError;

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        Self::from_headers(&parts.headers)
    }
}

/// A JSON body extractor that reports malformed input as `invalid_argument`
/// rather than axum's default plain-text 400, which Go could not decode.
#[derive(Debug, Clone, Copy, Default)]
pub struct Json<T>(pub T);

impl<T, S> FromRequest<S> for Json<T>
where
    axum::Json<T>: FromRequest<S, Rejection = JsonRejection>,
    S: Send + Sync,
{
    type Rejection = MlError;

    async fn from_request(req: axum::extract::Request, state: &S) -> Result<Self, Self::Rejection> {
        let axum::Json(value) = axum::Json::<T>::from_request(req, state)
            .await
            .map_err(|e| MlError::invalid(e.body_text()))?;
        Ok(Json(value))
    }
}
