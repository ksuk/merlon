use super::*;
use crate::monitoring::config::ScenarioConfig;
use crate::monitoring::engine::TransactionDirection;

fn build_tm_engine() -> TmEngine {
    let structuring =
        ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap();
    let rapid = ScenarioConfig::load("testdata/tm_rapid_movement.yaml").unwrap();
    TmEngine::new(vec![structuring, rapid]).unwrap()
}

fn make_transaction(
    id: &str,
    customer_id: &str,
    amount: f64,
    direction: TransactionDirection,
    executed_at_secs: i64,
) -> TransactionInput {
    TransactionInput {
        transaction_id: id.to_string(),
        customer_id: customer_id.to_string(),
        amount,
        currency: "JPY".to_string(),
        counterparty_id: String::new(),
        counterparty_country: String::new(),
        direction,
        executed_at_secs,
        channel: String::new(),
    }
}

#[test]
fn test_backtest_no_alerts() {
    let tm = build_tm_engine();
    let engine = BacktestEngine::new(&tm);

    let input = BacktestInput {
        customers: vec![BacktestCustomer {
            customer_id: "C001".to_string(),
            risk_tier: "MEDIUM".to_string(),
        }],
        transactions: vec![make_transaction(
            "T1",
            "C001",
            10000.0,
            TransactionDirection::Inbound,
            1000,
        )],
        scenario_ids: vec![],
        description: "test".to_string(),
    };

    let result = engine.run(&input);

    assert_eq!(result.total_transactions, 1);
    assert_eq!(result.total_customers, 1);
    assert_eq!(result.total_alerts, 0);
    assert!(result.scenario_results.is_empty());
    assert!(result.execution_time_ms >= 0.0);
    assert!(result.backtest_id.starts_with("bt_"));
}

#[test]
fn test_backtest_structuring_detection() {
    let tm = build_tm_engine();
    let engine = BacktestEngine::new(&tm);

    // threshold=1M, individual_below=500K, min_transactions=3
    let base_time = 1000i64;
    let input = BacktestInput {
        customers: vec![BacktestCustomer {
            customer_id: "C001".to_string(),
            risk_tier: "MEDIUM".to_string(),
        }],
        transactions: vec![
            make_transaction("T1", "C001", 400_000.0, TransactionDirection::Inbound, base_time),
            make_transaction("T2", "C001", 350_000.0, TransactionDirection::Inbound, base_time + 100),
            make_transaction("T3", "C001", 300_000.0, TransactionDirection::Inbound, base_time + 200),
        ],
        scenario_ids: vec![],
        description: "structuring test".to_string(),
    };

    let result = engine.run(&input);

    assert!(result.total_alerts > 0, "expected alerts, got 0. total_txns={}", result.total_transactions);
    let structuring = result
        .scenario_results
        .iter()
        .find(|s| s.scenario_id.contains("structuring"));
    assert!(structuring.is_some(), "expected structuring scenario alert, got: {:?}", result.scenario_results);
}

#[test]
fn test_backtest_multiple_customers() {
    let tm = build_tm_engine();
    let engine = BacktestEngine::new(&tm);

    let base_time = 1000i64;
    let input = BacktestInput {
        customers: vec![
            BacktestCustomer {
                customer_id: "C001".to_string(),
                risk_tier: "MEDIUM".to_string(),
            },
            BacktestCustomer {
                customer_id: "C002".to_string(),
                risk_tier: "HIGH".to_string(),
            },
        ],
        transactions: vec![
            make_transaction("T1", "C001", 400_000.0, TransactionDirection::Inbound, base_time),
            make_transaction("T2", "C001", 350_000.0, TransactionDirection::Inbound, base_time + 100),
            make_transaction("T3", "C001", 300_000.0, TransactionDirection::Inbound, base_time + 200),
            make_transaction("T4", "C002", 5000.0, TransactionDirection::Inbound, base_time),
        ],
        scenario_ids: vec![],
        description: "multi-customer".to_string(),
    };

    let result = engine.run(&input);

    assert_eq!(result.total_transactions, 4);
    assert_eq!(result.total_customers, 2);
}

#[test]
fn test_backtest_scenario_filter() {
    let tm = build_tm_engine();
    let engine = BacktestEngine::new(&tm);

    let base_time = 1000i64;
    let input = BacktestInput {
        customers: vec![BacktestCustomer {
            customer_id: "C001".to_string(),
            risk_tier: "MEDIUM".to_string(),
        }],
        transactions: vec![
            make_transaction("T1", "C001", 400_000.0, TransactionDirection::Inbound, base_time),
            make_transaction("T2", "C001", 350_000.0, TransactionDirection::Inbound, base_time + 100),
            make_transaction("T3", "C001", 300_000.0, TransactionDirection::Inbound, base_time + 200),
        ],
        scenario_ids: vec!["test_rapid_movement".to_string()],
        description: "filtered".to_string(),
    };

    let result = engine.run(&input);

    for sr in &result.scenario_results {
        assert!(
            !sr.scenario_id.contains("structuring"),
            "structuring should be filtered out"
        );
    }
}

#[test]
fn test_backtest_rapid_movement_detection() {
    let tm = build_tm_engine();
    let engine = BacktestEngine::new(&tm);

    // inbound_threshold=5M, outbound_threshold=5M, ratio>=0.80
    let base_time = 1000i64;
    let input = BacktestInput {
        customers: vec![BacktestCustomer {
            customer_id: "C001".to_string(),
            risk_tier: "MEDIUM".to_string(),
        }],
        transactions: vec![
            make_transaction("T1", "C001", 6_000_000.0, TransactionDirection::Inbound, base_time),
            make_transaction("T2", "C001", 5_500_000.0, TransactionDirection::Outbound, base_time + 100),
        ],
        scenario_ids: vec![],
        description: "rapid movement test".to_string(),
    };

    let result = engine.run(&input);

    let rapid = result
        .scenario_results
        .iter()
        .find(|s| s.scenario_id.contains("rapid_movement"));
    assert!(rapid.is_some(), "expected rapid movement alert, got: {:?}", result.scenario_results);
}

#[test]
fn test_backtest_severity_counts() {
    let tm = build_tm_engine();
    let engine = BacktestEngine::new(&tm);

    let base_time = 1000i64;
    let input = BacktestInput {
        customers: vec![BacktestCustomer {
            customer_id: "C001".to_string(),
            risk_tier: "MEDIUM".to_string(),
        }],
        transactions: vec![
            make_transaction("T1", "C001", 400_000.0, TransactionDirection::Inbound, base_time),
            make_transaction("T2", "C001", 350_000.0, TransactionDirection::Inbound, base_time + 100),
            make_transaction("T3", "C001", 300_000.0, TransactionDirection::Inbound, base_time + 200),
        ],
        scenario_ids: vec![],
        description: "severity test".to_string(),
    };

    let result = engine.run(&input);

    for sr in &result.scenario_results {
        let total = sr.high_severity_count + sr.medium_severity_count + sr.low_severity_count;
        assert_eq!(
            total, sr.alerts_generated,
            "severity counts should sum to total for {}",
            sr.scenario_id
        );
    }
}
