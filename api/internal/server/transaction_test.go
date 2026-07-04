package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func createTestCustomer(t *testing.T, s *Server) domain.Customer {
	t.Helper()
	body := `{"external_id":"TX_CUST","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create customer failed: %d %s", rec.Code, rec.Body.String())
	}
	var c domain.Customer
	json.NewDecoder(rec.Body).Decode(&c)
	return c
}

func TestCreateTransaction(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX001","amount":100000,"currency":"JPY","direction":"outbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var tx domain.Transaction
	json.NewDecoder(rec.Body).Decode(&tx)

	if tx.ID == "" {
		t.Error("expected non-empty ID")
	}
	if tx.Amount != 100000 {
		t.Errorf("amount = %f, want 100000", tx.Amount)
	}
}

func TestCreateTransactionMissingCustomer(t *testing.T) {
	s := testServer()

	body := `{"customer_id":"nonexistent","external_id":"TX002","amount":100,"direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransactionInvalidAmount(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX003","amount":-100,"direction":"inbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListTransactions(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	// Create 2 transactions
	for _, ext := range []string{"TX010", "TX011"} {
		body := `{"customer_id":"` + cust.ID + `","external_id":"` + ext + `","amount":50000,"currency":"JPY","direction":"inbound"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?customer_id="+cust.ID, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	txns, _ := decodeListResponse[domain.Transaction](t, rec.Body)

	if len(txns) != 2 {
		t.Errorf("len = %d, want 2", len(txns))
	}
}

func TestHandleListTransactions_CursorPagination(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	for _, ext := range []string{"TX030", "TX031", "TX032"} {
		body := `{"customer_id":"` + cust.ID + `","external_id":"` + ext + `","amount":1000,"currency":"JPY","direction":"inbound"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?customer_id="+cust.ID+"&limit=2", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, meta1 := decodeListResponse[domain.Transaction](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatal("expected has_more with a next_cursor on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?customer_id="+cust.ID+"&limit=2&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, meta2 := decodeListResponse[domain.Transaction](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if meta2.HasMore {
		t.Error("expected has_more = false on second page")
	}
}

func TestListTransactionsMissingCustomerID(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransactionInvalidDirection(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX004","amount":100,"direction":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransactionMissingDirection(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX005","amount":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetTransaction(t *testing.T) {
	s := testServer()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","external_id":"TX020","amount":75000,"currency":"JPY","direction":"outbound"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Transaction
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got domain.Transaction
	json.NewDecoder(rec.Body).Decode(&got)

	if got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
}
