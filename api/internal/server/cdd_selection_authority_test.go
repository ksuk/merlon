package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/store"
)

// cdd_rule_selection_v1 declares selection_authority, but ServerResolves() was
// only echoed back on the rule-set list. handleScoreCustomer never read it, so
// "server" and "client" produced byte-identical behaviour whenever a caller
// named a rule set: the setting described an intent the server did not apply.
//
// Under server authority a deliberate departure from the policy's choice is
// still allowed -- the operator may know something the policy does not -- but
// it is an override, and an override has to be attributable (ADR-0019).

const selectionPolicyYAML = `schema_version: cdd_rule_selection_v1
policy_version: "test-server-authority"
default_rule_set_id: ""
selection_authority: server
rules:
  - match:
      customer_type: [individual]
    rule_set_id: cdd_policy_choice
    priority: 100
`

func newSelectionServer(t *testing.T) (*Server, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "cdd_rule_selection_v1.yaml")
	if err := os.WriteFile(path, []byte(selectionPolicyYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	policies, err := policy.Load(policy.Paths{CDDRuleSelection: path})
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}

	customers := store.NewMemoryCustomerRepo()
	rules := store.NewMemoryRuleRepo()
	now := time.Now().UTC()
	for _, id := range []string{"cdd_policy_choice", "cdd_other_choice"} {
		if err := rules.Create(ctx, &domain.RuleDefinition{
			ID: id, Name: id, Type: domain.RuleTypeCDDWeight, Version: 1, IsActive: true,
			Definition: json.RawMessage(`{"weights":{}}`),
			CreatedAt:  now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create rule %s: %v", id, err)
		}
	}
	customerID := "00000000000000000000000000000e01"
	if err := customers.Create(ctx, &domain.Customer{
		ID: customerID, ExternalID: "sel-1", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{
		Customers: customers, Rules: rules, Audit: store.NewMemoryAuditRepo(),
		Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(),
		Scoring: &engine.MockScoringEngine{Score: 2.0, Tier: domain.RiskTierLow}, Policies: policies,
	})
	return s, customerID
}

func scoreWith(t *testing.T, s *Server, customerID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+customerID+"/score", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestScoreRejectsUnattributedDepartureFromPolicyChoice(t *testing.T) {
	s, customerID := newSelectionServer(t)

	rec := scoreWith(t, s, customerID, `{"rule_set_id":"cdd_other_choice"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: naming a rule set the policy did not choose is an override and needs a rationale (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestScoreAcceptsPolicyChoiceWithoutRationale(t *testing.T) {
	s, customerID := newSelectionServer(t)

	// Naming the rule set the policy would have picked anyway is not a
	// departure, so it must stay ordinary use.
	rec := scoreWith(t, s, customerID, `{"rule_set_id":"cdd_policy_choice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestScoreRecordsPolicyChoiceWhenOperatorOverrides(t *testing.T) {
	s, customerID := newSelectionServer(t)

	rec := scoreWith(t, s, customerID, `{"rule_set_id":"cdd_other_choice","rationale":"customer is a pilot cohort"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var record domain.ScoreRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.RuleSetID != "cdd_other_choice" {
		t.Errorf("rule_set_id = %q, want the operator's choice to be honoured", record.RuleSetID)
	}

	// The audit trail must carry both readings, or a reviewer cannot tell the
	// operator departed from the configured policy at all.
	entries, err := s.audit.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range entries {
		if entry.Details["cdd_rule_set_policy_choice"] == "cdd_policy_choice" &&
			entry.Details["cdd_rule_set_selected"] == "cdd_other_choice" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no audit entry recorded the policy choice against the operator's; entries: %+v", entries)
	}
}

func TestScoreUnderClientAuthorityNeedsNoRationale(t *testing.T) {
	// With client authority the operator picks freely; requiring a rationale
	// there would make the two authority values behave the same again.
	dir := t.TempDir()
	path := filepath.Join(dir, "cdd_rule_selection_v1.yaml")
	clientPolicy := strings.Replace(selectionPolicyYAML, "selection_authority: server", "selection_authority: client", 1)
	if err := os.WriteFile(path, []byte(clientPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	policies, err := policy.Load(policy.Paths{CDDRuleSelection: path})
	if err != nil {
		t.Fatal(err)
	}
	s, customerID := newSelectionServer(t)
	s.policies = policies

	rec := scoreWith(t, s, customerID, `{"rule_set_id":"cdd_other_choice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 under client authority (body: %s)", rec.Code, rec.Body.String())
	}
}
