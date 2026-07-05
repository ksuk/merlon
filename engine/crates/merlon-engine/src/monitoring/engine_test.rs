use super::*;
use super::super::config::ScenarioConfig;

fn load_structuring() -> ScenarioConfig {
    ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap()
}

fn load_rapid_movement() -> ScenarioConfig {
    ScenarioConfig::load("testdata/tm_rapid_movement.yaml").unwrap()
}

fn make_txn(id: &str, customer: &str, amount: f64, dir: TransactionDirection, secs: i64) -> TransactionInput {
    TransactionInput {
        transaction_id: id.to_string(),
        customer_id: customer.to_string(),
        amount,
        currency: "JPY".to_string(),
        counterparty_id: "CP001".to_string(),
        counterparty_country: "JP".to_string(),
        direction: dir,
        executed_at_secs: secs,
        channel: "web".to_string(),
    }
}

#[test]
fn test_engine_creation() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    assert_eq!(engine.scenario_ids(), vec!["test_structuring"]);
}

#[test]
fn test_engine_multiple_scenarios() {
    let engine = TmEngine::new(vec![load_structuring(), load_rapid_movement()]).unwrap();
    assert_eq!(engine.scenario_ids().len(), 2);
}

#[test]
fn test_engine_empty_scenarios() {
    let result = TmEngine::new(vec![]);
    assert!(result.is_err());
}

#[test]
fn test_structuring_detected() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 400_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 350_000.0, TransactionDirection::Outbound, base + 3600),
        make_txn("T3", "C001", 300_000.0, TransactionDirection::Outbound, base + 7200),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert_eq!(alerts.len(), 1);
    assert_eq!(alerts[0].scenario_id, "test_structuring");
    assert_eq!(alerts[0].transaction_ids.len(), 3);
    assert_eq!(alerts[0].severity, AlertSeverity::Medium);
}

#[test]
fn test_structuring_not_detected_below_threshold() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 200_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 200_000.0, TransactionDirection::Outbound, base + 3600),
        make_txn("T3", "C001", 200_000.0, TransactionDirection::Outbound, base + 7200),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert!(alerts.is_empty());
}

#[test]
fn test_structuring_not_detected_too_few_txns() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 400_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 400_000.0, TransactionDirection::Outbound, base + 3600),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert!(alerts.is_empty());
}

#[test]
fn test_structuring_high_risk_lower_threshold() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    // 2 transactions (min_transactions=2 for HIGH), total 300k >= 500k threshold? No.
    // Let's make total >= 500k
    let txns = vec![
        make_txn("T1", "C001", 300_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 250_000.0, TransactionDirection::Outbound, base + 3600),
    ];

    let alerts = engine.evaluate("C001", "individual", "HIGH", &txns, &[]);
    assert_eq!(alerts.len(), 1);
}

#[test]
fn test_structuring_low_risk_higher_threshold() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    // For LOW: threshold=2M, min_transactions=5
    // 5 transactions totaling 1.5M < 2M → should not trigger
    let txns = vec![
        make_txn("T1", "C001", 300_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 300_000.0, TransactionDirection::Outbound, base + 1000),
        make_txn("T3", "C001", 300_000.0, TransactionDirection::Outbound, base + 2000),
        make_txn("T4", "C001", 300_000.0, TransactionDirection::Outbound, base + 3000),
        make_txn("T5", "C001", 300_000.0, TransactionDirection::Outbound, base + 4000),
    ];

    let alerts = engine.evaluate("C001", "individual", "LOW", &txns, &[]);
    assert!(alerts.is_empty());
}

#[test]
fn test_structuring_outside_window() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    let day_secs = 24 * 3600;
    // 3 transactions but spread over 3 days → no single 24h window has 3
    let txns = vec![
        make_txn("T1", "C001", 400_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 400_000.0, TransactionDirection::Outbound, base + day_secs + 1),
        make_txn("T3", "C001", 400_000.0, TransactionDirection::Outbound, base + 2 * day_secs + 1),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert!(alerts.is_empty());
}

#[test]
fn test_rapid_movement_detected() {
    let engine = TmEngine::new(vec![load_rapid_movement()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 6_000_000.0, TransactionDirection::Inbound, base),
        make_txn("T2", "C001", 5_500_000.0, TransactionDirection::Outbound, base + 3600),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert_eq!(alerts.len(), 1);
    assert_eq!(alerts[0].scenario_id, "test_rapid_movement");
    // ratio = 5.5M / 6M ≈ 0.917 → HIGH severity
    assert_eq!(alerts[0].severity, AlertSeverity::High);
}

#[test]
fn test_rapid_movement_critical_severity() {
    let engine = TmEngine::new(vec![load_rapid_movement()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 6_000_000.0, TransactionDirection::Inbound, base),
        make_txn("T2", "C001", 5_900_000.0, TransactionDirection::Outbound, base + 3600),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert_eq!(alerts.len(), 1);
    // ratio = 5.9M / 6M ≈ 0.983 → CRITICAL
    assert_eq!(alerts[0].severity, AlertSeverity::Critical);
}

#[test]
fn test_rapid_movement_not_detected_low_ratio() {
    let engine = TmEngine::new(vec![load_rapid_movement()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 6_000_000.0, TransactionDirection::Inbound, base),
        make_txn("T2", "C001", 3_000_000.0, TransactionDirection::Outbound, base + 3600),
    ];

    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert!(alerts.is_empty());
}

#[test]
fn test_rapid_movement_high_risk_lower_threshold() {
    let engine = TmEngine::new(vec![load_rapid_movement()]).unwrap();
    let base = 1_000_000i64;
    // HIGH: inbound/outbound threshold = 2M
    let txns = vec![
        make_txn("T1", "C001", 2_500_000.0, TransactionDirection::Inbound, base),
        make_txn("T2", "C001", 2_300_000.0, TransactionDirection::Outbound, base + 3600),
    ];

    let alerts = engine.evaluate("C001", "individual", "HIGH", &txns, &[]);
    assert_eq!(alerts.len(), 1);
}

#[test]
fn test_scenario_filter() {
    let engine = TmEngine::new(vec![load_structuring(), load_rapid_movement()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 400_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C001", 400_000.0, TransactionDirection::Outbound, base + 3600),
        make_txn("T3", "C001", 400_000.0, TransactionDirection::Outbound, base + 7200),
    ];

    // Only run rapid_movement → structuring should not fire
    let alerts = engine.evaluate(
        "C001",
        "individual",
        "MEDIUM",
        &txns,
        &["test_rapid_movement".to_string()],
    );
    assert!(alerts.is_empty());

    // Only run structuring
    let alerts = engine.evaluate(
        "C001",
        "individual",
        "MEDIUM",
        &txns,
        &["test_structuring".to_string()],
    );
    assert_eq!(alerts.len(), 1);
}

#[test]
fn test_different_customer_ids() {
    let engine = TmEngine::new(vec![load_structuring()]).unwrap();
    let base = 1_000_000i64;
    let txns = vec![
        make_txn("T1", "C001", 400_000.0, TransactionDirection::Outbound, base),
        make_txn("T2", "C002", 400_000.0, TransactionDirection::Outbound, base + 3600),
        make_txn("T3", "C001", 400_000.0, TransactionDirection::Outbound, base + 7200),
    ];

    // C001 only has 2 txns → below min_transactions=3
    let alerts = engine.evaluate("C001", "individual", "MEDIUM", &txns, &[]);
    assert!(alerts.is_empty());
}
