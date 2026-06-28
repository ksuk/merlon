use serde::Deserialize;
use std::collections::HashMap;
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
pub struct ScenarioConfig {
    pub schema_version: String,
    pub scenario_id: String,
    pub name: String,
    pub description: String,
    pub parameters: HashMap<String, serde_yaml::Value>,
    #[serde(default)]
    pub risk_tier_adjustments: HashMap<String, HashMap<String, serde_yaml::Value>>,
}

impl ScenarioConfig {
    pub fn load(path: &str) -> Result<Self, ConfigError> {
        let content = std::fs::read_to_string(path)?;
        Self::from_yaml(&content)
    }

    pub fn from_yaml(yaml: &str) -> Result<Self, ConfigError> {
        let config: ScenarioConfig = serde_yaml::from_str(yaml)?;
        config.validate()?;
        Ok(config)
    }

    pub fn validate(&self) -> Result<(), ConfigError> {
        if self.scenario_id.is_empty() {
            return Err(ConfigError::Validation(
                "scenario_id must not be empty".to_string(),
            ));
        }
        if self.parameters.is_empty() {
            return Err(ConfigError::Validation(
                "parameters must not be empty".to_string(),
            ));
        }
        Ok(())
    }

    pub fn get_f64(&self, key: &str) -> Option<f64> {
        self.parameters.get(key).and_then(|v| v.as_f64())
    }

    pub fn get_i64(&self, key: &str) -> Option<i64> {
        self.parameters.get(key).and_then(|v| v.as_i64())
    }

    pub fn adjusted_f64(&self, key: &str, risk_tier: &str) -> Option<f64> {
        if let Some(adjustments) = self.risk_tier_adjustments.get(risk_tier) {
            if let Some(val) = adjustments.get(key).and_then(|v| v.as_f64()) {
                return Some(val);
            }
        }
        self.get_f64(key)
    }

    pub fn adjusted_i64(&self, key: &str, risk_tier: &str) -> Option<i64> {
        if let Some(adjustments) = self.risk_tier_adjustments.get(risk_tier) {
            if let Some(val) = adjustments.get(key).and_then(|v| v.as_i64()) {
                return Some(val);
            }
        }
        self.get_i64(key)
    }
}

#[cfg(test)]
#[path = "config_test.rs"]
mod tests;
