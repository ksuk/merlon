package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/store"
)

func testServer() *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
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
