package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func createTestCustomerAndAlert(t *testing.T, s *Server) (domain.Customer, domain.Alert) {
	t.Helper()

	// Create customer
	custBody := `{"external_id":"STR001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(custBody))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	// Create transaction
	txBody := `{"customer_id":"` + cust.ID + `","external_id":"TX_STR001","amount":500000,"currency":"JPY","direction":"inbound","channel":"web","counterparty_id":"CP1","counterparty_country":"JP"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(txBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var txn domain.Transaction
	json.NewDecoder(rec.Body).Decode(&txn)

	// Create alert directly in store
	now := time.Now()
	alert := domain.Alert{
		ID:             "ALT-TEST-001",
		CustomerID:     cust.ID,
		ScenarioID:     "test_structuring",
		Severity:       domain.AlertSeverityHigh,
		Status:         domain.AlertStatusOpen,
		Score:          1.5,
		Description:    "Suspicious structuring pattern",
		TransactionIDs: []string{txn.ID},
		DetectedAt:     now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.alerts.Create(req.Context(), &alert)

	return cust, alert
}

func TestCreateSTR(t *testing.T) {
	s := testServer()
	_, alert := createTestCustomerAndAlert(t, s)

	body := `{"alert_id":"` + alert.ID + `","suspicious_point":"分割取引の疑い","created_by":"analyst01"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var report domain.STRReport
	json.NewDecoder(rec.Body).Decode(&report)

	if report.ID == "" {
		t.Error("expected non-empty ID")
	}
	if report.AlertID != alert.ID {
		t.Errorf("alert_id = %q, want %q", report.AlertID, alert.ID)
	}
	if report.Status != domain.ReportStatusDraft {
		t.Errorf("status = %q, want %q", report.Status, domain.ReportStatusDraft)
	}
	if report.TotalAmount != 500000 {
		t.Errorf("total_amount = %f, want 500000", report.TotalAmount)
	}
}

func TestCreateSTRMissingAlert(t *testing.T) {
	s := testServer()

	body := `{"alert_id":"nonexistent","suspicious_point":"test","created_by":"analyst"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateSTRMissingSuspiciousPoint(t *testing.T) {
	s := testServer()

	body := `{"alert_id":"ALT-001"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestExportSTRCSV(t *testing.T) {
	s := testServer()
	_, alert := createTestCustomerAndAlert(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?alert_id="+alert.ID+"&format=csv", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "report_id") {
		t.Error("CSV should contain header row")
	}
	if !strings.Contains(body, alert.ID) {
		t.Error("CSV should contain alert ID")
	}
}

func TestExportSTRJSON(t *testing.T) {
	s := testServer()
	_, alert := createTestCustomerAndAlert(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?alert_id="+alert.ID+"&format=json", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var export strExportJSON
	json.NewDecoder(rec.Body).Decode(&export)

	if export.AlertID != alert.ID {
		t.Errorf("alert_id = %q, want %q", export.AlertID, alert.ID)
	}
	if export.Customer.ExternalID != "STR001" {
		t.Errorf("customer external_id = %q, want %q", export.Customer.ExternalID, "STR001")
	}
}

func TestExportSTRMissingAlertID(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
