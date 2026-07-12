package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func testServerWithWhitelist() *Server {
	return New(":0", Deps{
		Customers:      store.NewMemoryCustomerRepo(),
		Transactions:   store.NewMemoryTransactionRepo(),
		Alerts:         store.NewMemoryAlertRepo(),
		Scoring:        &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:     &engine.MockMonitoringEngine{},
		Screening:      &engine.MockScreeningEngine{},
		Backtest:       &engine.MockBacktestEngine{},
		Audit:          store.NewMemoryAuditRepo(),
		Cases:          store.NewMemoryCaseRepo(),
		Whitelist:      store.NewMemoryWhitelistRepo(),
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
	})
}

func createWhitelistTestCustomer(t *testing.T, s *Server, adminKey string) domain.Customer {
	t.Helper()
	body := `{"external_id":"WL_CUST_` + generateID() + `","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create customer failed: %d %s", rec.Code, rec.Body.String())
	}
	var c domain.Customer
	json.NewDecoder(rec.Body).Decode(&c)
	return c
}

func createWhitelistEntry(t *testing.T, s *Server, requesterKey, customerID, validUntil string) domain.WhitelistEntry {
	t.Helper()
	body := `{"customer_id":"` + customerID + `","reason":"trusted long-standing customer","valid_until":"` + validUntil + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+requesterKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create whitelist entry failed: %d %s", rec.Code, rec.Body.String())
	}
	var e domain.WhitelistEntry
	json.NewDecoder(rec.Body).Decode(&e)
	return e
}

func TestHandleCreateWhitelistEntry_RequiresReasonAndValidUntil(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)

	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	cases := []struct {
		name string
		body string
	}{
		{"missing reason", `{"customer_id":"` + cust.ID + `","valid_until":"` + validUntil + `"}`},
		{"empty reason", `{"customer_id":"` + cust.ID + `","reason":"","valid_until":"` + validUntil + `"}`},
		{"missing valid_until", `{"customer_id":"` + cust.ID + `","reason":"trusted customer"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+adminKey)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestHandleCreateWhitelistEntry_RejectsValidUntilBeyondMaxPeriod(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)

	// Default max is 365 days (WL-002); 400 days exceeds it.
	tooFar := time.Now().Add(400 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"customer_id":"` + cust.ID + `","reason":"trusted customer","valid_until":"` + tooFar + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCreateWhitelistEntry_UsesConfiguredMaxValidPeriod(t *testing.T) {
	s := New(":0", Deps{
		Customers:             store.NewMemoryCustomerRepo(),
		Whitelist:             store.NewMemoryWhitelistRepo(),
		APIKeys:               store.NewMemoryAPIKeyRepo(),
		BootstrapToken:        testBootstrapToken,
		WhitelistMaxValidDays: 30,
	})
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)

	// 60 days exceeds the configured 30-day max, even though it is well
	// within the 365-day hardcoded default.
	tooFar := time.Now().Add(60 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"customer_id":"` + cust.ID + `","reason":"trusted customer","valid_until":"` + tooFar + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	withinConfigured := time.Now().Add(20 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body2 := `{"customer_id":"` + cust.ID + `","reason":"trusted customer","valid_until":"` + withinConfigured + `"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer "+adminKey)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusCreated, rec2.Body.String())
	}
}

func TestHandleCreateWhitelistEntry_RejectsPastValidUntil(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)

	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"customer_id":"` + cust.ID + `","reason":"trusted customer","valid_until":"` + past + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleApproveWhitelistEntry_RejectsSameUserAsRequester(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	entry := createWhitelistEntry(t, s, adminKey, cust.ID, validUntil)

	// Same admin key both requested and is now attempting to approve.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleApproveWhitelistEntry_SetsActiveStatus(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	entry := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var approved domain.WhitelistEntry
	json.NewDecoder(rec.Body).Decode(&approved)
	if approved.Status != domain.WhitelistEntryStatusActive {
		t.Errorf("status = %q, want %q", approved.Status, domain.WhitelistEntryStatusActive)
	}
	if approved.ApprovedBy == nil || *approved.ApprovedBy == "" {
		t.Error("expected approved_by to be set")
	}
	if approved.ApprovedAt == nil {
		t.Error("expected approved_at to be set")
	}
}

