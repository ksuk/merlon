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

/// Distinguishes which on-disk TM scenario schema a `ScenarioConfig` was
/// loaded from (the rule schema §3.1). v1 is the pre-existing flat
/// parameters/risk_tier_adjustments shape; v2 is the by_customer_type ->
/// by_risk_tier nested shape (content/schema/tm_scenario_v2.json).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum ScenarioSchemaVersion {
    #[default]
    V1,
    V2,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ScenarioConfig {
    pub schema_version: String,
    pub scenario_id: String,
    pub name: String,
    pub description: String,
    #[serde(default)]
    pub parameters: HashMap<String, serde_yaml::Value>,
    #[serde(default)]
    pub risk_tier_adjustments: HashMap<String, HashMap<String, serde_yaml::Value>>,

    // Populated by load_dual/from_yaml_dual only; plain from_yaml/load leave
    // these at their defaults (v1, "both", None, empty), which matches the
    // v1 semantics those entry points have always served.
    #[serde(skip)]
    pub schema_version_kind: ScenarioSchemaVersion,
    #[serde(skip, default = "default_evaluation_mode")]
    pub evaluation_mode: String,
    #[serde(skip)]
    pub absolute_threshold: Option<f64>,
    /// customer_type -> risk_tier -> threshold. For v1 content the same
    /// map is repeated under every customer_type (the rule schema §3.1
    /// migration item 2: v1 has no customer_type axis).
    #[serde(skip)]
    pub by_customer_type: HashMap<String, HashMap<String, f64>>,
}

fn default_evaluation_mode() -> String {
    "both".to_string()
}

/// Which evaluation pass(es) a scenario runs under (the rule schema §1.2,
/// the transaction-monitoring design「評価モード」).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum EvaluationMode {
    Realtime,
    Batch,
    Both,
}

impl EvaluationMode {
    /// Unrecognized strings fall back to `Both` (Fail-Alert: never
    /// silently drop a scenario from a pass because its mode was
    /// unparseable).
    fn from_config_str(s: &str) -> Self {
        match s {
            "realtime" => EvaluationMode::Realtime,
            "batch" => EvaluationMode::Batch,
            _ => EvaluationMode::Both,
        }
    }

    /// Whether a scenario configured with `self` as its evaluation_mode
    /// should run under a pass filtering for `filter`.
    pub fn runs_under(self, filter: EvaluationMode) -> bool {
        match filter {
            EvaluationMode::Both => true,
            EvaluationMode::Realtime => self != EvaluationMode::Batch,
            EvaluationMode::Batch => self != EvaluationMode::Realtime,
        }
    }
}

const CUSTOMER_TYPES: [&str; 3] = ["individual", "corporate_domestic", "corporate_foreign"];
const RISK_TIERS: [&str; 3] = ["LOW", "MEDIUM", "HIGH"];

/// v1's risk_tier_adjustments/parameters use scenario-specific parameter
/// names for "the" monetary threshold (e.g. structuring uses
/// threshold_amount, rapid_movement uses inbound_threshold). resolve_threshold
/// needs one canonical name; "threshold" is tried first since it is what a
/// migrated scenario is expected to use going forward, falling back to the
/// legacy "threshold_amount" convention used by the shipped v1 content.
const V1_THRESHOLD_KEYS: [&str; 2] = ["threshold", "threshold_amount"];

/// System default for the absolute_threshold safety valve
/// (`tm.default_absolute_threshold`, the transaction-monitoring design「絶対閾値の
/// 安全弁」), applied whenever a scenario doesn't specify its own.
const DEFAULT_ABSOLUTE_THRESHOLD: f64 = 10_000_000.0;

