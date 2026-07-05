use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::monitoring::config::EvaluationMode;
use crate::monitoring::engine::{
    AlertSeverity, TmEngine, TransactionDirection, TransactionInput,
};
use crate::proto::merlon::v1::{
    monitoring_service_server::MonitoringService, Alert, AlertSeverity as ProtoAlertSeverity,
    CustomerType as ProtoCustomerType, EvaluateTransactionsRequest, EvaluateTransactionsResponse,
    HealthRequest, HealthResponse, RiskTier as ProtoRiskTier, Timestamp,
    TransactionDirection as ProtoDirection,
};

pub struct MonitoringServiceImpl {
    engine: Arc<TmEngine>,
}

impl MonitoringServiceImpl {
    pub fn new(engine: TmEngine) -> Self {
        Self {
            engine: Arc::new(engine),
        }
    }

    pub fn from_arc(engine: Arc<TmEngine>) -> Self {
        Self { engine }
    }

    pub fn engine_arc(&self) -> Arc<TmEngine> {
        Arc::clone(&self.engine)
    }
}

// tonic::Status is the error type every gRPC handler in this codebase uses;
// boxing it here alone would just move the cost to callers that unwrap it.
#[allow(clippy::result_large_err)]
fn proto_direction_to_internal(d: i32) -> Result<TransactionDirection, Status> {
    match ProtoDirection::try_from(d) {
        Ok(ProtoDirection::Inbound) => Ok(TransactionDirection::Inbound),
        Ok(ProtoDirection::Outbound) => Ok(TransactionDirection::Outbound),
        Ok(ProtoDirection::Internal) => Ok(TransactionDirection::Internal),
        _ => Err(Status::invalid_argument("invalid transaction direction")),
    }
}

fn risk_tier_str(tier: i32) -> &'static str {
    match ProtoRiskTier::try_from(tier) {
        Ok(ProtoRiskTier::Low) => "LOW",
        Ok(ProtoRiskTier::Medium) => "MEDIUM",
        Ok(ProtoRiskTier::High) => "HIGH",
        _ => "MEDIUM",
    }
}

// TM-004a: CUSTOMER_TYPE_UNSPECIFIED (and any unrecognized value) maps to a
// key absent from ScenarioConfig::by_customer_type on purpose, so
// resolve_threshold's Fail-Alert fallback (strictest known threshold) kicks
// in rather than silently defaulting to a specific, possibly lenient, type.
fn customer_type_str(customer_type: i32) -> &'static str {
    match ProtoCustomerType::try_from(customer_type) {
        Ok(ProtoCustomerType::Individual) => "individual",
        Ok(ProtoCustomerType::CorporateDomestic) => "corporate_domestic",
        Ok(ProtoCustomerType::CorporateForeign) => "corporate_foreign",
        _ => "unspecified",
    }
}

fn severity_to_proto(s: &AlertSeverity) -> i32 {
    match s {
        AlertSeverity::Low => ProtoAlertSeverity::Low as i32,
        AlertSeverity::Medium => ProtoAlertSeverity::Medium as i32,
        AlertSeverity::High => ProtoAlertSeverity::High as i32,
        AlertSeverity::Critical => ProtoAlertSeverity::Critical as i32,
    }
}

