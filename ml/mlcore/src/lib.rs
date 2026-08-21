//! Shared runtime for the Rust ML tier.
//!
//! Every ML service is an independent process that Go reaches only over the
//! network. This crate supplies the parts they all repeat: the Connect unary
//! JSON envelope, the error vocabulary Go's `mlclient` decodes, tenant and
//! request correlation, and the serve/shutdown loop.

pub mod error;
pub mod request;
pub mod serve;
pub mod stats;

pub use error::{MlError, MlResult};
pub use request::{CallContext, Json};
pub use serve::{serve, ServiceConfig};

/// Builds the Connect procedure path for a service method.
///
/// Go's `mlclient` posts to `/<fully.qualified.Service>/<Method>`, so the two
/// sides must agree on this string exactly.
#[macro_export]
macro_rules! procedure {
    ($service:literal, $method:literal) => {
        concat!("/", $service, "/", $method)
    };
}
