//! The serve loop every ML service shares: health endpoints, request limits,
//! tracing, and graceful shutdown on SIGTERM so Kubernetes can drain a pod.

use std::time::Duration;

use axum::http::StatusCode;
use axum::routing::get;
use axum::Router;
use tower_http::limit::RequestBodyLimitLayer;
use tower_http::timeout::TimeoutLayer;
use tower_http::trace::TraceLayer;

pub struct ServiceConfig {
    pub service_name: &'static str,
    pub model_version: &'static str,
    pub listen_addr: String,
    /// Bounds a single procedure. Go's client gives up at its own timeout; this
    /// stops an expensive solve from occupying a worker after the caller left.
    pub request_timeout: Duration,
    pub max_body_bytes: usize,
}

impl ServiceConfig {
    pub fn from_env(service_name: &'static str, model_version: &'static str, default_port: u16) -> Self {
        let listen_addr = std::env::var("LISTEN_ADDR").unwrap_or_else(|_| {
            let port = std::env::var("PORT")
                .ok()
                .and_then(|p| p.parse::<u16>().ok())
                .unwrap_or(default_port);
            format!("0.0.0.0:{port}")
        });
        let request_timeout = std::env::var("REQUEST_TIMEOUT_SECS")
            .ok()
            .and_then(|s| s.parse::<u64>().ok())
            .map(Duration::from_secs)
            .unwrap_or_else(|| Duration::from_secs(10));
        let max_body_bytes = std::env::var("MAX_BODY_BYTES")
            .ok()
            .and_then(|s| s.parse::<usize>().ok())
            .unwrap_or(4 * 1024 * 1024);

        Self {
            service_name,
            model_version,
            listen_addr,
            request_timeout,
            max_body_bytes,
        }
    }
}

pub fn init_tracing(service_name: &str) {
    use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

    let filter = EnvFilter::try_from_env("LOG_LEVEL").unwrap_or_else(|_| EnvFilter::new("info"));
    tracing_subscriber::registry()
        .with(filter)
        .with(tracing_subscriber::fmt::layer().json().with_target(true))
        .init();
    tracing::info!(service = service_name, "tracing initialised");
}

/// Serves `routes` with the shared middleware stack until SIGTERM or SIGINT.
pub async fn serve(cfg: ServiceConfig, routes: Router) -> std::io::Result<()> {
    let version = cfg.model_version;
    let name = cfg.service_name;

    let app = routes
        .route("/healthz", get(|| async { "ok" }))
        .route(
            "/readyz",
            get(move || async move { axum::Json(serde_json::json!({ "status": "ready", "service": name, "model_version": version })) }),
        )
        .layer(TraceLayer::new_for_http())
        // 503 rather than tower-http's legacy 408: the request was fine, this
        // service simply could not answer in time. Go's mlclient retries 5xx
        // and gives up on 4xx, and a timed-out ML call is worth retrying.
        .layer(TimeoutLayer::with_status_code(
            StatusCode::SERVICE_UNAVAILABLE,
            cfg.request_timeout,
        ))
        .layer(RequestBodyLimitLayer::new(cfg.max_body_bytes));

    let listener = tokio::net::TcpListener::bind(&cfg.listen_addr).await?;
    tracing::info!(
        service = cfg.service_name,
        model_version = cfg.model_version,
        addr = %cfg.listen_addr,
        "ml service listening"
    );

    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await
}

async fn shutdown_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c()
            .await
            .expect("failed to install SIGINT handler");
    };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
    tracing::info!("shutdown signal received, draining");
}
