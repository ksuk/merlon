use serde::Deserialize;
use std::fmt;

#[derive(Debug)]
pub enum ConfigError {
    Io(std::io::Error),
    Parse(serde_yaml::Error),
    Validation(String),
}

impl fmt::Display for ConfigError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ConfigError::Io(e) => write!(f, "IO error: {e}"),
            ConfigError::Parse(e) => write!(f, "parse error: {e}"),
            ConfigError::Validation(msg) => write!(f, "validation error: {msg}"),
        }
    }
}

impl std::error::Error for ConfigError {}

impl From<std::io::Error> for ConfigError {
    fn from(e: std::io::Error) -> Self {
        ConfigError::Io(e)
    }
}

impl From<serde_yaml::Error> for ConfigError {
    fn from(e: serde_yaml::Error) -> Self {
        ConfigError::Parse(e)
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct ScreeningListConfig {
    pub schema_version: String,
    pub list_id: String,
    pub list_type: String,
    pub name: String,
    pub source: String,
    pub entries: Vec<ListEntry>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ListEntry {
    pub entry_id: String,
    pub names: Vec<String>,
    #[serde(default)]
    pub country: Option<String>,
    #[serde(default, rename = "type")]
    pub entry_type: Option<String>,
}

impl ScreeningListConfig {
    pub fn load(path: &str) -> Result<Self, ConfigError> {
        let content = std::fs::read_to_string(path)?;
        Self::from_yaml(&content)
    }

    pub fn from_yaml(yaml: &str) -> Result<Self, ConfigError> {
        let config: ScreeningListConfig = serde_yaml::from_str(yaml)?;
        config.validate()?;
        Ok(config)
    }

    pub fn validate(&self) -> Result<(), ConfigError> {
        if self.list_id.is_empty() {
            return Err(ConfigError::Validation(
                "list_id must not be empty".to_string(),
            ));
        }
        if self.entries.is_empty() {
            return Err(ConfigError::Validation(
                "entries must not be empty".to_string(),
            ));
        }
        for entry in &self.entries {
            if entry.names.is_empty() {
                return Err(ConfigError::Validation(format!(
                    "entry '{}' must have at least one name",
                    entry.entry_id
                )));
            }
        }
        Ok(())
    }
}

#[cfg(test)]
#[path = "config_test.rs"]
mod tests;
