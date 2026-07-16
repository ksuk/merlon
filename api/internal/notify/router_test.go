package notify

import (
	"reflect"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func defaultTestRules() []RoutingRule {
	return DefaultRoutingRules()
}

// TestDefaultRoutingRules_MatchesSpecExample verifies the built-in fallback
// rules (used when NotifyRoutingPath is unset) match notifications.md §3's
// example routing exactly.
func TestDefaultRoutingRules_MatchesSpecExample(t *testing.T) {
	rules := DefaultRoutingRules()

	got := ResolveRoute(rules, domain.AlertSeverityCritical, "")
	want := []string{"email", "webhook"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveRoute(critical) = %v, want %v", got, want)
	}

	got = ResolveRoute(rules, domain.AlertSeverityLow, "")
	want = []string{"webhook"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveRoute(low) = %v, want %v", got, want)
	}
}

// TestResolveRoute_CriticalGoesToEmailAndWebhook verifies notifications.md
// §3's example: "CRITICALはメール＋Webhookの両方".
func TestResolveRoute_CriticalGoesToEmailAndWebhook(t *testing.T) {
	got := ResolveRoute(defaultTestRules(), domain.AlertSeverityCritical, "")
	want := []string{"email", "webhook"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveRoute(critical) = %v, want %v", got, want)
	}
}

// TestResolveRoute_LowGoesToWebhookOnly verifies notifications.md §3's
// example: "LOWはWebhookのみ".
func TestResolveRoute_LowGoesToWebhookOnly(t *testing.T) {
	got := ResolveRoute(defaultTestRules(), domain.AlertSeverityLow, "")
	want := []string{"webhook"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveRoute(low) = %v, want %v", got, want)
	}
}

// TestResolveRoute_ScenarioSpecificOverride verifies a scenario-specific rule
// takes precedence over the severity default (notifications.md §3: routing
// by "アラートの重要度（severity）やシナリオID別").
func TestResolveRoute_ScenarioSpecificOverride(t *testing.T) {
	rules := append(defaultTestRules(), RoutingRule{
		ScenarioID: "structuring_basic",
		Channels:   []string{"email"},
	})

	// Without the matching scenario, low severity still routes to webhook only.
	got := ResolveRoute(rules, domain.AlertSeverityLow, "unrelated_scenario")
	want := []string{"webhook"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveRoute(low, unrelated_scenario) = %v, want %v", got, want)
	}

	// The scenario-specific rule overrides the low-severity default.
	got = ResolveRoute(rules, domain.AlertSeverityLow, "structuring_basic")
	want = []string{"email"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveRoute(low, structuring_basic) = %v, want %v", got, want)
	}
}
