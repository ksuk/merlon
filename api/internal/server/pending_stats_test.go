package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

// #73 requires backlog count, oldest age, and exhausted/failed counts to be
// visible as stop conditions. The list endpoint returned a page and nothing
// else, so a queue could only approximate the backlog from whatever rows it
// happened to load -- which reads as "small backlog" precisely when the
// backlog is large enough to matter.

type pendingStatsResponse struct {
	Backlog          int            `json:"backlog"`
	OldestAgeSeconds int64          `json:"oldest_age_seconds"`
	OldestCreatedAt  *time.Time     `json:"oldest_created_at"`
	ByStatus         map[string]int `json:"by_status"`
	Failed           int            `json:"failed"`
	Exhausted        int            `json:"exhausted"`
}

func newPendingStatsServer(t *testing.T) (*Server, *store.MemoryPendingEvaluationRepo) {
	t.Helper()
	repo := store.NewMemoryPendingEvaluationRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(), Alerts: store.NewMemoryAlertRepo(),
		Cases: store.NewMemoryCaseRepo(), Audit: store.NewMemoryAuditRepo(),
		PendingEvaluations: repo,
	})
	return s, repo
}

func TestPendingEvaluationStatsReportsBacklogAndOldestAge(t *testing.T) {
	s, repo := newPendingStatsServer(t)
	ctx := context.Background()
	now := time.Now().UTC()

	seed := []struct {
		id      string
		status  domain.PendingEvaluationStatus
		age     time.Duration
		retries int
	}{
		{"pending-1", domain.PendingEvaluationStatusPendingReview, 72 * time.Hour, 0},
		{"pending-2", domain.PendingEvaluationStatusPendingReview, 2 * time.Hour, 2},
		{"failed-1", domain.PendingEvaluationStatusFailed, 48 * time.Hour, 5},
		{"resolved-1", domain.PendingEvaluationStatusResolved, 96 * time.Hour, 1},
	}
	for _, row := range seed {
		if err := repo.Create(ctx, &domain.PendingEvaluation{
			ID: row.id, CustomerID: "00000000000000000000000000000b01",
			TransactionIDs: []string{"t-" + row.id}, Status: row.status,
			Reason: "engine unavailable", RetryCount: row.retries,
			CreatedAt: now.Add(-row.age), UpdatedAt: now.Add(-row.age),
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pending-evaluations/stats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var out pendingStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	// Backlog is unresolved work: PENDING_REVIEW plus FAILED. A resolved
	// record is finished and must not inflate the stop condition.
	if out.Backlog != 3 {
		t.Errorf("backlog = %d, want 3 (2 pending + 1 failed, resolved excluded)", out.Backlog)
	}
	if out.Failed != 1 {
		t.Errorf("failed = %d, want 1", out.Failed)
	}
	// Oldest unresolved is the 72h pending record, not the 96h resolved one.
	if out.OldestAgeSeconds < int64(71*time.Hour/time.Second) || out.OldestAgeSeconds > int64(73*time.Hour/time.Second) {
		t.Errorf("oldest_age_seconds = %d, want ~72h in seconds (the oldest unresolved record)", out.OldestAgeSeconds)
	}
	if out.ByStatus["PENDING_REVIEW"] != 2 {
		t.Errorf("by_status[PENDING_REVIEW] = %d, want 2 (by_status = %v)", out.ByStatus["PENDING_REVIEW"], out.ByStatus)
	}
}

func TestPendingEvaluationStatsOnEmptyQueue(t *testing.T) {
	s, _ := newPendingStatsServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pending-evaluations/stats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var out pendingStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Backlog != 0 {
		t.Errorf("backlog = %d, want 0", out.Backlog)
	}
	// An empty queue must report no oldest record rather than an age of zero,
	// which would read as "something arrived just now".
	if out.OldestCreatedAt != nil {
		t.Errorf("oldest_created_at = %v, want null on an empty queue", out.OldestCreatedAt)
	}
}
