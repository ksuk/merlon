use tonic::{Request, Response, Status};

use crate::proto::merlon::v1::{
    config_service_server::ConfigService,
    ValidateConfigRequest, ValidateConfigResponse, ValidationError,
};
use crate::scoring::config::CddWeightConfig;
use crate::scoring::country_risk::CountryRiskTable;
use crate::monitoring::config::ScenarioConfig;
use crate::screening::config::ScreeningListConfig;

pub struct ConfigServiceImpl;

impl Default for ConfigServiceImpl {
    fn default() -> Self {
        Self::new()
    }
}

impl ConfigServiceImpl {
    pub fn new() -> Self {
        Self
    }
}

#[tonic::async_trait]
impl ConfigService for ConfigServiceImpl {
    async fn validate_config(
        &self,
        request: Request<ValidateConfigRequest>,
    ) -> Result<Response<ValidateConfigResponse>, Status> {
        let req = request.into_inner();

        if req.yaml_content.len() > 512 * 1024 {
            return Err(Status::invalid_argument("yaml_content too large (max 512KB)"));
        }

        let errors = match req.config_type.as_str() {
            "cdd_weights" => validate_cdd_weights(&req.yaml_content),
            "tm_scenarios" => validate_tm_scenarios(&req.yaml_content),
            "screening_lists" => validate_screening_lists(&req.yaml_content),
            "country_risk" => validate_country_risk(&req.yaml_content),
            _ => vec![ValidationError {
                field: "config_type".to_string(),
                message: format!("unknown config type: {}", req.config_type),
            }],
        };

        Ok(Response::new(ValidateConfigResponse {
            valid: errors.is_empty(),
            errors,
        }))
    }
}

fn validate_cdd_weights(yaml_content: &str) -> Vec<ValidationError> {
    let config: CddWeightConfig = match serde_yaml::from_str(yaml_content) {
        Ok(c) => c,
        Err(e) => {
            return vec![ValidationError {
                field: "yaml".to_string(),
                message: format!("parse error: {e}"),
            }];
        }
    };

    match config.validate() {
        Ok(()) => vec![],
        Err(e) => vec![ValidationError {
            field: "config".to_string(),
            message: e.to_string(),
        }],
    }
}

fn validate_tm_scenarios(yaml_content: &str) -> Vec<ValidationError> {
    let config: ScenarioConfig = match serde_yaml::from_str(yaml_content) {
        Ok(c) => c,
        Err(e) => {
            return vec![ValidationError {
                field: "yaml".to_string(),
                message: format!("parse error: {e}"),
            }];
        }
    };

    match config.validate() {
        Ok(()) => vec![],
        Err(e) => vec![ValidationError {
            field: "config".to_string(),
            message: e.to_string(),
        }],
    }
}

fn validate_screening_lists(yaml_content: &str) -> Vec<ValidationError> {
    let config: ScreeningListConfig = match serde_yaml::from_str(yaml_content) {
        Ok(c) => c,
        Err(e) => {
            return vec![ValidationError {
                field: "yaml".to_string(),
                message: format!("parse error: {e}"),
            }];
        }
    };

    match config.validate() {
        Ok(()) => vec![],
        Err(e) => vec![ValidationError {
            field: "config".to_string(),
            message: e.to_string(),
        }],
    }
}

fn validate_country_risk(yaml_content: &str) -> Vec<ValidationError> {
    match CountryRiskTable::from_yaml(yaml_content) {
        Ok(_) => vec![],
        Err(e) => vec![ValidationError {
            field: "config".to_string(),
            message: e.to_string(),
        }],
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_validate_valid_cdd_weights() {
        let svc = ConfigServiceImpl::new();
        let yaml = r#"
schema_version: "1.0"
preset_id: test
name: Test
risk_factors:
  geography:
    weight: 0.5
    values:
      JP: 1
      US: 3
  product:
    weight: 0.5
    values:
      basic: 1
      advanced: 3
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.0
    max: 3.5
  HIGH:
    min: 3.5
"#;
        let req = Request::new(ValidateConfigRequest {
            config_type: "cdd_weights".to_string(),
            yaml_content: yaml.to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(resp.valid);
        assert!(resp.errors.is_empty());
    }

    #[tokio::test]
    async fn test_validate_invalid_cdd_weights() {
        let svc = ConfigServiceImpl::new();
        let yaml = r#"
factors: []
thresholds:
  low_max: 2.0
  high_min: 3.5
"#;
        let req = Request::new(ValidateConfigRequest {
            config_type: "cdd_weights".to_string(),
            yaml_content: yaml.to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(!resp.valid);
        assert!(!resp.errors.is_empty());
    }

    #[tokio::test]
    async fn test_validate_invalid_yaml() {
        let svc = ConfigServiceImpl::new();
        let req = Request::new(ValidateConfigRequest {
            config_type: "cdd_weights".to_string(),
            yaml_content: "{{invalid".to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(!resp.valid);
    }

    #[tokio::test]
    async fn test_validate_valid_country_risk() {
        let svc = ConfigServiceImpl::new();
        let yaml = r#"
schema_version: "1.0"
content_type: country_risk_table
effective_date: "2026-07-01"
default_score: 3
countries:
  JP: { score: 1 }
  KP: { score: 5, reason: "FATF blacklist" }
"#;
        let req = Request::new(ValidateConfigRequest {
            config_type: "country_risk".to_string(),
            yaml_content: yaml.to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(resp.valid, "errors: {:?}", resp.errors);
    }

    #[tokio::test]
    async fn test_validate_invalid_country_risk_default_score_one() {
        let svc = ConfigServiceImpl::new();
        let yaml = r#"
schema_version: "1.0"
content_type: country_risk_table
effective_date: "2026-07-01"
default_score: 1
countries:
  JP: { score: 1 }
"#;
        let req = Request::new(ValidateConfigRequest {
            config_type: "country_risk".to_string(),
            yaml_content: yaml.to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(!resp.valid);
    }

    #[tokio::test]
    async fn test_validate_unknown_type() {
        let svc = ConfigServiceImpl::new();
        let req = Request::new(ValidateConfigRequest {
            config_type: "unknown".to_string(),
            yaml_content: "test".to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(!resp.valid);
        assert_eq!(resp.errors[0].field, "config_type");
    }

    #[tokio::test]
    async fn test_validate_valid_screening_list() {
        let svc = ConfigServiceImpl::new();
        let yaml = r#"
schema_version: "1.0"
list_id: test_list
list_type: sanctions
name: "Test List"
source: "test"
entries:
  - entry_id: E001
    names:
      - "Test Person"
    country: JP
    type: individual
"#;
        let req = Request::new(ValidateConfigRequest {
            config_type: "screening_lists".to_string(),
            yaml_content: yaml.to_string(),
        });
        let resp = svc.validate_config(req).await.unwrap().into_inner();
        assert!(resp.valid);
    }
}
