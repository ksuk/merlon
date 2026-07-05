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
    // WS-5 Task1: a customer_type absent from the fixture (here
    // corporate_foreign) falls back to the strictest (lowest) threshold
    // configured for that risk_tier among the customer_types that are
    // present, rather than resolving to no threshold (Fail-Alert
    // principle). See test_resolve_threshold_unknown_customer_type_falls_back
    // for the dedicated case.
    assert_eq!(
        config.resolve_threshold("corporate_foreign", "HIGH"),
        Some(1_000_000.0)
    );
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

// WS-5 Task8: conditions.additional (content/schema/tm_scenario_v2.json)
// feeds ScenarioConfig::parameters, so new v2 scenarios can carry
// scenario-specific parameters beyond the fixed threshold/absolute_threshold
// shape, the same way v1's flat `parameters` map always has.

#[test]
fn test_v2_conditions_additional_populates_parameters() {
    let yaml = r#"
schema_version: "2.0"
scenario_id: v2_additional_test
name: V2 Additional Params Test
description: Test fixture
type: aggregation
conditions:
  additional:
    window_hours: 1
    count_threshold: 10
    high_risk_countries: ["KP", "IR"]
severity: HIGH
"#;
    let config = ScenarioConfig::from_yaml_dual(yaml).unwrap();
    assert_eq!(config.get_i64("window_hours"), Some(1));
    assert_eq!(config.get_i64("count_threshold"), Some(10));
    assert_eq!(
        config.get_string_list("high_risk_countries"),
        vec!["KP".to_string(), "IR".to_string()]
    );
}

#[test]
fn test_get_string_list_defaults_to_empty_when_absent() {
    let config = ScenarioConfig::from_yaml_dual(
        r#"
schema_version: "2.0"
scenario_id: v2_no_additional_test
name: V2 No Additional Test
description: Test fixture
type: aggregation
conditions: {}
severity: LOW
"#,
    )
    .unwrap();
    assert!(config.get_string_list("high_risk_countries").is_empty());
}

// WS-5 Task1: by_customer_type -> by_risk_tier resolution against the
// canonical structuring example (transaction-monitoring.md TM-004a).

#[test]
fn test_resolve_threshold_individual_high_tier() {
    let config = ScenarioConfig::load_dual("testdata/tm_structuring_v2.yaml").unwrap();
    assert_eq!(
        config.resolve_threshold("individual", "HIGH"),
        Some(1_000_000.0)
    );
}

#[test]
fn test_resolve_threshold_corporate_domestic_low_tier() {
    let config = ScenarioConfig::load_dual("testdata/tm_structuring_v2.yaml").unwrap();
    assert_eq!(
        config.resolve_threshold("corporate_domestic", "LOW"),
        Some(20_000_000.0)
    );
}

#[test]
fn test_resolve_threshold_corporate_foreign_medium_tier() {
    let config = ScenarioConfig::load_dual("testdata/tm_structuring_v2.yaml").unwrap();
    assert_eq!(
        config.resolve_threshold("corporate_foreign", "MEDIUM"),
        Some(10_000_000.0)
    );
}

#[test]
fn test_resolve_threshold_unknown_customer_type_falls_back() {
    // Fail-Alert: an unrecognized customer_type must not silently skip
    // evaluation. resolve_threshold falls back to the strictest (lowest)
    // threshold configured for the risk_tier across known customer_types,
    // so an unknown type never gets a more lenient threshold than any
    // known one. For HIGH: individual=1,000,000 < corporate_foreign=3,000,000
    // < corporate_domestic=5,000,000, so the fallback is 1,000,000.
    let config = ScenarioConfig::load_dual("testdata/tm_structuring_v2.yaml").unwrap();
    assert_eq!(
        config.resolve_threshold("unknown_type", "HIGH"),
        Some(1_000_000.0)
    );
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
