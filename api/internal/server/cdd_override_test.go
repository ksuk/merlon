package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func cddServer(t *testing.T, withAuth bool) (*Server, *store.MemoryCustomerRepo, string) {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	deps := Deps{
		Customers: customers, Audit: store.NewMemoryAuditRepo(),
		Scoring: &engine.MockScoringEngine{Score: 7.5, Tier: domain.RiskTierHigh},
		Rules:   store.NewMemoryRuleRepo(),
	}
	if withAuth {
		deps.APIKeys = store.NewMemoryAPIKeyRepo()
	}
	s := New(":0", deps)
	id := "00000000000000000000000000000d01"
	if err := customers.Create(ctx, &domain.Customer{
		ID: id, ExternalID: "cdd-customer", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return s, customers, id
}

func postScore(t *testing.T, s *Server, customerID, body string, role domain.Role, actor string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+customerID+"/score", strings.NewReader(body))
	if role != "" {
		req = req.WithContext(auth.WithRole(req.Context(), role))
	}
	if actor != "" {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyPrincipal, Principal{UserID: actor}))
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// The approved breaking change: a role that may only read must not be able to
// move a customer's risk tier, which decides EDD, monitoring thresholds and
// rescreening frequency.
func TestScoreCustomerRequiresTheScoringPermission(t *testing.T) {
	s, _, id := cddServer(t, true)

	// The router's own middleware answers a credential-less request with 401
	// before the handler runs, so the permission itself is exercised directly.
	tests := []struct {
		role  domain.Role
		allow bool
	}{
		{domain.RoleViewer, false},
		{domain.RoleAnalyst, true},
		{domain.RoleAdmin, true},
	}
	for _, tc := range tests {
		called := false
		gate := s.requireRolePermission(auth.PermCDDScore, func(http.ResponseWriter, *http.Request) { called = true })
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+id+"/score", strings.NewReader(`{}`))
		req = req.WithContext(auth.WithRole(req.Context(), tc.role))
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if tc.allow && !called {
			t.Errorf("%s was refused with %d", tc.role, rec.Code)
		}
		if !tc.allow {
			if called {
				t.Errorf("%s reached the scoring handler", tc.role)
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s = %d, want 403", tc.role, rec.Code)
			}
		}
	}
}

