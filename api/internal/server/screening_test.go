package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/screening"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func TestHandleScreeningCheck_ExplicitRequestScreensCustomer(t *testing.T) {
	mockScreening := &engine.MockScreeningEngine{
		Result: &domain.ScreenResult{
			Hit:          true,
			Matches:      []domain.ScreenMatch{{ListID: "mof_japan", EntryID: "MOF-001", MatchedName: "Kim Jong Un", Similarity: 0.97, ListType: "sanctions", Source: "test"}},
			ListsChecked: 1,
			ScreenedAt:   time.Now(),
		},
	}
	s := testServerWithEngines(
		&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		&engine.MockMonitoringEngine{},
		mockScreening,
	)

	body := `{"external_id":"CHK001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created customer: %v", err)
	}

	checkBody := `{"customer_id":"` + created.ID + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(checkBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result screening.BatchResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Trigger != screening.TriggerAPIRequest {
		t.Errorf("Trigger = %q, want %q", result.Trigger, screening.TriggerAPIRequest)
	}
	if len(result.Outcomes) != 1 || !result.Outcomes[0].Screened || result.Outcomes[0].CustomerID != created.ID {
		t.Errorf("outcomes = %+v, want single screened outcome for %q", result.Outcomes, created.ID)
	}
}

func TestHandleScreeningCheck_MissingCustomerIDRejected(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleScreeningCheck_UnknownCustomerNotFound(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{"customer_id":"does-not-exist"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleScreeningCheck_NoEngineConfigured(t *testing.T) {
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/check", strings.NewReader(`{"customer_id":"whatever"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
