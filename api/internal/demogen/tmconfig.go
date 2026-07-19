package demogen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// scenarioConfig is demogen's own minimal reader of the TM scenario YAML
// files (content/_sample/tm_scenarios/). It exists so seeded transaction
// amounts and self-check (a)'s threshold verification both come from the
// real YAML content at generation/check time rather than a hardcoded number
// in Go source.
//
// It deliberately does not replicate api/internal/engine/native's full
// parameter-resolution/fallback logic (unexported, and overkill for this
// purpose): every scenario YAML in content/_sample/tm_scenarios/ currently
// repeats identical threshold numbers across individual/corporate_domestic/
// corporate_foreign, so reading the "individual" row represents every
// customer_type in this dataset. If a future scenario file diverges per
// customer_type, this reader would need to grow a matching lookup — self-
// check (a) calling the engine directly (see alerts.go) is the primary
// verification path precisely so a divergence like that cannot silently
// pass.
type scenarioConfig struct {
	ScenarioID      string
	EvaluationMode  string
	Severity        string
	WindowHours     int
	ThresholdByTier map[string]float64 // tier (upper) -> yen amount
	Additional      map[string]any     // conditions.additional, flattened
}

func (s scenarioConfig) additionalFloat(key string, fallback float64) float64 {
	if v, ok := s.Additional[key]; ok {
		if n, ok := toFloat(v); ok {
			return n
		}
	}
	return fallback
}

func (s scenarioConfig) additionalInt(key string, fallback int) int {
	return int(s.additionalFloat(key, float64(fallback)))
}

func (s scenarioConfig) additionalStrings(key string) []string {
	var out []string
	if v, ok := s.Additional[key].([]any); ok {
		for _, item := range v {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
	}
	return out
}

// threshold returns the seeded-generation threshold amount for tier
// ("LOW"/"MEDIUM"/"HIGH"), falling back to additionalFloat("threshold_amount", ...)
// for scenarios (like high_risk_country_transfer) that also carry a flat
// additional.threshold_amount, and finally to fallback.
func (s scenarioConfig) threshold(tier string, fallback float64) float64 {
	if v, ok := s.ThresholdByTier[strings.ToUpper(tier)]; ok {
		return v
	}
	return s.additionalFloat("threshold_amount", fallback)
}

func loadScenarioConfigs(dir string) (map[string]scenarioConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read tm scenarios dir %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := map[string]scenarioConfig{}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := yaml.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		cfg := scenarioConfig{
			ScenarioID:      stringOf(raw["scenario_id"]),
			EvaluationMode:  strings.ToLower(stringOf(raw["evaluation_mode"])),
			Severity:        strings.ToLower(stringOf(raw["severity"])),
			ThresholdByTier: map[string]float64{},
			Additional:      map[string]any{},
		}
		if conditions, ok := raw["conditions"].(map[string]any); ok {
			if agg, ok := conditions["aggregation"].(map[string]any); ok {
				if period, ok := agg["period"].(string); ok {
					cfg.WindowHours = parseHoursSuffix(period)
				}
			}
			if add, ok := conditions["additional"].(map[string]any); ok {
				for k, v := range add {
					cfg.Additional[k] = v
				}
			}
			if th, ok := conditions["threshold"].(map[string]any); ok {
				if byType, ok := th["by_customer_type"].(map[string]any); ok {
					if indiv, ok := byType["individual"].(map[string]any); ok {
						if byTier, ok := indiv["by_risk_tier"].(map[string]any); ok {
							for tier, v := range byTier {
								if n, ok := toFloat(v); ok {
									cfg.ThresholdByTier[strings.ToUpper(tier)] = n
								}
							}
						}
					}
				}
			}
		}
		if cfg.ScenarioID == "" {
			continue
		}
		out[cfg.ScenarioID] = cfg
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no TM scenarios found in %s", dir)
	}
	return out, nil
}

func parseHoursSuffix(period string) int {
	period = strings.TrimSuffix(strings.TrimSpace(period), "h")
	n, _ := strconv.Atoi(period)
	return n
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
