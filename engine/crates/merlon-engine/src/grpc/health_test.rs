//! OPS-002 (overview.md §4.4): standard grpc.health.v1 health check.
//! Exercises the same `tonic_health::server::health_reporter()` wiring
//! main.rs uses, via `HealthService::from_health_reporter` so the `Health`
//! trait's `check` method can be called directly without starting a server.

use tonic::Request;
use tonic_health::pb::health_server::Health;
use tonic_health::pb::HealthCheckRequest;
use tonic_health::server::HealthService;

#[tokio::test]
async fn test_health_reporter_reports_serving() {
    let (reporter, _health_service) = tonic_health::server::health_reporter();
    let health_service = HealthService::from_health_reporter(reporter);

    let resp = health_service
        .check(Request::new(HealthCheckRequest {
            service: String::new(),
        }))
        .await
        .expect("overall health check should succeed")
        .into_inner();

    assert_eq!(
        resp.status,
        tonic_health::pb::health_check_response::ServingStatus::Serving as i32
    );
}
