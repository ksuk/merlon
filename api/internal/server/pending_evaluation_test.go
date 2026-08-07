package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

// The whole /pending-evaluations surface shipped without a Go test. This queue
// is the record of transactions that were never screened, so a filter that
// quietly drops rows understates a compliance gap.

type pendingFixture struct {
	server  *Server
	pending *store.MemoryPendingEvaluationRepo
	ids     []string
}

func newPendingFixture(t *testing.T) *pendingFixture {
	t.Helper()
	ctx := context.Background()
	pending := store.NewMemoryPendingEvaluationRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), PendingEvaluations: pending,
		Audit: store.NewMemoryAuditRepo(), EventOutbox: store.NewMemoryEventOutboxRepo(),
	})
	f := &pendingFixture{server: s, pending: pending}

	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	batchRun := "batch-1"
	for i := range 5 {
		id := fmt.Sprintf("pe-%03d", i)
		record := &domain.PendingEvaluation{
			ID: id, CustomerID: fmt.Sprintf("cust-%d", i%2),
			TransactionIDs: []string{fmt.Sprintf("tx-%03d", i)},
			Status:         domain.PendingEvaluationStatusPendingReview,
			Reason:         "engine unavailable",
			CreatedAt:      base.Add(time.Duration(i) * time.Hour),
		}
		if i == 3 {
			record.Status = domain.PendingEvaluationStatusFailed
			record.BatchRunID = &batchRun
		}
		if err := pending.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
		f.ids = append(f.ids, id)
	}
	return f
}

func (f *pendingFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	// Admin throughout: the export route is gated by audit:read, which
	// RequirePermission refuses outright when the request carries no role.
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(auth.WithRole(req.Context(), domain.RoleAdmin))
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, req)
	return rec
}

type pendingPage struct {
	Data       []domain.PendingEvaluation `json:"data"`
	Pagination PaginationMeta             `json:"pagination"`
}

