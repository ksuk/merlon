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
