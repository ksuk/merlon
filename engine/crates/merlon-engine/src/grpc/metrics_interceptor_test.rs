//! Verifies `MetricsLayer` (Task 7, overview.md §4.4 OPS-003) records
//! `merlon_grpc_request_duration_seconds` for a real gRPC call, wrapping the
//! actual generated `MonitoringServiceServer` the same way main.rs does
//! (rather than a stand-in `tower::Service`), so this exercises the real
//! request/response framing tonic produces.

use bytes::{BufMut, Bytes, BytesMut};
use prometheus::Encoder;
use prost::Message;
use tower::{Layer, Service};

use super::metrics_interceptor::MetricsLayer;
use crate::grpc::monitoring_service::MonitoringServiceImpl;
use crate::monitoring::config::ScenarioConfig;
use crate::monitoring::engine::TmEngine;
use crate::proto::merlon::v1::monitoring_service_server::MonitoringServiceServer;
use crate::proto::merlon::v1::{
    EvaluateTransactionsRequest, RiskTier as ProtoRiskTier, Timestamp, TransactionData,
    TransactionDirection as ProtoDirection,
};

fn test_engine() -> TmEngine {
    let structuring = ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap();
    TmEngine::new(vec![structuring]).unwrap()
}

/// Encodes `message` into a single gRPC length-delimited frame (a 1-byte
/// compression flag, a 4-byte big-endian length, then the protobuf payload)
/// and wraps it in a `tonic::body::Body`, matching what a real gRPC client
/// sends over the wire.
fn grpc_request_body<M: Message>(message: &M) -> tonic::body::Body {
    let payload = message.encode_to_vec();
    let mut framed = BytesMut::with_capacity(5 + payload.len());
    framed.put_u8(0);
    framed.put_u32(payload.len() as u32);
    framed.extend_from_slice(&payload);
    tonic::body::Body::new(http_body_util::Full::from(Bytes::from(framed)))
}

#[tokio::test]
async fn test_metrics_layer_records_evaluate_transactions_duration() {
    let service = MonitoringServiceServer::new(MonitoringServiceImpl::new(test_engine()));
    let mut instrumented = MetricsLayer.layer(service);

    let request_body = grpc_request_body(&EvaluateTransactionsRequest {
        customer_id: "C001".to_string(),
        customer_risk_tier: ProtoRiskTier::Medium as i32,
        transactions: vec![TransactionData {
            transaction_id: "T1".to_string(),
            customer_id: "C001".to_string(),
            amount: 100_000.0,
            currency: "JPY".to_string(),
            counterparty_id: "CP001".to_string(),
            counterparty_country: "JP".to_string(),
            direction: ProtoDirection::Outbound as i32,
            executed_at: Some(Timestamp {
                seconds: 1_000_000,
                nanos: 0,
            }),
            channel: "web".to_string(),
        }],
        scenario_ids: vec![],
    });

    let request = http::Request::builder()
        .method(http::Method::POST)
        .uri("/merlon.v1.MonitoringService/EvaluateTransactions")
        .header("content-type", "application/grpc")
        .body(request_body)
        .expect("build gRPC request");

    let response = instrumented
        .call(request)
        .await
        .expect("MetricsLayer must forward the inner service's Ok response");
    assert_eq!(response.status(), http::StatusCode::OK);

    let families = prometheus::gather();
    let family = families
        .iter()
        .find(|f| f.name() == "merlon_grpc_request_duration_seconds")
        .expect("merlon_grpc_request_duration_seconds should be registered");

    let has_method_sample = family.get_metric().iter().any(|m| {
        m.get_label()
            .iter()
            .any(|l| l.name() == "method" && l.value() == "EvaluateTransactions")
    });
    assert!(
        has_method_sample,
        "expected a merlon_grpc_request_duration_seconds sample labeled method=EvaluateTransactions"
    );

    let mut buffer = Vec::new();
    prometheus::TextEncoder::new()
        .encode(&families, &mut buffer)
        .expect("encode metrics as text");
    let encoded = String::from_utf8(buffer).expect("metrics text encoding is valid UTF-8");
    assert!(encoded.contains("merlon_grpc_request_duration_seconds"));
    assert!(encoded.contains("EvaluateTransactions"));
}
