package backtest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

type versionedTestEngine struct {
	baseCalls      int
	candidateCalls int
}

func (e *versionedTestEngine) RunBacktest(context.Context, []domain.Customer, []domain.Transaction, []string, string) (*domain.BacktestResult, error) {
	e.baseCalls++
	return &domain.BacktestResult{TotalCustomers: 1}, nil
}

func (e *versionedTestEngine) RunBacktestWithRuleSet(_ context.Context, _ []domain.Customer, _ []domain.Transaction, _ []string, _, _ string, definition []byte) (*domain.BacktestResult, error) {
	e.candidateCalls++
	var raw map[string]any
	if err := json.Unmarshal(definition, &raw); err != nil {
		return nil, err
	}
	if raw["scenario_id"] != "tm_structuring_basic" {
		return nil, &domain.ErrNotFound{Entity: "scenario", ID: "candidate"}
	}
	return &domain.BacktestResult{TotalCustomers: 1, TotalAlerts: 1}, nil
}

func TestWorkerResolvesCandidateRuleDefinitionBeforeReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(ctx, &domain.Customer{ID: "c1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	txns := store.NewMemoryTransactionRepo()
	jobs := store.NewMemoryBacktestJobRepo()
	job := &domain.BacktestJob{ID: "candidate-job", From: now.Add(-time.Hour), To: now.Add(time.Hour), CustomerIDs: []string{"c1"}, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	rules := store.NewMemoryRuleRepo()
	definition := json.RawMessage(`{"scenario_id":"tm_structuring_basic"}`)
	if err := rules.Create(ctx, &domain.RuleDefinition{ID: "candidate-v1", Name: "candidate", Type: domain.RuleTypeTMScenario, Definition: definition, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	eng := &versionedTestEngine{}
	worker := &Worker{Jobs: jobs, Customers: customers, Transactions: txns, Engine: eng, Rules: rules}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eng.baseCalls != 1 || eng.candidateCalls != 1 || got.Candidate == nil || got.Candidate.TotalAlerts != 1 {
		t.Fatalf("calls base=%d candidate=%d job=%+v", eng.baseCalls, eng.candidateCalls, got)
	}
}

func TestWorkerFailsClosedWhenCandidateRuleIsMissing(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(ctx, &domain.Customer{ID: "c1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	jobs := store.NewMemoryBacktestJobRepo()
	job := &domain.BacktestJob{ID: "missing-rule-job", From: now.Add(-time.Hour), To: now.Add(time.Hour), CustomerIDs: []string{"c1"}, BaselineRuleSetID: "active", CandidateRuleSetID: "missing", SnapshotAt: now}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	worker := &Worker{Jobs: jobs, Customers: customers, Transactions: store.NewMemoryTransactionRepo(), Engine: &versionedTestEngine{}, Rules: store.NewMemoryRuleRepo()}
	if err := worker.RunOnce(ctx); err == nil {
		t.Fatal("expected missing candidate rule error")
	}
	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.BacktestJobFailed || got.Error == "" {
		t.Fatalf("job=%+v", got)
	}
}

func TestWorkerRunsDurableJobWithoutCreatingAlerts(t *testing.T) {
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	c := &domain.Customer{ID: "c1", CustomerType: domain.CustomerTypeIndividual, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := customers.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	txns := store.NewMemoryTransactionRepo()
	now := time.Now().UTC()
	if err := txns.Create(ctx, &domain.Transaction{ID: "t1", CustomerID: "c1", Amount: 10, Currency: "JPY", Direction: domain.DirectionInbound, ExecutedAt: now, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	jobs := store.NewMemoryBacktestJobRepo()
	job := &domain.BacktestJob{ID: "job1", From: now.Add(-time.Hour), To: now.Add(time.Hour), CustomerIDs: []string{"c1"}, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now.Add(time.Second)}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	w := &Worker{Jobs: jobs, Customers: customers, Transactions: txns, Engine: &engine.MockBacktestEngine{Result: &domain.BacktestResult{BacktestID: "x", TotalCustomers: 1, TotalTransactions: 1}}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Get(ctx, "job1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.BacktestJobCompleted || got.Candidate == nil || got.Progress != 1 {
		t.Fatalf("job=%+v", got)
	}
	ids, found, err := jobs.GetCustomerSnapshot(ctx, "job1")
	if err != nil || !found || len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("snapshot ids=%v found=%v err=%v", ids, found, err)
	}
}
