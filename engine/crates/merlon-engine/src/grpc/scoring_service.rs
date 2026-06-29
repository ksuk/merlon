use std::collections::HashMap;
use std::sync::Arc;

use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status};

use crate::proto::merlon::v1::{
    scoring_service_server::ScoringService, BatchEvaluateRequest, BatchEvaluateResponse,
    EvaluateCustomerRiskRequest, EvaluateCustomerRiskResponse, HealthRequest, HealthResponse,
    RiskFactor, RiskTier as ProtoRiskTier, Timestamp,
};
use crate::scoring::engine::{CddScoringEngine, RiskTier, ScoringInput};

pub struct ScoringServiceImpl {
    engine: Arc<CddScoringEngine>,
}

impl ScoringServiceImpl {
    pub fn new(engine: CddScoringEngine) -> Self {
        Self {
            engine: Arc::new(engine),
        }
    }

    #[allow(clippy::result_large_err)]
    fn evaluate_request(
        &self,
        req: &EvaluateCustomerRiskRequest,
    ) -> Result<EvaluateCustomerRiskResponse, Status> {
        let customer = req
            .customer
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("customer attributes required"))?;

        let customer_type = match customer.customer_type {
            1 => "individual",
            2 => "corporate_domestic",
            3 => "corporate_foreign",
            _ => return Err(Status::invalid_argument("invalid customer_type")),
        };

        let input = ScoringInput {
            customer_id: customer.customer_id.clone(),
            customer_type: customer_type.to_string(),
            country_code: customer.country_code.clone(),
            product_types: customer.product_types.clone(),
            attributes: HashMap::new(),
        };

        let result = self.engine.evaluate(&input);

        let tier = match result.tier {
            RiskTier::Low => ProtoRiskTier::Low as i32,
            RiskTier::Medium => ProtoRiskTier::Medium as i32,
            RiskTier::High => ProtoRiskTier::High as i32,
        };

        let factors: Vec<RiskFactor> = result
            .factors
            .iter()
            .map(|f| RiskFactor {
                name: f.name.clone(),
                axis: f.axis.clone(),
                score: f.contribution,
                description: f.description.clone(),
            })
            .collect();

        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default();

        Ok(EvaluateCustomerRiskResponse {
            customer_id: result.customer_id,
            score: result.score,
            tier,
            factors,
            rule_set_id: result.rule_set_id,
            rule_set_version: result.rule_set_version,
            scored_at: Some(Timestamp {
                seconds: now.as_secs() as i64,
                nanos: now.subsec_nanos() as i32,
            }),
        })
    }
}

#[tonic::async_trait]
impl ScoringService for ScoringServiceImpl {
    async fn evaluate_customer_risk(
        &self,
        request: Request<EvaluateCustomerRiskRequest>,
    ) -> Result<Response<EvaluateCustomerRiskResponse>, Status> {
        let resp = self.evaluate_request(request.get_ref())?;
        Ok(Response::new(resp))
    }

    type BatchEvaluateStream = ReceiverStream<Result<BatchEvaluateResponse, Status>>;

