//! Exercises the `/metrics` router (Task 8, OPS-003) in-process via
//! `tower::ServiceExt::oneshot`, the same style the rest of this crate uses
//! to test service handlers without opening a real socket
//! (grpc/health_test.rs, grpc/metrics_interceptor_test.rs).

use axum::body::Body;
use axum::http::{header, Request, StatusCode};
use http_body_util::BodyExt;
use tower::ServiceExt;

use super::router;

#[tokio::test]
async fn test_metrics_endpoint_returns_prometheus_format() {
    // Touch the gRPC duration histogram so the response body has at least
    // one non-comment line to assert against, matching what a live process
    // (with the MetricsLayer wired in) would expose.
    crate::grpc::metrics_interceptor::GRPC_REQUEST_DURATION
        .with_label_values(&["EvaluateTransactions", "ok"])
        .observe(0.01);

    let request = Request::builder()
        .method("GET")
        .uri("/metrics")
        .body(Body::empty())
        .unwrap();

    let response = router().oneshot(request).await.unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let content_type = response
        .headers()
        .get(header::CONTENT_TYPE)
        .expect("content-type header")
        .to_str()
        .unwrap();
    assert!(content_type.starts_with("text/plain"));

    let body = response.into_body().collect().await.unwrap().to_bytes();
    let body = String::from_utf8(body.to_vec()).unwrap();
    assert!(body.contains("merlon_grpc_request_duration_seconds"));
}