func (f *pendingFixture) list(t *testing.T, query string) pendingPage {
	t.Helper()
	rec := f.get(t, "/api/v1/pending-evaluations"+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, body=%s", query, rec.Code, rec.Body.String())
	}
	var out pendingPage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestListPendingEvaluationsFilters(t *testing.T) {
	f := newPendingFixture(t)

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"unfiltered", "", 5},
		{"status", "?status=FAILED", 1},
		{"two statuses", "?status=PENDING_REVIEW,FAILED", 5},
		{"customer", "?customer_id=cust-1", 2},
		{"batch run", "?batch_run_id=batch-1", 1},
		{"created_from", "?created_from=2026-08-01T11:00:00Z", 3},
		{"created_to", "?created_to=2026-08-01T10:00:00Z", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(f.list(t, tc.query).Data); got != tc.want {
				t.Fatalf("rows = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestListPendingEvaluationsRejectsInvalidFilters(t *testing.T) {
	f := newPendingFixture(t)
	for _, query := range []string{
		"?created_from=yesterday", "?created_to=nope", "?min_age_days=-1",
		"?max_age_days=abc", "?created_from=2026-08-02T00:00:00Z&created_to=2026-08-01T00:00:00Z",
	} {
		if code := f.get(t, "/api/v1/pending-evaluations"+query).Code; code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", query, code)
		}
	}
}

func TestListPendingEvaluationsPagesAcrossThreePages(t *testing.T) {
	f := newPendingFixture(t)
	seen := map[string]bool{}
	query := "?limit=2"
	for page := 0; page < 4; page++ {
		got := f.list(t, query)
		for _, item := range got.Data {
			if seen[item.ID] {
				t.Fatalf("record %s returned on two pages", item.ID)
			}
			seen[item.ID] = true
		}
		if !got.Pagination.HasMore {
			break
		}
		query = "?limit=2&cursor=" + got.Pagination.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("paged %d records, want all 5", len(seen))
	}
}

func TestGetPendingEvaluationAndHistory(t *testing.T) {
	f := newPendingFixture(t)
	ctx := context.Background()

	rec := f.get(t, "/api/v1/pending-evaluations/"+f.ids[0])
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if missing := f.get(t, "/api/v1/pending-evaluations/does-not-exist"); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", missing.Code)
	}

	if _, err := f.pending.TransitionPendingEvaluation(ctx, f.ids[0], "process", "analyst", "starting", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pending.TransitionPendingEvaluation(ctx, f.ids[0], "resolve", "analyst", "recovered", 2); err != nil {
		t.Fatal(err)
	}

	history := f.get(t, "/api/v1/pending-evaluations/"+f.ids[0]+"/history")
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d", history.Code)
	}
	var entries []domain.PendingEvaluationHistoryEntry
	if err := json.Unmarshal(history.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Action != "process" || entries[1].Action != "resolve" {
		t.Fatalf("history = %+v, want process then resolve in order", entries)
	}
}

func (f *pendingFixture) transition(t *testing.T, id, action string, version int, role domain.Role) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"reason":"operator action","expected_version":%d}`, version)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pending-evaluations/"+id+"/"+action, strings.NewReader(body))
	if role != "" {
		req = req.WithContext(auth.WithRole(req.Context(), role))
	}
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPendingEvaluationTransitionsCoverEveryAction(t *testing.T) {
	f := newPendingFixture(t)

	if rec := f.transition(t, f.ids[0], "process", 1, domain.RoleAnalyst); rec.Code != http.StatusOK {
		t.Fatalf("process = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec := f.transition(t, f.ids[0], "resolve", 2, domain.RoleAnalyst); rec.Code != http.StatusOK {
		t.Fatalf("resolve = %d, body=%s", rec.Code, rec.Body.String())
	}
	// A resolved record cannot be re-opened by retrying it.
	if rec := f.transition(t, f.ids[0], "retry", 3, domain.RoleAnalyst); rec.Code != http.StatusConflict {
		t.Fatalf("retry after resolve = %d, want 409", rec.Code)
	}

	if rec := f.transition(t, f.ids[1], "retry", 1, domain.RoleAnalyst); rec.Code != http.StatusOK {
		t.Fatalf("retry = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec := f.transition(t, f.ids[1], "escalate", 2, domain.RoleAnalyst); rec.Code != http.StatusOK {
		t.Fatalf("escalate = %d, body=%s", rec.Code, rec.Body.String())
	}

	// A stale expected_version is a conflict, an unknown action is a rejection.
	if rec := f.transition(t, f.ids[2], "retry", 99, domain.RoleAnalyst); rec.Code != http.StatusConflict {
		t.Fatalf("stale version = %d, want 409", rec.Code)
	}
	if rec := f.transition(t, f.ids[2], "teleport", 1, domain.RoleAnalyst); rec.Code == http.StatusOK {
		t.Fatalf("unknown action was accepted with %d", rec.Code)
	}
	if rec := f.transition(t, f.ids[2], "retry", 0, domain.RoleAnalyst); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing expected_version = %d, want 400", rec.Code)
	}
}

// A record the automatic budget gave up on may not be revived by the ordinary
// retry action, and reviving it deliberately requires approval authority and
// is counted separately.
func TestFailedPendingEvaluationRevivalIsBoundedAndPrivileged(t *testing.T) {
	f := newPendingFixture(t)
	failed := f.ids[3]

	if rec := f.transition(t, failed, "retry", 1, domain.RoleAdmin); rec.Code != http.StatusConflict {
		t.Fatalf("plain retry of a FAILED record = %d, want 409", rec.Code)
	}
	if rec := f.transition(t, failed, "manual_retry", 1, domain.RoleAnalyst); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst manual_retry = %d, want 403", rec.Code)
	}

	rec := f.transition(t, failed, "manual_retry", 1, domain.RoleAdmin)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin manual_retry = %d, body=%s", rec.Code, rec.Body.String())
	}
	var revived domain.PendingEvaluation
	if err := json.Unmarshal(rec.Body.Bytes(), &revived); err != nil {
		t.Fatal(err)
	}
	if revived.Status != domain.PendingEvaluationStatusPendingReview {
		t.Fatalf("status = %q, want the record back in the queue", revived.Status)
	}
	if revived.ManualRetryCount != 1 {
		t.Fatalf("manual_retry_count = %d, want 1", revived.ManualRetryCount)
	}
	if revived.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want the automatic budget untouched by a manual revival", revived.RetryCount)
	}
	if revived.EscalatedAt != nil {
		t.Fatal("EscalatedAt survived the revival; the record is no longer escalated")
	}
}

func TestExportPendingEvaluations(t *testing.T) {
	f := newPendingFixture(t)

	csvRec := f.get(t, "/api/v1/pending-evaluations/export")
	if csvRec.Code != http.StatusOK {
		t.Fatalf("csv export = %d, body=%s", csvRec.Code, csvRec.Body.String())
	}
	if got := csvRec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", got)
	}
	rows, err := csv.NewReader(strings.NewReader(csvRec.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("csv rows = %d, want a header plus 5 records", len(rows))
	}
	if rows[0][0] != "id" || rows[0][8] != "manual_retry_count" {
		t.Fatalf("header = %v, want the manual retry column present", rows[0])
	}

	// The export carries the listing endpoint's filter, so the file matches
	// what the operator was looking at.
	filtered := f.get(t, "/api/v1/pending-evaluations/export?status=FAILED&format=json")
	if filtered.Code != http.StatusOK {
		t.Fatalf("json export = %d, body=%s", filtered.Code, filtered.Body.String())
	}
	var items []domain.PendingEvaluation
	if err := json.Unmarshal(filtered.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != domain.PendingEvaluationStatusFailed {
		t.Fatalf("filtered export = %+v, want only the failed record", items)
	}

	if bad := f.get(t, "/api/v1/pending-evaluations/export?format=xml"); bad.Code != http.StatusBadRequest {
		t.Fatalf("unsupported format = %d, want 400", bad.Code)
	}
	if bad := f.get(t, "/api/v1/pending-evaluations/export?created_from=nope"); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter = %d, want 400", bad.Code)
	}
}

// The export is evidence about unscreened transactions, so it is gated by the
// same permission the audit log export uses.
func TestExportPendingEvaluationsRequiresAuditRead(t *testing.T) {
	pending := store.NewMemoryPendingEvaluationRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), PendingEvaluations: pending,
		Audit: store.NewMemoryAuditRepo(), APIKeys: store.NewMemoryAPIKeyRepo(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pending-evaluations/export", nil)
	req = req.WithContext(auth.WithRole(req.Context(), domain.RoleAnalyst))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("analyst export = %d, want a refusal", rec.Code)
	}
}
