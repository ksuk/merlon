package notify

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/ksuk/merlon/api/internal/domain"
)

// RoutingRule maps an alert severity (or a specific scenario, which takes
// precedence) to the notification channels it should reach (NOTIF-003,
// notifications.md §3: "例：CRITICALはメール＋Webhookの両方、LOWはWebhookのみ").
// Routing rules are content (Configuration as the Product), loaded from YAML.
type RoutingRule struct {
	Severity   domain.AlertSeverity `yaml:"severity"`
	ScenarioID string               `yaml:"scenario_id,omitempty"`
	Channels   []string             `yaml:"channels"`
}

type routingConfig struct {
	Rules []RoutingRule `yaml:"rules"`
}

// LoadRoutingRules reads routing rules from a YAML file shaped like:
//
//	rules:
//	  - severity: critical
//	    channels: [email, webhook]
//	  - severity: low
//	    channels: [webhook]
//	  - scenario_id: structuring_basic
//	    channels: [email, webhook]
func LoadRoutingRules(path string) ([]RoutingRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg routingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Rules, nil
}

// DefaultRoutingRules returns the built-in severity routing used when no
// NotifyRoutingPath is configured (notifications.md §3 example: "CRITICALは
// メール＋Webhookの両方、LOWはWebhookのみ").
func DefaultRoutingRules() []RoutingRule {
	return []RoutingRule{
		{Severity: domain.AlertSeverityCritical, Channels: []string{"email", "webhook"}},
		{Severity: domain.AlertSeverityHigh, Channels: []string{"email", "webhook"}},
		{Severity: domain.AlertSeverityMedium, Channels: []string{"webhook"}},
		{Severity: domain.AlertSeverityLow, Channels: []string{"webhook"}},
	}
}

// ResolveRoute returns the notification channels for a given alert severity
// and scenario ID. A rule naming ScenarioID takes precedence over a
// severity-only default rule, since a scenario is more specific
// (notifications.md §3).
func ResolveRoute(rules []RoutingRule, severity domain.AlertSeverity, scenarioID string) []string {
	var fallback []string
	for _, r := range rules {
		if r.ScenarioID != "" {
			if r.ScenarioID == scenarioID {
				return r.Channels
			}
			continue
		}
		if r.Severity == severity {
			fallback = r.Channels
		}
	}
	return fallback
}
