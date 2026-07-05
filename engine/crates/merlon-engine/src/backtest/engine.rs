use std::collections::{HashMap, HashSet};
use std::time::Instant;

use crate::monitoring::engine::{AlertOutput, AlertSeverity, TmEngine, TransactionInput};

pub struct BacktestInput {
    pub customers: Vec<BacktestCustomer>,
    pub transactions: Vec<TransactionInput>,
    pub scenario_ids: Vec<String>,
    pub description: String,
}

pub struct BacktestCustomer {
    pub customer_id: String,
    pub customer_type: String,
    pub risk_tier: String,
}

#[derive(Debug, Clone)]
pub struct ScenarioResult {
    pub scenario_id: String,
    pub alerts_generated: usize,
    pub high_severity_count: usize,
    pub medium_severity_count: usize,
    pub low_severity_count: usize,
    pub affected_customer_ids: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct BacktestResult {
    pub backtest_id: String,
    pub total_transactions: usize,
    pub total_customers: usize,
    pub total_alerts: usize,
    pub scenario_results: Vec<ScenarioResult>,
    pub execution_time_ms: f64,
}

pub struct BacktestEngine<'a> {
    tm_engine: &'a TmEngine,
}

impl<'a> BacktestEngine<'a> {
    pub fn new(tm_engine: &'a TmEngine) -> Self {
        Self { tm_engine }
    }

    pub fn run(&self, input: &BacktestInput) -> BacktestResult {
        let start = Instant::now();

        let customer_tiers: HashMap<&str, &str> = input
            .customers
            .iter()
            .map(|c| (c.customer_id.as_str(), c.risk_tier.as_str()))
            .collect();
        let customer_types: HashMap<&str, &str> = input
            .customers
            .iter()
            .map(|c| (c.customer_id.as_str(), c.customer_type.as_str()))
            .collect();

        let txns_by_customer = group_transactions(&input.transactions);

        let mut all_alerts: Vec<AlertOutput> = Vec::new();

        for (customer_id, txns) in &txns_by_customer {
            let risk_tier = customer_tiers
                .get(customer_id.as_str())
                .copied()
                .unwrap_or("MEDIUM");
            let customer_type = customer_types
                .get(customer_id.as_str())
                .copied()
                .unwrap_or("individual");

            let alerts =
                self.tm_engine
                    .evaluate(customer_id, customer_type, risk_tier, txns, &input.scenario_ids);

            all_alerts.extend(alerts);
        }

        let scenario_results = aggregate_by_scenario(&all_alerts);
        let total_alerts = all_alerts.len();
        let elapsed = start.elapsed();

        BacktestResult {
            backtest_id: generate_backtest_id(),
            total_transactions: input.transactions.len(),
            total_customers: input.customers.len(),
            total_alerts,
            scenario_results,
            execution_time_ms: elapsed.as_secs_f64() * 1000.0,
        }
    }
}

fn group_transactions(transactions: &[TransactionInput]) -> HashMap<String, Vec<TransactionInput>> {
    let mut map: HashMap<String, Vec<TransactionInput>> = HashMap::new();
    for txn in transactions {
        map.entry(txn.customer_id.clone())
            .or_default()
            .push(txn.clone());
    }
    map
}

fn aggregate_by_scenario(alerts: &[AlertOutput]) -> Vec<ScenarioResult> {
    let mut map: HashMap<String, (usize, usize, usize, usize, HashSet<String>)> = HashMap::new();

    for alert in alerts {
        let entry = map
            .entry(alert.scenario_id.clone())
            .or_insert((0, 0, 0, 0, HashSet::new()));

        entry.0 += 1;
        match alert.severity {
            AlertSeverity::High | AlertSeverity::Critical => entry.1 += 1,
            AlertSeverity::Medium => entry.2 += 1,
            AlertSeverity::Low => entry.3 += 1,
        }
        entry.4.insert(alert.customer_id.clone());
    }

    let mut results: Vec<ScenarioResult> = map
        .into_iter()
        .map(|(id, (total, high, medium, low, customers))| {
            let mut affected: Vec<String> = customers.into_iter().collect();
            affected.sort();
            ScenarioResult {
                scenario_id: id,
                alerts_generated: total,
                high_severity_count: high,
                medium_severity_count: medium,
                low_severity_count: low,
                affected_customer_ids: affected,
            }
        })
        .collect();

    results.sort_by(|a, b| a.scenario_id.cmp(&b.scenario_id));
    results
}

fn generate_backtest_id() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let ts = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    format!("bt_{ts}")
}

#[cfg(test)]
#[path = "engine_test.rs"]
mod tests;
