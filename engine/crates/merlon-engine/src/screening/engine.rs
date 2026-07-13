use super::config::{ConfigError, ScreeningListConfig};
use super::matching;

#[derive(Debug, Clone)]
pub struct ScreenMatch {
    pub list_id: String,
    pub entry_id: String,
    pub matched_name: String,
    pub similarity: f64,
    pub list_type: String,
    pub source: String,
}

pub struct ScreenInput {
    pub customer_id: String,
    pub name: String,
    pub name_kana: Option<String>,
    pub country_code: Option<String>,
}

pub struct ScreenResult {
    pub customer_id: String,
    pub hit: bool,
    pub matches: Vec<ScreenMatch>,
    pub lists_checked: usize,
}

pub struct ScreeningEngine {
    lists: Vec<ScreeningListConfig>,
    threshold: f64,
}

impl ScreeningEngine {
    pub fn new(lists: Vec<ScreeningListConfig>, threshold: f64) -> Result<Self, ConfigError> {
        if lists.is_empty() {
            return Err(ConfigError::Validation(
                "at least one screening list must be configured".to_string(),
            ));
        }
        if !(0.0..=1.0).contains(&threshold) {
            return Err(ConfigError::Validation(format!(
                "threshold must be between 0.0 and 1.0, got {threshold}"
            )));
        }
        for list in &lists {
            list.validate()?;
        }
        Ok(Self { lists, threshold })
    }

    pub fn screen(&self, input: &ScreenInput, list_ids: &[String]) -> ScreenResult {
        let mut matches = Vec::new();
        let mut lists_checked = 0;

        let query_names = self.collect_query_names(input);

        for list in &self.lists {
            if !list_ids.is_empty() && !list_ids.contains(&list.list_id) {
                continue;
            }
            lists_checked += 1;

            for entry in &list.entries {
                let best = self.best_match(&query_names, &entry.names);

                if best.similarity >= self.threshold {
                    matches.push(ScreenMatch {
                        list_id: list.list_id.clone(),
                        entry_id: entry.entry_id.clone(),
                        matched_name: best.matched_name,
                        similarity: best.similarity,
                        list_type: list.list_type.clone(),
                        source: list.source.clone(),
                    });
                }
            }
        }

        matches.sort_by(|a, b| {
            b.similarity
                .partial_cmp(&a.similarity)
                .unwrap_or(std::cmp::Ordering::Equal)
        });

        ScreenResult {
            customer_id: input.customer_id.clone(),
            hit: !matches.is_empty(),
            matches,
            lists_checked,
        }
    }

    pub fn list_ids(&self) -> Vec<&str> {
        self.lists.iter().map(|l| l.list_id.as_str()).collect()
    }

    fn collect_query_names(&self, input: &ScreenInput) -> Vec<String> {
        let mut names = vec![input.name.clone()];
        if let Some(kana) = &input.name_kana {
            if !kana.is_empty() {
                names.push(kana.clone());
            }
        }
        names
    }

    fn best_match(&self, query_names: &[String], entry_names: &[String]) -> BestMatch {
        let mut best = BestMatch {
            similarity: 0.0,
            matched_name: String::new(),
        };

        for q in query_names {
            for e in entry_names {
                let sim = matching::similarity(q, e);
                if sim > best.similarity {
                    best.similarity = sim;
                    best.matched_name = e.clone();
                }
            }
        }

        best
    }
}

struct BestMatch {
    similarity: f64,
    matched_name: String,
}

#[cfg(test)]
#[path = "engine_test.rs"]
mod tests;
