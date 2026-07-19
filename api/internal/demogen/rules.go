package demogen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"gopkg.in/yaml.v3"
)

// yamlFileToJSON reads a YAML file and re-encodes it as JSON, mirroring how
// the real rule-import path (api/internal/server/rules.go) turns an
// uploaded YAML rule document into a RuleDefinition.Definition
// (json.RawMessage): yaml.Unmarshal into a generic value, then json.Marshal
// that value.
func yamlFileToJSON(path string) (json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out, err := json.Marshal(normalizeYAMLValue(v))
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", path, err)
	}
	return out, nil
}

// normalizeYAMLValue converts the map[string]interface{}/map[interface{}]
// interface{} shapes yaml.v3 produces into the map[string]any/[]any shapes
// encoding/json requires, recursively.
func normalizeYAMLValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = normalizeYAMLValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeYAMLValue(vv)
		}
		return out
	default:
		return val
	}
}

// buildRuleDefinitions materializes A9's rule_definitions: the 5 TM
// scenarios, the funds_transfer CDD weight preset, and the country risk
// table, all recorded as active/registered (A9: "rule_definitions(TM5本+
// cdd_basic_weights+country_risk registered状態）" — this dataset uses
// funds_transfer.yaml, the preset actually wired into docker-compose.demo.yml,
// rather than the legacy-schema cdd_basic_weights.yaml the original A9 text
// names).
func buildRuleDefinitions(anchor time.Time, tmScenariosDir, cddWeightsPath, countryRiskPath string) ([]domain.RuleDefinition, error) {
	var out []domain.RuleDefinition

	entries, err := os.ReadDir(tmScenariosDir)
	if err != nil {
		return nil, fmt.Errorf("read tm scenarios dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	createdAt := anchor.AddDate(0, -6, 0) // rules registered well before the demo dataset's transaction history
	for i, name := range names {
		def, err := yamlFileToJSON(filepath.Join(tmScenariosDir, name))
		if err != nil {
			return nil, err
		}
		ruleName := strings.TrimSuffix(name, filepath.Ext(name))
		out = append(out, domain.RuleDefinition{
			ID:          fmt.Sprintf("demo-rule-tm-%02d", i+1),
			Type:        domain.RuleTypeTMScenario,
			Name:        ruleName,
			Description: "Sample TM scenario (content/_sample/tm_scenarios/" + name + ")",
			Definition:  def,
			Version:     1,
			IsActive:    true,
			CreatedBy:   "m.sato",
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		})
	}

	cddDef, err := yamlFileToJSON(cddWeightsPath)
	if err != nil {
		return nil, err
	}
	out = append(out, domain.RuleDefinition{
		ID:          "demo-rule-cdd-funds-transfer",
		Type:        domain.RuleTypeCDDWeight,
		Name:        "funds_transfer",
		Description: "Funds transfer service provider CDD weight preset (content/_sample/cdd_weights/funds_transfer.yaml)",
		Definition:  cddDef,
		Version:     1,
		IsActive:    true,
		CreatedBy:   "m.sato",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	})

	countryDef, err := yamlFileToJSON(countryRiskPath)
	if err != nil {
		return nil, err
	}
	out = append(out, domain.RuleDefinition{
		ID:          "demo-rule-country-risk",
		Type:        domain.RuleTypeCountryRisk,
		Name:        "country_risk_sample",
		Description: "Sample country risk table (content/_sample/country_risk_sample.yaml)",
		Definition:  countryDef,
		Version:     1,
		IsActive:    true,
		CreatedBy:   "m.sato",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	})

	return out, nil
}
