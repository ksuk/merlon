use crate::monitoring::config::{EvaluationMode, ScenarioConfig};
use crate::monitoring::engine::{AlertOutput, AlertSeverity, TransactionInput};
use crate::monitoring::scenarios::Scenario;

/// Detects a burst of many small-amount transactions in a short window
/// (velocity check distinct from structuring: structuring flags a
/// *sum* below a monetary threshold; this scenario flags transaction
/// *count* regardless of total sum, per-transaction amount capped at
/// max_amount_per_txn so large legitimate transfers don't count toward the
/// burst).
pub struct HighFrequencySmallAmountScenario {
    config: ScenarioConfig,
}

impl HighFrequencySmallAmountScenario {
    pub fn new(config: ScenarioConfig) -> Self {
        Self { config }
    }
}

impl Scenario for HighFrequencySmallAmountScenario {
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
        let window_hours = self.config.adjusted_i64("window_hours", risk_tier).unwrap_or(1);
        let count_threshold = self
            .config
            .adjusted_i64("count_threshold", risk_tier)
            .unwrap_or(10) as usize;
        let max_amount_per_txn = self
            .config
            .adjusted_f64("max_amount_per_txn", risk_tier)
            .unwrap_or(100_000.0);

        let customer_txns: Vec<&TransactionInput> = transactions
            .iter()
            .filter(|t| t.customer_id == customer_id)
            .collect();

        if customer_txns.len() < count_threshold {
            return Vec::new();
        }

        let window_secs = window_hours * 3600;

        let mut sorted_txns = customer_txns.clone();
        sorted_txns.sort_by_key(|t| t.executed_at_secs);

        for i in 0..sorted_txns.len() {
            let window_start = sorted_txns[i].executed_at_secs;
            let window_end = window_start + window_secs;

            let window_txns: Vec<&&TransactionInput> = sorted_txns[i..]
                .iter()
                .take_while(|t| t.executed_at_secs <= window_end)
                .filter(|t| t.amount <= max_amount_per_txn && t.amount > 0.0)
                .collect();

            if window_txns.len() >= count_threshold {
                let tx_ids: Vec<String> =
                    window_txns.iter().map(|t| t.transaction_id.clone()).collect();
                let total: f64 = window_txns.iter().map(|t| t.amount).sum();

                let severity = if window_txns.len() as f64 >= count_threshold as f64 * 2.0 {
                    AlertSeverity::High
                } else {
                    AlertSeverity::Medium
                };

                return vec![AlertOutput {
                    scenario_id: self.config.scenario_id.clone(),
                    severity,
                    customer_id: customer_id.to_string(),
                    transaction_ids: tx_ids,
                    description: format!(
                        "{} transactions (each <= {:.0}) totaling {:.0} within {} hours",
                        window_txns.len(),
                        max_amount_per_txn,
                        total,
                        window_hours
                    ),
                    score: window_txns.len() as f64 / count_threshold as f64,
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
            direction: TransactionDirection::Outbound,
            executed_at_secs,
            channel: "web".to_string(),
        }
    }

    fn config(yaml: &str) -> ScenarioConfig {
        ScenarioConfig::from_yaml_dual(yaml).unwrap()
    }

    const V2_FIXTURE: &str = r#"
schema_version: "2.0"
scenario_id: test_high_frequency_small_amount
name: Test Fixture
description: Test fixture
type: aggregation
conditions:
  additional:
    window_hours: 1
    count_threshold: 5
    max_amount_per_txn: 50000
evaluation_mode: batch
severity: MEDIUM
"#;

    #[test]
    fn test_high_frequency_small_amount_detects_burst() {
        let scenario = HighFrequencySmallAmountScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        let txns: Vec<TransactionInput> = (0..5)
            .map(|i| txn(&format!("T{i}"), "C001", 10_000.0, base + i * 60))
            .collect();

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert_eq!(alerts.len(), 1);
        assert_eq!(alerts[0].scenario_id, "test_high_frequency_small_amount");
        assert_eq!(alerts[0].transaction_ids.len(), 5);
    }

    #[test]
    fn test_high_frequency_small_amount_not_detected_below_count_threshold() {
        let scenario = HighFrequencySmallAmountScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        // Only 4 transactions, below count_threshold=5.
        let txns: Vec<TransactionInput> = (0..4)
            .map(|i| txn(&format!("T{i}"), "C001", 10_000.0, base + i * 60))
            .collect();

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }

    #[test]
    fn test_high_frequency_small_amount_excludes_large_transactions_from_count() {
        let scenario = HighFrequencySmallAmountScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        // 5 transactions but 2 exceed max_amount_per_txn=50000, so only 3
        // qualify -- below count_threshold=5.
        let mut txns: Vec<TransactionInput> = (0..3)
            .map(|i| txn(&format!("T{i}"), "C001", 10_000.0, base + i * 60))
            .collect();
        txns.push(txn("T3", "C001", 200_000.0, base + 180));
        txns.push(txn("T4", "C001", 200_000.0, base + 240));

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }

    #[test]
    fn test_high_frequency_small_amount_outside_window() {
        let scenario = HighFrequencySmallAmountScenario::new(config(V2_FIXTURE));
        let base = 1_000_000i64;
        // 5 transactions spread across 5 hours; window_hours=1 means they
        // never all fall within a single window.
        let txns: Vec<TransactionInput> = (0..5)
            .map(|i| txn(&format!("T{i}"), "C001", 10_000.0, base + i * 3600))
            .collect();

        let alerts = scenario.evaluate("C001", "individual", "MEDIUM", &txns);
        assert!(alerts.is_empty());
    }
}
