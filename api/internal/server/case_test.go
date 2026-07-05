package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	if c.Status != domain.CaseStatusNew {
		t.Errorf("status = %q, want %q", c.Status, domain.CaseStatusNew)
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

	cases, _ := decodeListResponse[domain.Case](t, rec.Body)
	if len(cases) < 1 {
		t.Errorf("expected at least 1 case, got %d", len(cases))
	}
}

func TestHandleListCases_CursorPagination(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	for _, summary := range []string{"Case A", "Case B", "Case C"} {
		body := `{"customer_id":"` + cust.ID + `","summary":"` + summary + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases?limit=2", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, meta1 := decodeListResponse[domain.Case](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatal("expected has_more with a next_cursor on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/cases?limit=2&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, meta2 := decodeListResponse[domain.Case](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if meta2.HasMore {
		t.Error("expected has_more = false on second page")
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

// TestCaseStatusChangeUpdatesGauge is Task 9 (overview.md §4.4 OPS-003):
// merlon_cases_open must track a case through its open sub-statuses and
// stop counting it once closed, without ever counting "closed" itself
// (closed cases are not "open" cases by definition).
func TestCaseStatusChangeUpdatesGauge(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	openBefore := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("new"))

	body := `{"customer_id":"` + cust.ID + `","summary":"Gauge test case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	openAfterCreate := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("new"))
	if openAfterCreate != openBefore+1 {
		t.Errorf("merlon_cases_open{status=new} after create = %v, want %v", openAfterCreate, openBefore+1)
	}

	investigatingBefore := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("investigating"))

	body = `{"status":"investigating"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	openAfterInvestigating := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("new"))
	if openAfterInvestigating != openAfterCreate-1 {
		t.Errorf("merlon_cases_open{status=new} after moving to investigating = %v, want %v", openAfterInvestigating, openAfterCreate-1)
	}
	investigatingAfter := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("investigating"))
	if investigatingAfter != investigatingBefore+1 {
		t.Errorf("merlon_cases_open{status=investigating} = %v, want %v", investigatingAfter, investigatingBefore+1)
	}

	body = `{"status":"closed"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	investigatingAfterClose := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("investigating"))
	if investigatingAfterClose != investigatingAfter-1 {
		t.Errorf("merlon_cases_open{status=investigating} after closing = %v, want %v", investigatingAfterClose, investigatingAfter-1)
	}
	closedGauge := testutil.ToFloat64(metrics.CasesOpen.WithLabelValues("closed"))
	if closedGauge != 0 {
		t.Errorf("merlon_cases_open{status=closed} = %v, want 0 (closed is never counted as open)", closedGauge)
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

	closeCase(t, s, created.ID)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+created.ID, nil)
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

func TestCaseNoteAuthorFromPrincipal(t *testing.T) {
	s := testServerWithAuth()
	apiKey := createAPIKey(t, s, "case-test", domain.RoleAdmin)

	body := `{"external_id":"CASE_AUTH","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	body = `{"customer_id":"` + cust.ID + `","summary":"Auth case"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	body = `{"content":"Note from principal"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+created.ID+"/notes", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var note domain.CaseNote
	json.NewDecoder(rec.Body).Decode(&note)
	if !strings.HasPrefix(note.Author, "apikey:") {
		t.Errorf("author = %q, want prefix 'apikey:'", note.Author)
	}
}

// closeCase moves a freshly created case (status "new") straight to
// "closed" so reopen tests have a valid starting point (new -> investigating
// -> closed, since new cannot close directly per the transition diagram).
func closeCase(t *testing.T, s *Server, caseID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseID, strings.NewReader(`{"status":"investigating"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("move to investigating failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseID, strings.NewReader(`{"status":"closed"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("close failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateCase_ReopenRequiresReason(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Reopen test case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	closeCase(t, s, created.ID)

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(`{"status":"reopened"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(`{"status":"reopened","reason":"New evidence surfaced"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var reopened domain.Case
	json.NewDecoder(rec.Body).Decode(&reopened)
	if reopened.Status != domain.CaseStatusReopened {
		t.Errorf("status = %q, want %q", reopened.Status, domain.CaseStatusReopened)
	}
	if reopened.ReopenReason != "New evidence surfaced" {
		t.Errorf("reopen_reason = %q, want %q", reopened.ReopenReason, "New evidence surfaced")
	}
}

func TestHandleUpdateCase_ReopenRequiresAnalystOrAbove(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	viewerKey := createAPIKeyAs(t, s, adminKey, "viewer", domain.RoleViewer)

	custBody := `{"external_id":"CASE_REOPEN_PERM","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(custBody))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var cust domain.Customer
	json.NewDecoder(rec.Body).Decode(&cust)

	body := `{"customer_id":"` + cust.ID + `","summary":"Reopen perm test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	for _, status := range []string{"investigating", "closed"} {
		req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(`{"status":"`+status+`"}`))
		req.Header.Set("Authorization", "Bearer "+adminKey)
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("move to %s failed: %d %s", status, rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(`{"status":"reopened","reason":"test"}`))
	req.Header.Set("Authorization", "Bearer "+viewerKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleUpdateCase_InvalidTransitionReturns400(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Invalid transition test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	// "new" cannot jump directly to "closed" per the transition diagram.
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(`{"status":"closed"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
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
