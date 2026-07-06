package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func testServerWithAccounts() (*Server, *store.MemoryCustomerRepo) {
	customers := store.NewMemoryCustomerRepo()
	s := New(":0", Deps{
		Customers: customers,
		Accounts:  store.NewMemoryAccountRepo(customers),
	})
	return s, customers
}

// testServerWithAccountsAndTransactions additionally wires Transactions, for
// tests exercising transactions.account_id (WS-11 Task 4).
func testServerWithAccountsAndTransactions() (*Server, *store.MemoryCustomerRepo) {
	customers := store.NewMemoryCustomerRepo()
	s := New(":0", Deps{
		Customers:    customers,
		Transactions: store.NewMemoryTransactionRepo(),
		Accounts:     store.NewMemoryAccountRepo(customers),
	})
	return s, customers
}

func createTestCustomerForAccount(t *testing.T, s *Server, externalID string) string {
	t.Helper()
	body := fmt.Sprintf(`{"external_id":%q,"customer_type":"individual","country_code":"JP"}`, externalID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create customer status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var c domain.Customer
	if err := json.NewDecoder(rec.Body).Decode(&c); err != nil {
		t.Fatalf("decode customer: %v", err)
	}
	return c.ID
}

func TestAccountCreateJointWithMultipleCustomers(t *testing.T) {
	s, _ := testServerWithAccounts()

	body := `{"external_id":"ACC-001","account_type":"joint"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var acc domain.Account
	json.NewDecoder(rec.Body).Decode(&acc)
	if acc.ID == "" {
		t.Fatal("expected non-empty account ID")
	}

	primaryID := createTestCustomerForAccount(t, s, "ACC-CUST-PRIMARY")
	coHolderID := createTestCustomerForAccount(t, s, "ACC-CUST-COHOLDER")

	for _, tc := range []struct {
		customerID string
		role       string
	}{
		{primaryID, "primary"},
		{coHolderID, "co_holder"},
	} {
		body := fmt.Sprintf(`{"customer_id":%q,"role":%q}`, tc.customerID, tc.role)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+acc.ID+"/customers", bytes.NewReader([]byte(body)))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("add customer (role=%s) status = %d, body: %s", tc.role, rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+acc.ID+"/customers", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list customers status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var links []domain.AccountCustomer
	json.NewDecoder(rec.Body).Decode(&links)
	if len(links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(links))
	}
}

// TestAccountScreeningAPIListsAllLinkedCustomers verifies data-model.md
// §1.1.3: every customer linked to a joint account can be enumerated
// individually for screening, not just the account's representative holder.
func TestAccountScreeningAPIListsAllLinkedCustomers(t *testing.T) {
	s, _ := testServerWithAccounts()

	body := `{"external_id":"ACC-SCREEN","account_type":"joint"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var acc domain.Account
	json.NewDecoder(rec.Body).Decode(&acc)

	customerIDs := []string{
		createTestCustomerForAccount(t, s, "SCREEN-CUST-1"),
		createTestCustomerForAccount(t, s, "SCREEN-CUST-2"),
		createTestCustomerForAccount(t, s, "SCREEN-CUST-3"),
	}
	roles := []string{"primary", "co_holder", "co_holder"}
	for i, cid := range customerIDs {
		body := fmt.Sprintf(`{"customer_id":%q,"role":%q}`, cid, roles[i])
		req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+acc.ID+"/customers", bytes.NewReader([]byte(body)))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("add customer status = %d, body: %s", rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+acc.ID+"/customers", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list customers status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var links []domain.AccountCustomer
	json.NewDecoder(rec.Body).Decode(&links)
	if len(links) != len(customerIDs) {
		t.Fatalf("len(links) = %d, want %d", len(links), len(customerIDs))
	}
	seen := make(map[string]bool)
	for _, l := range links {
		seen[l.CustomerID] = true
	}
	for _, cid := range customerIDs {
		if !seen[cid] {
			t.Errorf("customer %s missing from linked-customers listing", cid)
		}
	}
}

func TestAccountGetNotFound(t *testing.T) {
	s, _ := testServerWithAccounts()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAccountCreateRejectsInvalidAccountType(t *testing.T) {
	s, _ := testServerWithAccounts()
	body := `{"external_id":"ACC-BAD","account_type":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
