//! `/metrics` HTTP endpoint (Task 8, OPS-003, overview.md §4.4), exposed on
//! a separate port/process from the gRPC server so Prometheus scraping
//! never contends with the gRPC transport. Serves whatever is currently
//! registered in the process-wide `prometheus` default registry, which
//! includes `merlon_grpc_request_duration_seconds`
//! (grpc/metrics_interceptor.rs).

use std::net::SocketAddr;

use axum::http::{header, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::get;
use axum::Router;
use prometheus::{Encoder, TextEncoder};

async fn handle_metrics() -> Response {
    let families = prometheus::gather();
    let mut buffer = Vec::new();
    let encoder = TextEncoder::new();
    if let Err(err) = encoder.encode(&families, &mut buffer) {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("failed to encode metrics: {err}"),
        )
            .into_response();
    }

    (
        StatusCode::OK,
        [(header::CONTENT_TYPE, encoder.format_type().to_string())],
        buffer,
    )
        .into_response()
}

pub fn router() -> Router {
    Router::new().route("/metrics", get(handle_metrics))
}

/// Binds `addr` and serves `/metrics` until the process exits. Intended to
/// run as a background tokio task alongside the gRPC server (main.rs).
pub async fn serve(addr: SocketAddr) -> std::io::Result<()> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, router()).await
}

#[cfg(test)]
#[path = "metrics_server_test.rs"]
mod tests;
