use super::*;

#[test]
fn test_load_v2_scenario_by_customer_type() {
    let yaml = r#"
schema_version: "2.0"
scenario_id: v2_customer_type_test
name: V2 Customer Type Test
description: Test fixture
type: aggregation
conditions:
  transaction_type: [deposit]
  aggregation:
    field: amount
    function: sum
    period: 24h
    group_by: customer_id
  threshold:
    by_customer_type:
      individual:
        by_risk_tier:
          LOW: 5000000
          MEDIUM: 3000000
          HIGH: 1000000
      corporate_domestic:
        by_risk_tier:
          LOW: 20000000
          MEDIUM: 15000000
          HIGH: 5000000
severity: HIGH
"#;
    let config = ScenarioConfig::from_yaml_dual(yaml).unwrap();

    assert_eq!(config.schema_version_kind, ScenarioSchemaVersion::V2);
    assert_eq!(
        config.resolve_threshold("individual", "HIGH"),
        Some(1_000_000.0)
    );
    // v2 can define a different value per customer_type, unlike v1.
    assert_eq!(
        config.resolve_threshold("corporate_domestic", "HIGH"),
        Some(5_000_000.0)
    );
    // customer_type not present in the fixture resolves to no threshold.
    assert_eq!(config.resolve_threshold("corporate_foreign", "HIGH"), None);
}

#[test]
fn test_load_v2_scenario_evaluation_mode_and_absolute_threshold() {
    let yaml = r#"
schema_version: "2.0"
scenario_id: v2_safety_valve_test
name: V2 Safety Valve Test
description: Test fixture
type: aggregation
conditions:
  threshold:
    by_customer_type: {}
  absolute_threshold: 50000000
evaluation_mode: realtime
severity: CRITICAL
"#;
    let config = ScenarioConfig::from_yaml_dual(yaml).unwrap();
    assert_eq!(config.evaluation_mode, "realtime");
    assert_eq!(config.absolute_threshold, Some(50_000_000.0));
}

#[test]
fn test_load_v2_scenario_evaluation_mode_defaults_to_batch() {
    // rule-schema.md §1.2: evaluation_mode defaults to "batch" for v2
    // (unlike v1, which defaults to "both" for backward compatibility).
    let yaml = r#"
schema_version: "2.0"
scenario_id: v2_default_mode_test
name: V2 Default Mode Test
description: Test fixture
type: aggregation
conditions:
  threshold:
    by_customer_type: {}
severity: LOW
"#;
    let config = ScenarioConfig::from_yaml_dual(yaml).unwrap();
    assert_eq!(config.evaluation_mode, "batch");
}

#[test]
fn test_v1_v2_golden_equivalence() {
    let v1 = ScenarioConfig::load_dual("testdata/tm_golden_v1.yaml").unwrap();
    let v2 = ScenarioConfig::load_dual("testdata/tm_golden_v2.yaml").unwrap();

    assert_eq!(v1.schema_version_kind, ScenarioSchemaVersion::V1);
    assert_eq!(v2.schema_version_kind, ScenarioSchemaVersion::V2);

    // Both fixtures encode the identical threshold semantics (see the
    // testdata files' comments); resolve_threshold must agree for every
    // customer_type x risk_tier combination, which is the WS-0 contract
    // this dual loader establishes. Full transaction-evaluation parity is
    // WS-5 scope (evaluation_mode/absolute_threshold logic).
    for customer_type in ["individual", "corporate_domestic", "corporate_foreign"] {
        for risk_tier in ["LOW", "MEDIUM", "HIGH"] {
            assert_eq!(
                v1.resolve_threshold(customer_type, risk_tier),
                v2.resolve_threshold(customer_type, risk_tier),
                "mismatch for {customer_type}/{risk_tier}"
            );
        }
    }
}
