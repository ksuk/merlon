package backtest

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/transactionhistory"
)

type Worker struct {
	Jobs         domain.BacktestJobRepository
	Customers    domain.CustomerRepository
	Transactions domain.TransactionRepository
	Engine       engine.BacktestEngine
	Rules        domain.RuleRepository
	// OutcomeBuilder is optional because the current engine result contract is
	// aggregate-only. Engines that expose alert-shaped detections can inject a
	// matcher-backed builder without changing the durable job runner.
	OutcomeBuilder func(context.Context, *domain.BacktestJob) (*domain.BacktestOutcomeAnalysis, []domain.BacktestOutcomeDetail, error)
}

func (w *Worker) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = w.RunOnce(ctx) // failed jobs are durably marked; keep the worker alive
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	job, err := w.Jobs.ClaimNext(ctx)
	if err != nil || job == nil {
		return err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		w.monitorCancellation(jobCtx, job.ID, cancel)
	}()
	err = w.execute(jobCtx, job)
	cancel()
	<-monitorDone
	if err != nil {
		if ctx.Err() == nil {
			_ = w.Jobs.Fail(ctx, job.ID, err.Error())
		}
		return err
	}
	return nil
}

func (w *Worker) monitorCancellation(ctx context.Context, jobID string, cancel context.CancelFunc) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		job, err := w.Jobs.Get(ctx, jobID)
		if err == nil && job.Status == domain.BacktestJobCancelled {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) execute(ctx context.Context, job *domain.BacktestJob) error {
	customers, err := w.snapshotCustomers(ctx, job)
	if err != nil {
		return err
	}
	if err := w.Jobs.UpdateProgress(ctx, job.ID, 0, len(customers), nil); err != nil {
		return err
	}
	started := time.Now()
	allTxns := make([]domain.Transaction, 0)
	for i, c := range customers {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		txns, err := w.snapshotTransactions(ctx, c.ID, job)
		if err != nil {
			return err
		}
		allTxns = append(allTxns, txns...)
		var eta *int64
		if elapsed := time.Since(started); elapsed > 0 {
			remaining := len(customers) - (i + 1)
			secondsPerCustomer := elapsed.Seconds() / float64(i+1)
			value := int64(secondsPerCustomer * float64(remaining))
			eta = &value
		}
		if err := w.Jobs.UpdateProgress(ctx, job.ID, i+1, len(customers), eta); err != nil {
			return err
		}
	}
	baseline, err := w.runBacktest(ctx, customers, allTxns, job.ScenarioIDs, "baseline:"+job.BaselineRuleSetID, job.BaselineRuleSetID, job.BaselineRuleDefinition)
	if err != nil {
		return err
	}
	candidate, err := w.runBacktest(ctx, customers, allTxns, job.ScenarioIDs, "candidate:"+job.CandidateRuleSetID, job.CandidateRuleSetID, job.CandidateRuleDefinition)
	if err != nil {
		return err
	}
	delta := diffResult(baseline, candidate)
	if w.OutcomeBuilder != nil {
		repository, ok := w.Jobs.(domain.BacktestOutcomeRepository)
		if !ok {
			return fmt.Errorf("backtest outcome repository is not configured")
		}
		analysis, details, err := w.OutcomeBuilder(ctx, job)
		if err != nil {
			return err
		}
		if err := repository.SaveBacktestOutcomeAnalysis(ctx, job.ID, analysis, details); err != nil {
			return err
		}
	}
	return w.Jobs.Complete(ctx, job.ID, baseline, candidate, delta)
}

// runBacktest resolves a rule reference before invoking the engine. The
// active reference deliberately means the process' loaded, digest-pinned
// configuration. Any other reference uses the immutable definition captured
// when the job was created; legacy jobs without that snapshot resolve through
// the versioned repository. Failing here is safer than reporting a zero delta
// computed with the wrong rules.
func (w *Worker) runBacktest(ctx context.Context, customers []domain.Customer, transactions []domain.Transaction, scenarioIDs []string, description, ruleSetID string, definition []byte) (*domain.BacktestResult, error) {
	if ruleSetID == "" || ruleSetID == "active" || (len(definition) == 0 && w.Rules == nil) {
		return w.Engine.RunBacktest(ctx, customers, transactions, scenarioIDs, description)
	}
	if len(definition) == 0 {
		rule, err := w.Rules.GetActive(ctx, ruleSetID)
		if err != nil {
			return nil, fmt.Errorf("resolve rule set %q: %w", ruleSetID, err)
		}
		if rule == nil {
			return nil, fmt.Errorf("resolve rule set %q: empty definition", ruleSetID)
		}
		if rule.Type != domain.RuleTypeTMScenario {
			return nil, fmt.Errorf("rule set %q has unsupported type %q; backtests require TM_SCENARIO", ruleSetID, rule.Type)
		}
		definition = rule.Definition
	}
	versioned, ok := w.Engine.(engine.VersionedBacktestEngine)
	if !ok {
		return nil, fmt.Errorf("engine cannot replay versioned rule set %q", ruleSetID)
	}
	return versioned.RunBacktestWithRuleSet(ctx, customers, transactions, scenarioIDs, description, ruleSetID, definition)
}

