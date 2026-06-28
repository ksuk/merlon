use super::*;

#[test]
fn test_load_screening_list() {
    let config = ScreeningListConfig::load("testdata/screening_list.yaml").unwrap();
    assert_eq!(config.list_id, "test_sanctions");
    assert_eq!(config.list_type, "sanctions");
    assert_eq!(config.entries.len(), 3);
    assert_eq!(config.entries[0].entry_id, "S001");
    assert_eq!(config.entries[0].names.len(), 2);
    assert_eq!(config.entries[0].country.as_deref(), Some("KP"));
}

#[test]
fn test_validate_empty_list_id() {
    let yaml = r#"
schema_version: "1.0"
list_id: ""
list_type: sanctions
name: Bad
source: test
entries:
  - entry_id: E1
    names: ["Test"]
"#;
    let result = ScreeningListConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("list_id"), "error was: {err}");
}

#[test]
fn test_validate_empty_entries() {
    let yaml = r#"
schema_version: "1.0"
list_id: test
list_type: sanctions
name: Bad
source: test
entries: []
"#;
    let result = ScreeningListConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("entries"), "error was: {err}");
}

#[test]
fn test_validate_entry_no_names() {
    let yaml = r#"
schema_version: "1.0"
list_id: test
list_type: sanctions
name: Bad
source: test
entries:
  - entry_id: E1
    names: []
"#;
    let result = ScreeningListConfig::from_yaml(yaml);
    assert!(result.is_err());
    let err = result.unwrap_err().to_string();
    assert!(err.contains("name"), "error was: {err}");
}

#[test]
fn test_load_not_found() {
    let result = ScreeningListConfig::load("nonexistent.yaml");
    assert!(result.is_err());
}
