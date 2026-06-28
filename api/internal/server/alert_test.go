package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
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

	var alerts []domain.Alert
	json.NewDecoder(rec.Body).Decode(&alerts)

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

	var alerts []domain.Alert
	json.NewDecoder(rec.Body).Decode(&alerts)

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

	body := `{"status":"closed_false_positive","resolved_by":"analyst@example.com"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.Alert
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