// A deployment without authentication has no roles to check; refusing every
// score there would disable the product rather than enforce a control.
func TestScoreCustomerUngatedWithoutAuthentication(t *testing.T) {
	s, _, id := cddServer(t, false)
	if rec := postScore(t, s, id, `{}`, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("score without authentication = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestScoreOverrideEvidenceShapeIsValidated(t *testing.T) {
	s, _, id := cddServer(t, false)

	tests := []struct {
		name     string
		evidence string
	}{
		{"unknown field", `{"reason":"r","proposed_tier":"LOW","note":"smuggled"}`},
		{"missing reason", `{"proposed_tier":"LOW"}`},
		{"blank reason", `{"reason":"   ","proposed_tier":"LOW"}`},
		{"missing tier", `{"reason":"documented"}`},
		{"unknown tier", `{"reason":"documented","proposed_tier":"NEGLIGIBLE"}`},
		{"documents not a list", `{"reason":"documented","proposed_tier":"LOW","supporting_documents":"one"}`},
		{"blank document", `{"reason":"documented","proposed_tier":"LOW","supporting_documents":[""]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"rationale":"reviewed","override_evidence":%s}`, tc.evidence)
			if rec := postScore(t, s, id, body, "", ""); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	// A well-formed override is accepted, but only as a proposal.
	rec := postScore(t, s, id, `{"rationale":"documented mitigation","override_evidence":{"reason":"group-level KYC held by parent","proposed_tier":"MEDIUM","supporting_documents":["DOC-1"]}}`, "", "analyst-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid override = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Warning") == "" {
		t.Error("no warning that the override is only a proposal")
	}
}

// An override without a rationale is refused: an operator deviating from the
// computed result must be attributable.
func TestScoreOverrideRequiresARationale(t *testing.T) {
	s, _, id := cddServer(t, false)
	body := `{"override_evidence":{"reason":"documented","proposed_tier":"LOW"}}`
	if rec := postScore(t, s, id, body, "", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// The whole point of the maker-checker: the tier does not move until a second
// person approves.
func TestScoreOverrideRequiresApprovalBeforeItTakesEffect(t *testing.T) {
	s, customers, id := cddServer(t, false)
	ctx := context.Background()

	rec := postScore(t, s, id, `{"rationale":"documented mitigation","override_evidence":{"reason":"group-level KYC","proposed_tier":"LOW"}}`, "", "analyst-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("score = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		PendingOverride domain.CDDScoreOverride `json:"pending_override"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.PendingOverride.Status != domain.CDDOverridePendingApproval {
		t.Fatalf("override = %+v, want pending approval", body.PendingOverride)
	}

	// The customer still carries the computed tier.
	customer, err := customers.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if customer.RiskTier == nil || *customer.RiskTier != domain.RiskTierHigh {
		t.Fatalf("risk_tier = %v, want the computed HIGH until the override is approved", customer.RiskTier)
	}

	approve := func(actor string, expectedVersion int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/customers/%s/score-overrides/%s/approve", id, body.PendingOverride.ID),
			strings.NewReader(fmt.Sprintf(`{"rationale":"evidence checked","expected_version":%d}`, expectedVersion)))
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyPrincipal, Principal{UserID: actor}))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	if self := approve("analyst-1", 1); self.Code != http.StatusForbidden {
		t.Fatalf("self-approval = %d, want 403", self.Code)
	}
	if ok := approve("admin-1", 1); ok.Code != http.StatusOK {
		t.Fatalf("approval = %d, body=%s", ok.Code, ok.Body.String())
	}
	// Only now does the tier move.
	customer, err = customers.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if customer.RiskTier == nil || *customer.RiskTier != domain.RiskTierLow {
		t.Fatalf("risk_tier = %v, want the approved LOW", customer.RiskTier)
	}
	if again := approve("admin-2", 2); again.Code != http.StatusConflict {
		t.Fatalf("second decision = %d, want 409", again.Code)
	}
}

func TestScoreExplanationReconcilesAndExplainsTheTier(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	ctx := context.Background()
	id := "00000000000000000000000000000d02"
	if err := customers.Create(ctx, &domain.Customer{ID: id, ExternalID: "explain", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive}); err != nil {
		t.Fatal(err)
	}
	// Factors whose contributions sum to the recorded score.
	record := &domain.ScoreRecord{
		ID: "score-1", CustomerID: id, Score: 3.5, Tier: domain.RiskTierMedium,
		Factors: []domain.Factor{
			{Name: "customer_type", Score: 2, Weight: 0.5, Contribution: 1.0},
			{Name: "geography", Score: 5, Weight: 0.5, Contribution: 2.5},
		},
		RuleSetID: "cdd", ScoredAt: time.Now().UTC(),
	}
	if err := customers.SaveScoreRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{Customers: customers, Audit: store.NewMemoryAuditRepo()})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+id+"/score-explanation", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		TotalReconciled     float64 `json:"total_reconciled"`
		Reconciled          bool    `json:"reconciled"`
		ReconciliationDelta float64 `json:"reconciliation_delta"`
		TierReason          string  `json:"tier_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Reconciled || body.ReconciliationDelta > 1e-9 {
		t.Fatalf("body = %+v, want the factor contributions to reconcile with the score", body)
	}
	if body.TotalReconciled != 3.5 {
		t.Fatalf("total_reconciled = %v, want 3.5 -- contributions summed once, not double-counted", body.TotalReconciled)
	}
	if body.TierReason == "" {
		t.Error("the explanation states a tier without saying why")
	}
}

// A record whose factors disagree with its total must be reported as such.
// The old summation could not detect this at all.
func TestScoreExplanationDetectsAMismatch(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	ctx := context.Background()
	id := "00000000000000000000000000000d03"
	if err := customers.Create(ctx, &domain.Customer{ID: id, ExternalID: "mismatch", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP"}); err != nil {
		t.Fatal(err)
	}
	if err := customers.SaveScoreRecord(ctx, &domain.ScoreRecord{
		ID: "score-bad", CustomerID: id, Score: 9.0, Tier: domain.RiskTierHigh,
		Factors:  []domain.Factor{{Name: "customer_type", Score: 2, Contribution: 1.0}},
		ScoredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{Customers: customers, Audit: store.NewMemoryAuditRepo()})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+id+"/score-explanation", nil))
	var body struct {
		Reconciled          bool    `json:"reconciled"`
		ReconciliationDelta float64 `json:"reconciliation_delta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Reconciled {
		t.Fatal("factors summing to 1.0 were reported as reconciling with a score of 9.0")
	}
	if body.ReconciliationDelta < 7.9 {
		t.Fatalf("reconciliation_delta = %v, want the real gap reported", body.ReconciliationDelta)
	}
}

// The UI used to take whichever active rule set the API listed first.
func TestListCDDRuleSetsNamesTheRecommendation(t *testing.T) {
	s, _, id := cddServer(t, false)
	ctx := context.Background()
	for i, name := range []string{"legacy weights", "current weights"} {
		if err := s.rules.Create(ctx, &domain.RuleDefinition{
			ID: fmt.Sprintf("cdd-%d", i), Name: name, Type: domain.RuleTypeCDDWeight,
			Version: 1, IsActive: i == 1, Definition: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+id+"/cdd-rule-sets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			Digest      string `json:"digest"`
			IsActive    bool   `json:"is_active"`
			Recommended bool   `json:"recommended"`
		} `json:"data"`
		PolicyVersion string `json:"policy_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("data = %d rule sets, want 2", len(body.Data))
	}
	recommended := 0
	for _, item := range body.Data {
		if item.Digest == "" {
			t.Errorf("rule set %s has no digest to pin a score against", item.ID)
		}
		if item.Recommended {
			recommended++
			if !item.IsActive {
				t.Errorf("rule set %s is recommended but not active", item.ID)
			}
		}
	}
	if recommended != 1 {
		t.Fatalf("recommended = %d rule sets, want exactly 1", recommended)
	}
	if body.PolicyVersion == "" {
		t.Error("the recommendation was made without naming the policy behind it")
	}
}
