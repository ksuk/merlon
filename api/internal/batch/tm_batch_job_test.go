package batch

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func seedBatchCustomerAndTransaction(t *testing.T, customers domain.CustomerRepository, transactions domain.TransactionRepository, externalID string, txCreatedAt time.Time) (*domain.Customer, *domain.Transaction) {
	t.Helper()
	riskTier := domain.RiskTierMedium
	c := &domain.Customer{
		ID:           "cust-" + externalID,
		ExternalID:   externalID,
		CustomerType: domain.CustomerTypeIndividual,
		CountryCode:  "JP",
		RiskTier:     &riskTier,
	}
	if err := customers.Create(context.Background(), c); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	tx := &domain.Transaction{
		ID:         "tx-" + externalID,
		CustomerID: c.ID,
		ExternalID: "TX_" + externalID,
		Amount:     100000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
		CreatedAt:  txCreatedAt,
	}
	if err := transactions.Create(context.Background(), tx); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	return c, tx
}

func TestRunTMBatchEvaluation_EvaluatesAllCustomersAndCompletesRun(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	runs := store.NewMemoryBatchRunRepo()

	c1, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB001", time.Now().Add(-time.Hour))
	c2, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB002", time.Now().Add(-time.Hour))

	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, customerID string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			return []domain.Alert{{CustomerID: customerID, ScenarioID: "structuring", Severity: domain.AlertSeverityHigh, Description: "test"}}, nil
		},
	}

	deps := TMBatchEvaluationDeps{
		Runs:         runs,
		Customers:    customers,
		Transactions: transactions,
		Monitoring:   monitoring,
		Alerts:       alerts,
	}

	if err := RunTMBatchEvaluation(ctx, deps, "run-1"); err != nil {
		t.Fatalf("RunTMBatchEvaluation: %v", err)
	}

	run, err := runs.Get(ctx, "run-1")
	if err != nil {
		t.Fatalf("Get run: %v", err)
	}
	if run.Status != domain.BatchRunStatusCompleted {
		t.Errorf("run status = %s, want completed", run.Status)
	}
	if len(run.ProcessedCustomerIDs) != 2 {
		t.Errorf("ProcessedCustomerIDs = %v, want 2 entries", run.ProcessedCustomerIDs)
	}

	for _, c := range []*domain.Customer{c1, c2} {
		got, err := alerts.ListByCustomer(ctx, c.ID, 10, 0)
		if err != nil {
			t.Fatalf("ListByCustomer(%s): %v", c.ID, err)
		}
		if len(got) != 1 {
			t.Errorf("customer %s: alerts = %d, want 1", c.ID, len(got))
		}
	}
}

// TestRunTMBatchEvaluation_ResumesAfterKillSkipsAlreadyProcessed simulates a
// process kill partway through a batch: a batch_runs row is left in
// status=running with one customer already recorded as processed. Calling
// RunTMBatchEvaluation again (as main.go does on restart) must resume that
// same run, skip the already-processed customer, and only evaluate the rest
// (the operational design §4.4 バッチジョブ障害復旧, acceptance criterion "バッチを途中kill→
// 再実行で未処理分のみ処理").
func TestRunTMBatchEvaluation_ResumesAfterKillSkipsAlreadyProcessed(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	runs := store.NewMemoryBatchRunRepo()

	c1, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB101", time.Now().Add(-time.Hour))
	c2, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB102", time.Now().Add(-time.Hour))

	killedRunID := "killed-run"
	if err := runs.Create(ctx, &domain.BatchRun{ID: killedRunID, JobType: TMBatchEvaluationJobType, Status: domain.BatchRunStatusRunning}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := runs.AppendProcessedCustomer(ctx, killedRunID, c1.ID); err != nil {
		t.Fatalf("seed AppendProcessedCustomer: %v", err)
	}

	var evaluatedCustomers []string
	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, customerID string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			evaluatedCustomers = append(evaluatedCustomers, customerID)
			return []domain.Alert{{ScenarioID: "structuring", Severity: domain.AlertSeverityHigh, Description: "test"}}, nil
		},
	}

	deps := TMBatchEvaluationDeps{
		Runs:         runs,
		Customers:    customers,
		Transactions: transactions,
		Monitoring:   monitoring,
		Alerts:       alerts,
	}

	// candidateRunID differs from killedRunID: RunTMBatchEvaluation must
	// discover and resume killedRunID via ResumeOrCreateRun rather than
	// starting a fresh run under the candidate.
	if err := RunTMBatchEvaluation(ctx, deps, "brand-new-candidate"); err != nil {
		t.Fatalf("RunTMBatchEvaluation: %v", err)
	}

	if len(evaluatedCustomers) != 1 || evaluatedCustomers[0] != c2.ID {
		t.Errorf("evaluatedCustomers = %v, want only [%s] (c1 already processed)", evaluatedCustomers, c2.ID)
	}

	run, err := runs.Get(ctx, killedRunID)
	if err != nil {
		t.Fatalf("Get resumed run: %v", err)
	}
	if run.Status != domain.BatchRunStatusCompleted {
		t.Errorf("resumed run status = %s, want completed", run.Status)
	}
	if len(run.ProcessedCustomerIDs) != 2 {
		t.Errorf("ProcessedCustomerIDs = %v, want 2 entries", run.ProcessedCustomerIDs)
	}

	if _, err := runs.Get(ctx, "brand-new-candidate"); err == nil {
		t.Error("a fresh run should not have been created when a running run already existed")
	}
}

