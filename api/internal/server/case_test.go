package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
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

func TestHandleListCases_RiskSortRanksCriticalFirst(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	priorities := []domain.CasePriority{
		domain.CasePriorityLow,
		domain.CasePriorityMedium,
		domain.CasePriorityHigh,
		domain.CasePriorityCritical,
	}
	for i, priority := range priorities {
		if err := s.cases.Create(context.Background(), &domain.Case{
			ID: fmt.Sprintf("risk-api-case-%d", i), CustomerID: cust.ID,
			Status: domain.CaseStatusInvestigating, Priority: priority,
			Summary: "risk queue test", CreatedAt: created, UpdatedAt: created,
		}); err != nil {
			t.Fatalf("create case: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases?sort=risk&limit=4", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	cases, meta := decodeListResponse[domain.Case](t, rec.Body)
	if meta.HasMore {
		t.Error("has_more = true, want false")
	}
	want := []string{"risk-api-case-3", "risk-api-case-2", "risk-api-case-1", "risk-api-case-0"}
	for i, id := range want {
		if cases[i].ID != id {
			t.Errorf("cases[%d].ID = %q, want %q", i, cases[i].ID, id)
		}
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

// TestCaseStatusChangeUpdatesGauge is Task 9 (the operational design §4.4 OPS-003):
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

func createTestCustomerWithExternalID(t *testing.T, s *Server, externalID string) domain.Customer {
	t.Helper()
	body := `{"external_id":"` + externalID + `","customer_type":"individual","country_code":"JP"}`
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

func TestHandleGetRelatedCases_ReturnsSameCustomerHistory(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"First case for customer"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var first domain.Case
	json.NewDecoder(rec.Body).Decode(&first)

	body = `{"customer_id":"` + cust.ID + `","summary":"Second case for customer"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var second domain.Case
	json.NewDecoder(rec.Body).Decode(&second)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+first.ID+"/related", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var related []relatedCase
	json.NewDecoder(rec.Body).Decode(&related)
	if len(related) != 1 {
		t.Fatalf("expected 1 related case, got %d", len(related))
	}
	if related[0].Case.ID != second.ID {
		t.Errorf("related case id = %q, want %q", related[0].Case.ID, second.ID)
	}
	if related[0].LinkType != "auto" {
		t.Errorf("link_type = %q, want %q", related[0].LinkType, "auto")
	}
}

func TestHandleGetRelatedCases_IncludesManualLinks(t *testing.T) {
	s := testServerFull()
	custA := createTestCustomer(t, s)
	custB := createTestCustomerWithExternalID(t, s, "CUST_RELATED_B")

	body := `{"customer_id":"` + custA.ID + `","summary":"Case for customer A"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var caseA domain.Case
	json.NewDecoder(rec.Body).Decode(&caseA)

	body = `{"customer_id":"` + custB.ID + `","summary":"Case for customer B"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var caseB domain.Case
	json.NewDecoder(rec.Body).Decode(&caseB)

	linkBody := `{"related_case_id":"` + caseB.ID + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseA.ID+"/related", strings.NewReader(linkBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("link status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+caseA.ID+"/related", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var related []relatedCase
	json.NewDecoder(rec.Body).Decode(&related)

	if len(related) != 1 {
		t.Fatalf("expected 1 related case, got %d", len(related))
	}
	if related[0].Case.ID != caseB.ID {
		t.Errorf("related case id = %q, want %q", related[0].Case.ID, caseB.ID)
	}
	if related[0].LinkType != "manual" {
		t.Errorf("link_type = %q, want %q", related[0].LinkType, "manual")
	}
}

func TestHandleAddRelatedCase_ManualLink(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Case one"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var caseOne domain.Case
	json.NewDecoder(rec.Body).Decode(&caseOne)

	body = `{"customer_id":"` + cust.ID + `","summary":"Case two"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var caseTwo domain.Case
	json.NewDecoder(rec.Body).Decode(&caseTwo)

	linkBody := `{"related_case_id":"` + caseTwo.ID + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseOne.ID+"/related", strings.NewReader(linkBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domain.Case
	json.NewDecoder(rec.Body).Decode(&updated)
	if len(updated.RelatedCaseIDs) != 1 || updated.RelatedCaseIDs[0] != caseTwo.ID {
		t.Errorf("related_case_ids = %v, want [%q]", updated.RelatedCaseIDs, caseTwo.ID)
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

// TestCaseUpdateSucceedsWithCurrentUpdatedAt verifies a PATCH that supplies
// the case's current updated_at as expected_updated_at succeeds (WS-11 Task
// 8, the data model §3.9 optimistic locking).
func TestCaseUpdateSucceedsWithCurrentUpdatedAt(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Optimistic lock case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	updateBody := `{"status":"investigating","expected_updated_at":"` + created.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(updateBody))
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
}

// TestCaseUpdateConflictReturns409 verifies a PATCH that supplies a stale
// expected_updated_at (i.e. another update happened after the client last
// read the case) is rejected with 409 rather than silently overwriting the
// intervening change (the data model §3.9 "後勝ちを許容せず409").
func TestCaseUpdateConflictReturns409(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	body := `{"customer_id":"` + cust.ID + `","summary":"Optimistic lock conflict case"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var created domain.Case
	json.NewDecoder(rec.Body).Decode(&created)

	// Ensure the next update's timestamp is distinguishable from created's.
	time.Sleep(2 * time.Millisecond)

	// An intervening update (no lock requested) bumps updated_at.
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(`{"assigned_to":"analyst02"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("intervening update status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// A second client submits an update using the now-stale updated_at it
	// read before the intervening update.
	staleBody := `{"status":"investigating","expected_updated_at":"` + created.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+created.ID, strings.NewReader(staleBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}