#[tonic::async_trait]
impl MonitoringService for MonitoringServiceImpl {
    async fn evaluate_transactions(
        &self,
        request: Request<EvaluateTransactionsRequest>,
    ) -> Result<Response<EvaluateTransactionsResponse>, Status> {
        let req = request.into_inner();

        if req.customer_id.is_empty() {
            return Err(Status::invalid_argument("customer_id required"));
        }

        if req.transactions.is_empty() {
            return Err(Status::invalid_argument("transactions must not be empty"));
        }
        if req.transactions.len() > 10_000 {
            return Err(Status::invalid_argument("too many transactions (max 10000)"));
        }

        let risk_tier = risk_tier_str(req.customer_risk_tier);
        let customer_type = customer_type_str(req.customer_type);

        #[allow(clippy::result_large_err)]
        let transactions: Vec<TransactionInput> = req
            .transactions
            .iter()
            .map(|t| {
                Ok(TransactionInput {
                    transaction_id: t.transaction_id.clone(),
                    customer_id: t.customer_id.clone(),
                    amount: t.amount,
                    currency: t.currency.clone(),
                    counterparty_id: t.counterparty_id.clone(),
                    counterparty_country: t.counterparty_country.clone(),
                    direction: proto_direction_to_internal(t.direction)?,
                    executed_at_secs: t.executed_at.as_ref().map_or(0, |ts| ts.seconds),
                    channel: t.channel.clone(),
                })
            })
            .collect::<Result<Vec<_>, Status>>()?;

        // EvaluateTransactions is invoked on transaction arrival (the
        // realtime path in transaction-monitoring.md「評価モード」); batch
        // evaluation goes through the batch scheduler (WS-5 Task6/7)
        // instead of this RPC.
        let alerts = self.engine.evaluate_with_mode(
            &req.customer_id,
            customer_type,
            risk_tier,
            &transactions,
            &req.scenario_ids,
            EvaluationMode::Realtime,
        );

        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default();

        let proto_alerts: Vec<Alert> = alerts
            .iter()
            .enumerate()
            .map(|(i, a)| Alert {
                alert_id: format!("ALT-{}-{}", now.as_millis(), i),
                scenario_id: a.scenario_id.clone(),
                severity: severity_to_proto(&a.severity),
                customer_id: a.customer_id.clone(),
                transaction_ids: a.transaction_ids.clone(),
                description: a.description.clone(),
                score: a.score,
                detected_at: Some(Timestamp {
                    seconds: now.as_secs() as i64,
                    nanos: now.subsec_nanos() as i32,
                }),
            })
            .collect();

        Ok(Response::new(EvaluateTransactionsResponse {
            customer_id: req.customer_id,
            alerts: proto_alerts,
            scenarios_evaluated: self.engine.scenario_ids().len() as i32,
        }))
    }

    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        let h = crate::health();
        Ok(Response::new(HealthResponse {
            status: h.status,
            version: h.version,
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::monitoring::config::ScenarioConfig;
    use crate::proto::merlon::v1::TransactionData;

    fn test_engine() -> TmEngine {
        let structuring = ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap();
        let rapid = ScenarioConfig::load("testdata/tm_rapid_movement.yaml").unwrap();
        TmEngine::new(vec![structuring, rapid]).unwrap()
    }

    fn test_service() -> MonitoringServiceImpl {
        MonitoringServiceImpl::new(test_engine())
    }

    #[tokio::test]
    async fn test_health_rpc() {
        let service = test_service();
        let resp = service
            .health(Request::new(HealthRequest {}))
            .await
            .unwrap();
        let resp = resp.into_inner();
        assert_eq!(resp.status, "ok");
        assert!(!resp.version.is_empty());
    }

    #[tokio::test]
    async fn test_evaluate_structuring() {
        let service = test_service();
        let base = 1_000_000i64;
        let req = EvaluateTransactionsRequest {
            customer_id: "C001".to_string(),
            customer_risk_tier: ProtoRiskTier::Medium as i32,
            transactions: vec![
                TransactionData {
                    transaction_id: "T1".to_string(),
                    customer_id: "C001".to_string(),
                    amount: 400_000.0,
                    currency: "JPY".to_string(),
                    counterparty_id: "CP001".to_string(),
                    counterparty_country: "JP".to_string(),
                    direction: ProtoDirection::Outbound as i32,
                    executed_at: Some(Timestamp { seconds: base, nanos: 0 }),
                    channel: "web".to_string(),
                },
                TransactionData {
                    transaction_id: "T2".to_string(),
                    customer_id: "C001".to_string(),
                    amount: 350_000.0,
                    currency: "JPY".to_string(),
                    counterparty_id: "CP002".to_string(),
                    counterparty_country: "JP".to_string(),
                    direction: ProtoDirection::Outbound as i32,
                    executed_at: Some(Timestamp { seconds: base + 3600, nanos: 0 }),
                    channel: "web".to_string(),
                },
                TransactionData {
                    transaction_id: "T3".to_string(),
                    customer_id: "C001".to_string(),
                    amount: 300_000.0,
                    currency: "JPY".to_string(),
                    counterparty_id: "CP003".to_string(),
                    counterparty_country: "JP".to_string(),
                    direction: ProtoDirection::Outbound as i32,
                    executed_at: Some(Timestamp { seconds: base + 7200, nanos: 0 }),
                    channel: "web".to_string(),
                },
            ],
            scenario_ids: vec!["test_structuring".to_string()],
            customer_type: ProtoCustomerType::Individual as i32,
        };

        let resp = service
            .evaluate_transactions(Request::new(req))
            .await
            .unwrap();
        let resp = resp.into_inner();

        assert_eq!(resp.customer_id, "C001");
        assert_eq!(resp.alerts.len(), 1);
        assert_eq!(resp.alerts[0].scenario_id, "test_structuring");
        assert_eq!(
            resp.alerts[0].severity,
            ProtoAlertSeverity::Medium as i32
        );
        assert!(resp.alerts[0].detected_at.is_some());
    }

    #[tokio::test]
    async fn test_evaluate_empty_customer_id() {
        let service = test_service();
        let req = EvaluateTransactionsRequest {
            customer_id: String::new(),
            customer_risk_tier: ProtoRiskTier::Medium as i32,
            transactions: vec![TransactionData {
                transaction_id: "T1".to_string(),
                customer_id: "C001".to_string(),
                amount: 100.0,
                currency: "JPY".to_string(),
                counterparty_id: String::new(),
                counterparty_country: String::new(),
                direction: ProtoDirection::Inbound as i32,
                executed_at: Some(Timestamp { seconds: 0, nanos: 0 }),
                channel: String::new(),
            }],
            scenario_ids: vec![],
            customer_type: ProtoCustomerType::Individual as i32,
        };

        let result = service
            .evaluate_transactions(Request::new(req))
            .await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn test_evaluate_empty_transactions() {
        let service = test_service();
        let req = EvaluateTransactionsRequest {
            customer_id: "C001".to_string(),
            customer_risk_tier: ProtoRiskTier::Medium as i32,
            transactions: vec![],
            scenario_ids: vec![],
            customer_type: ProtoCustomerType::Individual as i32,
        };

        let result = service
            .evaluate_transactions(Request::new(req))
            .await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }
}
