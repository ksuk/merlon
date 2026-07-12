//! Generic tower middleware that records `merlon_grpc_request_duration_seconds`
//! (OPS-003, the operational design §4.4) for every gRPC call handled by this process,
//! regardless of which service handles it.
//!
//! `tonic::service::Interceptor` only sees the *request*, so it cannot time a
//! response; instrumenting duration requires wrapping the transport-level
//! `tower::Service` that tonic-generated servers implement instead. Apply
//! this layer once at the `Server::builder()` level so it wraps every
//! registered service without touching each service impl individually.

use std::future::Future;
use std::pin::Pin;
use std::sync::LazyLock;
use std::task::{Context, Poll};
use std::time::Instant;

use prometheus::{register_histogram_vec, HistogramVec};
use tower::{Layer, Service};

/// Matches the Go API's `merlon_grpc_request_duration_seconds` histogram
/// (api/internal/metrics/metrics.go) so both sides of the gRPC boundary
/// expose the same metric name, labeled `method`/`status`.
pub static GRPC_REQUEST_DURATION: LazyLock<HistogramVec> = LazyLock::new(|| {
    register_histogram_vec!(
        "merlon_grpc_request_duration_seconds",
        "gRPC call duration in seconds, as observed by the Rust engine server.",
        &["method", "status"]
    )
    .expect("register merlon_grpc_request_duration_seconds")
});

#[derive(Clone, Default)]
pub struct MetricsLayer;

impl<S> Layer<S> for MetricsLayer {
    type Service = MetricsService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        MetricsService { inner }
    }
}

#[derive(Clone)]
pub struct MetricsService<S> {
    inner: S,
}

impl<S, ReqBody, RespBody> Service<http::Request<ReqBody>> for MetricsService<S>
where
    S: Service<http::Request<ReqBody>, Response = http::Response<RespBody>> + Clone + Send + 'static,
    S::Future: Send + 'static,
    ReqBody: Send + 'static,
    RespBody: Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: http::Request<ReqBody>) -> Self::Future {
        let method = grpc_method_name(req.uri().path());
        let start = Instant::now();
        let mut inner = self.inner.clone();

        Box::pin(async move {
            let result = inner.call(req).await;
            let status = match &result {
                Ok(response) => grpc_status_label(response),
                Err(_) => "error",
            };
            GRPC_REQUEST_DURATION
                .with_label_values(&[method.as_str(), status])
                .observe(start.elapsed().as_secs_f64());
            result
        })
    }
}

/// Extracts the RPC method name from a gRPC request path
/// (`/<package>.<Service>/<Method>`), e.g.
/// `/merlon.v1.MonitoringService/EvaluateTransactions` -> `EvaluateTransactions`.
fn grpc_method_name(path: &str) -> String {
    path.rsplit('/').next().unwrap_or("unknown").to_string()
}

/// Reads the gRPC status from response headers, which tonic populates there
/// only for "Trailers-Only" responses (errors returned before any message is
/// streamed). Successful responses carry `grpc-status` in HTTP trailers
/// instead, which are available only after the body is fully read; since
/// this layer measures time-to-response-headers, such calls are
/// conservatively labeled "ok".
fn grpc_status_label<B>(response: &http::Response<B>) -> &'static str {
    match response.headers().get("grpc-status") {
        Some(value) if value.as_bytes() != b"0" => "error",
        _ => "ok",
    }
}
