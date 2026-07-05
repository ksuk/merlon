use crate::monitoring::config::{EvaluationMode, ScenarioConfig};
use crate::monitoring::engine::{
    AlertOutput, AlertSeverity, TransactionDirection, TransactionInput,
};
use crate::monitoring::scenarios::Scenario;

/// Detects outbound transfers to a configured list of high-risk countries
/// (rule-schema.md: this WS carries the list as a scenario parameter,
/// `high_risk_countries`; WS-2's independent `country_risk_table` content
/// can replace it later without changing this evaluate logic). Unlike the
/// aggregation scenarios, this is a per-transaction check: every qualifying
/// transaction produces its own alert rather than one alert per evaluation
/// window.
pub struct HighRiskCountryTransferScenario {
    config: ScenarioConfig,
}

impl HighRiskCountryTransferScenario {
    pub fn new(config: ScenarioConfig) -> Self {
        Self { config }
    }
}

impl Scenario for HighRiskCountryTransferScenario {
    fn scenario_id(&self) -> &str {
        &self.config.scenario_id
    }

    fn evaluation_mode(&self) -> EvaluationMode {
        self.config.evaluation_mode_kind()
    }

    fn evaluate(
        &self,
        customer_id: &str,
        _customer_type: &str,
        risk_tier: &str,
        transactions: &[TransactionInput],
    ) -> Vec<AlertOutput> {
        let threshold = self
            .config
            .adjusted_f64("threshold_amount", risk_tier)
            .unwrap_or(1_000_000.0);
        // Global Constraints: no hardcoded rule content. An unconfigured
        // list means this scenario has nothing to check against, so it
        // never fires rather than falling back to a built-in country list.
        let high_risk_countries = self.config.get_string_list("high_risk_countries");
        if high_risk_countries.is_empty() {
            return Vec::new();
        }

        transactions
            .iter()
            .filter(|t| {
                t.customer_id == customer_id
                    && t.direction == TransactionDirection::Outbound
                    && t.amount >= threshold
                    && high_risk_countries
                        .iter()
                        .any(|c| c.eq_ignore_ascii_case(&t.counterparty_country))
            })
            .map(|t| AlertOutput {
                scenario_id: self.config.scenario_id.clone(),
                severity: AlertSeverity::High,
                customer_id: customer_id.to_string(),
                transaction_ids: vec![t.transaction_id.clone()],
                description: format!(
                    "outbound transfer of {:.0} to high-risk country {} (threshold {:.0})",
                    t.amount, t.counterparty_country, threshold
                ),
                score: t.amount / threshold,
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn txn(
        id: &str,
        customer_id: &str,
        amount: f64,
        direction: TransactionDirection,
        counterparty_country: &str,
    ) -> TransactionInput {
        TransactionInput {
            transaction_id: id.to_string(),
            customer_id: customer_id.to_string(),
            amount,
            currency: "JPY".to_string(),
            counterparty_id: "CP1".to_string(),
            counterparty_country: counterparty_country.to_string(),
            direction,
            executed_at_secs: 1_000_000,
            channel: "web".to_string(),
        }
    }

    fn config(yaml: &str) -> ScenarioConfig {
        ScenarioConfig::from_yaml_dual(yaml).unwrap()
    }

    const V2_FIXTURE: &str = r#"
schema_version: "2.0"
scenario_id: test_high_risk_country_transfer
name: Test Fixture
description: Test fixture
type: aggregation
conditions:
  additional:
    threshold_amount: 1000000
    high_risk_countries: ["KP", "IR"]
evaluation_mode: realtime
severity: HIGH
"#;

    #[test]
    fn test_high_risk_country_transfer_detects_target_country() {
        let scenario = HighRiskCountryTransferScenario::new(config(V2_FIXTURE));
        let txns = vec![txn(
            "T1",
            "C001",
            2_000_000.0,
            TransactionDirection::Outbound,
            "KP",
        )];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert_eq!(alerts.len(), 1);
        assert_eq!(alerts[0].scenario_id, "test_high_risk_country_transfer");
        assert_eq!(alerts[0].transaction_ids, vec!["T1".to_string()]);
    }

    #[test]
    fn test_high_risk_country_transfer_not_detected_below_threshold() {
        let scenario = HighRiskCountryTransferScenario::new(config(V2_FIXTURE));
        let txns = vec![txn(
            "T1",
            "C001",
            500_000.0,
            TransactionDirection::Outbound,
            "KP",
        )];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }

    #[test]
    fn test_high_risk_country_transfer_not_detected_for_non_listed_country() {
        let scenario = HighRiskCountryTransferScenario::new(config(V2_FIXTURE));
        let txns = vec![txn(
            "T1",
            "C001",
            2_000_000.0,
            TransactionDirection::Outbound,
            "US",
        )];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }

    #[test]
    fn test_high_risk_country_transfer_ignores_inbound_direction() {
        let scenario = HighRiskCountryTransferScenario::new(config(V2_FIXTURE));
        let txns = vec![txn(
            "T1",
            "C001",
            2_000_000.0,
            TransactionDirection::Inbound,
            "KP",
        )];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }
}
