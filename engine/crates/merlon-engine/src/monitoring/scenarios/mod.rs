pub mod dormant_account_reactivation;
pub mod high_frequency_small_amount;
pub mod high_risk_country_transfer;
pub mod rapid_movement;
pub mod structuring;

use super::config::{EvaluationMode, ScenarioConfig};
use super::engine::{AlertOutput, TransactionInput};

pub trait Scenario: Send + Sync {
    fn scenario_id(&self) -> &str;

    fn evaluation_mode(&self) -> EvaluationMode;

    fn evaluate(
        &self,
        customer_id: &str,
        customer_type: &str,
        risk_tier: &str,
        transactions: &[TransactionInput],
    ) -> Vec<AlertOutput>;
}

pub fn build_scenario(config: ScenarioConfig) -> Option<Box<dyn Scenario>> {
    let id = config.scenario_id.as_str();
    if id.starts_with("tm_structuring") || id.starts_with("test_structuring") {
        Some(Box::new(structuring::StructuringScenario::new(config)))
    } else if id.starts_with("tm_rapid_movement") || id.starts_with("test_rapid_movement") {
        Some(Box::new(rapid_movement::RapidMovementScenario::new(config)))
    } else if id.starts_with("tm_high_frequency_small_amount")
        || id.starts_with("test_high_frequency_small_amount")
    {
        Some(Box::new(
            high_frequency_small_amount::HighFrequencySmallAmountScenario::new(config),
        ))
    } else if id.starts_with("tm_dormant_account_reactivation")
        || id.starts_with("test_dormant_account_reactivation")
    {
        Some(Box::new(
            dormant_account_reactivation::DormantAccountReactivationScenario::new(config),
        ))
    } else if id.starts_with("tm_high_risk_country_transfer")
        || id.starts_with("test_high_risk_country_transfer")
    {
        Some(Box::new(
            high_risk_country_transfer::HighRiskCountryTransferScenario::new(config),
        ))
    } else {
        None
    }
}

#[cfg(test)]
#[path = "absolute_threshold_test.rs"]
mod absolute_threshold_tests;