#[derive(Debug, Deserialize)]
struct V2Raw {
    schema_version: String,
    scenario_id: String,
    name: String,
    #[serde(default)]
    description: String,
    #[serde(rename = "type")]
    #[expect(
        dead_code,
        reason = "kept for schema compatibility and future validation"
    )]
    scenario_type: String,
    conditions: V2Conditions,
    #[serde(default = "default_v2_evaluation_mode")]
    evaluation_mode: String,
    #[serde(default)]
    #[expect(
        dead_code,
        reason = "kept for schema compatibility and future validation"
    )]
    severity: String,
}

fn default_v2_evaluation_mode() -> String {
    "batch".to_string()
}

#[derive(Debug, Default, Deserialize)]
struct V2Conditions {
    #[serde(default)]
    threshold: Option<V2Threshold>,
    #[serde(default)]
    absolute_threshold: Option<f64>,
    /// Free-form scenario-specific parameters (content/schema/tm_scenario_v2.json
    /// `conditions.additional`) beyond the fixed threshold/absolute_threshold
    /// shape, e.g. a velocity scenario's window/count parameters or a
    /// high-risk-country list. Fed into ScenarioConfig::parameters so
    /// get_f64/get_i64/adjusted_f64/adjusted_i64/get_string_list work
    /// identically for v1 and v2 content.
    #[serde(default)]
    additional: HashMap<String, serde_yaml::Value>,
}

#[derive(Debug, Default, Deserialize)]
struct V2Threshold {
    #[serde(default)]
    by_customer_type: HashMap<String, V2CustomerTypeThreshold>,
}

