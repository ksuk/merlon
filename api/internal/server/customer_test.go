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
)

func TestCreateCustomer(t *testing.T) {
	s := testServer()

	body := `{"external_id":"EXT001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var c domain.Customer
	json.NewDecoder(rec.Body).Decode(&c)

	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	if c.ExternalID != "EXT001" {
		t.Errorf("external_id = %q, want %q", c.ExternalID, "EXT001")
	}
	if c.CustomerType != domain.CustomerTypeIndividual {
		t.Errorf("customer_type = %q, want %q", c.CustomerType, domain.CustomerTypeIndividual)
	}
}

func TestCreateCustomerMissingExternalID(t *testing.T) {
	s := testServer()

	body := `{"customer_type":"individual"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetCustomer(t *testing.T) {
	s := testServer()

	// Create
	body := `{"external_id":"EXT002","customer_type":"corporate_domestic","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	// Get
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got domain.Customer
	json.NewDecoder(rec.Body).Decode(&got)

	if got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
}

func TestGetCustomerNotFound(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListCustomers(t *testing.T) {
	s := testServer()

	// Create two customers
	for _, ext := range []string{"EXT010", "EXT011"} {
		body := `{"external_id":"` + ext + `","customer_type":"individual","country_code":"JP"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var customers []domain.Customer
	json.NewDecoder(rec.Body).Decode(&customers)

	if len(customers) != 2 {
		t.Errorf("len = %d, want 2", len(customers))
	}
}

func TestUpdateCustomer(t *testing.T) {
	s := testServer()

	// Create
	body := `{"external_id":"EXT020","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	// Update
	updateBody := `{"country_code":"US","attributes":{"pep":"true"}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/customers/"+created.ID, strings.NewReader(updateBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.Customer
	json.NewDecoder(rec.Body).Decode(&updated)

	if updated.CountryCode != "US" {
		t.Errorf("country_code = %q, want %q", updated.CountryCode, "US")
	}
	if updated.Attributes["pep"] != "true" {
		t.Errorf("attributes[pep] = %q, want %q", updated.Attributes["pep"], "true")
	}
}

func TestScoreCustomer(t *testing.T) {
	s := testServer()

	// Create customer
	body := `{"external_id":"SCORE001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	// Score
	scoreBody := `{"rule_set_id":"test_preset"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var record domain.ScoreRecord
	json.NewDecoder(rec.Body).Decode(&record)

	if record.Score != 2.5 {
		t.Errorf("score = %f, want 2.5", record.Score)
	}
	if record.Tier != domain.RiskTierMedium {
		t.Errorf("tier = %q, want %q", record.Tier, domain.RiskTierMedium)
	}

	// Verify customer was updated
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var updated domain.Customer
	json.NewDecoder(rec.Body).Decode(&updated)

	if updated.RiskScore == nil || *updated.RiskScore != 2.5 {
		t.Errorf("risk_score not updated")
	}
	if updated.RiskTier == nil || *updated.RiskTier != domain.RiskTierMedium {
		t.Errorf("risk_tier not updated")
	}

	// Verify score history
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID+"/scores", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var history []domain.ScoreRecord
	json.NewDecoder(rec.Body).Decode(&history)

	if len(history) != 1 {
		t.Errorf("score history len = %d, want 1", len(history))
	}
}

func TestScoreCustomerNotFound(t *testing.T) {
	s := testServer()

	body := `{"rule_set_id":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/nonexistent/score", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestScoreCustomerNoEngine(t *testing.T) {
	s := testServerWithEngine(nil, nil)

	// Create customer first
	body := `{"external_id":"NOENG","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	scoreBody := `{"rule_set_id":"test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/score", strings.NewReader(scoreBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestScreenCustomerHit(t *testing.T) {
	mockScreening := &engine.MockScreeningEngine{
		Result: &domain.ScreenResult{
			CustomerID:   "will-be-overridden",
			Hit:          true,
			Matches:      []domain.ScreenMatch{{ListID: "sanctions", EntryID: "S001", MatchedName: "Test Name", Similarity: 0.95, ListType: "sanctions", Source: "test"}},
			ListsChecked: 1,
			ScreenedAt:   time.Now(),
		},
	}
	s := testServerWithEngines(
		&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		&engine.MockMonitoringEngine{},
		mockScreening,
	)

	body := `{"external_id":"SCR001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	screenBody := `{"list_ids":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/screen", strings.NewReader(screenBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result domain.ScreenResult
	json.NewDecoder(rec.Body).Decode(&result)

	if !result.Hit {
		t.Error("expected hit=true")
	}
	if len(result.Matches) != 1 {
		t.Errorf("matches len = %d, want 1", len(result.Matches))
	}
}

func TestScreenCustomerNoHit(t *testing.T) {
	s := testServer()

	body := `{"external_id":"SCR002","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	screenBody := `{"list_ids":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/screen", strings.NewReader(screenBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result domain.ScreenResult
	json.NewDecoder(rec.Body).Decode(&result)

	if result.Hit {
		t.Error("expected hit=false")
	}
}

func TestScreenCustomerNotFound(t *testing.T) {
	s := testServer()

	screenBody := `{"list_ids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/nonexistent/screen", strings.NewReader(screenBody))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestScreenCustomerNoEngine(t *testing.T) {
	s := testServerWithEngines(nil, nil, nil)

	body := `{"external_id":"SCR003","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Customer
	json.NewDecoder(rec.Body).Decode(&created)

	screenBody := `{"list_ids":[]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+created.ID+"/screen", strings.NewReader(screenBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