func TestRunTMBatchEvaluation_ExcludesTransactionsIngestedDuringBatch(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	runs := store.NewMemoryBatchRunRepo()

	c, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB201", time.Now().Add(-time.Hour))
	// Simulate a transaction ingested after the batch's snapshot point by
	// creating it with a future CreatedAt.
	lateTx := &domain.Transaction{
		ID:         "tx-late",
		CustomerID: c.ID,
		ExternalID: "TX_LATE",
		Amount:     50000,
		Currency:   "JPY",
		Direction:  domain.DirectionInbound,
		CreatedAt:  time.Now().Add(time.Hour),
	}
	if err := transactions.Create(ctx, lateTx); err != nil {
		t.Fatalf("seed late transaction: %v", err)
	}

	var seenTxIDs []string
	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, _ string, _ domain.RiskTier, txns []domain.Transaction, _ []string) ([]domain.Alert, error) {
			for _, t := range txns {
				seenTxIDs = append(seenTxIDs, t.ID)
			}
			return nil, nil
		},
	}

	deps := TMBatchEvaluationDeps{
		Runs:         runs,
		Customers:    customers,
		Transactions: transactions,
		Monitoring:   monitoring,
		Alerts:       alerts,
	}

	if err := RunTMBatchEvaluation(ctx, deps, "run-snapshot"); err != nil {
		t.Fatalf("RunTMBatchEvaluation: %v", err)
	}

	for _, id := range seenTxIDs {
		if id == "tx-late" {
			t.Errorf("seenTxIDs = %v, should not include tx-late (ingested after batch start)", seenTxIDs)
		}
	}
}

// TestRunTMBatchEvaluation_SkipsClosedAndDormantCustomers verifies
// the data model §1.1.2's per-status scheduled-TM-batch scope: closed
// customers stop TM evaluation entirely, and dormant customers are
// evaluated only "取引発生時" (the realtime path, WS-11 Task 2's
// handleBatchMonitor) rather than on this periodic 02:00 schedule — even
// though both have outstanding transactions the way an active customer
// would (TestDormantCustomerContinuesScreeningSkipsTMWithoutTransaction:
// dormant continues periodic screening rescreening, covered separately in
// the screening package, but skips this scheduled TM pass).
func TestRunTMBatchEvaluation_SkipsClosedAndDormantCustomers(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	runs := store.NewMemoryBatchRunRepo()

	active, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB201", time.Now().Add(-time.Hour))
	closed, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB202", time.Now().Add(-time.Hour))
	dormant, _ := seedBatchCustomerAndTransaction(t, customers, transactions, "TB203", time.Now().Add(-time.Hour))

	closed.Status = domain.CustomerStatusClosed
	if err := customers.Update(ctx, closed); err != nil {
		t.Fatalf("update closed customer: %v", err)
	}
	dormant.Status = domain.CustomerStatusDormant
	if err := customers.Update(ctx, dormant); err != nil {
		t.Fatalf("update dormant customer: %v", err)
	}

	var evaluatedCustomers []string
	monitoring := &engine.MockMonitoringEngine{
		EvaluateFunc: func(_ context.Context, customerID string, _ domain.RiskTier, _ []domain.Transaction, _ []string) ([]domain.Alert, error) {
			evaluatedCustomers = append(evaluatedCustomers, customerID)
			return nil, nil
		},
	}

	deps := TMBatchEvaluationDeps{
		Runs:         runs,
		Customers:    customers,
		Transactions: transactions,
		Monitoring:   monitoring,
		Alerts:       alerts,
	}

	if err := RunTMBatchEvaluation(ctx, deps, "run-status-scope"); err != nil {
		t.Fatalf("RunTMBatchEvaluation: %v", err)
	}

	foundActive := false
	for _, id := range evaluatedCustomers {
		if id == closed.ID {
			t.Errorf("closed customer %s was evaluated, want excluded", closed.ID)
		}
		if id == dormant.ID {
			t.Errorf("dormant customer %s was evaluated on the scheduled batch, want excluded", dormant.ID)
		}
		if id == active.ID {
			foundActive = true
		}
	}
	if !foundActive {
		t.Errorf("active customer %s was not evaluated, want included", active.ID)
	}
}
