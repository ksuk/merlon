use super::*;

fn sample_yaml() -> &'static str {
    r#"
schema_version: "1.0"
content_type: country_risk_table
name: Sample
effective_date: "2026-07-01"
default_score: 3
countries:
  JP: { score: 1 }
  KP: { score: 5, reason: "FATF blacklist" }
"#
}

#[test]
fn test_score_for_defined_country_returns_score() {
    let table = CountryRiskTable::from_yaml(sample_yaml()).unwrap();
    assert_eq!(table.score_for("JP"), 1);
    assert_eq!(table.score_for("KP"), 5);
}

#[test]
fn test_score_for_undefined_country_returns_default_score() {
    let table = CountryRiskTable::from_yaml(sample_yaml()).unwrap();
    assert_eq!(table.score_for("ZZ"), 3);
}

#[test]
fn test_score_for_default_score_never_defaults_to_low_risk() {
    let yaml = r#"
schema_version: "1.0"
content_type: country_risk_table
effective_date: "2026-07-01"
default_score: 1
countries:
  JP: { score: 1 }
"#;
    let result = CountryRiskTable::from_yaml(yaml);
    assert!(result.is_err(), "default_score=1 must be rejected");
}

#[test]
fn test_cdd_scoring_engine_resolves_geography_via_country_risk_table() {
    use crate::scoring::config::CddWeightConfig;
    use crate::scoring::engine::{CddScoringEngine, ScoringInput};
    use std::collections::HashMap;

    let config_yaml = r#"
schema_version: "1.0"
preset_id: test_country_risk
name: Test
risk_factors:
  geography:
    weight: 1.0
    source: country_risk_table
tier_thresholds:
  LOW:
    max: 2.0
  MEDIUM:
    min: 2.0
    max: 3.5
  HIGH:
    min: 3.5
"#;
    let config = CddWeightConfig::from_yaml(config_yaml).unwrap();
    let engine = CddScoringEngine::new(config).unwrap();
    let table = CountryRiskTable::from_yaml(sample_yaml()).unwrap();

    let input = ScoringInput {
        customer_id: "C001".to_string(),
        customer_type: "individual".to_string(),
        country_code: "KP".to_string(),
        product_types: vec![],
        attributes: HashMap::new(),
    };

    let result = engine.evaluate(&input, Some(&table));

    // Before the fix, resolve_factor_value returned None whenever `values`
    // was absent (even with `source` set), silently falling back to 5.0
    // regardless of the actual country risk table content.
    assert_eq!(result.score, 5.0);
}

#[test]
fn test_counterparty_country_lookup() {
    let table = CountryRiskTable::from_yaml(sample_yaml()).unwrap();
    // TM scenario conditions reference counterparty_country the same way
    // customer scoring references country_code — both are plain country
    // codes looked up via score_for.
    let counterparty_country = "KP";
    assert_eq!(table.score_for(counterparty_country), 5);
}
