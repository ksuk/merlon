use super::config::{ConfigError, ScenarioConfig};
use super::scenarios::{self, Scenario};

#[derive(Debug, Clone)]
pub struct TransactionInput {
    pub transaction_id: String,
    pub customer_id: String,
    pub amount: f64,
    pub currency: String,
    pub counterparty_id: String,
    pub counterparty_country: String,
    pub direction: TransactionDirection,
    pub executed_at_secs: i64,
    pub channel: String,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TransactionDirection {
    Inbound,
    Outbound,
    Internal,
}

#[derive(Debug, Clone, PartialEq)]
pub enum AlertSeverity {
    Low,
    Medium,
    High,
    Critical,
}

impl AlertSeverity {
    pub fn as_str(&self) -> &'static str {
        match self {
            AlertSeverity::Low => "LOW",
            AlertSeverity::Medium => "MEDIUM",
            AlertSeverity::High => "HIGH",
            AlertSeverity::Critical => "CRITICAL",
        }
    }
}

#[derive(Debug, Clone)]
pub struct AlertOutput {
    pub scenario_id: String,
    pub severity: AlertSeverity,
    pub customer_id: String,
    pub transaction_ids: Vec<String>,
    pub description: String,
    pub score: f64,
}

pub struct TmEngine {
    scenarios: Vec<Box<dyn Scenario>>,
}

impl TmEngine {
    pub fn new(configs: Vec<ScenarioConfig>) -> Result<Self, ConfigError> {
        let mut built = Vec::new();
        for config in configs {
            config.validate()?;
            match scenarios::build_scenario(config.clone()) {
                Some(s) => built.push(s),
                None => {
                    return Err(ConfigError::Validation(format!(
                        "unknown scenario type: {}",
                        config.scenario_id
                    )));
                }
            }
        }
        if built.is_empty() {
            return Err(ConfigError::Validation(
                "at least one scenario must be configured".to_string(),
            ));
        }
        Ok(Self { scenarios: built })
    }

    pub fn evaluate(
        &self,
        customer_id: &str,
        risk_tier: &str,
        transactions: &[TransactionInput],
        scenario_ids: &[String],
    ) -> Vec<AlertOutput> {
        let mut alerts = Vec::new();
        for scenario in &self.scenarios {
            if !scenario_ids.is_empty() && !scenario_ids.contains(&scenario.scenario_id().to_string())
            {
                continue;
            }
            let mut scenario_alerts = scenario.evaluate(customer_id, risk_tier, transactions);
            alerts.append(&mut scenario_alerts);
        }
        alerts
    }

    pub fn scenario_ids(&self) -> Vec<&str> {
        self.scenarios.iter().map(|s| s.scenario_id()).collect()
    }
}

#[cfg(test)]
#[path = "engine_test.rs"]
mod tests;
