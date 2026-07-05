use super::*;
use super::super::config::ScreeningListConfig;

fn load_test_list() -> ScreeningListConfig {
    ScreeningListConfig::load("testdata/screening_list.yaml").unwrap()
}

#[test]
fn test_engine_creation() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    assert_eq!(engine.list_ids(), vec!["test_sanctions"]);
}

#[test]
fn test_engine_empty_lists() {
    let result = ScreeningEngine::new(vec![], 0.85);
    assert!(result.is_err());
}

#[test]
fn test_engine_invalid_threshold() {
    let result = ScreeningEngine::new(vec![load_test_list()], 1.5);
    assert!(result.is_err());
}

#[test]
fn test_exact_name_hit() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    let input = ScreenInput {
        customer_id: "C001".to_string(),
        name: "Kim Jong Un".to_string(),
        name_kana: None,
        country_code: Some("KP".to_string()),
    };

    let result = engine.screen(&input, &[]);
    assert!(result.hit);
    assert_eq!(result.matches.len(), 1);
    assert_eq!(result.matches[0].entry_id, "S001");
    assert!((result.matches[0].similarity - 1.0).abs() < f64::EPSILON);
}

#[test]
fn test_japanese_name_hit() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    let input = ScreenInput {
        customer_id: "C002".to_string(),
        name: "金正恩".to_string(),
        name_kana: None,
        country_code: None,
    };

    let result = engine.screen(&input, &[]);
    assert!(result.hit);
    assert_eq!(result.matches[0].entry_id, "S001");
}

#[test]
fn test_kana_name_hit() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    let input = ScreenInput {
        customer_id: "C003".to_string(),
        name: "Unrelated Name".to_string(),
        name_kana: Some("田中太郎".to_string()),
        country_code: Some("JP".to_string()),
    };

    let result = engine.screen(&input, &[]);
    assert!(result.hit);
    assert_eq!(result.matches[0].entry_id, "S003");
}

#[test]
fn test_close_match_above_threshold() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.80).unwrap();
    let input = ScreenInput {
        customer_id: "C004".to_string(),
        name: "Kim Jong-Un".to_string(),
        name_kana: None,
        country_code: None,
    };

    let result = engine.screen(&input, &[]);
    assert!(result.hit);
}

#[test]
fn test_no_hit() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    let input = ScreenInput {
        customer_id: "C005".to_string(),
        name: "John Smith".to_string(),
        name_kana: None,
        country_code: Some("US".to_string()),
    };

    let result = engine.screen(&input, &[]);
    assert!(!result.hit);
    assert!(result.matches.is_empty());
}

#[test]
fn test_list_filter() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    let input = ScreenInput {
        customer_id: "C006".to_string(),
        name: "Kim Jong Un".to_string(),
        name_kana: None,
        country_code: None,
    };

    let result = engine.screen(&input, &["nonexistent_list".to_string()]);
    assert!(!result.hit);
    assert_eq!(result.lists_checked, 0);
}

#[test]
fn test_multiple_matches_sorted() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.50).unwrap();
    let input = ScreenInput {
        customer_id: "C007".to_string(),
        name: "Taro Tanaka".to_string(),
        name_kana: None,
        country_code: None,
    };

    let result = engine.screen(&input, &[]);
    assert!(result.hit);
    // Should match S003 "Taro Tanaka" with high similarity
    assert_eq!(result.matches[0].entry_id, "S003");

    // Verify sorted by similarity descending
    for i in 1..result.matches.len() {
        assert!(result.matches[i - 1].similarity >= result.matches[i].similarity);
    }
}

// PEP-RCA (family/close-associate) entries must pass through list_type
// unchanged: RCA relationship/name-matching logic is the provider's
// responsibility, not this engine's (screening.md "RCA 自体の名寄せ・関係性
// 判定ロジック...は本システムでは実装しない").
const PEP_RCA_LIST_YAML: &str = r#"
schema_version: "1.0"
list_id: pep_rca_provider
list_type: PEP-RCA
name: "PEP Family/Close Associate (test)"
source: "PEP provider (test)"
entries:
  - entry_id: "RCA-001"
    names:
      - "Jane Relative"
    country: "JP"
    type: individual
"#;

#[test]
fn test_pep_rca_list_type_preserved() {
    let list = ScreeningListConfig::from_yaml(PEP_RCA_LIST_YAML).unwrap();
    let engine = ScreeningEngine::new(vec![list], 0.85).unwrap();

    let input = ScreenInput {
        customer_id: "C009".to_string(),
        name: "Jane Relative".to_string(),
        name_kana: None,
        country_code: Some("JP".to_string()),
    };

    let result = engine.screen(&input, &[]);
    assert!(result.hit);
    assert_eq!(result.matches.len(), 1);
    assert_eq!(result.matches[0].list_type, "PEP-RCA");
    assert_eq!(result.matches[0].entry_id, "RCA-001");
}

#[test]
fn test_lists_checked_count() {
    let engine = ScreeningEngine::new(vec![load_test_list()], 0.85).unwrap();
    let input = ScreenInput {
        customer_id: "C008".to_string(),
        name: "Nobody".to_string(),
        name_kana: None,
        country_code: None,
    };

    let result = engine.screen(&input, &[]);
    assert_eq!(result.lists_checked, 1);
}