func TestHandleApproveWhitelistEntry_RequiresApprovePermission(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst2", domain.RoleAnalyst)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	entry := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)

	// A second analyst (not the requester) still lacks whitelist:approve.
	otherAnalystKey := createAPIKeyAs(t, s, adminKey, "analyst3", domain.RoleAnalyst)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+otherAnalystKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleApproveWhitelistEntry_OptimisticLockConflict(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	admin2Key := createAPIKeyAs(t, s, adminKey, "admin2", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	entry := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)

	// Two concurrent approvals racing on the same pending_approval entry:
	// exactly one must win (200) and the other must see the version bumped
	// out from under it (409), proving optimistic locking (whitelist.md §3.1).
	var wg sync.WaitGroup
	codes := make([]int, 2)
	keys := []string{adminKey, admin2Key}
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/approve", nil)
			req.Header.Set("Authorization", "Bearer "+keys[i])
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("codes = %v, want exactly one 200 and one 409", codes)
	}
}

func TestHandleRevokeWhitelistEntry_NoApprovalRequired(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	// Revoke straight from pending_approval (withdrawal), no approval needed.
	pending := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+pending.ID+"/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+analystKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke pending: status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var revoked domain.WhitelistEntry
	json.NewDecoder(rec.Body).Decode(&revoked)
	if revoked.Status != domain.WhitelistEntryStatusRevoked {
		t.Errorf("status = %q, want %q", revoked.Status, domain.WhitelistEntryStatusRevoked)
	}

	// Revoke from active (post-approval), also no approval needed.
	cust2 := createWhitelistTestCustomer(t, s, adminKey)
	active := createWhitelistEntry(t, s, analystKey, cust2.ID, validUntil)
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+active.ID+"/approve", nil)
	approveReq.Header.Set("Authorization", "Bearer "+adminKey)
	approveRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve: status = %d, body: %s", approveRec.Code, approveRec.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+active.ID+"/revoke", nil)
	revokeReq.Header.Set("Authorization", "Bearer "+analystKey)
	revokeRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("revoke active: status = %d, want %d, body: %s", revokeRec.Code, http.StatusOK, revokeRec.Body.String())
	}
	var revokedActive domain.WhitelistEntry
	json.NewDecoder(revokeRec.Body).Decode(&revokedActive)
	if revokedActive.Status != domain.WhitelistEntryStatusRevoked {
		t.Errorf("status = %q, want %q", revokedActive.Status, domain.WhitelistEntryStatusRevoked)
	}
}

func TestHandleCreateWhitelistEntry_ActiveCustomerConflict(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	admin2Key := createAPIKeyAs(t, s, adminKey, "admin2", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	first := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)
	approveFirst := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+first.ID+"/approve", nil)
	approveFirst.Header.Set("Authorization", "Bearer "+adminKey)
	approveFirstRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(approveFirstRec, approveFirst)
	if approveFirstRec.Code != http.StatusOK {
		t.Fatalf("approve first: status = %d, body: %s", approveFirstRec.Code, approveFirstRec.Body.String())
	}

	// A second application for the same customer is allowed at request time...
	second := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)

	// ...but approving it while the first is still active must conflict
	// (partial unique index on active entries, whitelist.md §3.1).
	approveSecond := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+second.ID+"/approve", nil)
	approveSecond.Header.Set("Authorization", "Bearer "+admin2Key)
	approveSecondRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(approveSecondRec, approveSecond)
	if approveSecondRec.Code != http.StatusConflict {
		t.Fatalf("approve second: status = %d, want %d, body: %s", approveSecondRec.Code, http.StatusConflict, approveSecondRec.Body.String())
	}
}

func TestHandleGetWhitelistEntry_NotFound(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/whitelist/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleListWhitelistEntries(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	createWhitelistEntry(t, s, adminKey, cust.ID, validUntil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/whitelist", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, _ := decodeListResponse[domain.WhitelistEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Errorf("expected at least 1 entry, got %d", len(entries))
	}
}

// createActiveWhitelistEntryViaAPI drives the request+approve flow through
// the HTTP handlers so review tests exercise a real status=active entry.
func createActiveWhitelistEntryViaAPI(t *testing.T, s *Server, adminKey, requesterKey, customerID, validUntil string) domain.WhitelistEntry {
	t.Helper()
	entry := createWhitelistEntry(t, s, requesterKey, customerID, validUntil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve entry failed: %d %s", rec.Code, rec.Body.String())
	}
	var approved domain.WhitelistEntry
	json.NewDecoder(rec.Body).Decode(&approved)
	return approved
}

func getWhitelistEntry(t *testing.T, s *Server, key, id string) domain.WhitelistEntry {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whitelist/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get entry failed: %d %s", rec.Code, rec.Body.String())
	}
	var e domain.WhitelistEntry
	json.NewDecoder(rec.Body).Decode(&e)
	return e
}

