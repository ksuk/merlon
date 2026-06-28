package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
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
