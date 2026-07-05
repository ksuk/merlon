package batch

import (
	"context"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/store"
)

// TestBatchEvaluation_SkipsAlreadyAlertedRealtimeTransaction is the
// acceptance-criteria test for "同一取引へのバッチ/リアルタイム二重評価でアラートが1件に
// 抑止される" (transaction-monitoring.md「バッチ/リアルタイム評価の重複アラート防止」):
// an evaluation_mode=both scenario that already raised an alert via the
// realtime path (server.handleBatchMonitor, simulated here directly through
// AlertRepository.CreateIfNotDuplicate with no BatchRunID) must not produce
// a second alert when the same scenario/window fires again during the daily
// TM batch pass. The pre-existing alert is annotated as batch-reviewed
// instead, and its batch_run_id is backfilled since the realtime creator
// left it unset.
func TestBatchEvaluation_SkipsAlreadyAlertedRealtimeTransaction(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	runs := store.NewMemoryBatchRunRepo()

	c, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "DEDUP001", time.Now().Add(-time.Hour))

	detectedAt := time.Now()
	windowStart := domain.DailyAggregationWindowStart(detectedAt)
	realtimeAlert := &domain.Alert{
		ID:                     "realtime-alert-1",
		CustomerID:             c.ID,
		ScenarioID:             "structuring",
		Severity:               domain.AlertSeverityHigh,
		Status:                 domain.AlertStatusOpen,
		Description:            "realtime pass",
		DetectedAt:             detectedAt,
		AggregationWindowStart: &windowStart,
		CreatedAt:              detectedAt,
		UpdatedAt:              detectedAt,
	}
	created, _, err := alerts.CreateIfNotDuplicate(ctx, realtimeAlert)
	if err != nil {
		t.Fatalf("seed CreateIfNotDuplicate: %v", err)
	}
	if !created {
		t.Fatalf("seed realtime alert should have been created fresh")
	}

	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, customerID string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			return []domain.Alert{{
				CustomerID:  customerID,
				ScenarioID:  "structuring",
				Severity:    domain.AlertSeverityHigh,
				Description: "batch pass",
				DetectedAt:  detectedAt,
			}}, nil
		},
	}

	deps := TMBatchEvaluationDeps{
		Runs:         runs,
		Customers:    customers,
		Transactions: transactions,
		Monitoring:   monitoring,
		Alerts:       alerts,
		Cases:        store.NewMemoryCaseRepo(),
	}

	if err := RunTMBatchEvaluation(ctx, deps, "batch-run-1"); err != nil {
		t.Fatalf("RunTMBatchEvaluation: %v", err)
	}

	got, err := alerts.ListByCustomer(ctx, c.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("alerts count = %d, want 1 (batch pass must not duplicate the realtime alert)", len(got))
	}

	existing, err := alerts.Get(ctx, realtimeAlert.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if existing.BatchReviewedAt == nil {
		t.Error("existing realtime alert should be annotated as batch-reviewed")
	}
	if existing.BatchRunID != "batch-run-1" {
		t.Errorf("BatchRunID = %q, want backfilled to batch-run-1", existing.BatchRunID)
	}
}
