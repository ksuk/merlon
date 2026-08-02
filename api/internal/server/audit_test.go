package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// mustCreateAuditEntry writes an audit entry directly through the
// repository (bypassing HTTP), so tests can control CreatedAt precisely for
// period-filter and pagination assertions (ALD-001/002).
func mustCreateAuditEntry(t *testing.T, s *Server, ctx context.Context, userID, resourceType, resourceID string, createdAt time.Time) {
	t.Helper()
	entry := &domain.AuditEntry{
		UserID:       userID,
		Action:       "create",
		ResourceType: resourceType,
		ResourceID:   resourceID,
		CreatedAt:    createdAt,
	}
	if err := s.audit.Create(ctx, entry); err != nil {
		t.Fatalf("create audit entry: %v", err)
	}
}

func TestDemoTourWritesAreAudited(t *testing.T) {
	s := testServerFull()
	cust := createTestCustomer(t, s)

	// Score the customer through the same endpoint used by the demo tour.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers/"+cust.ID+"/score", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("score status = %d, body: %s", rec.Code, rec.Body.String())
	}

	now := time.Now()
	txn := &domain.Transaction{
		ID: "audit-tour-txn", CustomerID: cust.ID, ExternalID: "AUDIT-TOUR-TXN", Amount: 1000,
		Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now, CreatedAt: now,
	}
	if err := s.transactions.Create(context.Background(), txn); err != nil {
		t.Fatalf("create transaction fixture: %v", err)
	}
	alert := &domain.Alert{
		ID: "audit-tour-alert", CustomerID: cust.ID, ScenarioID: "audit-tour", Severity: domain.AlertSeverityHigh,
		Status: domain.AlertStatusOpen, Score: 90, Description: "audit tour fixture", TransactionIDs: []string{txn.ID},
		DetectedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.alerts.Create(context.Background(), alert); err != nil {
		t.Fatalf("create alert fixture: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(`{"customer_id":"`+cust.ID+`","alert_ids":["`+alert.ID+`"],"summary":"audit tour","priority":"high"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("case status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var kase domain.Case
	if err := json.NewDecoder(rec.Body).Decode(&kase); err != nil {
		t.Fatalf("decode case: %v", err)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+kase.ID, strings.NewReader(`{"status":"investigating"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("case update status = %d, body: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+kase.ID+"/notes", strings.NewReader(`{"content":"audit tour note"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("case note status = %d, body: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(`{"alert_id":"`+alert.ID+`","suspicious_point":"audit tour"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("STR status = %d, body: %s", rec.Code, rec.Body.String())
	}

	entries, err := s.audit.List(context.Background(), domain.AuditListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list audit entries: %v", err)
	}
	found := map[string]bool{}
	for _, entry := range entries {
		key := entry.Action + ":" + entry.ResourceType + ":" + entry.ResourceID
		found[key] = true
	}
	for _, want := range []string{
		"score_customer:customers:" + cust.ID,
		"create:cases:" + kase.ID,
		"update_status:cases:" + kase.ID,
		"create_str:reports:str",
	} {
		if !found[want] {
			t.Errorf("missing audit entry %q", want)
		}
	}
}

func TestAuditMiddlewareRecordsWrite(t *testing.T) {
	s := testServerFull()

	body := `{"external_id":"AUD001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=customers", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Action != "create" {
		t.Errorf("action = %q, want %q", entry.Action, "create")
	}
	if entry.UserID != "anonymous" {
		t.Errorf("user_id = %q, want %q", entry.UserID, "anonymous")
	}
	if entry.ResourceType != "customers" {
		t.Errorf("resource_type = %q, want %q", entry.ResourceType, "customers")
	}
}

func TestAuditUserIDFromPrincipal(t *testing.T) {
	s := testServerWithAuth()
	apiKey := createAPIKey(t, s, "audit-test", domain.RoleAdmin)

	body := `{"external_id":"AUD_PRINCIPAL","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=customers", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if !strings.HasPrefix(entry.UserID, "apikey:") {
		t.Errorf("user_id = %q, want prefix 'apikey:'", entry.UserID)
	}
}

func TestAuditIPValidation(t *testing.T) {
	s := testServerFull()

	body := `{"external_id":"AUD_IP","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", "not-an-ip, 1.2.3.4")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=customers", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 audit entry, got %d", len(entries))
	}

	ip := entries[0].IPAddress
	if ip == "not-an-ip" {
		t.Errorf("invalid IP should be rejected, got %q", ip)
	}
}

func TestAuditUsesResolvedClientIPFromTrustedProxy(t *testing.T) {
	s := testServerFull()
	s.clientIPs = newClientIPResolver([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
	})

	body := `{"external_id":"AUD_PROXY_IP","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.2:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.42")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", rec.Code, http.StatusCreated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=customers", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 audit entry, got %d", len(entries))
	}
	if got := entries[0].IPAddress; got != "198.51.100.42" {
		t.Errorf("audit IP = %q, want resolved client IP", got)
	}
}

func TestAuditListPreservesIPv4IPv6AndNullIPAddresses(t *testing.T) {
	s := testServerFull()
	userID := "audit-handler-inet"
	base := time.Now().UTC()
	for _, entry := range []*domain.AuditEntry{
		{UserID: userID, Action: "ipv4", ResourceType: "audit", ResourceID: "ipv4", IPAddress: "192.0.2.10", CreatedAt: base},
		{UserID: userID, Action: "ipv6", ResourceType: "audit", ResourceID: "ipv6", IPAddress: "2001:db8::10", CreatedAt: base.Add(time.Second)},
		{UserID: userID, Action: "null", ResourceType: "audit", ResourceID: "null", CreatedAt: base.Add(2 * time.Second)},
	} {
		if err := s.audit.Create(context.Background(), entry); err != nil {
			t.Fatalf("create audit entry: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?user_id="+url.QueryEscape(userID), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	got := map[string]string{}
	for _, entry := range entries {
		got[entry.ResourceID] = entry.IPAddress
	}
	if got["ipv4"] != "192.0.2.10" || got["ipv6"] != "2001:db8::10" || got["null"] != "" {
		t.Fatalf("IP addresses = %#v, want IPv4/IPv6/empty", got)
	}
}

func TestAuditMiddlewareSkipsGET(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) != 0 {
		t.Errorf("expected 0 audit entries for GET, got %d", len(entries))
	}
}

func TestAuditMiddlewareSkipsHealthz(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) != 0 {
		t.Errorf("expected 0 audit entries, got %d", len(entries))
	}
}

// TestAuditListFilterByPeriod verifies ALD-001's since/until period filter
// excludes entries outside the requested range.
func TestAuditListFilterByPeriod(t *testing.T) {
	s := testServerFull()
	ctx := context.Background()

	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()

	mustCreateAuditEntry(t, s, ctx, "period-user", "customers", "old-id", old)
	mustCreateAuditEntry(t, s, ctx, "period-user", "customers", "recent-id", recent)

	since := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?user_id=period-user&since="+url.QueryEscape(since), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	for _, e := range entries {
		if e.ResourceID == "old-id" {
			t.Errorf("expected old-id entry to be excluded by since filter")
		}
	}
	found := false
	for _, e := range entries {
		if e.ResourceID == "recent-id" {
			found = true
		}
	}
	if !found {
		t.Error("expected recent-id entry to be included")
	}
}

// TestAuditListFilterByActor verifies ALD-001's user_id (operator) filter.
func TestAuditListFilterByActor(t *testing.T) {
	s := testServerFull()
	ctx := context.Background()
	now := time.Now()

	mustCreateAuditEntry(t, s, ctx, "actor-a", "customers", "a-1", now)
	mustCreateAuditEntry(t, s, ctx, "actor-b", "customers", "b-1", now)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?user_id=actor-a", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatal("expected at least 1 entry for actor-a")
	}
	for _, e := range entries {
		if e.UserID != "actor-a" {
			t.Errorf("user_id = %q, want %q", e.UserID, "actor-a")
		}
	}
}

// TestAuditListFilterByActionCategory verifies ALD-001's operation-category
// filter, which groups entries by resource_type (domain.ResourceTypesForCategory).
func TestAuditListFilterByActionCategory(t *testing.T) {
	s := testServerFull()
	ctx := context.Background()
	now := time.Now()

	mustCreateAuditEntry(t, s, ctx, "cat-user", "customers", "cust-1", now)
	mustCreateAuditEntry(t, s, ctx, "cat-user", "rules", "rule-1", now.Add(time.Second))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?user_id=cat-user&action_category="+url.QueryEscape("顧客データ"), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, _ := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(entries) < 1 {
		t.Fatal("expected at least 1 entry for category 顧客データ")
	}
	for _, e := range entries {
		if e.ResourceType != "customers" {
			t.Errorf("resource_type = %q, want %q for category 顧客データ", e.ResourceType, "customers")
		}
	}
}

// TestAuditListPagination verifies ALD-002's cursor-based pagination.
func TestAuditListPagination(t *testing.T) {
	s := testServerFull()
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)

	for i := 0; i < 3; i++ {
		mustCreateAuditEntry(t, s, ctx, "pager", "customers", fmt.Sprintf("page-%d", i), base.Add(time.Duration(i)*time.Second))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?user_id=pager&limit=2", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	page1, meta1 := decodeListResponse[domain.AuditEntry](t, rec.Body)
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatal("expected has_more with a next_cursor on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/audit?user_id=pager&limit=2&cursor="+url.QueryEscape(meta1.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	page2, meta2 := decodeListResponse[domain.AuditEntry](t, rec2.Body)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if meta2.HasMore {
		t.Error("expected has_more = false on second page")
	}
}

// TestAuditExportCSVPreservesFilter verifies ALD-004's CSV export keeps the
// same filter as the listing endpoint.
func TestAuditExportCSVPreservesFilter(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "export-admin-csv", domain.RoleAdmin)

	for _, ext := range []string{"EXPORTCSV001", "EXPORTCSV002"} {
		body := `{"external_id":"` + ext + `","customer_type":"individual","country_code":"JP"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminKey)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create customer: status = %d, body: %s", rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export?format=csv&resource_type=customers", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv prefix", ct)
	}

	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) < 3 { // header + 2 data rows
		t.Fatalf("rows = %d, want >= 3", len(rows))
	}
	resourceTypeCol := -1
	for i, h := range rows[0] {
		if h == "resource_type" {
			resourceTypeCol = i
		}
	}
	if resourceTypeCol == -1 {
		t.Fatal("resource_type column missing from CSV header")
	}
	for _, row := range rows[1:] {
		if row[resourceTypeCol] != "customers" {
			t.Errorf("resource_type = %q, want %q", row[resourceTypeCol], "customers")
		}
	}
}

// TestAuditExportJSONPreservesFilter verifies ALD-004's JSON export keeps
// the same filter as the listing endpoint.
func TestAuditExportJSONPreservesFilter(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "export-admin-json", domain.RoleAdmin)

	body := `{"external_id":"EXPORTJSON001","customer_type":"individual","country_code":"JP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create customer: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit/export?format=json&resource_type=customers", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var entries []domain.AuditEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode JSON export: %v", err)
	}
	if len(entries) < 1 {
		t.Fatal("expected at least 1 exported entry")
	}
	for _, e := range entries {
		if e.ResourceType != "customers" {
			t.Errorf("resource_type = %q, want %q", e.ResourceType, "customers")
		}
	}
}

// TestAuditExportRequiresAdminRole verifies ALD-005: exporting audit logs
// requires auth.PermAuditRead, which only Admin holds.
func TestAuditExportRequiresAdminRole(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "export-role-admin", domain.RoleAdmin)
	analystKey := createAPIKeyAs(t, s, adminKey, "export-role-analyst", domain.RoleAnalyst)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export", nil)
	req.Header.Set("Authorization", "Bearer "+analystKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// TestAuditExportRecordsAuditLog verifies ALD-006: the export operation
// itself is recorded in the audit log, even though it is a GET request.
func TestAuditExportRecordsAuditLog(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "export-audit-admin", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export?format=json", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit?resource_type=audit", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminKey)
	listRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body: %s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	entries, _ := decodeListResponse[domain.AuditEntry](t, listRec.Body)
	found := false
	for _, e := range entries {
		if e.Action == "export_audit_logs" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an export_audit_logs audit entry, got %+v", entries)
	}
}

// TestAuditDeleteEndpointNotExposed verifies ALD-005: audit logs cannot be
// deleted — no DELETE route is registered for /api/v1/audit, so the request
// falls through to a 404 rather than reaching a handler.
func TestAuditDeleteEndpointNotExposed(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/audit/1", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (no delete route should be registered for audit logs)", rec.Code, http.StatusNotFound)
	}
}
