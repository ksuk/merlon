package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func seedAlert(t *testing.T, s *Server, customerID string) domain.Alert {
	t.Helper()
	a := &domain.Alert{
		ID:             generateID(),
		CustomerID:     customerID,
		ScenarioID:     "test_structuring",
		Severity:       domain.AlertSeverityMedium,
		Status:         domain.AlertStatusOpen,
		Score:          1.5,
		Description:    "test alert",
		TransactionIDs: []string{"tx1", "tx2"},
		DetectedAt:     time.Now(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := s.alerts.Create(context.Background(), a); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	return *a
}

func TestListOpenAlerts(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	seedAlert(t, s, cust.ID)
	seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	alerts, _ := decodeListResponse[domain.Alert](t, rec.Body)

	if len(alerts) != 2 {
		t.Errorf("len = %d, want 2", len(alerts))
	}
}

func TestListAlertsByCustomer(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?customer_id="+cust.ID, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	alerts, _ := decodeListResponse[domain.Alert](t, rec.Body)

	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1", len(alerts))
	}
}

func TestGetAlert(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	alert := seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+alert.ID, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got domain.Alert
	json.NewDecoder(rec.Body).Decode(&got)

	if got.ID != alert.ID {
		t.Errorf("id = %q, want %q", got.ID, alert.ID)
	}
}

func TestGetAlertNotFound(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateAlertStatus(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	alert := seedAlert(t, s, cust.ID)

	body := `{"status":"investigating"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.Alert
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated.Status != domain.AlertStatusInvestigating {
		t.Fatalf("status = %q, want %q before terminal close", updated.Status, domain.AlertStatusInvestigating)
	}

	body = `{"status":"closed_false_positive","resolved_by":"analyst@example.com"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminal close status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	json.NewDecoder(rec.Body).Decode(&updated)

	if updated.Status != domain.AlertStatusClosedFalsePositive {
		t.Errorf("status = %q, want %q", updated.Status, domain.AlertStatusClosedFalsePositive)
	}
	if updated.ResolvedBy != "analyst@example.com" {
		t.Errorf("resolved_by = %q, want %q", updated.ResolvedBy, "analyst@example.com")
	}
}

func TestUpdateAlertStatusNotFound(t *testing.T) {
	s := testServer()

	body := `{"status":"investigating"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/nonexistent", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateAlertStatusRejectsDirectTerminalClose(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	alert := seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(`{"status":"closed_false_positive","resolved_by":"analyst@example.com"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	assertErrorCode(t, rec, "invalid_state_transition")
}

// TestAlertStatusUpdateOptimisticLock verifies a PATCH supplying the
// alert's current updated_at as expected_updated_at succeeds, and a PATCH
// supplying a stale updated_at (an intervening update happened first) is
// rejected with 409 rather than silently overwriting it (WS-11 Task 8,
// the data model §3.9).
func TestAlertStatusUpdateOptimisticLock(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	alert := seedAlert(t, s, cust.ID)

	// Ensure the update's timestamp is distinguishable from the seed's
	// (some platforms' clocks have coarse enough resolution that two
	// time.Now() calls microseconds apart can otherwise read identical).
	time.Sleep(2 * time.Millisecond)

	body := `{"status":"investigating","expected_updated_at":"` + alert.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// A second client submits an update using the now-stale updated_at it
	// read before the update above.
	staleBody := `{"status":"closed_false_positive","resolved_by":"analyst@example.com","expected_updated_at":"` + alert.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(staleBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleListAlerts_CursorPagination(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	seedAlert(t, s, cust.ID)
	seedAlert(t, s, cust.ID)
	seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?limit=2", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, meta1 := decodeListResponse[domain.Alert](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if !meta1.HasMore {
		t.Fatal("expected has_more = true on first page")
	}
	if meta1.NextCursor == "" {
		t.Fatal("expected non-empty next_cursor on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?limit=2&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, meta2 := decodeListResponse[domain.Alert](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if meta2.HasMore {
		t.Error("expected has_more = false on second page")
	}
}

func TestHandleListAlerts_RiskSortRanksCriticalFirst(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	severities := []domain.AlertSeverity{
		domain.AlertSeverityLow,
		domain.AlertSeverityMedium,
		domain.AlertSeverityHigh,
		domain.AlertSeverityCritical,
	}
	for i, severity := range severities {
		if err := s.alerts.Create(context.Background(), &domain.Alert{
			ID: fmt.Sprintf("risk-api-alert-%d", i), CustomerID: cust.ID, Severity: severity,
			Status: domain.AlertStatusOpen, CreatedAt: created, UpdatedAt: created,
		}); err != nil {
			t.Fatalf("create alert: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?sort=risk&limit=4", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	alerts, meta := decodeListResponse[domain.Alert](t, rec.Body)
	if meta.HasMore {
		t.Error("has_more = true, want false")
	}
	want := []string{"risk-api-alert-3", "risk-api-alert-2", "risk-api-alert-1", "risk-api-alert-0"}
	for i, id := range want {
		if alerts[i].ID != id {
			t.Errorf("alerts[%d].ID = %q, want %q", i, alerts[i].ID, id)
		}
	}
}

func TestHandleListAlerts_OffsetStillWorks(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	seedAlert(t, s, cust.ID)
	seedAlert(t, s, cust.ID)
	seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?limit=2&offset=0", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, _ := decodeListResponse[domain.Alert](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if rec.Header().Get("Deprecation") != "true" {
		t.Error("expected Deprecation header when offset param is used")
	}
	if rec.Header().Get("Sunset") == "" {
		t.Error("expected Sunset header when offset param is used")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?limit=2&offset=2", nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, _ := decodeListResponse[domain.Alert](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
}

func TestCursorPagination_MatchesOffsetTraversal(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	const n = 7
	seeded := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		a := seedAlert(t, s, cust.ID)
		seeded[a.ID] = true
	}

	offsetIDs := map[string]bool{}
	offset := 0
	for i := 0; i < n+2; i++ {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/alerts?limit=2&offset=%d", offset), nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		page, _ := decodeListResponse[domain.Alert](t, rec.Body)
		if len(page) == 0 {
			break
		}
		for _, a := range page {
			offsetIDs[a.ID] = true
		}
		offset += len(page)
	}

	cursorIDs := map[string]bool{}
	cursorParam := ""
	for i := 0; i < n+2; i++ {
		reqURL := "/api/v1/alerts?limit=2"
		if cursorParam != "" {
			reqURL += "&cursor=" + url.QueryEscape(cursorParam)
		}
		req := httptest.NewRequest(http.MethodGet, reqURL, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		page, meta := decodeListResponse[domain.Alert](t, rec.Body)
		for _, a := range page {
			cursorIDs[a.ID] = true
		}
		if !meta.HasMore {
			break
		}
		cursorParam = meta.NextCursor
	}

	if len(offsetIDs) != n {
		t.Errorf("offset traversal found %d alerts, want %d", len(offsetIDs), n)
	}
	if len(cursorIDs) != n {
		t.Errorf("cursor traversal found %d alerts, want %d", len(cursorIDs), n)
	}
	for id := range seeded {
		if !offsetIDs[id] {
			t.Errorf("offset traversal missing alert %s", id)
		}
		if !cursorIDs[id] {
			t.Errorf("cursor traversal missing alert %s", id)
		}
	}
}

func TestListAlerts_ResponseFieldsUnchanged(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)
	seedAlert(t, s, cust.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected at least one alert in response")
	}

	// Fields the pre-Task-2 bare-array response exposed on each alert; none
	// may be dropped now that a "data"/"pagination" envelope wraps them.
	previousKeys := []string{
		"id", "customer_id", "scenario_id", "severity", "status", "score",
		"description", "transaction_ids", "detected_at", "created_at", "updated_at",
	}
	got := resp.Data[0]
	for _, key := range previousKeys {
		if _, ok := got[key]; !ok {
			t.Errorf("field %q missing from list response item", key)
		}
	}
}

// TestCreateAlertIncrementsMetric is Task 9 (the operational design §4.4 OPS-003):
// creating an alert (via batch monitoring, the only path that raises
// alerts) must increment merlon_alerts_total for that alert's
// scenario/severity.
func TestCreateAlertIncrementsMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.AlertsTotal.WithLabelValues("test_metric_scenario", "high"))

	recordAlertCreated(&domain.Alert{
		ScenarioID: "test_metric_scenario",
		Severity:   domain.AlertSeverityHigh,
	})

	after := testutil.ToFloat64(metrics.AlertsTotal.WithLabelValues("test_metric_scenario", "high"))
	if after != before+1 {
		t.Errorf("merlon_alerts_total{scenario_id=test_metric_scenario,severity=high} = %v, want %v", after, before+1)
	}
}
