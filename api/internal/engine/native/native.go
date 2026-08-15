// Package native is the in-process Go implementation of Merlon's evaluation
// contract. It deliberately exposes the same narrow interfaces used by the HTTP
// layer, so evaluation remains independent from persistence and audit behavior.
package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/config"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/metrics"
	"github.com/ksuk/merlon/api/internal/screening"
	"gopkg.in/yaml.v3"
)

type riskFactor struct {
	Weight      float64            `yaml:"weight"`
	Values      map[string]float64 `yaml:"values"`
	Source      string             `yaml:"source"`
	Description string             `yaml:"description"`
	Applies     []string           `yaml:"applies_to"`
}

type cddConfig struct {
	SchemaVersion  string                `yaml:"schema_version"`
	PresetID       string                `yaml:"preset_id"`
	RiskFactors    map[string]riskFactor `yaml:"risk_factors"`
	TierThresholds map[string]threshold  `yaml:"tier_thresholds"`
}

type threshold struct {
	Min float64 `yaml:"min"`
	Max float64 `yaml:"max"`
}

type countryRisk struct {
	DefaultScore float64 `yaml:"default_score"`
	Countries    map[string]struct {
		Score float64 `yaml:"score"`
	} `yaml:"countries"`
}

type scenario struct {
	ID                string
	Name              string
	Description       string
	SchemaVersion     string
	Detector          string
	LegacyRouting     bool
	LegacyWindowAlias bool
	Mode              string
	Severity          domain.AlertSeverity
	Parameters        map[string]any
	Thresholds        map[string]map[string]float64
	TransactionTypes  []string
	Aggregation       aggregationSpec
}

type aggregationSpec struct {
	Field    string
	Function string
	Period   time.Duration
	GroupBy  string
}

type Engine struct {
	cdd                     cddConfig
	cddDigest               string
	country                 *countryRisk
	scenarios               []scenario
	tmDigest                string
	tmCompatibilityWarnings []string
	listsMu                 sync.RWMutex
	lists                   []screeningList
	screeningDigest         string
	screeningThreshold      float64
}

type screeningList struct {
	ID      string           `yaml:"list_id"`
	Type    string           `yaml:"list_type"`
	Name    string           `yaml:"name"`
	Source  string           `yaml:"source"`
	Entries []screeningEntry `yaml:"entries"`
}
type screeningEntry struct {
	ID    string   `yaml:"entry_id"`
	Names []string `yaml:"names"`
}

// NewFromEnv loads the configured content roots for the native engine.
// It returns an error for a missing required CDD/TM root; callers may keep the
// API in explicitly engine-disabled mode by choosing not to construct it.
func NewFromEnv() (*Engine, error) {
	cddPath := envOr("MERLON_CDD_WEIGHTS_PATH", "cdd_weights.yaml")
	tmPath := envOr("MERLON_TM_SCENARIOS_PATH", "tm_scenarios")
	screeningPath := envOr("MERLON_SCREENING_LISTS_PATH", "screening_lists")
	e, err := New(cddPath, tmPath, screeningPath, envOr("MERLON_COUNTRY_RISK_PATH", ""))
	if err != nil {
		return nil, err
	}
	if v := os.Getenv("MERLON_SCREENING_THRESHOLD"); v != "" {
		if n, parseErr := strconv.ParseFloat(v, 64); parseErr == nil && n >= 0 && n <= 1 {
			e.screeningThreshold = n
		}
	}
	return e, nil
}

func New(cddPath, tmPath, screeningPath, countryPath string) (*Engine, error) {
	cddYAML, err := os.ReadFile(cddPath)
	if err != nil {
		return nil, fmt.Errorf("read CDD config: %w", err)
	}
	var cdd cddConfig
	if err := yaml.Unmarshal(cddYAML, &cdd); err != nil {
		return nil, fmt.Errorf("parse CDD config: %w", err)
	}
	if err := normalizeCDD(&cdd); err != nil {
		return nil, err
	}
	if err := validateCDD(cdd); err != nil {
		return nil, err
	}
	var paths []string
	if info, statErr := os.Stat(tmPath); statErr != nil {
		return nil, fmt.Errorf("stat TM config: %w", statErr)
	} else if info.IsDir() {
		entries, readErr := os.ReadDir(tmPath)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() && isYAML(entry.Name()) {
				paths = append(paths, filepath.Join(tmPath, entry.Name()))
			}
		}
	} else {
		paths = []string{tmPath}
	}
	sort.Strings(paths)
	e := &Engine{cdd: cdd, cddDigest: digest(cddYAML), screeningThreshold: 0.85}
	for _, path := range paths {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		s, parseErr := parseScenario(content)
		if parseErr != nil {
			return nil, fmt.Errorf("parse TM config %s: %w", path, parseErr)
		}
		if !scenarioHasSupportedDetector(s) {
			return nil, fmt.Errorf("unknown scenario type: %s", s.ID)
		}
		if s.LegacyRouting {
			e.tmCompatibilityWarnings = append(e.tmCompatibilityWarnings, fmt.Sprintf("scenario %q uses deprecated ID-based detector routing; declare detector before 2027-08-15", s.ID))
		}
		if s.LegacyWindowAlias {
			e.tmCompatibilityWarnings = append(e.tmCompatibilityWarnings, fmt.Sprintf("scenario %q uses deprecated conditions.additional.window_hours; use conditions.aggregation.period before 2027-08-15", s.ID))
		}
		e.scenarios = append(e.scenarios, s)
	}
	if len(e.scenarios) == 0 {
		return nil, fmt.Errorf("at least one TM scenario must be configured")
	}
	e.tmDigest, err = config.DigestPath(tmPath)
	if err != nil {
		return nil, err
	}
	if countryPath != "" {
		if b, readErr := os.ReadFile(countryPath); readErr == nil {
			var table countryRisk
			if err := yaml.Unmarshal(b, &table); err == nil {
				if err := validateCountryRisk(table); err != nil {
					return nil, err
				}
				e.country = &table
			} else {
				return nil, fmt.Errorf("parse country risk config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("read country risk config: %w", readErr)
		}
	}
	if info, statErr := os.Stat(screeningPath); statErr == nil {
		var listPaths []string
		if info.IsDir() {
			entries, _ := os.ReadDir(screeningPath)
			for _, entry := range entries {
				if !entry.IsDir() && isYAML(entry.Name()) {
					listPaths = append(listPaths, filepath.Join(screeningPath, entry.Name()))
				}
			}
		} else {
			listPaths = []string{screeningPath}
		}
		sort.Strings(listPaths)
		for _, path := range listPaths {
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, readErr
			}
			var l screeningList
			if err := yaml.Unmarshal(b, &l); err != nil {
				return nil, fmt.Errorf("parse screening list %s: %w", path, err)
			}
			if l.ID == "" {
				return nil, fmt.Errorf("screening list %s list_id must not be empty", path)
			}
			if len(l.Entries) == 0 {
				return nil, fmt.Errorf("screening list %s entries must not be empty", path)
			}
			for _, entry := range l.Entries {
				if len(entry.Names) == 0 {
					return nil, fmt.Errorf("screening list %s entry %s names must not be empty", path, entry.ID)
				}
			}
			e.lists = append(e.lists, l)
		}
		e.screeningDigest, _ = config.DigestPath(screeningPath)
	}
	return e, nil
}

