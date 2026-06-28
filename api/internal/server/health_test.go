package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func testServer() *Server {
	return testServerWithEngines(
		&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		&engine.MockMonitoringEngine{},
		&engine.MockScreeningEngine{},
	)
}

func testServerWithEngine(scoring engine.ScoringEngine, monitoring engine.MonitoringEngine) *Server {
	return testServerWithEngines(scoring, monitoring, &engine.MockScreeningEngine{})
}

func testServerWithEngines(scoring engine.ScoringEngine, monitoring engine.MonitoringEngine, screening engine.ScreeningEngine) *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      scoring,
		Monitoring:   monitoring,
		Screening:    screening,
	})
}

func TestHandleHealth(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	if body := rec.Body.String(); !strings.Contains(body, "ok") {
		t.Errorf("body = %q, want it to contain %q", body, "ok")
	}
}
