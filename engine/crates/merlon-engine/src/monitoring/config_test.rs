use super::*;

#[test]
fn test_load_structuring_config() {
    let config = ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap();
    assert_eq!(config.scenario_id, "test_structuring");
    assert_eq!(config.get_i64("window_hours"), Some(24));
    assert_eq!(config.get_f64("threshold_amount"), Some(1_000_000.0));
    assert_eq!(config.get_i64("min_transactions"), Some(3));
}

#[test]
fn test_load_rapid_movement_config() {
    let config = ScenarioConfig::load("testdata/tm_rapid_movement.yaml").unwrap();
    assert_eq!(config.scenario_id, "test_rapid_movement");
    assert_eq!(config.get_f64("inbound_threshold"), Some(5_000_000.0));
    assert_eq!(config.get_f64("outbound_ratio_min"), Some(0.80));
}

#[test]
fn test_risk_tier_adjustment() {
    let config = ScenarioConfig::load("testdata/tm_structuring.yaml").unwrap();

    assert_eq!(
        config.adjusted_f64("threshold_amount", "HIGH"),
        Some(500_000.0)
    );
    assert_eq!(config.adjusted_i64("min_transactions", "HIGH"), Some(2));

    assert_eq!(
        config.adjusted_f64("threshold_amount", "MEDIUM"),
        Some(1_000_000.0)
    );

    assert_eq!(
        config.adjusted_f64("threshold_amount", "LOW"),
        Some(2_000_000.0)
    );
    assert_eq!(config.adjusted_i64("min_transactions", "LOW"), Some(5));
}

#[test]
fn test_from_yaml_inline() {
    let yaml = r#"
schema_version: "1.0"
scenario_id: inline_test
name: Inline
description: Test
parameters:
  window_hours: 12
  threshold: 100
risk_tier_adjustments: {}
"#;
    let config = ScenarioConfig::from_yaml(yaml).unwrap();
    assert_eq!(config.scenario_id, "inline_test");
    assert_eq!(config.get_i64("window_hours"), Some(12));
}

#[test]
fn test_validate_empty_scenario_id() {
    let yaml = r#"
schema_version: "1.0"
scenario_id: ""
name: Bad
description: Bad
parameters:
  x: 1
"#;
    let result = ScenarioConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("scenario_id"), "error was: {err}");
}

#[test]
fn test_validate_empty_parameters() {
    let yaml = r#"
schema_version: "1.0"
scenario_id: empty_params
name: Bad
description: Bad
parameters: {}
"#;
    let result = ScenarioConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("parameters"), "error was: {err}");
}

#[test]
fn test_load_not_found() {
    let result = ScenarioConfig::load("nonexistent.yaml");
    assert!(result.is_err());
}

// v1/v2 dual loader (rule-schema.md §3.1 migration item 2/3).

#[test]
fn test_load_v1_scenario_converts_to_by_risk_tier() {
    let config = ScenarioConfig::load_dual("testdata/tm_structuring.yaml").unwrap();

    assert_eq!(config.schema_version_kind, ScenarioSchemaVersion::V1);
    // v1 has no customer_type axis: the same risk-tier-adjusted
    // threshold_amount value must resolve for every customer_type.
    assert_eq!(
        config.resolve_threshold("individual", "HIGH"),
        Some(500_000.0)
    );
    assert_eq!(
        config.resolve_threshold("corporate_domestic", "HIGH"),
        Some(500_000.0)
    );
    assert_eq!(
        config.resolve_threshold("corporate_foreign", "HIGH"),
        Some(500_000.0)
    );
}

#[test]
fn test_load_v1_scenario_threshold_key_from_task_spec() {
    let yaml = r#"
schema_version: "1.0"
scenario_id: threshold_key_test
name: Threshold Key Test
description: Uses the literal "threshold" parameter name
parameters:
  threshold: 100
risk_tier_adjustments:
  HIGH:
    threshold: 100
"#;
    let config = ScenarioConfig::from_yaml_dual(yaml).unwrap();
    assert_eq!(config.resolve_threshold("individual", "HIGH"), Some(100.0));
    assert_eq!(
        config.resolve_threshold("corporate_domestic", "HIGH"),
        Some(100.0)
    );
}

#[test]
fn test_v1_evaluation_mode_defaults_to_both() {
    let config = ScenarioConfig::load_dual("testdata/tm_structuring.yaml").unwrap();
    assert_eq!(config.evaluation_mode, "both");
}

#[test]
fn test_v1_absolute_threshold_defaults_to_parameters_max() {
    let config = ScenarioConfig::load_dual("testdata/tm_structuring.yaml").unwrap();
    // testdata/tm_structuring.yaml has no absolute_threshold concept (v1
    // predates it); the system default is the largest numeric parameter.
    assert_eq!(config.absolute_threshold, Some(1_000_000.0));
}

#[test]
fn test_dual_loader_still_validates_v1() {
    let yaml = r#"
schema_version: "1.0"
scenario_id: ""
name: Bad
description: Bad
parameters:
  x: 1
"#;
    let result = ScenarioConfig::from_yaml_dual(yaml);
    assert!(result.is_err());
}
