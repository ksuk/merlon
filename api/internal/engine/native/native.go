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
	Weight  float64            `yaml:"weight"`
	Values  map[string]float64 `yaml:"values"`
	Source  string             `yaml:"source"`
	Applies []string           `yaml:"applies_to"`
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
	ID          string
	Name        string
	Description string
	Mode        string
	Severity    domain.AlertSeverity
	Parameters  map[string]any
	Thresholds  map[string]map[string]float64
}

type Engine struct {
	cdd                cddConfig
	cddDigest          string
	country            *countryRisk
	scenarios          []scenario
	tmDigest           string
	listsMu            sync.RWMutex
	lists              []screeningList
	screeningDigest    string
	screeningThreshold float64
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
		if !knownScenarioID(s.ID) {
			return nil, fmt.Errorf("unknown scenario type: %s", s.ID)
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

func validateCountryRisk(table countryRisk) error {
	if !validCountryRiskScore(table.DefaultScore) {
		return fmt.Errorf("country risk default_score must be an integer between 1 and 5")
	}
	for code, row := range table.Countries {
		if !validCountryRiskScore(row.Score) {
			return fmt.Errorf("country %q score must be an integer between 1 and 5", code)
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
		return fmt.Errorf("CDD risk_factors must not be empty")
	}
	var total float64
	for name, factor := range c.RiskFactors {
		if factor.Weight <= 0 {
			return fmt.Errorf("CDD risk_factor %q weight must be positive", name)
		}
		if len(factor.Values) == 0 && factor.Source == "" {
			return fmt.Errorf("CDD risk_factor %q must have either values or source", name)
		}
		total += factor.Weight
	}
	if total < 0.99 || total > 1.01 {
		return fmt.Errorf("CDD risk_factor weights must sum to 1.0, got %.4f", total)
	}
	if low, ok := c.TierThresholds["LOW"]; ok {
		if medium, ok := c.TierThresholds["MEDIUM"]; ok && low.Max != 0 && medium.Min != 0 && low.Max != medium.Min {
			return fmt.Errorf("LOW.max (%.6g) must equal MEDIUM.min (%.6g)", low.Max, medium.Min)
		}
	}
	if medium, ok := c.TierThresholds["MEDIUM"]; ok {
		if high, ok := c.TierThresholds["HIGH"]; ok && medium.Max != 0 && high.Min != 0 && medium.Max != high.Min {
			return fmt.Errorf("MEDIUM.max (%.6g) must equal HIGH.min (%.6g)", medium.Max, high.Min)
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

func knownScenarioID(id string) bool {
	id = strings.ToLower(id)
	for _, prefix := range []string{
		"tm_structuring", "test_structuring", "tm_rapid_movement", "test_rapid_movement",
		"tm_high_frequency_small_amount", "test_high_frequency_small_amount",
		"tm_dormant_account_reactivation", "test_dormant_account_reactivation",
		"tm_high_risk_country_transfer", "test_high_risk_country_transfer",
	} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
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
	s := scenario{ID: stringValue(raw["scenario_id"]), Name: stringValue(raw["name"]), Description: stringValue(raw["description"]), Mode: strings.ToLower(stringValue(raw["evaluation_mode"])), Parameters: map[string]any{}, Thresholds: map[string]map[string]float64{}}
	if s.Mode == "" {
		s.Mode = defaultMode
	}
	s.Severity = domain.AlertSeverity(strings.ToLower(stringValue(raw["severity"])))
	if s.Severity == "" {
		s.Severity = domain.AlertSeverityMedium
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
	if conditions, ok := raw["conditions"].(map[string]any); ok {
		if absolute, ok := conditions["absolute_threshold"]; ok {
			s.Parameters["absolute_threshold"] = absolute
		}
		if add, ok := conditions["additional"].(map[string]any); ok {
			for k, v := range add {
				s.Parameters[k] = v
			}
		}
		if th, ok := conditions["threshold"].(map[string]any); ok {
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
		return scenario{}, fmt.Errorf("scenario_id is required")
	}
	return s, nil
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
	factors := make([]pair, 0, len(e.cdd.RiskFactors))
	for name, f := range e.cdd.RiskFactors {
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
	resultFactors := make([]domain.Factor, 0, len(factors))
	for _, p := range factors {
		if err := ctx.Err(); err != nil {
			status = "error"
			return nil, err
		}
		value, resolved := e.resolveFactor(p.name, p.f, customer, attrs)
		if !resolved {
			value = 5
		}
		contribution := p.f.Weight / weight * value
		total += contribution
		description := p.name + "=" + strconv.FormatFloat(value, 'g', -1, 64)
		if !resolved {
			description += " (fallback: unresolved)"
		}
		resultFactors = append(resultFactors, domain.Factor{Name: p.name, Axis: p.name, Score: contribution, Description: description})
	}
	tier := domain.RiskTierHigh
	if th, ok := e.cdd.TierThresholds["LOW"]; ok && (th.Max == 0 || total < th.Max) {
		tier = domain.RiskTierLow
	}
	if th, ok := e.cdd.TierThresholds["MEDIUM"]; ok && total >= th.Min && (th.Max == 0 || total < th.Max) {
		tier = domain.RiskTierMedium
	}
	// The scoring contract treats the request's rule_set_id as a routing
	// hint and returns the loaded preset identifier. Keep the native path
	// byte-compatible until versioned rule-set loading is introduced.
	ruleSetID = e.cdd.PresetID
	now := time.Now().UTC()
	return &domain.ScoreRecord{CustomerID: customer.ID, Score: total, Tier: tier, Factors: resultFactors, RuleSetID: ruleSetID, RuleSetSHA256: e.cddDigest, RuleSetVersion: fingerprint(e.cddDigest), ScoredAt: now}, nil
}
func fingerprint(s string) int {
	if len(s) < 8 {
		return 1
	}
	n, _ := strconv.ParseUint(s[:8], 16, 32)
	n &= 0x7fffffff
	if n == 0 {
		n = 1
	}
	return int(n)
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
	return e.evaluate(ctx, customerID, "unspecified", tier, txns, ids, "realtime")
}

func (e *Engine) Evaluate(ctx context.Context, req engine.MonitoringRequest) ([]domain.Alert, error) {
	customerType := string(req.CustomerType)
	if customerType == "" {
		customerType = "unspecified"
	}
	if req.Mode == engine.EvaluationModeBatch {
		return e.evaluate(ctx, req.CustomerID, customerType, req.RiskTier, req.Transactions, req.ScenarioIDs, "batch")
	}
	return e.evaluate(ctx, req.CustomerID, customerType, req.RiskTier, req.Transactions, req.ScenarioIDs, "realtime")
}
func (e *Engine) EvaluateTransactionsBatch(ctx context.Context, customerID string, tier domain.RiskTier, txns []domain.Transaction, ids []string) ([]domain.Alert, error) {
	return e.evaluate(ctx, customerID, "unspecified", tier, txns, ids, "batch")
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
		var parameter string
		var fallback int64
		switch {
		case strings.Contains(strings.ToLower(s.ID), "structuring"):
			parameter, fallback = "window_hours", 24
		case strings.Contains(strings.ToLower(s.ID), "rapid_movement"):
			parameter, fallback = "window_hours", 48
		case strings.Contains(strings.ToLower(s.ID), "high_frequency_small_amount"):
			parameter, fallback = "window_hours", 1
		case strings.Contains(strings.ToLower(s.ID), "dormant_account_reactivation"):
			// Dormancy detection needs the immediately preceding transaction
			// even when it is older than dormant_days, so a finite lookback
			// cannot preserve the scenario's semantics.
			return 0, false
		default:
			continue
		}
		unit := time.Hour
		if parameter == "dormant_days" {
			unit = 24 * time.Hour
		}
		for _, tier := range []string{"LOW", "MEDIUM", "HIGH"} {
			window := time.Duration(s.intParam(parameter, tier, fallback)) * unit
			if window > longest {
				longest = window
			}
		}
	}
	return longest, true
}

func (e *Engine) evaluate(ctx context.Context, customerID, customerType string, tier domain.RiskTier, txns []domain.Transaction, ids []string, mode string) ([]domain.Alert, error) {
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
			out = append(out, domain.Alert{CustomerID: customerID, ScenarioID: s.ID, Severity: a.severity, Status: domain.AlertStatusOpen, Score: a.score, Description: a.description, TransactionIDs: a.ids, DetectedAt: now, CreatedAt: now, UpdatedAt: now})
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
	id := strings.ToLower(s.ID)
	switch {
	case strings.Contains(id, "structuring"):
		return evalStructuring(ctx, s, customerType, tier, customer)
	case strings.Contains(id, "rapid_movement"):
		return evalRapid(ctx, s, customerType, tier, customer)
	case strings.Contains(id, "high_frequency_small_amount"):
		return evalHFSA(ctx, s, customerType, tier, customer)
	case strings.Contains(id, "dormant_account_reactivation"):
		return evalDormant(ctx, s, customerType, tier, customer)
	case strings.Contains(id, "high_risk_country_transfer"):
		return evalHighRisk(ctx, s, customerType, tier, customer)
	}
	return nil, nil
}
func seconds(t time.Time) int64 { return t.Unix() }
func evalStructuring(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	window := s.intParam("window_hours", tier, 24) * 3600
	threshold := s.paramFor("threshold_amount", customerType, tier, s.paramFor("threshold", customerType, tier, 1000000))
	min := int(s.intParam("min_transactions", tier, s.intParam("min_transaction_count", tier, 3)))
	below := s.param("individual_below", tier, 500000)
	absoluteThreshold := s.paramFor("absolute_threshold", customerType, tier, 10000000)
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
	window := s.intParam("window_hours", tier, 48) * 3600
	inTh := s.paramFor("inbound_threshold", customerType, tier, s.paramFor("threshold", customerType, tier, 5000000))
	outTh := s.paramFor("outbound_threshold", customerType, tier, 5000000)
	ratioMin := s.param("outbound_ratio_min", tier, .8)
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
		if in >= inTh && out >= outTh && ratio >= ratioMin {
			sev := domain.AlertSeverityMedium
			if ratio >= .95 {
				sev = domain.AlertSeverityCritical
			} else if ratio >= .9 {
				sev = domain.AlertSeverityHigh
			}
			return []scenarioAlert{{sev, ratio, fmt.Sprintf("inbound %.0f, outbound %.0f (ratio %.2f) within %d hours", in, out, ratio, window/3600), idsOf(txns[left:right])}}, nil
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
	window := s.intParam("window_hours", tier, 1) * 3600
	count := int(s.paramFor("count_threshold", customerType, tier, s.paramFor("threshold", customerType, tier, 10)))
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
		if right-left >= count {
			n := right - left
			sev := domain.AlertSeverityMedium
			if n >= count*2 {
				sev = domain.AlertSeverityHigh
			}
			return []scenarioAlert{{sev, float64(n) / float64(count), fmt.Sprintf("%d transactions (each <= %.0f) totaling %.0f within %d hours", n, max, total, window/3600), idsOf(q[left:right])}}, nil
		}
	}
	return nil, nil
}
func evalDormant(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	days := s.intParam("dormant_days", tier, 180)
	threshold := s.paramFor("reactivation_threshold", customerType, tier, 1000000)
	for i := 1; i < len(txns); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		gap := txns[i].ExecutedAt.Sub(txns[i-1].ExecutedAt)
		if gap >= time.Duration(days)*24*time.Hour && txns[i].Amount >= threshold {
			sev := domain.AlertSeverityMedium
			if txns[i].Amount >= threshold*2 {
				sev = domain.AlertSeverityHigh
			}
			return []scenarioAlert{{severity: sev, score: txns[i].Amount / threshold,
				description: fmt.Sprintf("dormant for %d days, reactivated with %.0f (threshold %.0f)", int64(gap/(24*time.Hour)), txns[i].Amount, threshold),
				ids:         []string{txns[i].ID}}}, nil
		}
	}
	return nil, nil
}
func evalHighRisk(ctx context.Context, s scenario, customerType, tier string, txns []domain.Transaction) ([]scenarioAlert, error) {
	threshold := s.paramFor("threshold_amount", customerType, tier, 1000000)
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
		if hit && t.Direction == domain.DirectionOutbound && t.Amount >= threshold {
			out = append(out, scenarioAlert{domain.AlertSeverityHigh, t.Amount / threshold, fmt.Sprintf("outbound transfer of %.0f to high-risk country %s (threshold %.0f)", t.Amount, t.CounterpartyCountry, threshold), []string{t.ID}})
		}
	}
	return out, nil
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
		alerts, err := e.evaluate(ctx, c.ID, customerType, tier, txns, ids, "both")
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
	if !knownScenarioID(s.ID) {
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
	result := &engine.ConfigValidationResult{Valid: true}
	addError := func(field string, err error) {
		result.Valid = false
		result.Errors = append(result.Errors, engine.ConfigValidationError{Field: field, Message: err.Error()})
	}
	if len(content) > 512*1024 {
		addError("yaml", fmt.Errorf("yaml_content too large (max 512KB)"))
		return result, nil
	}
	switch typ {
	case "cdd_weights":
		var cdd cddConfig
		if err := yaml.Unmarshal([]byte(content), &cdd); err != nil {
			addError("yaml", fmt.Errorf("parse error: %w", err))
		} else if err := normalizeCDD(&cdd); err != nil {
			addError("config", err)
		} else if err := validateCDD(cdd); err != nil {
			addError("config", err)
		}
	case "tm_scenarios":
		s, err := parseScenario([]byte(content))
		if err != nil {
			addError("yaml", fmt.Errorf("parse error: %w", err))
		} else if !knownScenarioID(s.ID) {
			addError("config", fmt.Errorf("unknown scenario type: %s", s.ID))
		} else {
			var raw map[string]any
			_ = yaml.Unmarshal([]byte(content), &raw)
			isV2 := strings.HasPrefix(stringValue(raw["schema_version"]), "2") || raw["conditions"] != nil
			if !isV2 && len(s.Parameters) == 0 {
				addError("config", fmt.Errorf("parameters must not be empty"))
			}
		}
	case "screening_lists":
		var list screeningList
		if err := yaml.Unmarshal([]byte(content), &list); err != nil {
			addError("yaml", fmt.Errorf("parse error: %w", err))
		} else if err := validateScreeningList(list); err != nil {
			addError("config", err)
		}
	case "country_risk":
		var table countryRisk
		if err := yaml.Unmarshal([]byte(content), &table); err != nil {
			addError("yaml", fmt.Errorf("parse error: %w", err))
		} else if err := validateCountryRisk(table); err != nil {
			addError("config", err)
		}
	default:
		addError("config_type", fmt.Errorf("unknown config type: %s", typ))
	}
	return result, nil
}

func validateScreeningList(list screeningList) error {
	if list.ID == "" {
		return fmt.Errorf("list_id must not be empty")
	}
	if len(list.Entries) == 0 {
		return fmt.Errorf("entries must not be empty")
	}
	for _, entry := range list.Entries {
		if len(entry.Names) == 0 {
			return fmt.Errorf("entry %q must have at least one name", entry.ID)
		}
	}
	return nil
}
