use crate::monitoring::config::{EvaluationMode, ScenarioConfig};
use crate::monitoring::engine::{AlertOutput, AlertSeverity, TransactionInput};
use crate::monitoring::scenarios::Scenario;

pub struct StructuringScenario {
    config: ScenarioConfig,
}

impl StructuringScenario {
    pub fn new(config: ScenarioConfig) -> Self {
        Self { config }
    }
}

impl Scenario for StructuringScenario {
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
        let window_hours = self.config.adjusted_i64("window_hours", risk_tier).unwrap_or(24);
        let threshold = self
            .config
            .adjusted_f64("threshold_amount", risk_tier)
            .unwrap_or(1_000_000.0);
        let min_txns = self
            .config
            .adjusted_i64("min_transactions", risk_tier)
            .unwrap_or(3) as usize;
        let individual_below = self
            .config
            .adjusted_f64("individual_below", risk_tier)
            .unwrap_or(500_000.0);

        let customer_txns: Vec<&TransactionInput> = transactions
            .iter()
            .filter(|t| t.customer_id == customer_id)
            .collect();

        if customer_txns.len() < min_txns {
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
                .filter(|t| t.amount < individual_below && t.amount > 0.0)
                .collect();

            if window_txns.len() >= min_txns {
                let total: f64 = window_txns.iter().map(|t| t.amount).sum();

                if total >= threshold {
                    let tx_ids: Vec<String> =
                        window_txns.iter().map(|t| t.transaction_id.clone()).collect();

                    let severity = if total >= threshold * 2.0 {
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
                            "{} transactions totaling {:.0} within {} hours, each below {:.0}",
                            window_txns.len(),
                            total,
                            window_hours,
                            individual_below
                        ),
                        score: total / threshold,
                    };
                    alerts.push(alert);
                    break;
                }
            }
        }

        alerts
    }
}
