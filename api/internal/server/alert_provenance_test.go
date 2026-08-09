package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func provenanceTestServer(t *testing.T) (*Server, domain.AlertRepository, domain.RuleRepository) {
	t.Helper()
	alerts := store.NewMemoryAlertRepo()
	rules := store.NewMemoryRuleRepo()
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       alerts,
		Cases:        store.NewMemoryCaseRepo(),
		Rules:        rules,
	})
	return s, alerts, rules
}

func seedProvenanceAlert(t *testing.T, alerts domain.AlertRepository, a *domain.Alert) string {
	t.Helper()
	if err := alerts.Create(context.Background(), a); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	return a.ID
}

func getAlertProvenance(t *testing.T, s *Server, id string) domain.AlertProvenance {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+id, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Provenance *domain.AlertProvenance `json:"provenance"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Provenance == nil {
		t.Fatal("alert detail carried no provenance object at all; every alert must state which case it is in")
	}
	return *body.Provenance
}

// TestAlertProvenance_LegacyAlertIsNotCaptured is the requirement that current
// configuration is never backfilled as historical fact.
func TestAlertProvenance_LegacyAlertIsNotCaptured(t *testing.T) {
	s, alerts, rules := provenanceTestServer(t)

	// A rule with this name exists right now, which is exactly the temptation:
	// resolving it would produce a record that looks like evidence.
	if err := rules.Create(context.Background(), &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeTMScenario, Name: "tm_structuring_basic",
		Definition: json.RawMessage(`{"threshold":1000}`), Version: 4,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	id := seedProvenanceAlert(t, alerts, &domain.Alert{
		ID: generateID(), CustomerID: generateID(), ScenarioID: "tm_structuring_basic",
		Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen,
		DetectedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	got := getAlertProvenance(t, s, id)
	if got.Availability != domain.ProvenanceNotCaptured {
		t.Errorf("availability = %q, want %q for an alert generated before provenance existed", got.Availability, domain.ProvenanceNotCaptured)
	}
	if got.RuleVersion != nil {
		t.Errorf("rule_version = %v; the current version was filled in as though it were historical fact", *got.RuleVersion)
	}
	if len(got.ConfigDigests) != 0 {
		t.Errorf("config_digests = %v; nothing was captured for this alert", got.ConfigDigests)
	}
}

// TestAlertProvenance_ResolvesToTheStoredRuleVersion covers the ordinary case.
func TestAlertProvenance_ResolvesToTheStoredRuleVersion(t *testing.T) {
	s, alerts, rules := provenanceTestServer(t)

	if err := rules.Create(context.Background(), &domain.RuleDefinition{
		ID: generateID(), Type: domain.RuleTypeTMScenario, Name: "tm_rapid_movement",
		Definition: json.RawMessage(`{"threshold":5000}`), Version: 2,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	threshold := 5000.0
	evaluatedAt := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	id := seedProvenanceAlert(t, alerts, &domain.Alert{
		ID: generateID(), CustomerID: generateID(), ScenarioID: "tm_rapid_movement",
		Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen,
		DetectedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Provenance: &domain.AlertProvenance{
			ScenarioID:       "tm_rapid_movement",
			ConfigDigests:    map[string]string{"tm_scenarios": "digest-abc"},
			EngineVersion:    "test",
			EvaluationMode:   "batch",
			EvaluatedAt:      &evaluatedAt,
			AppliedThreshold: &threshold,
			Availability:     domain.ProvenanceAvailable,
		},
	})

	stored, err := rules.Get(context.Background(), "tm_rapid_movement")
	if err != nil {
		t.Fatalf("read back rule: %v", err)
	}

	got := getAlertProvenance(t, s, id)

	if got.Availability != domain.ProvenanceRestricted {
		t.Errorf("availability = %q, want %q: the identifier and version travel, the body does not", got.Availability, domain.ProvenanceRestricted)
	}
	if got.RuleVersion == nil || *got.RuleVersion != stored.Version {
		t.Errorf("rule_version = %v, want the stored version %d", got.RuleVersion, stored.Version)
	}
	if got.RuleDigest == "" {
		t.Error("rule_digest is empty; a reviewer cannot tell whether the artifact they fetched is the one named")
	}
	if got.ConfigDigests["tm_scenarios"] != "digest-abc" {
		t.Errorf("config digest lost in the round trip: %v", got.ConfigDigests)
	}
	if got.AppliedThreshold == nil || *got.AppliedThreshold != 5000 {
		t.Errorf("applied_threshold = %v, want 5000", got.AppliedThreshold)
	}
	if got.EvaluationMode != "batch" {
		t.Errorf("evaluation_mode = %q, want batch", got.EvaluationMode)
	}
}

// TestAlertProvenance_UnresolvableReferenceIsMissing: a rule version that can
// no longer be produced is reported, not reconstructed.
func TestAlertProvenance_UnresolvableReferenceIsMissing(t *testing.T) {
	s, alerts, _ := provenanceTestServer(t)

	id := seedProvenanceAlert(t, alerts, &domain.Alert{
		ID: generateID(), CustomerID: generateID(), ScenarioID: "tm_scenario_since_deleted",
		Severity: domain.AlertSeverityMedium, Status: domain.AlertStatusOpen,
		DetectedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Provenance: &domain.AlertProvenance{
			ScenarioID:    "tm_scenario_since_deleted",
			ConfigDigests: map[string]string{"tm_scenarios": "digest-old"},
			Availability:  domain.ProvenanceAvailable,
		},
	})

	got := getAlertProvenance(t, s, id)

	if got.Availability != domain.ProvenanceMissing {
		t.Errorf("availability = %q, want %q", got.Availability, domain.ProvenanceMissing)
	}
	// What was captured is still true and still reported.
	if got.ConfigDigests["tm_scenarios"] != "digest-old" {
		t.Errorf("captured digests were discarded along with the unresolvable reference: %v", got.ConfigDigests)
	}
	if got.RuleVersion != nil {
		t.Error("a rule version was invented for a reference that does not resolve")
	}
}

// TestAlertProvenance_SurvivesALifecycleUpdate: an alert's status, assignment
// and disposition change over its life; its provenance must not.
func TestAlertProvenance_SurvivesALifecycleUpdate(t *testing.T) {
	s, alerts, _ := provenanceTestServer(t)

	id := seedProvenanceAlert(t, alerts, &domain.Alert{
		ID: generateID(), CustomerID: generateID(), ScenarioID: "tm_structuring_basic",
		Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen,
		DetectedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Provenance: &domain.AlertProvenance{
			ScenarioID:    "tm_structuring_basic",
			ConfigDigests: map[string]string{"tm_scenarios": "digest-at-detection"},
			Availability:  domain.ProvenanceAvailable,
		},
	})

	if err := alerts.UpdateStatus(context.Background(), id, domain.AlertStatusInvestigating, "analyst@example.com"); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got := getAlertProvenance(t, s, id)
	if got.ConfigDigests["tm_scenarios"] != "digest-at-detection" {
		t.Errorf("provenance changed with the alert's lifecycle: %v", got.ConfigDigests)
	}
}
