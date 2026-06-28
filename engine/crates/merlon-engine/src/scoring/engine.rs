use std::collections::HashMap;

use super::config::{CddWeightConfig, ConfigError};

#[derive(Debug, Clone, PartialEq)]
pub enum RiskTier {
    Low,
    Medium,
    High,
}

impl RiskTier {
    pub fn as_str(&self) -> &'static str {
        match self {
            RiskTier::Low => "LOW",
            RiskTier::Medium => "MEDIUM",
            RiskTier::High => "HIGH",
        }
    }
}

pub struct ScoringInput {
    pub customer_id: String,
    pub customer_type: String,
    pub country_code: String,
    pub product_types: Vec<String>,
    pub attributes: HashMap<String, String>,
}

pub struct ContributingFactor {
    pub name: String,
    pub axis: String,
    pub raw_value: f64,
    pub weight: f64,
    pub effective_weight: f64,
    pub contribution: f64,
    pub description: String,
}

pub struct ScoringResult {
    pub customer_id: String,
    pub score: f64,
    pub tier: RiskTier,
    pub factors: Vec<ContributingFactor>,
    pub rule_set_id: String,
    pub rule_set_version: i32,
}

const FALLBACK_MAX_VALUE: f64 = 5.0;

pub struct CddScoringEngine {
    config: CddWeightConfig,
}

impl CddScoringEngine {
    pub fn new(config: CddWeightConfig) -> Result<Self, ConfigError> {
        config.validate()?;
        Ok(Self { config })
    }

    pub fn evaluate(&self, input: &ScoringInput) -> ScoringResult {
        let applicable: Vec<(&String, &super::config::RiskFactorDef)> = self
            .config
            .risk_factors
            .iter()
            .filter(|(_, def)| {
                def.applies_to
                    .as_ref()
                    .is_none_or(|types| types.contains(&input.customer_type))
            })
            .collect();

        let total_applicable_weight: f64 = applicable.iter().map(|(_, def)| def.weight).sum();

        let mut factors = Vec::new();
        let mut score = 0.0;

        for (name, def) in &applicable {
            let effective_weight = if total_applicable_weight > 0.0 {
                def.weight / total_applicable_weight
            } else {
                0.0
            };

            let raw_value = self
                .resolve_factor_value(name, def, input)
                .unwrap_or(FALLBACK_MAX_VALUE);

            let contribution = effective_weight * raw_value;
            score += contribution;

            let description = if self.resolve_factor_value(name, def, input).is_some() {
                format!("{name}={raw_value}")
            } else {
                format!("{name}={raw_value} (fallback: unresolved)")
            };

            factors.push(ContributingFactor {
                name: (*name).clone(),
                axis: (*name).clone(),
                raw_value,
                weight: def.weight,
                effective_weight,
                contribution,
                description,
            });
        }

        let tier = self.classify_tier(score);

        ScoringResult {
            customer_id: input.customer_id.clone(),
            score,
            tier,
            factors,
            rule_set_id: self.config.preset_id.clone(),
            rule_set_version: 1,
        }
    }

    fn resolve_factor_value(
        &self,
        factor_name: &str,
        def: &super::config::RiskFactorDef,
        input: &ScoringInput,
    ) -> Option<f64> {
        let values = def.values.as_ref()?;

        let key = match factor_name {
            "customer_type" => Some(input.customer_type.as_str()),
            "geography" => Some(input.country_code.as_str()),
            "product_channel" => input.product_types.first().map(|s| s.as_str()),
            _ => input.attributes.get(factor_name).map(|s| s.as_str()),
        };

        let key = key?;
        values.get(key).copied()
    }

    fn classify_tier(&self, score: f64) -> RiskTier {
        let th = &self.config.tier_thresholds;

        let in_low = th.low.min.is_none_or(|min| score >= min)
            && th.low.max.is_none_or(|max| score < max);
        if in_low {
            return RiskTier::Low;
        }

        let in_medium = th.medium.min.is_none_or(|min| score >= min)
            && th.medium.max.is_none_or(|max| score < max);
        if in_medium {
            return RiskTier::Medium;
        }

        RiskTier::High
    }
}

#[cfg(test)]
#[path = "engine_test.rs"]
mod tests;
