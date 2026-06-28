pub mod rapid_movement;
pub mod structuring;

use super::config::ScenarioConfig;
use super::engine::{AlertOutput, TransactionInput};

pub trait Scenario: Send + Sync {
    fn scenario_id(&self) -> &str;

    fn evaluate(
        &self,
        customer_id: &str,
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
    } else {
        None
    }
}
