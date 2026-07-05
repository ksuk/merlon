use crate::monitoring::config::ScenarioConfig;
use crate::monitoring::engine::{TmEngine, TransactionDirection, TransactionInput};

fn make_txn(id: &str, customer_id: &str, amount: f64, secs: i64) -> TransactionInput {
    TransactionInput {
        transaction_id: id.to_string(),
        customer_id: customer_id.to_string(),
        amount,
        currency: "JPY".to_string(),
        counterparty_id: "CP001".to_string(),
        counterparty_country: "JP".to_string(),
        direction: TransactionDirection::Outbound,
        executed_at_secs: secs,
        channel: "web".to_string(),
    }
}

// Explicit absolute_threshold (900,000) set below the tier threshold, which
// defaults to 1,000,000 for a v2-loaded StructuringScenario (v2 content has
// no parameters/risk_tier_adjustments, so threshold_amount always falls
// back to its hardcoded default). This isolates the safety valve: any total
// in [900_000, 1_000_000) can only fire via absolute_threshold, never via
// the tier threshold.
const V2_STRUCTURING_ABS_900K: &str = r#"
schema_version: "2.0"
scenario_id: test_structuring_abs_valve
name: Structuring Absolute Threshold Test
description: Test fixture
type: aggregation
conditions:
  threshold:
    by_customer_type: {}
  absolute_threshold: 900000
severity: HIGH
"#;

fn engine_with_abs_900k() -> TmEngine {
    let config = ScenarioConfig::from_yaml_dual(V2_STRUCTURING_ABS_900K).unwrap();
    TmEngine::new(vec![config]).unwrap()
}

#[test]
fn test_absolute_threshold_fires_independent_of_tier_threshold() {
    let engine = engine_with_abs_900k();
    let base = 1_000_000i64;

    // Below absolute_threshold (900,000 - 1): no alert from either path.
    let below = vec![
        make_txn("T1", "C001", 300_000.0, base),
        make_txn("T2", "C001", 300_000.0, base + 100),
        make_txn("T3", "C001", 299_999.0, base + 200),
    ];
    let alerts = engine.evaluate("C001", "individual", "LOW", &below, &[]);
    assert!(alerts.is_empty(), "total 899,999 must not fire");

    // Exactly at absolute_threshold: fires via the safety valve even though
    // 900,000 < the tier threshold's default of 1,000,000.
    let exact = vec![
        make_txn("T1", "C001", 300_000.0, base),
        make_txn("T2", "C001", 300_000.0, base + 100),
        make_txn("T3", "C001", 300_000.0, base + 200),
    ];
    let alerts = engine.evaluate("C001", "individual", "LOW", &exact, &[]);
    assert_eq!(alerts.len(), 1, "total == absolute_threshold must fire");

    // One yen above absolute_threshold: still fires, still below tier
    // threshold (900,001 < 1,000,000).
    let above = vec![
        make_txn("T1", "C001", 300_000.0, base),
        make_txn("T2", "C001", 300_000.0, base + 100),
        make_txn("T3", "C001", 300_001.0, base + 200),
    ];
    let alerts = engine.evaluate("C001", "individual", "LOW", &above, &[]);
    assert_eq!(alerts.len(), 1, "total == absolute_threshold + 1 must fire");
}

#[test]
fn test_absolute_threshold_does_not_suppress_tier_based_alert() {
    let engine = engine_with_abs_900k();
    let base = 1_000_000i64;

    // Total (1,200,000) exceeds both the tier threshold (1,000,000 default)
    // and absolute_threshold (900,000): exactly one alert, not two.
    let txns = vec![
        make_txn("T1", "C001", 400_000.0, base),
        make_txn("T2", "C001", 400_000.0, base + 100),
        make_txn("T3", "C001", 400_000.0, base + 200),
    ];
    let alerts = engine.evaluate("C001", "individual", "LOW", &txns, &[]);
    assert_eq!(alerts.len(), 1);
}

#[test]
fn test_absolute_threshold_default_when_omitted() {
    // v1 content has no absolute_threshold field at all, so it resolves to
    // the system default of 10,000,000 (rule-schema.md §3.1 migration item
    // 3). The tier threshold is overridden to 50,000,000 (far above the
    // test totals) so only the default absolute_threshold can trigger.
    let yaml = r#"
schema_version: "1.0"
scenario_id: test_structuring_abs_default
name: Absolute Threshold Default Test
description: Test fixture
parameters:
  window_hours: 24
  threshold_amount: 50000000
  min_transactions: 3
  individual_below: 20000000
risk_tier_adjustments: {}
"#;
    let config = ScenarioConfig::from_yaml_dual(yaml).unwrap();
    assert_eq!(config.absolute_threshold, None);
    assert_eq!(config.absolute_threshold(), 10_000_000.0);

    let engine = TmEngine::new(vec![config]).unwrap();
    let base = 1_000_000i64;

    // Total just below the default: no alert (tier threshold is 50M, far
    // higher, so it can't be the one firing).
    let below = vec![
        make_txn("T1", "C001", 4_000_000.0, base),
        make_txn("T2", "C001", 3_500_000.0, base + 100),
        make_txn("T3", "C001", 2_499_999.0, base + 200),
    ];
    let alerts = engine.evaluate("C001", "individual", "LOW", &below, &[]);
    assert!(alerts.is_empty(), "total just below the 10M default must not fire");

    // Total exactly at the default: fires via the safety valve.
    let exact = vec![
        make_txn("T1", "C001", 4_000_000.0, base),
        make_txn("T2", "C001", 3_500_000.0, base + 100),
        make_txn("T3", "C001", 2_500_000.0, base + 200),
    ];
    let alerts = engine.evaluate("C001", "individual", "LOW", &exact, &[]);
    assert_eq!(alerts.len(), 1, "total == default absolute_threshold (10M) must fire");
}