#[derive(Debug, Default, Deserialize)]
struct V2CustomerTypeThreshold {
    #[serde(default)]
    by_risk_tier: HashMap<String, f64>,
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
        // v2 content carries its threshold in by_customer_type rather than
        // the flat v1 parameters map, so parameters is legitimately empty
        // for a well-formed v2 scenario (see from_v2_raw).
        if self.schema_version_kind == ScenarioSchemaVersion::V1 && self.parameters.is_empty() {
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

    /// Reads a YAML sequence-of-strings parameter (e.g. a high-risk country
    /// list), returning an empty Vec if the key is absent or not a sequence
    /// of strings (Global Constraints: no hardcoded fallback list, since an
    /// unconfigured list means the scenario has nothing to check against).
    pub fn get_string_list(&self, key: &str) -> Vec<String> {
        self.parameters
            .get(key)
            .and_then(|v| v.as_sequence())
            .map(|seq| {
                seq.iter()
                    .filter_map(|v| v.as_str().map(str::to_string))
                    .collect()
            })
            .unwrap_or_default()
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

    /// Loads a TM scenario file, auto-detecting whether it is tm_scenario_v1
    /// or tm_scenario_v2 (the rule schema §3.1). Use this (rather than `load`)
    /// wherever scenario content from `content/schema/` may be either
    /// version; `load`/`from_yaml` remain v1-only for existing callers.
    pub fn load_dual(path: &str) -> Result<Self, ConfigError> {
        let content = std::fs::read_to_string(path)?;
        Self::from_yaml_dual(&content)
    }

    pub fn from_yaml_dual(content: &str) -> Result<Self, ConfigError> {
        // v2 has required fields (type, conditions, severity) absent from
        // v1 content, so a v2 parse attempt fails closed on v1 input and we
        // fall back to the existing v1 path. This is more robust than
        // branching on the schema_version string, since shipped v1 content
        // uses "1.0" rather than the tm_scenario_v1.json schema's literal
        // "tm_scenario_v1" constant.
        if let Ok(raw) = serde_yaml::from_str::<V2Raw>(content) {
            return Self::from_v2_raw(raw);
        }

        let mut config = Self::from_yaml(content)?;
        config.apply_v1_conversion();
        Ok(config)
    }

    /// Converts v1's flat, customer-type-agnostic risk_tier_adjustments into
    /// the unified by_customer_type/by_risk_tier representation, per
    /// the rule schema §3.1 migration item 2: the same threshold applies to
    /// every customer_type since v1 has no such axis. Semantics are
    /// unchanged; only the internal representation resolve_threshold reads
    /// from is added.
    fn apply_v1_conversion(&mut self) {
        self.schema_version_kind = ScenarioSchemaVersion::V1;
        self.evaluation_mode = default_evaluation_mode();

        let threshold_key = V1_THRESHOLD_KEYS.into_iter().find(|key| {
            self.parameters.contains_key(*key)
                || self
                    .risk_tier_adjustments
                    .values()
                    .any(|tier| tier.contains_key(*key))
        });

        let mut per_tier = HashMap::new();
        if let Some(key) = threshold_key {
            for tier in RISK_TIERS {
                if let Some(value) = self.adjusted_f64(key, tier) {
                    per_tier.insert(tier.to_string(), value);
                }
            }
        }

        // v1 has no absolute_threshold concept at all (the rule schema
        // §3.1 migration item 3), so it is left unspecified here and
        // resolved to the system default by `absolute_threshold()`,
        // exactly like an omitted v2 `conditions.absolute_threshold`.
        self.absolute_threshold = None;

        self.by_customer_type = CUSTOMER_TYPES
            .into_iter()
            .map(|ct| (ct.to_string(), per_tier.clone()))
            .collect();
    }

    fn from_v2_raw(raw: V2Raw) -> Result<Self, ConfigError> {
        if raw.scenario_id.is_empty() {
            return Err(ConfigError::Validation(
                "scenario_id must not be empty".to_string(),
            ));
        }

        let conditions = raw.conditions;
        let by_customer_type = conditions
            .threshold
            .unwrap_or_default()
            .by_customer_type
            .into_iter()
            .map(|(customer_type, t)| (customer_type, t.by_risk_tier))
            .collect();

        Ok(ScenarioConfig {
            schema_version: raw.schema_version,
            scenario_id: raw.scenario_id,
            name: raw.name,
            description: raw.description,
            parameters: conditions.additional,
            risk_tier_adjustments: HashMap::new(),
            schema_version_kind: ScenarioSchemaVersion::V2,
            evaluation_mode: raw.evaluation_mode,
            absolute_threshold: conditions.absolute_threshold,
            by_customer_type,
        })
    }

    /// Resolves a threshold for a given customer_type/risk_tier regardless
    /// of whether this config was loaded from v1 or v2 content.
    ///
    /// An unrecognized customer_type falls back to the strictest (lowest)
    /// threshold configured for that risk_tier across all known
    /// customer_types, rather than skipping evaluation (Fail-Alert
    /// principle: an unmapped type must never be more lenient than a
    /// known one).
    pub fn evaluation_mode_kind(&self) -> EvaluationMode {
        EvaluationMode::from_config_str(&self.evaluation_mode)
    }

    /// Resolves the absolute_threshold safety valve (the transaction-monitoring design
    /// 「絶対閾値の安全弁」), falling back to the system default
    /// (`tm.default_absolute_threshold`, 1,000万円) when the scenario
    /// doesn't specify one (v2 `conditions.absolute_threshold` omitted, or
    /// v1 content, which has no such field at all).
    pub fn absolute_threshold(&self) -> f64 {
        self.absolute_threshold
            .unwrap_or(DEFAULT_ABSOLUTE_THRESHOLD)
    }

    pub fn resolve_threshold(&self, customer_type: &str, risk_tier: &str) -> Option<f64> {
        if let Some(value) = self
            .by_customer_type
            .get(customer_type)
            .and_then(|tiers| tiers.get(risk_tier))
        {
            return Some(*value);
        }

        self.by_customer_type
            .values()
            .filter_map(|tiers| tiers.get(risk_tier))
            .copied()
            .fold(None, |acc: Option<f64>, v| {
                Some(acc.map_or(v, |a| a.min(v)))
            })
    }
}

#[cfg(test)]
#[path = "config_test.rs"]
mod tests;

#[cfg(test)]
#[path = "config_v2_test.rs"]
mod v2_tests;
