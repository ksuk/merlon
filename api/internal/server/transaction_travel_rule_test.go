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

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/store"
)

func travelRuleServer(t *testing.T) (*Server, *store.MemoryPendingEvaluationRepo, string) {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	pending := store.NewMemoryPendingEvaluationRepo()
	s := New(":0", Deps{
		Customers: customers, Transactions: store.NewMemoryTransactionRepo(),
		Alerts: store.NewMemoryAlertRepo(), Audit: store.NewMemoryAuditRepo(),
		PendingEvaluations: pending, Monitoring: &engine.MockMonitoringEngine{},
	})
	id := "00000000000000000000000000000f01"
	if err := customers.Create(ctx, &domain.Customer{
		ID: id, ExternalID: "travel-rule", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return s, pending, id
}

func postTransaction(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body)))
	return rec
}

type travelRuleTransaction struct {
	domain.Transaction
	TravelRuleStatus     string         `json:"travel_rule_status"`
	TravelRuleAssessment map[string]any `json:"travel_rule_assessment"`
}

func decodeTransaction(t *testing.T, rec *httptest.ResponseRecorder) travelRuleTransaction {
	t.Helper()
	var out travelRuleTransaction
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

// Every transaction is assessed, including one with no counterparty block at
// all: leaving those unassessed made an un-evaluated transaction
// indistinguishable from one that predated the field.
func TestTravelRuleAssessmentIsAlwaysRecorded(t *testing.T) {
	s, _, customerID := travelRuleServer(t)
	threshold := policy.DefaultTravelRule()

	tests := []struct {
		name           string
		body           string
		wantApplicable bool
		wantStatus     string
	}{
		{
			name:           "fiat payment is out of scope",
			body:           fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-FIAT","amount":5000000,"currency":"JPY","direction":"outbound","channel":"wire"}`, customerID),
			wantApplicable: false,
			wantStatus:     string(domain.TravelRuleNotApplicable),
		},
		{
			name:           "crypto below the threshold",
			body:           fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-SMALL","amount":%v,"currency":"JPY","direction":"outbound","channel":"crypto","counterparty":{"counterparty_type":"vasp","travel_rule_status":"not_applicable"},"travel_rule_not_applicable_reason":"below threshold"}`, customerID, threshold.ThresholdAmount-1),
			wantApplicable: false,
			wantStatus:     string(domain.TravelRuleNotApplicable),
		},
		{
			name:           "crypto at exactly the threshold is covered",
			body:           fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-EXACT","amount":%v,"currency":"JPY","direction":"outbound","channel":"crypto","counterparty":{"counterparty_type":"vasp","travel_rule_status":"incomplete"}}`, customerID, threshold.ThresholdAmount),
			wantApplicable: true,
			wantStatus:     string(domain.TravelRuleIncomplete),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := postTransaction(t, s, tc.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			got := decodeTransaction(t, rec)
			if got.TravelRuleAssessment == nil {
				t.Fatal("no assessment was recorded")
			}
			if got.TravelRuleAssessment["applicable"] != tc.wantApplicable {
				t.Errorf("applicable = %v, want %v", got.TravelRuleAssessment["applicable"], tc.wantApplicable)
			}
			if got.TravelRuleStatus != tc.wantStatus {
				t.Errorf("travel_rule_status = %q, want %q", got.TravelRuleStatus, tc.wantStatus)
			}
			if got.TravelRuleAssessment["policy_version"] == "" {
				t.Error("the assessment does not say which policy version decided it")
			}
		})
	}
}

// The client's assertion is kept; the disagreement is the finding.
func TestTravelRuleConflictIsRecordedNotOverwritten(t *testing.T) {
	s, _, customerID := travelRuleServer(t)

	// A fiat wire the client insists is covered.
	body := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-CONFLICT","amount":5000000,"currency":"JPY","direction":"outbound","channel":"wire","travel_rule_applicable":true}`, customerID)
	rec := postTransaction(t, s, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeTransaction(t, rec)
	if got.TravelRuleApplicable == nil || !*got.TravelRuleApplicable {
		t.Fatal("the client's assertion was overwritten; under client authority it must be preserved")
	}
	if got.TravelRuleAssessment["conflict"] != true {
		t.Fatalf("assessment = %v, want the disagreement recorded", got.TravelRuleAssessment)
	}
	if rec.Header().Get("Warning") == "" {
		t.Error("no warning about the conflicting assertion")
	}
}

// Declaring complete while required evidence is absent is self-contradictory
// data, so it is refused -- nothing coherent was previously accepted here.
func TestTravelRuleCompleteWithoutEvidenceIsRejected(t *testing.T) {
	s, _, customerID := travelRuleServer(t)
	threshold := policy.DefaultTravelRule()

	body := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-LIAR","amount":%v,"currency":"JPY","direction":"outbound","channel":"crypto","counterparty":{"counterparty_type":"vasp","travel_rule_status":"complete"}}`, customerID, threshold.ThresholdAmount)
	rec := postTransaction(t, s, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	for _, field := range threshold.RequiredEvidenceFields[domain.CounterpartyTypeVASP] {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("body does not name the missing field %q: %s", field, rec.Body.String())
		}
	}
}

func TestTravelRuleReasonCodeIsAClosedEnum(t *testing.T) {
	s, _, customerID := travelRuleServer(t)

	bad := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-CODE-BAD","amount":1000,"currency":"JPY","direction":"outbound","channel":"wire","travel_rule_applicable":false,"travel_rule_not_applicable_reason":"x","travel_rule_not_applicable_reason_code":"because_i_said_so"}`, customerID)
	if rec := postTransaction(t, s, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown reason code = %d, want 400", rec.Code)
	}

	// Free text with no code still works and maps to `other`, so an existing
	// integration is unaffected.
	legacy := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-CODE-LEGACY","amount":1000,"currency":"JPY","direction":"outbound","channel":"wire","travel_rule_applicable":false,"travel_rule_not_applicable_reason":"domestic transfer between own accounts"}`, customerID)
	rec := postTransaction(t, s, legacy)
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy free-text reason = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeTransaction(t, rec); got.TravelRuleNotApplicableReasonCode != policy.TravelRuleReasonOther {
		t.Fatalf("reason code = %q, want %q", got.TravelRuleNotApplicableReasonCode, policy.TravelRuleReasonOther)
	}
}

// Missing Travel Rule evidence is a compliance gap, not a rejection: the
// transaction happened. It joins the queue an engine outage uses.
func TestIncompleteTravelRuleEvidenceIsQueuedForReview(t *testing.T) {
	s, pending, customerID := travelRuleServer(t)
	threshold := policy.DefaultTravelRule()

	body := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-QUEUE","amount":%v,"currency":"JPY","direction":"outbound","channel":"crypto","counterparty":{"counterparty_type":"vasp","travel_rule_status":"incomplete"}}`, customerID, threshold.ThresholdAmount)
	if rec := postTransaction(t, s, body); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	queued, err := pending.ListByStatus(context.Background(), domain.PendingEvaluationStatusPendingReview, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range queued {
		if strings.HasPrefix(item.Reason, "travel_rule_incomplete") {
			found = true
		}
	}
	if !found {
		t.Fatalf("queue = %+v, want the incomplete travel rule evidence queued", queued)
	}
}

// A payload from before the Travel Rule fields existed must still be accepted,
// and must now come back with an assessment rather than a permanent nil.
func TestLegacyTransactionPayloadStillAcceptedAndAssessed(t *testing.T) {
	s, _, customerID := travelRuleServer(t)

	body := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-LEGACY","amount":12345,"currency":"JPY","direction":"inbound"}`, customerID)
	rec := postTransaction(t, s, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy payload = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeTransaction(t, rec)
	if got.TravelRuleAssessment == nil {
		t.Fatal("a legacy payload was stored with no assessment, which is the state that read as 'legacy' forever")
	}
	if got.TravelRuleAssessment["reason_code"] == "" {
		t.Error("the assessment does not say why the rule does not apply")
	}
}

// Optional metadata survives the round trip untouched.
func TestTravelRuleEvidenceRoundTrips(t *testing.T) {
	s, _, customerID := travelRuleServer(t)
	threshold := policy.DefaultTravelRule()

	evidence := map[string]any{}
	for _, field := range threshold.RequiredEvidenceFields[domain.CounterpartyTypeUnhostedWallet] {
		evidence[field] = "supplied-" + field
	}
	evidence["operator_note"] = "wallet ownership attested"
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"customer_id":%q,"external_id":"TR-EVIDENCE","amount":%v,"currency":"JPY","direction":"outbound","channel":"crypto","counterparty":{"counterparty_type":"unhosted_wallet","travel_rule_status":"complete"},"travel_rule_evidence":%s}`,
		customerID, threshold.ThresholdAmount, encoded)
	rec := postTransaction(t, s, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeTransaction(t, rec)
	if got.TravelRuleEvidence["operator_note"] != "wallet ownership attested" {
		t.Fatalf("evidence = %v, want the optional metadata preserved", got.TravelRuleEvidence)
	}
	if got.TravelRuleStatus != string(domain.TravelRuleComplete) {
		t.Fatalf("status = %q, want complete once every required field is present", got.TravelRuleStatus)
	}
}
