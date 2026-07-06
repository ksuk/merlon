package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func TestSetup_FirstCallSucceeds(t *testing.T) {
	users := store.NewMemoryUserRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     users,
	})

	body := `{"email":"admin@example.com","password":"correct-horse-battery-staple"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp meResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Role != domain.RoleAdmin {
		t.Errorf("role = %s, want %s", resp.Role, domain.RoleAdmin)
	}

	count, err := users.Count(req.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}
}

func TestSetup_SecondCallRejected(t *testing.T) {
	users := store.NewMemoryUserRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     users,
	})

	body := `{"email":"admin@example.com","password":"correct-horse-battery-staple"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first setup status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	body = `{"email":"second-admin@example.com","password":"correct-horse-battery-staple"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict && rec.Code != http.StatusForbidden {
		t.Errorf("second setup status = %d, want %d or %d", rec.Code, http.StatusConflict, http.StatusForbidden)
	}
	assertErrorCode(t, rec, "conflict")
}