func TestHandleCreateWhitelistReview_RenewedExtendsValidUntil(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	entry := createActiveWhitelistEntryViaAPI(t, s, adminKey, analystKey, cust.ID, validUntil)

	newValidUntil := time.Now().Add(300 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"decision":"renewed","review_notes":"still trustworthy","next_review_date":"2027-01-15","new_valid_until":"` + newValidUntil + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/reviews", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var review domain.WhitelistReview
	json.NewDecoder(rec.Body).Decode(&review)
	if review.Decision != domain.WhitelistReviewDecisionRenewed {
		t.Errorf("Decision = %q, want %q", review.Decision, domain.WhitelistReviewDecisionRenewed)
	}
	if review.NextReviewDate == nil {
		t.Error("expected next_review_date to be recorded")
	}

	updated := getWhitelistEntry(t, s, adminKey, entry.ID)
	if updated.Status != domain.WhitelistEntryStatusActive {
		t.Errorf("Status = %q, want %q", updated.Status, domain.WhitelistEntryStatusActive)
	}
	wantValidUntil, _ := time.Parse(time.RFC3339, newValidUntil)
	if !updated.ValidUntil.Equal(wantValidUntil) {
		t.Errorf("ValidUntil = %v, want %v", updated.ValidUntil, wantValidUntil)
	}
}

func TestHandleCreateWhitelistReview_RevokedExpiresEntry(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
	entry := createActiveWhitelistEntryViaAPI(t, s, adminKey, analystKey, cust.ID, validUntil)

	body := `{"decision":"revoked","review_notes":"no longer trusted"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/reviews", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	updated := getWhitelistEntry(t, s, adminKey, entry.ID)
	if updated.Status != domain.WhitelistEntryStatusExpired {
		t.Errorf("Status = %q, want %q", updated.Status, domain.WhitelistEntryStatusExpired)
	}
}

func TestAuditRecordsWhitelistOperations(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "analyst", domain.RoleAnalyst)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	entry := createWhitelistEntry(t, s, analystKey, cust.ID, validUntil)

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/approve", nil)
	approveReq.Header.Set("Authorization", "Bearer "+adminKey)
	approveRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve: status = %d, body: %s", approveRec.Code, approveRec.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist/"+entry.ID+"/revoke", nil)
	revokeReq.Header.Set("Authorization", "Bearer "+analystKey)
	revokeRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("revoke: status = %d, body: %s", revokeRec.Code, revokeRec.Body.String())
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=whitelist", nil)
	auditReq.Header.Set("Authorization", "Bearer "+adminKey)
	auditRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit: status = %d, body: %s", auditRec.Code, auditRec.Body.String())
	}

	logEntries, _ := decodeListResponse[domain.AuditEntry](t, auditRec.Body)

	wantActions := map[string]bool{"create": false, "approve_whitelist_entry": false, "revoke_whitelist_entry": false}
	for _, e := range logEntries {
		if e.ResourceType != "whitelist" {
			continue
		}
		if _, ok := wantActions[e.Action]; ok {
			wantActions[e.Action] = true
		}
	}
	for action, found := range wantActions {
		if !found {
			t.Errorf("expected an audit entry with action %q, got entries: %+v", action, logEntries)
		}
	}
}

func TestHandleCreateWhitelistEntry_PartialExclusion_StoresRuleIDs(t *testing.T) {
	s := testServerWithWhitelist()
	adminKey := createAPIKey(t, s, "admin", domain.RoleAdmin)
	cust := createWhitelistTestCustomer(t, s, adminKey)
	validUntil := time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)

	body := `{"customer_id":"` + cust.ID + `","reason":"partial exclusion","valid_until":"` + validUntil + `","excluded_rule_ids":["rule-a","rule-b"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/whitelist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created domain.WhitelistEntry
	json.NewDecoder(rec.Body).Decode(&created)
	if len(created.ExcludedRuleIDs) != 2 {
		t.Fatalf("ExcludedRuleIDs = %v, want 2 entries", created.ExcludedRuleIDs)
	}

	fetched := getWhitelistEntry(t, s, adminKey, created.ID)
	if len(fetched.ExcludedRuleIDs) != 2 || fetched.ExcludedRuleIDs[0] != "rule-a" || fetched.ExcludedRuleIDs[1] != "rule-b" {
		t.Errorf("fetched ExcludedRuleIDs = %v, want [rule-a rule-b]", fetched.ExcludedRuleIDs)
	}
}
