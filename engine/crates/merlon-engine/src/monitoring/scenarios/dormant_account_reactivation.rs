use crate::monitoring::config::{EvaluationMode, ScenarioConfig};
use crate::monitoring::engine::{AlertOutput, AlertSeverity, TransactionInput};
use crate::monitoring::scenarios::Scenario;

/// Detects a customer resuming activity, above a threshold amount, after a
/// long gap with no transactions (dormant_days). Requires at least two
/// transactions in the evaluated set to measure a gap; a customer with only
/// one (or zero) transactions in this evaluation has no prior activity to
/// compare against and cannot be flagged.
pub struct DormantAccountReactivationScenario {
    config: ScenarioConfig,
}

impl DormantAccountReactivationScenario {
    pub fn new(config: ScenarioConfig) -> Self {
        Self { config }
    }
}

impl Scenario for DormantAccountReactivationScenario {
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
        let dormant_days = self.config.adjusted_i64("dormant_days", risk_tier).unwrap_or(180);
        let reactivation_threshold = self
            .config
            .adjusted_f64("reactivation_threshold", risk_tier)
            .unwrap_or(1_000_000.0);

        let mut customer_txns: Vec<&TransactionInput> = transactions
            .iter()
            .filter(|t| t.customer_id == customer_id)
            .collect();

        if customer_txns.len() < 2 {
            return Vec::new();
        }

        customer_txns.sort_by_key(|t| t.executed_at_secs);

        let dormant_secs = dormant_days * 86_400;

        for pair in customer_txns.windows(2) {
            let prev = pair[0];
            let curr = pair[1];
            let gap_secs = curr.executed_at_secs - prev.executed_at_secs;

            if gap_secs >= dormant_secs && curr.amount >= reactivation_threshold {
                let severity = if curr.amount >= reactivation_threshold * 2.0 {
                    AlertSeverity::High
                } else {
                    AlertSeverity::Medium
                };

                return vec![AlertOutput {
                    scenario_id: self.config.scenario_id.clone(),
                    severity,
                    customer_id: customer_id.to_string(),
                    transaction_ids: vec![curr.transaction_id.clone()],
                    description: format!(
                        "dormant for {} days, reactivated with {:.0} (threshold {:.0})",
                        gap_secs / 86_400,
                        curr.amount,
                        reactivation_threshold
                    ),
                    score: curr.amount / reactivation_threshold,
                }];
            }
        }

        Vec::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::monitoring::engine::TransactionDirection;

    fn txn(id: &str, customer_id: &str, amount: f64, executed_at_secs: i64) -> TransactionInput {
        TransactionInput {
            transaction_id: id.to_string(),
            customer_id: customer_id.to_string(),
            amount,
            currency: "JPY".to_string(),
            counterparty_id: "CP1".to_string(),
            counterparty_country: "JP".to_string(),
            direction: TransactionDirection::Inbound,
            executed_at_secs,
            channel: "web".to_string(),
        }
    }

    fn config(yaml: &str) -> ScenarioConfig {
        ScenarioConfig::from_yaml_dual(yaml).unwrap()
    }

    const V2_FIXTURE: &str = r#"
schema_version: "2.0"
scenario_id: test_dormant_account_reactivation
name: Test Fixture
description: Test fixture
type: aggregation
conditions:
  additional:
    dormant_days: 180
    reactivation_threshold: 1000000
evaluation_mode: batch
severity: MEDIUM
"#;

    const DAY: i64 = 86_400;

    #[test]
    fn test_dormant_account_reactivation_detects_sudden_activity() {
        let scenario = DormantAccountReactivationScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        let txns = vec![
            txn("T1", "C001", 50_000.0, base),
            // 200 days later (> 180 dormant_days threshold), reactivates
            // with an amount above reactivation_threshold.
            txn("T2", "C001", 2_000_000.0, base + 200 * DAY),
        ];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert_eq!(alerts.len(), 1);
        assert_eq!(alerts[0].scenario_id, "test_dormant_account_reactivation");
        assert_eq!(alerts[0].transaction_ids, vec!["T2".to_string()]);
    }

    #[test]
    fn test_dormant_account_reactivation_not_detected_below_dormant_period() {
        let scenario = DormantAccountReactivationScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        let txns = vec![
            txn("T1", "C001", 50_000.0, base),
            // Only 30 days later -- not dormant long enough.
            txn("T2", "C001", 2_000_000.0, base + 30 * DAY),
        ];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }

    #[test]
    fn test_dormant_account_reactivation_not_detected_below_amount_threshold() {
        let scenario = DormantAccountReactivationScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        let txns = vec![
            txn("T1", "C001", 50_000.0, base),
            // Dormant long enough, but reactivation amount is below
            // reactivation_threshold=1,000,000.
            txn("T2", "C001", 500_000.0, base + 200 * DAY),
        ];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }

    #[test]
    fn test_dormant_account_reactivation_requires_at_least_two_transactions() {
        let scenario = DormantAccountReactivationScenario::new(config(V2_FIXTURE));
        let txns = vec![txn("T1", "C001", 5_000_000.0, 1_000_000)];

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }
}
