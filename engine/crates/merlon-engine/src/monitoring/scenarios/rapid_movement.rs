use crate::monitoring::config::{EvaluationMode, ScenarioConfig};
use crate::monitoring::engine::{
    AlertOutput, AlertSeverity, TransactionDirection, TransactionInput,
};
use crate::monitoring::scenarios::Scenario;

pub struct RapidMovementScenario {
    config: ScenarioConfig,
}

impl RapidMovementScenario {
    pub fn new(config: ScenarioConfig) -> Self {
        Self { config }
    }
}

impl Scenario for RapidMovementScenario {
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
        let window_hours = self.config.adjusted_i64("window_hours", risk_tier).unwrap_or(48);
        let inbound_threshold = self
            .config
            .adjusted_f64("inbound_threshold", risk_tier)
            .unwrap_or(5_000_000.0);
        let outbound_threshold = self
            .config
            .adjusted_f64("outbound_threshold", risk_tier)
            .unwrap_or(5_000_000.0);
        let outbound_ratio_min = self
            .config
            .adjusted_f64("outbound_ratio_min", risk_tier)
            .unwrap_or(0.80);

        let customer_txns: Vec<&TransactionInput> = transactions
            .iter()
            .filter(|t| t.customer_id == customer_id)
            .collect();

        if customer_txns.is_empty() {
            return Vec::new();
        }

        let window_secs = window_hours * 3600;

        let mut sorted_txns = customer_txns.clone();
        sorted_txns.sort_by_key(|t| t.executed_at_secs);

        let mut alerts = Vec::new();

        for i in 0..sorted_txns.len() {
            let window_start = sorted_txns[i].executed_at_secs;
            let window_end = window_start + window_secs;

            let window_txns: Vec<&&TransactionInput> = sorted_txns[i..]
                .iter()
                .take_while(|t| t.executed_at_secs <= window_end)
                .collect();

            let total_inbound: f64 = window_txns
                .iter()
                .filter(|t| t.direction == TransactionDirection::Inbound)
                .map(|t| t.amount)
                .sum();

            let total_outbound: f64 = window_txns
                .iter()
                .filter(|t| t.direction == TransactionDirection::Outbound)
                .map(|t| t.amount)
                .sum();

            if total_inbound >= inbound_threshold && total_outbound >= outbound_threshold {
                let ratio = if total_inbound > 0.0 {
                    total_outbound / total_inbound
                } else {
                    0.0
                };

                if ratio >= outbound_ratio_min {
                    let tx_ids: Vec<String> =
                        window_txns.iter().map(|t| t.transaction_id.clone()).collect();

                    let severity = if ratio >= 0.95 {
                        AlertSeverity::Critical
                    } else if ratio >= 0.90 {
                        AlertSeverity::High
                    } else {
                        AlertSeverity::Medium
                    };

                    let alert = AlertOutput {
                        scenario_id: self.config.scenario_id.clone(),
                        severity,
                        customer_id: customer_id.to_string(),
                        transaction_ids: tx_ids,
                        description: format!(
                            "inbound {:.0}, outbound {:.0} (ratio {:.2}) within {} hours",
                            total_inbound, total_outbound, ratio, window_hours
                        ),
                        score: ratio,
                    };
                    alerts.push(alert);
                    break;
                }
            }
        }

        alerts
    }
}