// snapshotTransactions pins the same half-open event-time and inclusive
// ingestion-time snapshot semantics used by realtime and batch evaluation.
func (w *Worker) snapshotTransactions(ctx context.Context, customerID string, job *domain.BacktestJob) ([]domain.Transaction, error) {
	createdBefore := job.SnapshotAt
	if createdBefore.IsZero() {
		createdBefore = time.Now().UTC()
	}
	return transactionhistory.ListCustomerTransactionsAsOf(ctx, w.Transactions, customerID, transactionhistory.Query{
		From:           job.From,
		To:             job.To,
		CreatedThrough: createdBefore,
	})
}

func (w *Worker) snapshotCustomers(ctx context.Context, job *domain.BacktestJob) ([]domain.Customer, error) {
	var snapshots domain.BacktestCustomerSnapshotRepository
	if repo, ok := w.Jobs.(domain.BacktestCustomerSnapshotRepository); ok {
		snapshots = repo
		if ids, found, err := snapshots.GetCustomerSnapshot(ctx, job.ID); err != nil {
			return nil, err
		} else if found {
			return w.loadCustomersByID(ctx, ids)
		}
	}
	var ids []string
	var scannedCustomers []domain.Customer
	scannedFilter := false
	if len(job.CustomerIDs) > 0 {
		ids = append(ids, job.CustomerIDs...)
	} else {
		scannedFilter = true
		var after *domain.Cursor
		for {
			page, err := w.Customers.ListByCursor(ctx, 500, after)
			if err != nil {
				return nil, err
			}
			if len(page) == 0 {
				break
			}
			for _, c := range page {
				if !job.SnapshotAt.IsZero() && c.CreatedAt.After(job.SnapshotAt) {
					continue
				}
				if job.CustomerFilter.Matches(c) {
					ids = append(ids, c.ID)
					scannedCustomers = append(scannedCustomers, c)
				}
			}
			if len(page) < 500 {
				break
			}
			last := page[len(page)-1]
			after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
	}
	if snapshots != nil {
		if err := snapshots.SaveCustomerSnapshot(ctx, job.ID, ids); err != nil {
			return nil, err
		}
	}
	if scannedFilter {
		return scannedCustomers, nil
	}
	return w.loadCustomersByID(ctx, ids)
}

func (w *Worker) loadCustomersByID(ctx context.Context, ids []string) ([]domain.Customer, error) {
	out := make([]domain.Customer, 0, len(ids))
	for _, id := range ids {
		c, err := w.Customers.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, nil
}
func diffResult(base, cand *domain.BacktestResult) *domain.BacktestResult {
	if base == nil || cand == nil {
		return nil
	}
	d := *cand
	d.BacktestID = "delta"
	d.TotalAlerts = cand.TotalAlerts - base.TotalAlerts
	d.TotalTransactions = cand.TotalTransactions - base.TotalTransactions
	d.TotalCustomers = cand.TotalCustomers - base.TotalCustomers
	d.ScenarioResults = nil
	for _, c := range cand.ScenarioResults {
		var b *domain.BacktestScenarioResult
		for i := range base.ScenarioResults {
			if base.ScenarioResults[i].ScenarioID == c.ScenarioID {
				b = &base.ScenarioResults[i]
				break
			}
		}
		x := c
		if b != nil {
			x.AlertsGenerated -= b.AlertsGenerated
			x.HighSeverityCount -= b.HighSeverityCount
			x.MediumSeverityCount -= b.MediumSeverityCount
			x.LowSeverityCount -= b.LowSeverityCount
			baselineIDs := make(map[string]struct{}, len(b.AffectedCustomerIDs))
			for _, id := range b.AffectedCustomerIDs {
				baselineIDs[id] = struct{}{}
			}
			candidateIDs := make(map[string]struct{}, len(c.AffectedCustomerIDs))
			for _, id := range c.AffectedCustomerIDs {
				candidateIDs[id] = struct{}{}
				if _, exists := baselineIDs[id]; !exists {
					x.AddedCustomerIDs = append(x.AddedCustomerIDs, id)
				}
			}
			for _, id := range b.AffectedCustomerIDs {
				if _, exists := candidateIDs[id]; !exists {
					x.RemovedCustomerIDs = append(x.RemovedCustomerIDs, id)
				}
			}
			sort.Strings(x.AddedCustomerIDs)
			sort.Strings(x.RemovedCustomerIDs)
		}
		d.ScenarioResults = append(d.ScenarioResults, x)
	}
	return &d
}