    async fn batch_evaluate(
        &self,
        request: Request<BatchEvaluateRequest>,
    ) -> Result<Response<Self::BatchEvaluateStream>, Status> {
        let batch = request.into_inner();
        if batch.customers.len() > 10_000 {
            return Err(Status::invalid_argument("too many customers (max 10000)"));
        }
        let (tx, rx) = mpsc::channel(128);
        let engine = Arc::clone(&self.engine);

        tokio::spawn(async move {
            let service = ScoringServiceImpl {
                engine: Arc::clone(&engine),
            };
            for customer_req in &batch.customers {
                let mut req = customer_req.clone();
                if req.rule_set_id.is_empty() {
                    req.rule_set_id.clone_from(&batch.rule_set_id);
                }
                let result = service.evaluate_request(&req);
                let resp = BatchEvaluateResponse {
                    result: result.ok(),
                };
                if tx.send(Ok(resp)).await.is_err() {
                    break;
                }
            }
        });

        Ok(Response::new(ReceiverStream::new(rx)))
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
    use crate::proto::merlon::v1::{CustomerAttributes, CustomerType};
    use crate::scoring::config::CddWeightConfig;

    fn test_engine() -> CddScoringEngine {
        let config = CddWeightConfig::load("testdata/valid_cdd_weights.yaml").unwrap();
        CddScoringEngine::new(config).unwrap()
    }

    fn test_service() -> ScoringServiceImpl {
        ScoringServiceImpl::new(test_engine())
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
    async fn test_evaluate_customer_risk() {
        let service = test_service();
        let req = EvaluateCustomerRiskRequest {
            customer: Some(CustomerAttributes {
                customer_id: "C001".to_string(),
                customer_type: CustomerType::Individual as i32,
                country_code: "JP".to_string(),
                product_types: vec!["spot_trading".to_string()],
            }),
            rule_set_id: "test_preset".to_string(),
        };

        let resp = service
            .evaluate_customer_risk(Request::new(req))
            .await
            .unwrap();
        let resp = resp.into_inner();

        assert_eq!(resp.customer_id, "C001");
        assert!(resp.score > 0.0);
        // transaction_pattern not available via proto → fallback to 5.0
        // score = 0.20*1 + 0.30*1 + 0.25*1 + 0.25*5 = 2.0 → MEDIUM
        assert_eq!(resp.tier, ProtoRiskTier::Medium as i32);
        assert!(!resp.factors.is_empty());
        assert!(resp.scored_at.is_some());
    }

    #[tokio::test]
    async fn test_evaluate_missing_customer() {
        let service = test_service();
        let req = EvaluateCustomerRiskRequest {
            customer: None,
            rule_set_id: "test".to_string(),
        };

        let result = service
            .evaluate_customer_risk(Request::new(req))
            .await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn test_evaluate_high_risk_customer() {
        let service = test_service();
        let req = EvaluateCustomerRiskRequest {
            customer: Some(CustomerAttributes {
                customer_id: "C002".to_string(),
                customer_type: CustomerType::CorporateForeign as i32,
                country_code: "KP".to_string(),
                product_types: vec!["defi_bridge".to_string()],
            }),
            rule_set_id: "test_preset".to_string(),
        };

        let resp = service
            .evaluate_customer_risk(Request::new(req))
            .await
            .unwrap();
        let resp = resp.into_inner();

        assert_eq!(resp.tier, ProtoRiskTier::High as i32);
        assert!(resp.score >= 3.5);
    }

    #[tokio::test]
    async fn test_evaluate_invalid_customer_type() {
        let service = test_service();
        let req = EvaluateCustomerRiskRequest {
            customer: Some(CustomerAttributes {
                customer_id: "C999".to_string(),
                customer_type: 99,
                country_code: "JP".to_string(),
                product_types: vec!["spot_trading".to_string()],
            }),
            rule_set_id: "test_preset".to_string(),
        };

        let result = service
            .evaluate_customer_risk(Request::new(req))
            .await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn test_batch_evaluate() {
        let service = test_service();
        let req = BatchEvaluateRequest {
            customers: vec![
                EvaluateCustomerRiskRequest {
                    customer: Some(CustomerAttributes {
                        customer_id: "C001".to_string(),
                        customer_type: CustomerType::Individual as i32,
                        country_code: "JP".to_string(),
                        product_types: vec!["spot_trading".to_string()],
                    }),
                    rule_set_id: String::new(),
                },
                EvaluateCustomerRiskRequest {
                    customer: Some(CustomerAttributes {
                        customer_id: "C002".to_string(),
                        customer_type: CustomerType::CorporateForeign as i32,
                        country_code: "KP".to_string(),
                        product_types: vec!["defi_bridge".to_string()],
                    }),
                    rule_set_id: String::new(),
                },
            ],
            rule_set_id: "test_preset".to_string(),
        };

        let resp = service
            .batch_evaluate(Request::new(req))
            .await
            .unwrap();

        let mut stream = resp.into_inner();
        let mut results = Vec::new();
        while let Some(item) = tokio_stream::StreamExt::next(&mut stream).await {
            results.push(item.unwrap());
        }

        assert_eq!(results.len(), 2);
        assert!(results[0].result.is_some());
        assert!(results[1].result.is_some());
        assert_eq!(results[0].result.as_ref().unwrap().customer_id, "C001");
        assert_eq!(results[1].result.as_ref().unwrap().customer_id, "C002");
    }
}
