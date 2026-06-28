package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func TestCreateCase(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Suspicious structuring detected","priority":"high","assigned_to":"analyst01"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var c domain.Case
	json.NewDecoder(rec.Body).Decode(&c)

	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	if c.CustomerID != cust.ID {
		t.Errorf("customer_id = %q, want %q", c.CustomerID, cust.ID)
	}
	if c.Status != domain.CaseStatusOpen {
		t.Errorf("status = %q, want %q", c.Status, domain.CaseStatusOpen)
	}
	if c.Priority != domain.CasePriorityHigh {
		t.Errorf("priority = %q, want %q", c.Priority, domain.CasePriorityHigh)
	}
	if c.AssignedTo != "analyst01" {
		t.Errorf("assigned_to = %q, want %q", c.AssignedTo, "analyst01")
	}
}

func TestCreateCaseMissingSummary(t *testing.T) {
	s := testServerFull()
	body := `{"customer_id":"CUST1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateCaseCustomerNotFound(t *testing.T) {
	s := testServerFull()
	body := `{"customer_id":"nonexistent","summary":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetCase(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Test case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var fetched domain.Case
	json.NewDecoder(rec.Body).Decode(&fetched)
	if fetched.ID != created.ID {
		t.Errorf("id = %q, want %q", fetched.ID, created.ID)
	}
}

func TestListCases(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Case 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cases", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var cases []domain.Case
	json.NewDecoder(rec.Body).Decode(&cases)
	if len(cases) < 1 {
		t.Errorf("expected at least 1 case, got %d", len(cases))
	}
}

func TestUpdateCase(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Test case","priority":"low"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	body = `{"status":"investigating","assigned_to":"senior_analyst"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.Case
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated.Status != domain.CaseStatusInvestigating {
		t.Errorf("status = %q, want %q", updated.Status, domain.CaseStatusInvestigating)
	}
	if updated.AssignedTo != "senior_analyst" {
		t.Errorf("assigned_to = %q, want %q", updated.AssignedTo, "senior_analyst")
	}
}

func TestCloseCase(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Will close"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	body = `{"status":"closed"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var closed domain.Case
	json.NewDecoder(rec.Body).Decode(&closed)
	if closed.ClosedAt == nil {
		t.Error("expected closed_at to be set")
	}
}

func TestAddCaseNote(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Note case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	body = `{"author":"analyst01","content":"Reviewed transactions - confirmed structuring pattern"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+created.ID+"/notes", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var note domain.CaseNote
	json.NewDecoder(rec.Body).Decode(&note)
	if note.ID == "" {
		t.Error("expected non-empty note ID")
	}
	if note.Content != "Reviewed transactions - confirmed structuring pattern" {
		t.Errorf("unexpected content: %q", note.Content)
	}

	// Verify note is in case
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var fetched domain.Case
	json.NewDecoder(rec.Body).Decode(&fetched)
	if len(fetched.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(fetched.Notes))
	}
}

func TestAddCaseNoteEmptyContent(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Note case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	body = `{"author":"analyst01","content":""}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+created.ID+"/notes", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
