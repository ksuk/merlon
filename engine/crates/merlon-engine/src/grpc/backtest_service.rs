use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::backtest::engine::{
    BacktestCustomer, BacktestEngine, BacktestInput,
};
use crate::monitoring::engine::{TmEngine, TransactionDirection, TransactionInput};
use crate::proto::merlon::v1::{
    backtest_service_server::BacktestService, HealthRequest, HealthResponse,
    RunBacktestRequest, RunBacktestResponse, ScenarioResult as ProtoScenarioResult,
};

pub struct BacktestServiceImpl {
    tm_engine: Arc<TmEngine>,
}

impl BacktestServiceImpl {
    pub fn new(tm_engine: Arc<TmEngine>) -> Self {
        Self { tm_engine }
    }
}

// tonic::Status is the error type every gRPC handler in this codebase uses;
// boxing it here alone would just move the cost to callers that unwrap it.
#[allow(clippy::result_large_err)]
fn direction_from_proto(d: i32) -> Result<TransactionDirection, Status> {
    match d {
        1 => Ok(TransactionDirection::Inbound),
        2 => Ok(TransactionDirection::Outbound),
        3 => Ok(TransactionDirection::Internal),
        _ => Err(Status::invalid_argument("invalid transaction direction")),
    }
}

fn risk_tier_from_proto(t: i32) -> String {
    match t {
        1 => "LOW".to_string(),
        2 => "MEDIUM".to_string(),
        3 => "HIGH".to_string(),
        _ => "MEDIUM".to_string(),
    }
}

#[tonic::async_trait]
impl BacktestService for BacktestServiceImpl {
    async fn run_backtest(
        &self,
        request: Request<RunBacktestRequest>,
    ) -> Result<Response<RunBacktestResponse>, Status> {
        let req = request.into_inner();

        if req.transactions.is_empty() {
            return Err(Status::invalid_argument("transactions required"));
        }
        if req.customers.is_empty() {
            return Err(Status::invalid_argument("customers required"));
        }
        if req.customers.len() > 1_000 {
            return Err(Status::invalid_argument("too many customers (max 1000)"));
        }
        if req.transactions.len() > 100_000 {
            return Err(Status::invalid_argument("too many transactions (max 100000)"));
        }

        let customers: Vec<BacktestCustomer> = req
            .customers
            .iter()
            .map(|c| BacktestCustomer {
                customer_id: c.customer_id.clone(),
                risk_tier: risk_tier_from_proto(c.risk_tier),
            })
            .collect();

        #[allow(clippy::result_large_err)]
        let transactions: Vec<TransactionInput> = req
            .transactions
            .iter()
            .map(|t| {
                let executed_at_secs = t
                    .executed_at
                    .as_ref()
                    .map(|ts| ts.seconds)
                    .unwrap_or(0);

                Ok(TransactionInput {
                    transaction_id: t.transaction_id.clone(),
                    customer_id: t.customer_id.clone(),
                    amount: t.amount,
                    currency: t.currency.clone(),
                    counterparty_id: t.counterparty_id.clone(),
                    counterparty_country: t.counterparty_country.clone(),
                    direction: direction_from_proto(t.direction)?,
                    executed_at_secs,
                    channel: t.channel.clone(),
                })
            })
            .collect::<Result<Vec<_>, Status>>()?;

        let input = BacktestInput {
            customers,
            transactions,
            scenario_ids: req.scenario_ids,
            description: req.description,
        };

        let engine = BacktestEngine::new(&self.tm_engine);
        let result = engine.run(&input);

        let scenario_results: Vec<ProtoScenarioResult> = result
            .scenario_results
            .iter()
            .map(|sr| ProtoScenarioResult {
                scenario_id: sr.scenario_id.clone(),
                alerts_generated: sr.alerts_generated as i32,
                high_severity_count: sr.high_severity_count as i32,
                medium_severity_count: sr.medium_severity_count as i32,
                low_severity_count: sr.low_severity_count as i32,
                affected_customer_ids: sr.affected_customer_ids.clone(),
            })
            .collect();

        Ok(Response::new(RunBacktestResponse {
            backtest_id: result.backtest_id,
            total_transactions: result.total_transactions as i32,
            total_customers: result.total_customers as i32,
            total_alerts: result.total_alerts as i32,
            scenario_results,
            execution_time_ms: result.execution_time_ms,
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
    use crate::proto::merlon::v1::{BacktestCustomer as PbCust, BacktestTransaction, Timestamp};

    fn test_tm_engine() -> TmEngine {
        let structuring = ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap();
        let rapid = ScenarioConfig::load("testdata/tm_rapid_movement.yaml").unwrap();
        TmEngine::new(vec![structuring, rapid]).unwrap()
    }

    fn test_service() -> BacktestServiceImpl {
        BacktestServiceImpl::new(Arc::new(test_tm_engine()))
    }

    #[tokio::test]
    async fn test_health_rpc() {
        let service = test_service();
        let resp = service
            .health(Request::new(HealthRequest {}))
            .await
            .unwrap();
        assert_eq!(resp.into_inner().status, "ok");
    }

    #[tokio::test]
    async fn test_backtest_rpc() {
        let service = test_service();

        let req = RunBacktestRequest {
            customers: vec![PbCust {
                customer_id: "C001".to_string(),
                customer_type: 0,
                country_code: "JP".to_string(),
                product_types: vec![],
                risk_tier: 2, // MEDIUM
            }],
            transactions: vec![
                BacktestTransaction {
                    transaction_id: "T1".to_string(),
                    customer_id: "C001".to_string(),
                    amount: 400_000.0,
                    currency: "JPY".to_string(),
                    counterparty_id: String::new(),
                    counterparty_country: String::new(),
                    direction: 1, // INBOUND
                    executed_at: Some(Timestamp { seconds: 1000, nanos: 0 }),
                    channel: String::new(),
                },
                BacktestTransaction {
                    transaction_id: "T2".to_string(),
                    customer_id: "C001".to_string(),
                    amount: 350_000.0,
                    currency: "JPY".to_string(),
                    counterparty_id: String::new(),
                    counterparty_country: String::new(),
                    direction: 1,
                    executed_at: Some(Timestamp { seconds: 1100, nanos: 0 }),
                    channel: String::new(),
                },
                BacktestTransaction {
                    transaction_id: "T3".to_string(),
                    customer_id: "C001".to_string(),
                    amount: 300_000.0,
                    currency: "JPY".to_string(),
                    counterparty_id: String::new(),
                    counterparty_country: String::new(),
                    direction: 1,
                    executed_at: Some(Timestamp { seconds: 1200, nanos: 0 }),
                    channel: String::new(),
                },
            ],
            scenario_ids: vec![],
            description: "rpc test".to_string(),
        };

        let resp = service
            .run_backtest(Request::new(req))
            .await
            .unwrap();
        let resp = resp.into_inner();

        assert_eq!(resp.total_transactions, 3);
        assert_eq!(resp.total_customers, 1);
        assert!(resp.total_alerts > 0);
        assert!(!resp.backtest_id.is_empty());
    }

    #[tokio::test]
    async fn test_backtest_empty_transactions() {
        let service = test_service();
        let req = RunBacktestRequest {
            customers: vec![PbCust {
                customer_id: "C001".to_string(),
                customer_type: 0,
                country_code: "JP".to_string(),
                product_types: vec![],
                risk_tier: 2,
            }],
            transactions: vec![],
            scenario_ids: vec![],
            description: String::new(),
        };

        let result = service.run_backtest(Request::new(req)).await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }
}
