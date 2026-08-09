package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// POST /batch/runs writes a durable batch_runs row, but the two routes an
// operator actually reaches from the batch screen -- POST /batch/score and
// POST /batch/monitor -- executed against live customer data and returned
// response-local aggregates only. Nothing recorded that the execution
// happened, so after the response was closed there was no way to answer who
// ran what, over which population, with which configuration, or what it did.
//
// #74 requires a durable run record before *each* accepted manual score or
// monitor execution, not only the ones routed through the new endpoint.

func newLegacyBatchFixture(t *testing.T, monitoring engine.MonitoringEngine) (*Server, *store.MemoryBatchRunRepo, *store.MemoryCustomerRepo) {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	runs := store.NewMemoryBatchRunRepo()
	if monitoring == nil {
		monitoring = &engine.MockMonitoringEngine{}
	}
	s := New(":0", Deps{
		Customers:          customers,
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             store.NewMemoryAlertRepo(),
		Cases:              store.NewMemoryCaseRepo(),
		Audit:              store.NewMemoryAuditRepo(),
		BatchRuns:          runs,
		Wave3:              store.NewMemoryWave3Repo(),
		PendingEvaluations: store.NewMemoryPendingEvaluationRepo(),
		Scoring:            &engine.MockScoringEngine{Score: 3.0, Tier: domain.RiskTierMedium},
		Monitoring:         monitoring,
	})
	for _, id := range []string{
		"00000000000000000000000000000c01",
		"00000000000000000000000000000c02",
	} {
		if err := customers.Create(ctx, &domain.Customer{
			ID: id, ExternalID: "legacy-" + id[30:], CustomerType: domain.CustomerTypeIndividual,
			CountryCode: "JP", Status: domain.CustomerStatusActive,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s, runs, customers
}

func listRuns(t *testing.T, runs *store.MemoryBatchRunRepo, operation string) []domain.BatchRun {
	t.Helper()
	found, err := runs.ListBatchRuns(context.Background(), domain.BatchRunFilter{Operation: operation}, 50)
	if err != nil {
		t.Fatalf("list batch runs: %v", err)
	}
	return found
}

func TestBatchScoreRecordsDurableRun(t *testing.T) {
	s, runs, _ := newLegacyBatchFixture(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/score", strings.NewReader(`{"target_mode":"all"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	found := listRuns(t, runs, "score")
	if len(found) != 1 {
		t.Fatalf("durable score runs = %d, want 1", len(found))
	}
	run := found[0]
	if run.Status != domain.BatchRunStatusCompleted {
		t.Errorf("status = %q, want %q", run.Status, domain.BatchRunStatusCompleted)
	}
	if run.ResultCounts["total"] != 2 || run.ResultCounts["succeeded"] != 2 {
		t.Errorf("result_counts = %v, want total=2 succeeded=2", run.ResultCounts)
	}
	if run.CompletedAt == nil {
		t.Error("completed_at is nil; a finished run must record when it ended")
	}
	// The response must name the record so an operator can find it again.
	var resp batchScoreResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RunID != run.ID {
		t.Errorf("response run_id = %q, want %q", resp.RunID, run.ID)
	}
}

func TestBatchMonitorRecordsDurableRunWithQueuedCount(t *testing.T) {
	// A monitoring engine that always fails routes every customer to
	// PENDING_REVIEW. The durable record must carry that count, because
	// "queued for review" is the outcome an operator must not read as success.
	s, runs, customers := newLegacyBatchFixture(t, &failingMonitoringEngine{})
	ctx := context.Background()
	all, err := customers.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if err := s.transactions.Create(ctx, &domain.Transaction{
			ID: generateID(), CustomerID: c.ID, Amount: 10000, Currency: "JPY",
			Direction: "inbound", ExecutedAt: time.Now().UTC().Add(-time.Hour),
			CreatedAt: time.Now().UTC().Add(-time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/monitor", strings.NewReader(`{"target_mode":"all"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	found := listRuns(t, runs, "monitor")
	if len(found) != 1 {
		t.Fatalf("durable monitor runs = %d, want 1", len(found))
	}
	run := found[0]
	if run.ResultCounts["queued_for_review"] != 2 {
		t.Errorf("queued_for_review = %d, want 2 (counts = %v)", run.ResultCounts["queued_for_review"], run.ResultCounts)
	}
	// Every customer went to review, so nothing succeeded. A run reported as
	// completed here would tell an operator monitoring finished cleanly.
	if run.Status == domain.BatchRunStatusCompleted {
		t.Errorf("status = %q; a run whose whole population is queued for review is not completed", run.Status)
	}
}

func TestBatchScoreRejectedRequestRecordsNoRun(t *testing.T) {
	// The record marks an *accepted* execution. A request rejected before any
	// customer is touched must not leave a run behind, or the history fills
	// with executions that never ran.
	s, runs, _ := newLegacyBatchFixture(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/score", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (empty selection is not all customers)", rec.Code)
	}
	if found := listRuns(t, runs, "score"); len(found) != 0 {
		t.Fatalf("durable runs after a rejected request = %d, want 0", len(found))
	}
}

type failingMonitoringEngine struct{}

func (e *failingMonitoringEngine) EvaluateTransactions(_ context.Context, _ string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
	return nil, errors.New("monitoring engine unavailable")
}

func (e *failingMonitoringEngine) EvaluateTransactionsBatch(_ context.Context, _ string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
	return nil, errors.New("monitoring engine unavailable")
}
