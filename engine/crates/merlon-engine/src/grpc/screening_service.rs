use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::proto::merlon::v1::{
    screening_service_server::ScreeningService, HealthRequest, HealthResponse,
    ScreenCustomerRequest, ScreenCustomerResponse, ScreenMatch as ProtoScreenMatch, Timestamp,
};
use crate::screening::engine::{ScreenInput, ScreeningEngine};

pub struct ScreeningServiceImpl {
    engine: Arc<ScreeningEngine>,
}

impl ScreeningServiceImpl {
    pub fn new(engine: ScreeningEngine) -> Self {
        Self {
            engine: Arc::new(engine),
        }
    }
}

#[tonic::async_trait]
impl ScreeningService for ScreeningServiceImpl {
    async fn screen_customer(
        &self,
        request: Request<ScreenCustomerRequest>,
    ) -> Result<Response<ScreenCustomerResponse>, Status> {
        let req = request.into_inner();

        if req.name.is_empty() {
            return Err(Status::invalid_argument("name required"));
        }

        let input = ScreenInput {
            customer_id: req.customer_id.clone(),
            name: req.name,
            name_kana: if req.name_kana.is_empty() {
                None
            } else {
                Some(req.name_kana)
            },
            country_code: if req.country_code.is_empty() {
                None
            } else {
                Some(req.country_code)
            },
        };

        let result = self.engine.screen(&input, &req.list_ids);

        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default();

        let matches: Vec<ProtoScreenMatch> = result
            .matches
            .iter()
            .map(|m| ProtoScreenMatch {
                list_id: m.list_id.clone(),
                entry_id: m.entry_id.clone(),
                matched_name: m.matched_name.clone(),
                similarity: m.similarity,
                list_type: m.list_type.clone(),
                source: m.source.clone(),
            })
            .collect();

        Ok(Response::new(ScreenCustomerResponse {
            customer_id: result.customer_id,
            hit: result.hit,
            matches,
            lists_checked: result.lists_checked as i32,
            screened_at: Some(Timestamp {
                seconds: now.as_secs() as i64,
                nanos: now.subsec_nanos() as i32,
            }),
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
    use crate::screening::config::ScreeningListConfig;

    fn test_engine() -> ScreeningEngine {
        let list = ScreeningListConfig::load("testdata/screening_list.yaml").unwrap();
        ScreeningEngine::new(vec![list], 0.85).unwrap()
    }

    fn test_service() -> ScreeningServiceImpl {
        ScreeningServiceImpl::new(test_engine())
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
    }

    #[tokio::test]
    async fn test_screen_hit() {
        let service = test_service();
        let req = ScreenCustomerRequest {
            customer_id: "C001".to_string(),
            name: "Kim Jong Un".to_string(),
            name_kana: String::new(),
            country_code: "KP".to_string(),
            date_of_birth: String::new(),
            list_ids: vec![],
        };

        let resp = service
            .screen_customer(Request::new(req))
            .await
            .unwrap();
        let resp = resp.into_inner();

        assert!(resp.hit);
        assert_eq!(resp.matches.len(), 1);
        assert_eq!(resp.matches[0].entry_id, "S001");
        assert!(resp.screened_at.is_some());
    }

    #[tokio::test]
    async fn test_screen_no_hit() {
        let service = test_service();
        let req = ScreenCustomerRequest {
            customer_id: "C002".to_string(),
            name: "John Smith".to_string(),
            name_kana: String::new(),
            country_code: "US".to_string(),
            date_of_birth: String::new(),
            list_ids: vec![],
        };

        let resp = service
            .screen_customer(Request::new(req))
            .await
            .unwrap();
        let resp = resp.into_inner();

        assert!(!resp.hit);
        assert!(resp.matches.is_empty());
    }

    #[tokio::test]
    async fn test_screen_missing_name() {
        let service = test_service();
        let req = ScreenCustomerRequest {
            customer_id: "C003".to_string(),
            name: String::new(),
            name_kana: String::new(),
            country_code: String::new(),
            date_of_birth: String::new(),
            list_ids: vec![],
        };

        let result = service
            .screen_customer(Request::new(req))
            .await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }
}
