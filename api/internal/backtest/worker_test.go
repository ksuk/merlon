package backtest

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

type countingCustomerRepository struct {
	domain.CustomerRepository
	getCalls int
}

func (r *countingCustomerRepository) Get(ctx context.Context, id string) (*domain.Customer, error) {
	r.getCalls++
	return r.CustomerRepository.Get(ctx, id)
}

type cancellationTestEngine struct {
	started  chan struct{}
	canceled chan struct{}
	once     sync.Once
}

func (e *cancellationTestEngine) RunBacktest(ctx context.Context, _ []domain.Customer, _ []domain.Transaction, _ []string, _ string) (*domain.BacktestResult, error) {
	e.once.Do(func() { close(e.started) })
	<-ctx.Done()
	close(e.canceled)
	return nil, ctx.Err()
}

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

func TestWorkerPersistsOptionalOutcomeAnalysisBeforeCompletion(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(ctx, &domain.Customer{ID: "outcome-customer", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	jobs := store.NewMemoryBacktestJobRepo()
	job := &domain.BacktestJob{ID: "outcome-job", From: now.Add(-time.Hour), To: now.Add(time.Hour), CustomerIDs: []string{"outcome-customer"}, BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	w := &Worker{Jobs: jobs, Customers: customers, Transactions: store.NewMemoryTransactionRepo(), Engine: &engine.MockBacktestEngine{Result: &domain.BacktestResult{TotalCustomers: 1}}, OutcomeBuilder: func(_ context.Context, job *domain.BacktestJob) (*domain.BacktestOutcomeAnalysis, []domain.BacktestOutcomeDetail, error) {
		return &domain.BacktestOutcomeAnalysis{MatcherVersion: "outcome-matcher-v1", SnapshotAt: job.SnapshotAt}, nil, nil
	}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.GetBacktestOutcomeAnalysis(ctx, job.ID)
	if err != nil || got == nil || got.MatcherVersion != "outcome-matcher-v1" {
		t.Fatalf("analysis=%#v err=%v", got, err)
	}
}

func TestWorkerUsesCustomersReturnedByFilterScanWithoutRefetching(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	baseCustomers := store.NewMemoryCustomerRepo()
	for _, id := range []string{"c1", "c2"} {
		if err := baseCustomers.Create(ctx, &domain.Customer{ID: id, CountryCode: "JP", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	customers := &countingCustomerRepository{CustomerRepository: baseCustomers}
	jobs := store.NewMemoryBacktestJobRepo()
	job := &domain.BacktestJob{
		ID: "filter-job", From: now.Add(-time.Hour), To: now.Add(time.Hour),
		CustomerFilter:    &domain.BacktestCustomerFilter{CountryCode: "JP"},
		BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now.Add(time.Second),
	}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	worker := &Worker{
		Jobs: jobs, Customers: customers, Transactions: store.NewMemoryTransactionRepo(),
		Engine: &engine.MockBacktestEngine{Result: &domain.BacktestResult{TotalCustomers: 2}},
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if customers.getCalls != 0 {
		t.Fatalf("CustomerRepository.Get calls = %d, want 0 after filter scan", customers.getCalls)
	}
	ids, found, err := jobs.GetCustomerSnapshot(ctx, job.ID)
	if err != nil || !found || len(ids) != 2 {
		t.Fatalf("snapshot ids=%v found=%v err=%v", ids, found, err)
	}
}

func TestWorkerCancelsRunningEngineWhenJobIsCancelled(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(ctx, &domain.Customer{ID: "c1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	jobs := store.NewMemoryBacktestJobRepo()
	job := &domain.BacktestJob{
		ID: "cancel-job", From: now.Add(-time.Hour), To: now.Add(time.Hour), CustomerIDs: []string{"c1"},
		BaselineRuleSetID: "active", CandidateRuleSetID: "candidate", SnapshotAt: now,
	}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	eng := &cancellationTestEngine{started: make(chan struct{}), canceled: make(chan struct{})}
	worker := &Worker{Jobs: jobs, Customers: customers, Transactions: store.NewMemoryTransactionRepo(), Engine: eng}
	done := make(chan error, 1)
	go func() { done <- worker.RunOnce(ctx) }()

	select {
	case <-eng.started:
	case <-time.After(time.Second):
		t.Fatal("engine did not start")
	}
	if err := jobs.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-eng.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("engine context was not cancelled")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RunOnce returned nil after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("RunOnce did not return after cancellation")
	}
	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.BacktestJobCancelled || got.Error != "" {
		t.Fatalf("cancelled job was overwritten: %+v", got)
	}
}

func TestDiffResultReportsAddedAndRemovedCustomers(t *testing.T) {
	base := &domain.BacktestResult{TotalCustomers: 2, TotalAlerts: 1, ScenarioResults: []domain.BacktestScenarioResult{{
		ScenarioID: "scenario", AlertsGenerated: 1, AffectedCustomerIDs: []string{"c1", "c2"},
	}}}
	candidate := &domain.BacktestResult{TotalCustomers: 2, TotalAlerts: 2, ScenarioResults: []domain.BacktestScenarioResult{{
		ScenarioID: "scenario", AlertsGenerated: 2, AffectedCustomerIDs: []string{"c2", "c3"},
	}}}
	delta := diffResult(base, candidate)
	if delta == nil || len(delta.ScenarioResults) != 1 {
		t.Fatalf("delta = %+v, want one scenario", delta)
	}
	got := delta.ScenarioResults[0]
	if len(got.AddedCustomerIDs) != 1 || got.AddedCustomerIDs[0] != "c3" {
		t.Fatalf("added customers = %v, want [c3]", got.AddedCustomerIDs)
	}
	if len(got.RemovedCustomerIDs) != 1 || got.RemovedCustomerIDs[0] != "c1" {
		t.Fatalf("removed customers = %v, want [c1]", got.RemovedCustomerIDs)
	}
}
