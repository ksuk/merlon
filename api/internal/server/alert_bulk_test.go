package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func seedAlertWith(t *testing.T, s *Server, customerID, scenarioID string, severity domain.AlertSeverity, detectedAt time.Time) domain.Alert {
	t.Helper()
	a := &domain.Alert{
		ID:          generateID(),
		CustomerID:  customerID,
		ScenarioID:  scenarioID,
		Severity:    severity,
		Status:      domain.AlertStatusOpen,
		Score:       1.0,
		Description: "test alert",
		DetectedAt:  detectedAt,
		CreatedAt:   detectedAt,
		UpdatedAt:   detectedAt,
	}
	if err := s.alerts.Create(context.Background(), a); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	return *a
}

// TestHandleBulkCloseAlerts_RequiresReason verifies the case-management workflow: bulk
// close requires a common reason (text, required).
func TestHandleBulkCloseAlerts_RequiresReason(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/bulk-close", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestHandleBulkCloseAlerts_FiltersByScenarioPeriodSeverity verifies only
// alerts matching all three filter axes are closed; others are untouched.
func TestHandleBulkCloseAlerts_FiltersByScenarioPeriodSeverity(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomerWithExternalID(t, s, "BULK_FILTER")

	inWindow := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	outOfWindow := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	match := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, inWindow)
	wrongScenario := seedAlertWith(t, s, cust.ID, "rapid_movement", domain.AlertSeverityHigh, inWindow)
	wrongSeverity := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityLow, inWindow)
	wrongPeriod := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, outOfWindow)

	periodFrom := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	periodTo := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(bulkCloseAlertsRequest{
		ScenarioID: "structuring_basic",
		Severity:   domain.AlertSeverityHigh,
		PeriodFrom: &periodFrom,
		PeriodTo:   &periodTo,
		Reason:     "known recurring salary pattern",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/bulk-close", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp bulkCloseAlertsResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.ClosedCount != 1 {
		t.Fatalf("closed_count = %d, want 1 (body: %+v)", resp.ClosedCount, resp)
	}
	if len(resp.AlertIDs) != 1 || resp.AlertIDs[0] != match.ID {
		t.Errorf("closed alert IDs = %v, want [%s]", resp.AlertIDs, match.ID)
	}

	for _, other := range []domain.Alert{wrongScenario, wrongSeverity, wrongPeriod} {
		got, err := s.alerts.Get(context.Background(), other.ID)
		if err != nil {
			t.Fatalf("Get(%s): %v", other.ID, err)
		}
		if got.Status != domain.AlertStatusOpen {
			t.Errorf("alert %s status = %q, want unchanged %q", other.ID, got.Status, domain.AlertStatusOpen)
		}
	}
}

// TestHandleBulkCloseAlerts_RecordsIndividualAuditEntries verifies
// the case-management workflow: "一括操作は個別アラートごとに監査ログを記録する" — one
// audit entry per closed alert must exist, not just the request-level entry.
func TestHandleBulkCloseAlerts_RecordsIndividualAuditEntries(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomerWithExternalID(t, s, "BULK_AUDIT")

	now := time.Now()
	a1 := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, now)
	a2 := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, now)

	body, _ := json.Marshal(bulkCloseAlertsRequest{
		ScenarioID: "structuring_basic",
		Severity:   domain.AlertSeverityHigh,
		Reason:     "bulk review complete",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/bulk-close", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	for _, alertID := range []string{a1.ID, a2.ID} {
		entries, err := s.audit.List(context.Background(), domain.AuditListFilter{ResourceType: "alert", ResourceID: alertID, Limit: 10})
		if err != nil {
			t.Fatalf("audit List(%s): %v", alertID, err)
		}
		found := false
		for _, e := range entries {
			if e.Action == "bulk_close_alert" {
				found = true
				if e.Details["reason"] != "bulk review complete" {
					t.Errorf("audit entry for %s missing reason detail: %+v", alertID, e.Details)
				}
			}
		}
		if !found {
			t.Errorf("no bulk_close_alert audit entry recorded for alert %s", alertID)
		}
	}
}

// TestHandleBulkCaseAssignment_AddsToExistingCase verifies selected alerts
// are appended to an existing case's alert_ids.
func TestHandleBulkCaseAssignment_AddsToExistingCase(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomerWithExternalID(t, s, "BULK_CASE_EXIST")
	a1 := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, time.Now())
	a2 := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, time.Now())

	existingCase := &domain.Case{
		ID:         "case-existing-1",
		CustomerID: cust.ID,
		Status:     domain.CaseStatusNew,
		Priority:   domain.CasePriorityMedium,
		Summary:    "existing investigation",
	}
	if err := s.cases.Create(context.Background(), existingCase); err != nil {
		t.Fatalf("seed case: %v", err)
	}

	body, _ := json.Marshal(bulkCaseAssignmentRequest{
		AlertIDs: []string{a1.ID, a2.ID},
		CaseID:   existingCase.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/bulk-case", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp bulkCaseAssignmentResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Created {
		t.Error("Created = true, want false (existing case)")
	}
	if resp.CaseID != existingCase.ID {
		t.Errorf("CaseID = %q, want %q", resp.CaseID, existingCase.ID)
	}

	got, err := s.cases.Get(context.Background(), existingCase.ID)
	if err != nil {
		t.Fatalf("Get case: %v", err)
	}
	if len(got.AlertIDs) != 2 {
		t.Errorf("case alert_ids = %v, want 2 entries", got.AlertIDs)
	}
}

// TestHandleBulkCaseAssignment_CreatesNewCase verifies that omitting case_id
// bundles the selected alerts into a newly created case.
func TestHandleBulkCaseAssignment_CreatesNewCase(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomerWithExternalID(t, s, "BULK_CASE_NEW")
	a1 := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, time.Now())
	a2 := seedAlertWith(t, s, cust.ID, "structuring_basic", domain.AlertSeverityHigh, time.Now())

	body, _ := json.Marshal(bulkCaseAssignmentRequest{
		AlertIDs:   []string{a1.ID, a2.ID},
		CustomerID: cust.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/bulk-case", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp bulkCaseAssignmentResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Created {
		t.Error("Created = false, want true (new case)")
	}
	if resp.CaseID == "" {
		t.Fatal("CaseID is empty")
	}

	got, err := s.cases.Get(context.Background(), resp.CaseID)
	if err != nil {
		t.Fatalf("Get case: %v", err)
	}
	if len(got.AlertIDs) != 2 {
		t.Errorf("case alert_ids = %v, want 2 entries", got.AlertIDs)
	}
	if got.CustomerID != cust.ID {
		t.Errorf("case customer_id = %q, want %q", got.CustomerID, cust.ID)
	}
}
