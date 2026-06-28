use super::*;

#[test]
fn test_load_valid_config() {
    let config = CddWeightConfig::load("testdata/valid_cdd_weights.yaml").unwrap();
    assert_eq!(config.schema_version, "1.0");
    assert_eq!(config.preset_id, "test_preset");
    assert_eq!(config.risk_factors.len(), 4);
    assert!((config.risk_factors["customer_type"].weight - 0.20).abs() < f64::EPSILON);
    assert!((config.risk_factors["geography"].weight - 0.30).abs() < f64::EPSILON);
}

#[test]
fn test_from_yaml() {
    let yaml = r#"
schema_version: "1.0"
preset_id: inline_test
name: Inline Test
risk_factors:
  factor_a:
    weight: 0.6
    values:
      low: 1
      high: 5
  factor_b:
    weight: 0.4
    values:
      normal: 1
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.0
    max: 3.5
  HIGH:
    min: 3.5
"#;
    let config = CddWeightConfig::from_yaml(yaml).unwrap();
    assert_eq!(config.preset_id, "inline_test");
    assert_eq!(config.risk_factors.len(), 2);
}

#[test]
fn test_load_config_not_found() {
    let result = CddWeightConfig::load("nonexistent.yaml");
    assert!(result.is_err());
}

#[test]
fn test_validate_weights_sum_invalid() {
    let result = CddWeightConfig::load("testdata/invalid_weights_sum.yaml");
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("sum to 1.0"), "error was: {err}");
}

#[test]
fn test_validate_empty_risk_factors() {
    let yaml = r#"
schema_version: "1.0"
preset_id: empty
name: Empty
risk_factors: {}
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.0
    max: 3.5
  HIGH:
    min: 3.5
"#;
    let result = CddWeightConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("must not be empty"), "error was: {err}");
}

#[test]
fn test_validate_no_values_or_source() {
    let yaml = r#"
schema_version: "1.0"
preset_id: missing
name: Missing
risk_factors:
  bad_factor:
    weight: 1.0
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.0
    max: 3.5
  HIGH:
    min: 3.5
"#;
    let result = CddWeightConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(
        err.contains("values") || err.contains("source"),
        "error was: {err}"
    );
}

#[test]
fn test_validate_threshold_continuity() {
    let yaml = r#"
schema_version: "1.0"
preset_id: gap
name: Gap
risk_factors:
  f:
    weight: 1.0
    values:
      a: 1
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.5
    max: 3.5
  HIGH:
    min: 3.5
"#;
    let result = CddWeightConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("LOW.max"), "error was: {err}");
}

#[test]
fn test_applies_to_parsed() {
    let config = CddWeightConfig::load("testdata/corporate_cdd_weights.yaml").unwrap();
    let bo = &config.risk_factors["beneficial_owner_opacity"];
    assert_eq!(
        bo.applies_to.as_ref().unwrap(),
        &vec!["corporate_domestic".to_string(), "corporate_foreign".to_string()]
    );
}

#[test]
fn test_source_field_parsed() {
    let yaml = r#"
schema_version: "1.0"
preset_id: src
name: Source Test
risk_factors:
  geo:
    weight: 1.0
    source: country_risk_table
    values:
      JP: 1
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.0
    max: 3.5
  HIGH:
    min: 3.5
"#;
    let config = CddWeightConfig::from_yaml(yaml).unwrap();
    assert_eq!(
        config.risk_factors["geo"].source.as_deref(),
        Some("country_risk_table")
    );
}
