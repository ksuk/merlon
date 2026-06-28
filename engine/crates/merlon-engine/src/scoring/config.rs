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
pub struct CddWeightConfig {
    pub schema_version: String,
    pub preset_id: String,
    pub name: String,
    pub risk_factors: HashMap<String, RiskFactorDef>,
    pub tier_thresholds: TierThresholds,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RiskFactorDef {
    pub weight: f64,
    #[serde(default)]
    pub values: Option<HashMap<String, f64>>,
    #[serde(default)]
    pub source: Option<String>,
    #[serde(default)]
    pub applies_to: Option<Vec<String>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct TierThresholds {
    #[serde(rename = "LOW")]
    pub low: ThresholdRange,
    #[serde(rename = "MEDIUM")]
    pub medium: ThresholdRange,
    #[serde(rename = "HIGH")]
    pub high: ThresholdRange,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ThresholdRange {
    #[serde(default)]
    pub min: Option<f64>,
    #[serde(default)]
    pub max: Option<f64>,
}

impl CddWeightConfig {
    pub fn load(path: &str) -> Result<Self, ConfigError> {
        let content = std::fs::read_to_string(path)?;
        Self::from_yaml(&content)
    }

    pub fn from_yaml(yaml: &str) -> Result<Self, ConfigError> {
        let config: CddWeightConfig = serde_yaml::from_str(yaml)?;
        config.validate()?;
        Ok(config)
    }

    pub fn validate(&self) -> Result<(), ConfigError> {
        if self.risk_factors.is_empty() {
            return Err(ConfigError::Validation(
                "risk_factors must not be empty".to_string(),
            ));
        }

        let total_weight: f64 = self.risk_factors.values().map(|f| f.weight).sum();
        if (total_weight - 1.0).abs() > 0.01 {
            return Err(ConfigError::Validation(format!(
                "risk_factor weights must sum to 1.0, got {total_weight:.4}"
            )));
        }

        for (name, factor) in &self.risk_factors {
            if factor.weight <= 0.0 {
                return Err(ConfigError::Validation(format!(
                    "risk_factor '{name}' weight must be positive"
                )));
            }
            if factor.values.is_none() && factor.source.is_none() {
                return Err(ConfigError::Validation(format!(
                    "risk_factor '{name}' must have either 'values' or 'source'"
                )));
            }
        }

        if let Some(max) = self.tier_thresholds.low.max
            && let Some(min) = self.tier_thresholds.medium.min
            && (max - min).abs() > f64::EPSILON * 100.0
        {
            return Err(ConfigError::Validation(format!(
                "LOW.max ({max}) must equal MEDIUM.min ({min})"
            )));
        }

        if let Some(max) = self.tier_thresholds.medium.max
            && let Some(min) = self.tier_thresholds.high.min
            && (max - min).abs() > f64::EPSILON * 100.0
        {
            return Err(ConfigError::Validation(format!(
                "MEDIUM.max ({max}) must equal HIGH.min ({min})"
            )));
        }

        Ok(())
    }
}

#[cfg(test)]
#[path = "config_test.rs"]
mod tests;