// TMContract reports the contract the native engine actually loaded. The
// active TM digest is exposed as the default reference so an operator can
// reconcile the status page with the immutable evaluation provenance.
func (e *Engine) TMContract() engine.TMContractInfo {
	info := engine.DefaultTMContractInfo()
	info.DefaultDigest = e.tmDigest
	info.CompatibilityWarnings = append([]string(nil), e.tmCompatibilityWarnings...)
	return info
}

func validateCountryRisk(table countryRisk) error {
	if !validCountryRiskScore(table.DefaultScore) {
		return fieldErrorf("default_score", "country risk default_score must be an integer between 1 and 5")
	}
	for code, row := range table.Countries {
		if !validCountryRiskScore(row.Score) {
			return fieldErrorf("countries."+code+".score", "country %q score must be an integer between 1 and 5", code)
		}
	}
	return nil
}

func validCountryRiskScore(score float64) bool {
	return score >= 1 && score <= 5 && math.Trunc(score) == score
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func validateCDD(c cddConfig) error {
	if len(c.RiskFactors) == 0 {
		return fieldErrorf("risk_factors", "CDD risk_factors must not be empty")
	}
	var total float64
	for name, factor := range c.RiskFactors {
		if factor.Weight <= 0 {
			return fieldErrorf("risk_factors."+name+".weight", "CDD risk_factor %q weight must be positive", name)
		}
		if len(factor.Values) == 0 && factor.Source == "" {
			return fieldErrorf("risk_factors."+name, "CDD risk_factor %q must have either values or source", name)
		}
		total += factor.Weight
	}
	if total < 0.99 || total > 1.01 {
		return fieldErrorf("risk_factors", "CDD risk_factor weights must sum to 1.0, got %.4f", total)
	}
	if low, ok := c.TierThresholds["LOW"]; ok {
		if medium, ok := c.TierThresholds["MEDIUM"]; ok && low.Max != 0 && medium.Min != 0 && low.Max != medium.Min {
			return fieldErrorf("tier_thresholds", "LOW.max (%.6g) must equal MEDIUM.min (%.6g)", low.Max, medium.Min)
		}
	}
	if medium, ok := c.TierThresholds["MEDIUM"]; ok {
		if high, ok := c.TierThresholds["HIGH"]; ok && medium.Max != 0 && high.Min != 0 && medium.Max != high.Min {
			return fieldErrorf("tier_thresholds", "MEDIUM.max (%.6g) must equal HIGH.min (%.6g)", medium.Max, high.Min)
		}
	}
	return nil
}

func normalizeCDD(c *cddConfig) error {
	normalized, err := normalizeRiskTierMap(c.TierThresholds, "tier_thresholds")
	if err != nil {
		return err
	}
	c.TierThresholds = normalized
	return nil
}

func normalizeRiskTierMap[T any](values map[string]T, field string) (map[string]T, error) {
	normalized := make(map[string]T, len(values))
	for key, value := range values {
		canonical := strings.ToUpper(key)
		switch canonical {
		case "LOW", "MEDIUM", "HIGH":
		default:
			return nil, fmt.Errorf("%s contains unknown risk tier %q", field, key)
		}
		if _, duplicate := normalized[canonical]; duplicate {
			return nil, fmt.Errorf("%s contains case-colliding risk tier %q", field, key)
		}
		normalized[canonical] = value
	}
	return normalized, nil
}

const (
	detectorStructuring = "structuring"
	detectorRapid       = "rapid_movement"
	detectorHFSA        = "high_frequency_small_amount"
	detectorDormant     = "dormant_account_reactivation"
	detectorHighRisk    = "high_risk_country_transfer"
)

var supportedDetectors = map[string]struct{}{
	detectorStructuring: {}, detectorRapid: {}, detectorHFSA: {},
	detectorDormant: {}, detectorHighRisk: {},
}

// legacyDetectorForID is deliberately limited to the prefixes accepted by
// the pre-v2.1 loader. New documents must declare detector explicitly; this
// compatibility map only keeps already-valid v1/v2.0 files running during the
// contract-stability window.
func legacyDetectorForID(id string) (string, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, item := range []struct {
		prefix   string
		detector string
	}{
		{"tm_structuring", detectorStructuring}, {"test_structuring", detectorStructuring},
		{"tm_rapid_movement", detectorRapid}, {"test_rapid_movement", detectorRapid},
		{"tm_high_frequency_small_amount", detectorHFSA}, {"test_high_frequency_small_amount", detectorHFSA},
		{"tm_dormant_account_reactivation", detectorDormant}, {"test_dormant_account_reactivation", detectorDormant},
		{"tm_high_risk_country_transfer", detectorHighRisk}, {"test_high_risk_country_transfer", detectorHighRisk},
	} {
		if strings.HasPrefix(id, item.prefix) {
			return item.detector, true
		}
	}
	return "", false
}

func knownScenarioID(id string) bool {
	_, ok := legacyDetectorForID(id)
	return ok
}

func supportedDetector(detector string) bool {
	_, ok := supportedDetectors[strings.ToLower(strings.TrimSpace(detector))]
	return ok
}

func scenarioHasSupportedDetector(s scenario) bool {
	if supportedDetector(s.Detector) {
		return true
	}
	_, ok := legacyDetectorForID(s.ID)
	return ok
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func parseScenario(content []byte) (scenario, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return scenario{}, err
	}
	schemaVersion := stringValue(raw["schema_version"])
	defaultMode := "both"
	if strings.HasPrefix(schemaVersion, "2") {
		defaultMode = "batch"
	}
	s := scenario{ID: stringValue(raw["scenario_id"]), Name: stringValue(raw["name"]), Description: stringValue(raw["description"]), SchemaVersion: schemaVersion, Detector: strings.ToLower(strings.TrimSpace(stringValue(raw["detector"]))), Mode: strings.ToLower(stringValue(raw["evaluation_mode"])), Parameters: map[string]any{}, Thresholds: map[string]map[string]float64{}}
	if s.Mode == "" {
		s.Mode = defaultMode
	}
	s.Severity = domain.AlertSeverity(strings.ToLower(stringValue(raw["severity"])))
	if s.Severity == "" {
		s.Severity = domain.AlertSeverityMedium
	}
	if err := validateScenarioTopLevel(raw); err != nil {
		return scenario{}, err
	}
	if scenarioType := strings.ToLower(strings.TrimSpace(stringValue(raw["type"]))); scenarioType != "" && scenarioType != "aggregation" {
		return scenario{}, fieldErrorf("type", "unsupported TM scenario type %q", scenarioType)
	} else if strings.HasPrefix(schemaVersion, "2.1") && scenarioType == "" {
		return scenario{}, fieldErrorf("type", "schema v2.1 requires type")
	}
	if strings.HasPrefix(schemaVersion, "2.1") && s.Detector == "" {
		return scenario{}, fieldErrorf("detector", "schema v2.1 requires detector")
	}
	if s.Detector != "" && !supportedDetector(s.Detector) {
		return scenario{}, fieldErrorf("detector", "unsupported detector %q", s.Detector)
	}
	if s.Detector == "" {
		if detector, ok := legacyDetectorForID(s.ID); ok {
			s.Detector, s.LegacyRouting = detector, true
		}
	}
	if p, ok := raw["parameters"].(map[string]any); ok {
		for k, v := range p {
			s.Parameters[k] = v
		}
	}
	if adj, ok := raw["risk_tier_adjustments"].(map[string]any); ok {
		normalized, err := normalizeRiskTierMap(adj, "risk_tier_adjustments")
		if err != nil {
			return scenario{}, err
		}
		for tier, values := range normalized {
			if m, ok := values.(map[string]any); ok {
				for k, v := range m {
					mm, ok := s.Parameters[k].(map[string]any)
					if !ok {
						mm = map[string]any{}
						if base := s.Parameters[k]; base != nil {
							mm[""] = base
						}
						s.Parameters[k] = mm
					}
					mm[tier] = v
				}
			}
		}
	}
	if rawConditions, exists := raw["conditions"]; exists {
		conditions, ok := rawConditions.(map[string]any)
		if !ok {
			return scenario{}, fieldErrorf("conditions", "conditions must be an object")
		}
		if err := validateScenarioConditions(conditions); err != nil {
			return scenario{}, err
		}
		if absolute, ok := conditions["absolute_threshold"]; ok {
			s.Parameters["absolute_threshold"] = absolute
		}
		if types, ok := conditions["transaction_type"]; ok {
			parsed, err := parseTransactionTypes(types)
			if err != nil {
				return scenario{}, err
			}
			s.TransactionTypes = parsed
		}
		if rawAggregation, exists := conditions["aggregation"]; exists {
			aggregation, ok := rawAggregation.(map[string]any)
			if !ok {
				return scenario{}, fieldErrorf("conditions.aggregation", "aggregation must be an object")
			}
			parsed, err := parseAggregation(aggregation)
			if err != nil {
				return scenario{}, err
			}
			s.Aggregation = parsed
			s.Parameters["window_seconds"] = float64(parsed.Period.Seconds())
		}
		if rawAdditional, exists := conditions["additional"]; exists {
			add, ok := rawAdditional.(map[string]any)
			if !ok {
				return scenario{}, fieldErrorf("conditions.additional", "additional must be an object")
			}
			if _, legacy := add["window_hours"]; legacy && !strings.HasPrefix(schemaVersion, "2.1") {
				s.LegacyWindowAlias = true
			}
			for k, v := range add {
				s.Parameters[k] = v
			}
		}
		if rawThreshold, exists := conditions["threshold"]; exists {
			th, ok := rawThreshold.(map[string]any)
			if !ok {
				return scenario{}, fieldErrorf("conditions.threshold", "threshold must be an object")
			}
			if byType, ok := th["by_customer_type"].(map[string]any); ok {
				for ct, v := range byType {
					if m, ok := v.(map[string]any); ok {
						if byTier, ok := m["by_risk_tier"].(map[string]any); ok {
							normalized, err := normalizeRiskTierMap(byTier, fmt.Sprintf("conditions.threshold.by_customer_type.%s.by_risk_tier", ct))
							if err != nil {
								return scenario{}, err
							}
							s.Thresholds[ct] = map[string]float64{}
							for tier, n := range normalized {
								s.Thresholds[ct][tier] = number(n)
							}
						}
					}
				}
			}
		}
	}
	if s.ID == "" {
		return scenario{}, fieldErrorf("scenario_id", "scenario_id is required")
	}
	if err := validateDetectorAggregation(s); err != nil {
		return scenario{}, err
	}
	return s, nil
}

func validateDetectorAggregation(s scenario) error {
	if s.Aggregation.Function == "" {
		return nil
	}
	want := map[string]string{
		detectorStructuring: "sum",
		detectorRapid:       "sum",
		detectorHFSA:        "count",
		detectorDormant:     "sum",
		detectorHighRisk:    "sum",
	}
	if expected, ok := want[s.Detector]; ok && s.Aggregation.Function != expected {
		return fieldErrorf("conditions.aggregation.function", "detector %q requires aggregation function %s", s.Detector, expected)
	}
	return nil
}

func validateScenarioTopLevel(raw map[string]any) error {
	allowed := map[string]struct{}{
		"schema_version": {}, "scenario_id": {}, "name": {}, "description": {}, "detector": {},
		"type": {}, "conditions": {}, "parameters": {}, "risk_tier_adjustments": {},
		"evaluation_mode": {}, "severity": {}, "tags": {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return fieldErrorf(key, "unknown TM scenario key %q", key)
		}
	}
	return nil
}

func validateScenarioConditions(conditions map[string]any) error {
	allowed := map[string]struct{}{"transaction_type": {}, "aggregation": {}, "threshold": {}, "absolute_threshold": {}, "additional": {}}
	for key := range conditions {
		if _, ok := allowed[key]; !ok {
			return fieldErrorf("conditions."+key, "unknown TM condition %q", key)
		}
	}
	if additional, ok := conditions["additional"].(map[string]any); ok {
		allowedAdditional := map[string]struct{}{
			"min_transactions": {}, "min_transaction_count": {}, "individual_below": {},
			"inbound_threshold": {}, "outbound_threshold": {}, "outbound_ratio_min": {},
			"window_hours": {}, "count_threshold": {}, "max_amount_per_txn": {},
			"dormant_days": {}, "reactivation_threshold": {}, "threshold_amount": {},
			"high_risk_countries": {},
		}
		for key := range additional {
			if _, ok := allowedAdditional[key]; !ok {
				return fieldErrorf("conditions.additional."+key, "unknown TM additional parameter %q", key)
			}
		}
	}
	if aggregation, ok := conditions["aggregation"].(map[string]any); ok {
		for key := range aggregation {
			switch key {
			case "field", "function", "period", "group_by":
			default:
				return fieldErrorf("conditions.aggregation."+key, "unknown aggregation key %q", key)
			}
		}
	}
	if rawThreshold, exists := conditions["threshold"]; exists {
		threshold, ok := rawThreshold.(map[string]any)
		if !ok {
			return fieldErrorf("conditions.threshold", "threshold must be an object")
		}
		for key := range threshold {
			if key != "by_customer_type" {
				return fieldErrorf("conditions.threshold."+key, "unknown threshold key %q", key)
			}
		}
		if rawByType, exists := threshold["by_customer_type"]; exists {
			byType, ok := rawByType.(map[string]any)
			if !ok {
				return fieldErrorf("conditions.threshold.by_customer_type", "by_customer_type must be an object")
			}
			for customerType, rawCustomer := range byType {
				customer, ok := rawCustomer.(map[string]any)
				if !ok {
					return fieldErrorf("conditions.threshold.by_customer_type."+customerType, "customer type thresholds must be an object")
				}
				for key := range customer {
					if key != "by_risk_tier" {
						return fieldErrorf("conditions.threshold.by_customer_type."+customerType+"."+key, "unknown customer type threshold key %q", key)
					}
				}
				rawTier, exists := customer["by_risk_tier"]
				if !exists {
					continue
				}
				byTier, ok := rawTier.(map[string]any)
				if !ok {
					return fieldErrorf("conditions.threshold.by_customer_type."+customerType+".by_risk_tier", "risk tier thresholds must be an object")
				}
				for key := range byTier {
					switch strings.ToUpper(key) {
					case "LOW", "MEDIUM", "HIGH":
					default:
						return fieldErrorf("conditions.threshold.by_customer_type."+customerType+".by_risk_tier."+key, "unknown risk tier %q", key)
					}
				}
			}
		}
	}
	return nil
}

func parseTransactionTypes(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		if strings.TrimSpace(stringValue(value)) != "" {
			return []string{strings.ToLower(strings.TrimSpace(stringValue(value)))}, nil
		}
		return nil, fieldErrorf("conditions.transaction_type", "transaction_type must be an array of strings")
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		token := strings.ToLower(strings.TrimSpace(stringValue(item)))
		if token == "" {
			return nil, fieldErrorf(fmt.Sprintf("conditions.transaction_type[%d]", i), "transaction type must not be empty")
		}
		out = append(out, token)
	}
	return out, nil
}

