use serde::Deserialize;
use std::collections::HashMap;

use super::config::ConfigError;

/// Independent country risk table content (the rule schema §3.5,
/// content/schema/country_risk_v1.json), referenced by a CDD risk factor
/// whose `source` is `"country_risk_table"` instead of an inline `values` map.
#[derive(Debug, Clone, Deserialize)]
pub struct CountryRiskTable {
    pub schema_version: String,
    pub content_type: String,
    pub effective_date: String,
    pub default_score: u8,
    pub countries: HashMap<String, CountryRiskEntry>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CountryRiskEntry {
    pub score: u8,
    #[serde(default)]
    pub reason: Option<String>,
}

impl CountryRiskTable {
    pub fn from_yaml(yaml: &str) -> Result<Self, ConfigError> {
        let table: CountryRiskTable = serde_yaml::from_str(yaml)?;
        table.validate()?;
        Ok(table)
    }

    /// Score for a country code, falling back to `default_score` for
    /// countries absent from the table (the rule schema §3.5 "未定義国の扱い").
    pub fn score_for(&self, country_code: &str) -> u8 {
        self.countries
            .get(country_code)
            .map(|e| e.score)
            .unwrap_or(self.default_score)
    }

    /// `default_score` must never be 1 (low risk): an absent/misconfigured
    /// entry must not silently read as low risk (Secure and Conservative by
    /// Default, the rule schema §3.5).
    fn validate(&self) -> Result<(), ConfigError> {
        if self.default_score == 1 {
            return Err(ConfigError::Validation(
                "default_score must not be 1 (low risk) — undefined countries must not default to low risk".to_string(),
            ));
        }
        if !(1..=5).contains(&self.default_score) {
            return Err(ConfigError::Validation(format!(
                "default_score must be between 1 and 5, got {}",
                self.default_score
            )));
        }
        for (code, entry) in &self.countries {
            if !(1..=5).contains(&entry.score) {
                return Err(ConfigError::Validation(format!(
                    "country '{code}' score must be between 1 and 5, got {}",
                    entry.score
                )));
            }
        }
        Ok(())
    }
}

#[cfg(test)]
#[path = "country_risk_test.rs"]
mod tests;