func parseAggregation(raw map[string]any) (aggregationSpec, error) {
	field, function, groupBy := strings.ToLower(strings.TrimSpace(stringValue(raw["field"]))), strings.ToLower(strings.TrimSpace(stringValue(raw["function"]))), strings.ToLower(strings.TrimSpace(stringValue(raw["group_by"])))
	if field != "amount" {
		return aggregationSpec{}, fieldErrorf("conditions.aggregation.field", "aggregation field must be amount")
	}
	if function != "sum" && function != "count" {
		return aggregationSpec{}, fieldErrorf("conditions.aggregation.function", "aggregation function must be sum or count")
	}
	if groupBy != "customer_id" {
		return aggregationSpec{}, fieldErrorf("conditions.aggregation.group_by", "aggregation group_by must be customer_id")
	}
	period := strings.TrimSpace(stringValue(raw["period"]))
	duration, err := time.ParseDuration(period)
	if err != nil || duration <= 0 {
		return aggregationSpec{}, fieldErrorf("conditions.aggregation.period", "aggregation period must be a positive duration")
	}
	return aggregationSpec{Field: field, Function: function, GroupBy: groupBy, Period: duration}, nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func number(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}
func (s scenario) param(name, tier string, fallback float64) float64 {
	return s.paramFor(name, "", tier, fallback)
}

func (s scenario) paramFor(name, customerType, tier string, fallback float64) float64 {
	tier = strings.ToUpper(tier)
	if thresholdParameter(name) {
		if byTier, ok := s.Thresholds[customerType]; ok {
			if n, ok := byTier[tier]; ok {
				return n
			}
		}
		var strictest *float64
		for _, byTier := range s.Thresholds {
			if n, ok := byTier[tier]; ok && (strictest == nil || n < *strictest) {
				value := n
				strictest = &value
			}
		}
		if strictest != nil {
			return *strictest
		}
	}
	if v, ok := s.Parameters[name]; ok {
		if m, ok := v.(map[string]any); ok {
			if n, ok := m[tier]; ok {
				return number(n)
			}
			if n, ok := m[""]; ok {
				return number(n)
			}
		}
		if n := number(v); n != 0 {
			return n
		}
	}
	return fallback
}

func thresholdParameter(name string) bool {
	switch name {
	case "threshold", "threshold_amount", "inbound_threshold", "outbound_threshold", "reactivation_threshold", "count_threshold":
		return true
	default:
		return false
	}
}
func (s scenario) intParam(name, tier string, fallback int64) int64 {
	return int64(s.param(name, tier, float64(fallback)))
}

func (s scenario) windowSeconds(tier string, fallback time.Duration) int64 {
	if s.Aggregation.Period > 0 {
		return int64(s.Aggregation.Period / time.Second)
	}
	if seconds := s.param("window_seconds", tier, 0); seconds > 0 {
		return int64(seconds)
	}
	return s.intParam("window_hours", tier, int64(fallback/time.Hour)) * int64(time.Hour/time.Second)
}

func (s scenario) windowLabel(tier string, fallback time.Duration) string {
	seconds := s.windowSeconds(tier, fallback)
	// Preserve the v1/v2.0 description contract byte-for-byte. New typed
	// aggregation windows still use a human-readable duration, but legacy
	// window_hours keeps its established "N hours" wording.
	if s.Aggregation.Period == 0 && s.param("window_seconds", tier, 0) == 0 {
		return fmt.Sprintf("%d hours", seconds/3600)
	}
	if seconds%3600 == 0 {
		return fmt.Sprintf("%d hours", seconds/3600)
	}
	return (time.Duration(seconds) * time.Second).String()
}

func effectiveTransactionType(t domain.Transaction) string {
	if value := strings.ToLower(strings.TrimSpace(string(t.TransactionType))); value != "" {
		return value
	}
	switch t.Direction {
	case domain.DirectionInbound:
		return "transfer_in"
	case domain.DirectionOutbound:
		return "transfer_out"
	case domain.DirectionInternal:
		return "transfer"
	default:
		return ""
	}
}

func (s scenario) filterTransactions(txns []domain.Transaction) []domain.Transaction {
	if len(s.TransactionTypes) == 0 {
		return txns
	}
	wanted := make(map[string]struct{}, len(s.TransactionTypes))
	for _, value := range s.TransactionTypes {
		wanted[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	out := make([]domain.Transaction, 0, len(txns))
	for _, txn := range txns {
		if _, ok := wanted[effectiveTransactionType(txn)]; ok {
			out = append(out, txn)
		}
	}
	return out
}

func (s scenario) absoluteThreshold(customerType, tier, detector string) float64 {
	defaultValue := 10000000.0
	if detector == detectorHFSA {
		defaultValue = 25
	}
	return s.paramFor("absolute_threshold", customerType, tier, defaultValue)
}
func (s scenario) listParam(name string) []string {
	var out []string
	if v, ok := s.Parameters[name].([]any); ok {
		for _, item := range v {
			if x, ok := item.(string); ok {
				out = append(out, strings.ToUpper(x))
			}
		}
	}
	if v, ok := s.Parameters[name].([]string); ok {
		for _, x := range v {
			out = append(out, strings.ToUpper(x))
		}
	}
	return out
}

func (e *Engine) ScoreCustomer(ctx context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error) {
	// Preserve the established native parity contract: an unversioned score
	// reports the digest-pinned configuration loaded by the process, regardless
	// of a legacy routing hint supplied by the caller.
	return e.scoreCustomer(ctx, customer, e.cdd.PresetID, e.cdd, e.cddDigest)
}

// ScoreCustomerWithRuleSet evaluates a caller-selected immutable CDD
// definition.  The definition is parsed and validated before it is used, and
// the returned evidence is pinned to its digest and selected identifier.  The
// live engine's TM/screening configuration is shared read-only; only the CDD
// weight set is replaced for this scoring operation.
func (e *Engine) ScoreCustomerWithRuleSet(ctx context.Context, customer *domain.Customer, ruleSetID string, definition []byte) (*domain.ScoreRecord, error) {
	if strings.TrimSpace(ruleSetID) == "" {
		return nil, fmt.Errorf("rule set id must not be empty")
	}
	var cdd cddConfig
	if err := yaml.Unmarshal(definition, &cdd); err != nil {
		return nil, fmt.Errorf("parse CDD rule set %q: %w", ruleSetID, err)
	}
	if err := normalizeCDD(&cdd); err != nil {
		return nil, fmt.Errorf("normalize CDD rule set %q: %w", ruleSetID, err)
	}
	if err := validateCDD(cdd); err != nil {
		return nil, fmt.Errorf("validate CDD rule set %q: %w", ruleSetID, err)
	}
	return e.scoreCustomer(ctx, customer, ruleSetID, cdd, digest(definition))
}

func (e *Engine) scoreCustomer(ctx context.Context, customer *domain.Customer, reportedRuleSetID string, cdd cddConfig, cddDigest string) (*domain.ScoreRecord, error) {
	started := time.Now()
	status := "ok"
	defer func() {
		metrics.EngineEvalDuration.WithLabelValues("ScoreCustomer", status).Observe(time.Since(started).Seconds())
	}()
	if err := ctx.Err(); err != nil {
		status = "error"
		return nil, err
	}
	if customer == nil {
		status = "error"
		return nil, fmt.Errorf("customer is required")
	}
	type pair struct {
		name string
		f    riskFactor
	}
	factors := make([]pair, 0, len(cdd.RiskFactors))
	for name, f := range cdd.RiskFactors {
		if err := ctx.Err(); err != nil {
			status = "error"
			return nil, err
		}
		applies := len(f.Applies) == 0
		for _, ct := range f.Applies {
			if ct == string(customer.CustomerType) {
				applies = true
			}
		}
		if applies {
			factors = append(factors, pair{name, f})
		}
	}
	sort.Slice(factors, func(i, j int) bool { return factors[i].name < factors[j].name })
	var weight float64
	for _, p := range factors {
		weight += p.f.Weight
	}
	if weight == 0 {
		return nil, fmt.Errorf("CDD factors have no applicable weight")
	}
	attrs := map[string]string{}
	for k, v := range customer.Attributes {
		attrs[k] = fmt.Sprint(v)
	}
	var total float64
	usedFallback := false
	resultFactors := make([]domain.Factor, 0, len(factors))
	for _, p := range factors {
		if err := ctx.Err(); err != nil {
			status = "error"
			return nil, err
		}
		value, resolved := e.resolveFactor(p.name, p.f, customer, attrs)
		if !resolved {
			value = 5
			usedFallback = true
		}
		contribution := p.f.Weight / weight * value
		total += contribution
		description := p.name + "=" + strconv.FormatFloat(value, 'g', -1, 64)
		if !resolved {
			description += " (fallback: unresolved)"
		}
		observed := factorObservedValue(p.name, customer, attrs)
		rule := p.f.Source
		if rule == "" {
			rule = "values"
		}
		businessMeaning := p.f.Description
		if businessMeaning == "" {
			businessMeaning = "CDD factor " + p.name
		}
		// Score is the factor's own normalised value (0-10); Contribution is
		// what it added to the total after weighting. They previously both held
		// the contribution, which made the two fields indistinguishable and let
		// any consumer that summed Score double-count the weighting -- including
		// the score explanation's own reconciliation check, which therefore
		// could never detect a mismatch.
		resultFactors = append(resultFactors, domain.Factor{
			Name: p.name, Axis: p.name, Score: value, Weight: p.f.Weight,
			Contribution: contribution, Description: description, ObservedValue: observed,
			BusinessMeaning: businessMeaning, Rule: rule, Fallback: !resolved,
		})
	}
	tier := domain.RiskTierHigh
	if th, ok := cdd.TierThresholds["LOW"]; ok && (th.Max == 0 || total < th.Max) {
		tier = domain.RiskTierLow
	}
	if th, ok := cdd.TierThresholds["MEDIUM"]; ok && total >= th.Min && (th.Max == 0 || total < th.Max) {
		tier = domain.RiskTierMedium
	}
	// Missing or unknown factor mappings are fail-alert conditions. Preserve
	// the numeric score and factor-level fallback evidence, but never let an
	// unresolved input produce a low/medium operational priority silently.
	if usedFallback {
		tier = domain.RiskTierHigh
	}
	if reportedRuleSetID == "" {
		reportedRuleSetID = cdd.PresetID
	}
	now := time.Now().UTC()
	rationale := ""
	if usedFallback {
		rationale = "Fail-alert fallback applied for one or more unresolved CDD factors"
	}
	return &domain.ScoreRecord{CustomerID: customer.ID, Score: total, Tier: tier, Factors: resultFactors, RuleSetID: reportedRuleSetID, RuleSetSHA256: cddDigest, Rationale: rationale, ScoredAt: now}, nil
}

// TierThresholds reports the active CDD configuration's score bands as
// [min, max) pairs. A zero max means unbounded above.
func (e *Engine) TierThresholds() map[string][2]float64 {
	if len(e.cdd.TierThresholds) == 0 {
		return nil
	}
	out := make(map[string][2]float64, len(e.cdd.TierThresholds))
	for tier, band := range e.cdd.TierThresholds {
		out[tier] = [2]float64{band.Min, band.Max}
	}
	return out
}

func factorObservedValue(name string, customer *domain.Customer, attrs map[string]string) string {
	if value, ok := attrs[name]; ok {
		return value
	}
	switch name {
	case "customer_type":
		return string(customer.CustomerType)
	case "country_code":
		return customer.CountryCode
	case "product_type":
		return strings.Join(customer.ProductTypes, ",")
	case "risk_tier":
		if customer.RiskTier != nil {
			return string(*customer.RiskTier)
		}
	}
	return ""
}
func (e *Engine) resolveFactor(name string, f riskFactor, c *domain.Customer, attrs map[string]string) (float64, bool) {
	if f.Source == "country_risk_table" && e.country != nil {
		code := c.CountryCode
		if name != "geography" {
			code = attrs[name]
		}
		if row, ok := e.country.Countries[code]; ok {
			return row.Score, true
		}
		return e.country.DefaultScore, true
	}
	key := attrs[name]
	switch name {
	case "customer_type":
		key = string(c.CustomerType)
	case "geography":
		key = c.CountryCode
	case "product_channel", "product_type":
		if len(c.ProductTypes) > 0 {
			key = c.ProductTypes[0]
		}
	}
	value, ok := f.Values[key]
	return value, ok
}

func (e *Engine) EvaluateTransactions(ctx context.Context, customerID string, tier domain.RiskTier, txns []domain.Transaction, ids []string) ([]domain.Alert, error) {
	// Legacy entry point: no request, so no configuration to pin. See the note
	// on EvaluateTransactionsBatch.
	return e.evaluate(ctx, customerID, "unspecified", tier, txns, ids, "realtime", provenanceContext{})
}

func (e *Engine) Evaluate(ctx context.Context, req engine.MonitoringRequest) ([]domain.Alert, error) {
	customerType := string(req.CustomerType)
	if customerType == "" {
		customerType = "unspecified"
	}
	mode := "realtime"
	if req.Mode == engine.EvaluationModeBatch {
		mode = "batch"
	}
	// Provenance is attached here rather than at each call site because
	// realtime, batch and recovery monitoring all reach the engine through this
	// one request; stamping it in the engine is what makes the three produce
	// identical semantics (ADR-0025).
	return e.evaluate(ctx, req.CustomerID, customerType, req.RiskTier, req.Transactions, req.ScenarioIDs, mode, provenanceContextFrom(req))
}
func (e *Engine) EvaluateTransactionsBatch(ctx context.Context, customerID string, tier domain.RiskTier, txns []domain.Transaction, ids []string) ([]domain.Alert, error) {
	// The legacy entry point carries no request, so there is nothing to pin.
	// The alert is left without provenance and reported as not_captured, which
	// is accurate: nobody told this call what configuration was effective.
	return e.evaluate(ctx, customerID, "unspecified", tier, txns, ids, "batch", provenanceContext{})
}

// RealtimeHistoryWindow returns the largest window used by any enabled
// realtime scenario. It deliberately mirrors the evaluator's parameter
// resolution so the server-side query cannot truncate data the engine needs.
func (e *Engine) RealtimeHistoryWindow() (time.Duration, bool) {
	var longest time.Duration
	for _, s := range e.scenarios {
		if !runsUnder(s.Mode, "realtime") {
			continue
		}
		detector := s.Detector
		if detector == "" {
			detector, _ = legacyDetectorForID(s.ID)
		}
		switch detector {
		case detectorStructuring, detectorRapid, detectorHFSA:
			fallback := 24 * time.Hour
			if detector == detectorRapid {
				fallback = 48 * time.Hour
			} else if detector == detectorHFSA {
				fallback = time.Hour
			}
			for _, tier := range []string{"LOW", "MEDIUM", "HIGH"} {
				window := time.Duration(s.windowSeconds(tier, fallback)) * time.Second
				if window > longest {
					longest = window
				}
			}
		case detectorDormant:
			// Dormancy detection needs the immediately preceding transaction
			// even when it is older than dormant_days, so a finite lookback
			// cannot preserve the scenario's semantics.
			return 0, false
		}
	}
	return longest, true
}

func (e *Engine) evaluate(ctx context.Context, customerID, customerType string, tier domain.RiskTier, txns []domain.Transaction, ids []string, mode string, prov provenanceContext) ([]domain.Alert, error) {
	started := time.Now()
	defer func() {
		metrics.EngineEvalDuration.WithLabelValues("EvaluateTransactions", "ok").Observe(time.Since(started).Seconds())
	}()
	customerTxns, err := customerTransactions(ctx, customerID, txns)
	if err != nil {
		return nil, err
	}
	if len(customerTxns) == 0 {
		return nil, nil
	}
	var out []domain.Alert
	for _, s := range e.scenarios {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !scenarioSelected(s, ids) || !runsUnder(s.Mode, mode) {
			continue
		}
		alerts, err := evaluateScenario(ctx, s, customerType, string(tier), customerTxns)
		if err != nil {
			return nil, err
		}
		for _, a := range alerts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			now := time.Now().UTC()
			out = append(out, domain.Alert{CustomerID: customerID, ScenarioID: s.ID, Severity: a.severity, Status: domain.AlertStatusOpen, Score: a.score, Description: a.description, TransactionIDs: a.ids, DetectedAt: now, CreatedAt: now, UpdatedAt: now, Provenance: prov.forScenario(s, customerType, string(tier), mode, e.digests())})
		}
	}
	return out, nil
}

func customerTransactions(ctx context.Context, customerID string, txns []domain.Transaction) ([]domain.Transaction, error) {
	customer := make([]domain.Transaction, 0, len(txns))
	for _, txn := range txns {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if txn.CustomerID == customerID {
			customer = append(customer, txn)
		}
	}
	sort.SliceStable(customer, func(i, j int) bool {
		if customer[i].ExecutedAt.Equal(customer[j].ExecutedAt) {
			return customer[i].ID < customer[j].ID
		}
		return customer[i].ExecutedAt.Before(customer[j].ExecutedAt)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return customer, nil
}
func scenarioSelected(s scenario, ids []string) bool {
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		if id == s.ID {
			return true
		}
	}
	return false
}
func runsUnder(configMode, mode string) bool {
	if mode == "both" || configMode == "both" || configMode == "" {
		return true
	}
	return configMode == mode
}

type scenarioAlert struct {
	severity    domain.AlertSeverity
	score       float64
	description string
	ids         []string
}

func evaluateScenario(ctx context.Context, s scenario, customerType, tier string, customer []domain.Transaction) ([]scenarioAlert, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	detector := strings.ToLower(strings.TrimSpace(s.Detector))
	if detector == "" {
		var ok bool
		detector, ok = legacyDetectorForID(s.ID)
		if !ok {
			return nil, fmt.Errorf("scenario %q has no supported detector", s.ID)
		}
	}
	if !supportedDetector(detector) {
		return nil, fmt.Errorf("scenario %q has unsupported detector %q", s.ID, detector)
	}
	s.Detector = detector
	customer = s.filterTransactions(customer)
	switch detector {
	case detectorStructuring:
		return evalStructuring(ctx, s, customerType, tier, customer)
	case detectorRapid:
		return evalRapid(ctx, s, customerType, tier, customer)
	case detectorHFSA:
		return evalHFSA(ctx, s, customerType, tier, customer)
	case detectorDormant:
		return evalDormant(ctx, s, customerType, tier, customer)
	case detectorHighRisk:
		return evalHighRisk(ctx, s, customerType, tier, customer)
	}
	return nil, fmt.Errorf("scenario %q has unsupported detector %q", s.ID, detector)
}
func seconds(t time.Time) int64 { return t.Unix() }
func evalStructuring(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	window := s.windowSeconds(tier, 24*time.Hour)
	threshold := s.paramFor("threshold_amount", customerType, tier, s.paramFor("threshold", customerType, tier, 1000000))
	min := int(s.intParam("min_transactions", tier, s.intParam("min_transaction_count", tier, 3)))
	below := s.param("individual_below", tier, 500000)
	absoluteThreshold := s.absoluteThreshold(customerType, tier, detectorStructuring)
	var q []domain.Transaction
	for _, t := range txns {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if t.Amount > 0 && t.Amount < below {
			q = append(q, t)
		}
	}
	right := 0
	total := 0.0
	for left := range q {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := seconds(q[left].ExecutedAt) + window
		for right < len(q) && seconds(q[right].ExecutedAt) <= end {
			total += q[right].Amount
			right++
		}
		if left > 0 {
			total -= q[left-1].Amount
		}
		if right-left >= min {
			breachesTier := total >= threshold
			breachesAbsolute := total >= absoluteThreshold
			if !breachesTier && !breachesAbsolute {
				continue
			}
			ids := idsOf(q[left:right])
			sev := domain.AlertSeverityMedium
			if breachesAbsolute || total >= threshold*2 {
				sev = domain.AlertSeverityHigh
			}
			description := fmt.Sprintf("%d transactions totaling %.0f within %d hours, each below %.0f", right-left, total, window/3600, below)
			if breachesAbsolute {
				description = fmt.Sprintf("%d transactions totaling %.0f within %d hours, each below %.0f (absolute_threshold safety valve, threshold=%.0f)", right-left, total, window/3600, below, absoluteThreshold)
			}
			return []scenarioAlert{{sev, total / threshold, description, ids}}, nil
		}
	}
	return nil, nil
}
func evalRapid(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	window := s.windowSeconds(tier, 48*time.Hour)
	inTh := s.paramFor("inbound_threshold", customerType, tier, s.paramFor("threshold", customerType, tier, 5000000))
	outTh := s.paramFor("outbound_threshold", customerType, tier, 5000000)
	ratioMin := s.param("outbound_ratio_min", tier, .8)
	absoluteThreshold := s.absoluteThreshold(customerType, tier, detectorRapid)
	right, left := 0, 0
	in, out := 0.0, 0.0
	for left < len(txns) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := seconds(txns[left].ExecutedAt) + window
		for right < len(txns) && seconds(txns[right].ExecutedAt) <= end {
			if txns[right].Direction == domain.DirectionInbound {
				in += txns[right].Amount
			}
			if txns[right].Direction == domain.DirectionOutbound {
				out += txns[right].Amount
			}
			right++
		}
		ratio := 0.0
		if in > 0 {
			ratio = out / in
		}
		breachesTier := in >= inTh && out >= outTh
		breachesAbsolute := in >= absoluteThreshold && out >= absoluteThreshold
		if (breachesTier || breachesAbsolute) && ratio >= ratioMin {
			sev := domain.AlertSeverityMedium
			if ratio >= .95 {
				sev = domain.AlertSeverityCritical
			} else if ratio >= .9 {
				sev = domain.AlertSeverityHigh
			}
			description := fmt.Sprintf("inbound %.0f, outbound %.0f (ratio %.2f) within %s", in, out, ratio, s.windowLabel(tier, 48*time.Hour))
			if breachesAbsolute && !breachesTier {
				description += fmt.Sprintf(" (absolute_threshold safety valve, threshold=%.0f)", absoluteThreshold)
			}
			return []scenarioAlert{{sev, ratio, description, idsOf(txns[left:right])}}, nil
		}
		if txns[left].Direction == domain.DirectionInbound {
			in -= txns[left].Amount
		}
		if txns[left].Direction == domain.DirectionOutbound {
			out -= txns[left].Amount
		}
		left++
		if right < left {
			right = left
		}
	}
	return nil, nil
}
func evalHFSA(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	window := s.windowSeconds(tier, time.Hour)
	count := int(s.paramFor("count_threshold", customerType, tier, s.paramFor("threshold", customerType, tier, 10)))
	absoluteCount := int(s.absoluteThreshold(customerType, tier, detectorHFSA))
	max := s.param("max_amount_per_txn", tier, 100000)
	var q []domain.Transaction
	for _, t := range txns {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if t.Amount > 0 && t.Amount <= max {
			q = append(q, t)
		}
	}
	right := 0
	total := 0.0
	for left := range q {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := seconds(q[left].ExecutedAt) + window
		for right < len(q) && seconds(q[right].ExecutedAt) <= end {
			total += q[right].Amount
			right++
		}
		if left > 0 {
			total -= q[left-1].Amount
		}
		n := right - left
		breachesTier := n >= count
		breachesAbsolute := n >= absoluteCount
		if breachesTier || breachesAbsolute {
			sev := domain.AlertSeverityMedium
			if breachesAbsolute || n >= count*2 {
				sev = domain.AlertSeverityHigh
			}
			description := fmt.Sprintf("%d transactions (each <= %.0f) totaling %.0f within %s", n, max, total, s.windowLabel(tier, time.Hour))
			if breachesAbsolute && !breachesTier {
				description += fmt.Sprintf(" (absolute_threshold safety valve, threshold=%d)", absoluteCount)
			}
			return []scenarioAlert{{sev, float64(n) / float64(maxInt(count, 1)), description, idsOf(q[left:right])}}, nil
		}
	}
	return nil, nil
}
func evalDormant(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	days := s.intParam("dormant_days", tier, 180)
	threshold := s.paramFor("reactivation_threshold", customerType, tier, 1000000)
	absoluteThreshold := s.absoluteThreshold(customerType, tier, detectorDormant)
	for i := 1; i < len(txns); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		gap := txns[i].ExecutedAt.Sub(txns[i-1].ExecutedAt)
		breachesTier := txns[i].Amount >= threshold
		breachesAbsolute := txns[i].Amount >= absoluteThreshold
		if gap >= time.Duration(days)*24*time.Hour && (breachesTier || breachesAbsolute) {
			sev := domain.AlertSeverityMedium
			if txns[i].Amount >= threshold*2 {
				sev = domain.AlertSeverityHigh
			}
			description := fmt.Sprintf("dormant for %d days, reactivated with %.0f (threshold %.0f)", int64(gap/(24*time.Hour)), txns[i].Amount, threshold)
			if breachesAbsolute && !breachesTier {
				description += fmt.Sprintf(" (absolute_threshold safety valve, threshold=%.0f)", absoluteThreshold)
			}
			return []scenarioAlert{{severity: sev, score: txns[i].Amount / maxFloat(threshold, 1),
				description: description,
				ids:         []string{txns[i].ID}}}, nil
		}
	}
	return nil, nil
}
func evalHighRisk(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	threshold := s.paramFor("threshold_amount", customerType, tier, 1000000)
	absoluteThreshold := s.absoluteThreshold(customerType, tier, detectorHighRisk)
	countries := s.listParam("high_risk_countries")
	var out []scenarioAlert
	for _, t := range txns {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hit := false
		for _, c := range countries {
			if strings.EqualFold(c, t.CounterpartyCountry) {
				hit = true
			}
		}
		breachesTier := t.Amount >= threshold
		breachesAbsolute := t.Amount >= absoluteThreshold
		if hit && t.Direction == domain.DirectionOutbound && (breachesTier || breachesAbsolute) {
			description := fmt.Sprintf("outbound transfer of %.0f to high-risk country %s (threshold %.0f)", t.Amount, t.CounterpartyCountry, threshold)
			if breachesAbsolute && !breachesTier {
				description += fmt.Sprintf(" (absolute_threshold safety valve, threshold=%.0f)", absoluteThreshold)
			}
			out = append(out, scenarioAlert{domain.AlertSeverityHigh, t.Amount / maxFloat(threshold, 1), description, []string{t.ID}})
		}
	}
	return out, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func idsOf(txns []domain.Transaction) []string {
	ids := make([]string, len(txns))
	for i, t := range txns {
		ids[i] = t.ID
	}
	return ids
}

func (e *Engine) ScreenCustomer(ctx context.Context, customer *domain.Customer, listIDs []string) (*domain.ScreenResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, fmt.Errorf("customer is required")
	}
	name, _ := customer.Attributes["name"].(string)
	if name == "" {
		name = customer.ExternalID
	}
	kana, _ := customer.Attributes["name_kana"].(string)
	queries := []string{name}
	if kana != "" {
		queries = append(queries, kana)
	}
	var matches []domain.ScreenMatch
	checked := 0
	e.listsMu.RLock()
	lists := append([]screeningList(nil), e.lists...)
	e.listsMu.RUnlock()
	for _, l := range lists {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(listIDs) > 0 && !slices.Contains(listIDs, l.ID) {
			continue
		}
		checked++
		for _, entry := range l.Entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			best, bestName := 0.0, ""
			for _, q := range queries {
				for _, n := range entry.Names {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					sim := similarity(q, n)
					if sim > best {
						best, bestName = sim, n
					}
				}
			}
			if best >= e.screeningThreshold {
				matches = append(matches, domain.ScreenMatch{ListID: l.ID, EntryID: entry.ID, MatchedName: bestName, Similarity: best, ListType: l.Type, Source: l.Source})
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Similarity > matches[j].Similarity })
	return &domain.ScreenResult{CustomerID: customer.ID, Hit: len(matches) > 0, Matches: matches, ListsChecked: checked, ScreenedAt: time.Now().UTC()}, nil
}

// ReplaceScreeningLists atomically swaps the last-good imported snapshot. The
// scheduler calls this only after a successful durable import; a failed fetch
// therefore cannot erase the list currently used for screening.
func (e *Engine) ReplaceScreeningLists(raw []screening.RawListData) {
	lists := make([]screeningList, 0, len(raw))
	for _, item := range raw {
		l := screeningList{ID: item.ListID, Type: item.ListType, Name: item.Name, Source: item.Source}
		for _, entry := range item.Entries {
			l.Entries = append(l.Entries, screeningEntry{ID: entry.EntryID, Names: append([]string(nil), entry.Names...)})
		}
		lists = append(lists, l)
	}
	sort.Slice(lists, func(i, j int) bool { return lists[i].ID < lists[j].ID })
	e.listsMu.Lock()
	e.lists = lists
	e.listsMu.Unlock()
}
func similarity(a, b string) float64 {
	a = normalize(a)
	b = normalize(b)
	if a == b {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 || len(br) == 0 {
		if len(ar) == 0 && len(br) == 0 {
			return 1
		}
		return 0
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ar {
		cur := make([]int, len(br)+1)
		cur[0] = i + 1
		for j, cb := range br {
			cost := 0
			if ca != cb {
				cost = 1
			}
			cur[j+1] = min(prev[j+1]+1, cur[j]+1, prev[j]+cost)
		}
		prev = cur
	}
	max := len(ar)
	if len(br) > max {
		max = len(br)
	}
	return 1 - float64(prev[len(br)])/float64(max)
}
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x21 && r <= 0x7e && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) && r != '-' {
			continue
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func (e *Engine) RunBacktest(ctx context.Context, customers []domain.Customer, txns []domain.Transaction, ids []string, _ string) (*domain.BacktestResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	started := time.Now()
	result := &domain.BacktestResult{BacktestID: fmt.Sprintf("native-%d", started.UnixNano()), TotalCustomers: len(customers), TotalTransactions: len(txns)}
	for _, c := range customers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tier := func() domain.RiskTier {
			if c.RiskTier != nil {
				return *c.RiskTier
			}
			return domain.RiskTierMedium
		}()
		customerType := string(c.CustomerType)
		if customerType == "" {
			customerType = "unspecified"
		}
		// Backtests intentionally evaluate every configured scenario,
		// irrespective of realtime/batch mode; the mode filter is a serving
		// concern, not a historical replay concern.
		// A backtest already pins its own rule snapshot and digests on the job
		// row (migrations 027/032); per-alert provenance would duplicate it.
		alerts, err := e.evaluate(ctx, c.ID, customerType, tier, txns, ids, "both", provenanceContext{})
		if err != nil {
			return nil, err
		}
		for _, a := range alerts {
			result.TotalAlerts++
			found := -1
			for i := range result.ScenarioResults {
				if result.ScenarioResults[i].ScenarioID == a.ScenarioID {
					found = i
				}
			}
			if found < 0 {
				result.ScenarioResults = append(result.ScenarioResults, domain.BacktestScenarioResult{ScenarioID: a.ScenarioID})
				found = len(result.ScenarioResults) - 1
			}
			sr := &result.ScenarioResults[found]
			sr.AlertsGenerated++
			if a.Severity == domain.AlertSeverityHigh || a.Severity == domain.AlertSeverityCritical {
				sr.HighSeverityCount++
			}
			if a.Severity == domain.AlertSeverityMedium {
				sr.MediumSeverityCount++
			}
			if a.Severity == domain.AlertSeverityLow {
				sr.LowSeverityCount++
			}
			if !slices.Contains(sr.AffectedCustomerIDs, c.ID) {
				sr.AffectedCustomerIDs = append(sr.AffectedCustomerIDs, c.ID)
			}
		}
	}
	sort.Slice(result.ScenarioResults, func(i, j int) bool {
		return result.ScenarioResults[i].ScenarioID < result.ScenarioResults[j].ScenarioID
	})
	for i := range result.ScenarioResults {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sort.Strings(result.ScenarioResults[i].AffectedCustomerIDs)
	}
	result.ExecutionTimeMs = float64(time.Since(started).Microseconds()) / 1000
	return result, nil
}

// RunBacktestWithRuleSet replays a candidate TM scenario against an isolated
// copy of the loaded engine. CDD, country-risk, screening data, and their
// digests remain pinned to the process snapshot; only the requested scenario
// definition is replaced. This keeps candidate comparisons auditable and
// prevents a rule-version probe from mutating the live evaluator.
func (e *Engine) RunBacktestWithRuleSet(ctx context.Context, customers []domain.Customer, txns []domain.Transaction, ids []string, description, ruleSetID string, definition []byte) (*domain.BacktestResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ruleSetID) == "" {
		return nil, fmt.Errorf("rule set id must not be empty")
	}
	s, err := parseScenario(definition)
	if err != nil {
		return nil, fmt.Errorf("parse rule set %q: %w", ruleSetID, err)
	}
	if !scenarioHasSupportedDetector(s) {
		return nil, fmt.Errorf("rule set %q has unknown scenario type: %s", ruleSetID, s.ID)
	}
	e.listsMu.RLock()
	lists := append([]screeningList(nil), e.lists...)
	e.listsMu.RUnlock()
	candidate := &Engine{
		cdd:                e.cdd,
		cddDigest:          e.cddDigest,
		country:            e.country,
		scenarios:          []scenario{s},
		tmDigest:           e.tmDigest,
		lists:              lists,
		screeningDigest:    e.screeningDigest,
		screeningThreshold: e.screeningThreshold,
	}
	return candidate.RunBacktest(ctx, customers, txns, ids, description)
}

func (e *Engine) ValidateConfig(ctx context.Context, typ, content string) (*engine.ConfigValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := newConfigValidationCollector(content)
	if len(content) > 512*1024 {
		c.add("yaml", engine.ConfigErrorSchema, fmt.Errorf("yaml_content too large (max 512KB)"))
		return c.result, nil
	}
	switch typ {
	case "cdd_weights":
		var cdd cddConfig
		if err := yaml.Unmarshal([]byte(content), &cdd); err != nil {
			c.addSyntax(err)
		} else if err := normalizeCDD(&cdd); err != nil {
			c.add("config", engine.ConfigErrorSchema, err)
		} else if err := validateCDD(cdd); err != nil {
			c.add("config", engine.ConfigErrorSchema, err)
		}
	case "tm_scenarios":
		// parseScenario reports both kinds of failure through one error. Ask
		// the parser first, so a document it accepted is never reported as a
		// syntax error the operator cannot find.
		var probe map[string]any
		if err := yaml.Unmarshal([]byte(content), &probe); err != nil {
			c.addSyntax(err)
			return c.result, nil
		}
		s, err := parseScenario([]byte(content))
		if err != nil {
			c.add("config", engine.ConfigErrorSchema, err)
		} else if !scenarioHasSupportedDetector(s) {
			// The document is well formed; it names a scenario this engine does
			// not implement. That is a different mistake from a malformed
			// document and has a different fix.
			c.addPathError("config", "scenario_id", engine.ConfigErrorCrossReference, fmt.Errorf("unknown scenario type: %s", s.ID))
		} else {
			var raw map[string]any
			_ = yaml.Unmarshal([]byte(content), &raw)
			isV2 := strings.HasPrefix(stringValue(raw["schema_version"]), "2") || raw["conditions"] != nil
			if !isV2 && len(s.Parameters) == 0 {
				c.addPathError("config", "parameters", engine.ConfigErrorSchema, fmt.Errorf("parameters must not be empty"))
			}
		}
	case "screening_lists":
		var list screeningList
		if err := yaml.Unmarshal([]byte(content), &list); err != nil {
			c.addSyntax(err)
		} else if err := validateScreeningList(list); err != nil {
			c.add("config", engine.ConfigErrorSchema, err)
		}
	case "country_risk":
		var table countryRisk
		if err := yaml.Unmarshal([]byte(content), &table); err != nil {
			c.addSyntax(err)
		} else if err := validateCountryRisk(table); err != nil {
			c.add("config", engine.ConfigErrorSchema, err)
		}
	default:
		// config_type is a request parameter, not a location in the document.
		// Reporting a line here would send the operator to an innocent line.
		c.add("config_type", engine.ConfigErrorSchema, fmt.Errorf("unknown config type: %s", typ))
	}
	return c.result, nil
}

func validateScreeningList(list screeningList) error {
	if list.ID == "" {
		return fieldErrorf("list_id", "list_id must not be empty")
	}
	if len(list.Entries) == 0 {
		return fieldErrorf("entries", "entries must not be empty")
	}
	for i, entry := range list.Entries {
		if len(entry.Names) == 0 {
			return fieldErrorf(fmt.Sprintf("entries[%d].names", i), "entry %q must have at least one name", entry.ID)
		}
	}
	return nil
}
