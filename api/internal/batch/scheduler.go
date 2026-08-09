package batch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/casemgmt"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/metrics"
	"github.com/ksuk/merlon/api/internal/transactionhistory"
)

// DefaultTMBatchSchedule is the transaction-monitoring design's default daily run
// time for TM batch evaluation ("バッチ評価のスケジューリング": デフォルト実行時刻は
// 毎日02:00、システム設定のタイムゾーンに従う。設定で変更可能).
const DefaultTMBatchSchedule = "02:00"

// TMBatchEvaluationJobType is the job_type recorded on batch_runs
// (migrations/013_batch_runs.sql) for the daily TM batch evaluation.
const TMBatchEvaluationJobType = "tm_batch_evaluation"

// Scheduler runs a job once per day at a fixed HH:MM time. A full cron
// syntax is intentionally not supported: daily-at-fixed-time is the only
// schedule the transaction-monitoring design specifies for TM batch evaluation, so a
// time.Timer computing the delay until the next occurrence is sufficient
// without adding a third-party cron dependency.
type Scheduler struct {
	// Location is the timezone hour/minute are interpreted in
	// (the transaction-monitoring design「システム設定のタイムゾーンに従う」). Defaults to
	// time.Local; set directly after construction to override.
	Location *time.Location

	hour, minute int
	job          func(ctx context.Context, runID string) error
	now          func() time.Time // overridable in tests
}

// NewScheduler parses cronExpr as an "HH:MM" (24-hour) daily run time and
// returns a Scheduler that invokes job once per day at that time. An
// unparsable cronExpr falls back to DefaultTMBatchSchedule rather than
// disabling the schedule (Fail-Alert: a malformed config value should not
// silently stop the batch from ever running).
func NewScheduler(cronExpr string, job func(ctx context.Context, runID string) error) *Scheduler {
	hour, minute, err := parseHHMM(cronExpr)
	if err != nil {
		hour, minute = 2, 0
	}
	return &Scheduler{
		Location: time.Local,
		hour:     hour,
		minute:   minute,
		job:      job,
		now:      time.Now,
	}
}

func parseHHMM(v string) (hour, minute int, err error) {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid HH:MM schedule: %q", v)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour in schedule: %q", v)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute in schedule: %q", v)
	}
	return hour, minute, nil
}

// Start blocks, invoking job once per day at hour:minute (in Location) until
// ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	for {
		timer := time.NewTimer(s.durationUntilNext())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if _, err := s.RunNow(ctx); err != nil {
				slog.ErrorContext(ctx, "scheduled batch run failed", "error", err)
			}
		}
	}
}

func (s *Scheduler) durationUntilNext() time.Duration {
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	now := s.now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), s.hour, s.minute, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}

// RunNow invokes job immediately with a freshly generated candidate
// batch_run_id and returns that ID alongside any error the job returned. A
// job resuming a previously interrupted run (via ResumeOrCreateRun) may
// reuse an existing run's ID internally instead of the candidate.
func (s *Scheduler) RunNow(ctx context.Context) (string, error) {
	runID := generateID()
	return runID, s.job(ctx, runID)
}

// ResumeOrCreateRun returns the batch_run_id a job invocation should use: if
// a previous run of jobType is still status=running (left behind by a
// killed process, the operational design §4.4「再起動時は未処理分のみを再開」), its ID and
// recorded progress are reused so ProcessCustomersResumably skips
// already-processed customers; otherwise a new run is created under
// candidateID.
func ResumeOrCreateRun(ctx context.Context, runs domain.BatchRunRepository, jobType, candidateID string) (runID string, alreadyProcessed map[string]bool, err error) {
	existing, err := runs.GetLatestRunning(ctx, jobType)
	if err != nil {
		return "", nil, err
	}
	if existing != nil {
		processed := make(map[string]bool, len(existing.ProcessedCustomerIDs))
		for _, id := range existing.ProcessedCustomerIDs {
			processed[id] = true
		}
		return existing.ID, processed, nil
	}

	if err := runs.Create(ctx, &domain.BatchRun{
		ID:      candidateID,
		JobType: jobType,
		Status:  domain.BatchRunStatusRunning,
	}); err != nil {
		return "", nil, err
	}
	return candidateID, map[string]bool{}, nil
}

// ProcessCustomersResumably runs process for each customer not already
// present in alreadyProcessed, recording each one via
// runs.AppendProcessedCustomer as it completes so a subsequent resume (after
// a kill) skips it (the operational design §4.4). It stops and returns the first error
// from process, leaving customers not yet reached unprocessed for the next
// resume.
func ProcessCustomersResumably(
	ctx context.Context,
	runs domain.BatchRunRepository,
	runID string,
	customers []domain.Customer,
	alreadyProcessed map[string]bool,
	process func(ctx context.Context, c *domain.Customer) error,
) error {
	for i := range customers {
		c := &customers[i]
		if alreadyProcessed[c.ID] {
			continue
		}
		if err := process(ctx, c); err != nil {
			return err
		}
		if err := runs.AppendProcessedCustomer(ctx, runID, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// TMBatchEvaluationDeps bundles what RunTMBatchEvaluation needs to evaluate
// every customer's transactions in one daily batch pass
// (the transaction-monitoring design「バッチ評価のスケジューリング」). Alert creation routes
// through domain.AlertRepository.CreateIfNotDuplicate and (for newly created
// alerts) casemgmt.ConsolidateAlert, sharing the dedup constraint and case
// consolidation with the realtime evaluation path (server.handleBatchMonitor).
type TMBatchEvaluationDeps struct {
	Runs          domain.BatchRunRepository
	Customers     domain.CustomerRepository
	Transactions  domain.TransactionRepository
	Monitoring    engine.MonitoringEngine
	Alerts        domain.AlertRepository
	Cases         domain.CaseRepository
	CaseLifecycle domain.CaseAlertLifecycleRepository
	ConfigDigests map[string]string
}

func copyDigests(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// maxTMBatchCustomers bounds the customer page size for a batch pass. Transaction
// history is streamed in event-time pages separately, so this is not a
// transaction-count cap.
const maxTMBatchCustomers = 1000

// RunTMBatchEvaluation is the daily TM batch evaluation job body
// (TMBatchEvaluationJobType). It resumes an interrupted previous run
// instead of starting over (the operational design §4.4「再起動時は未処理分のみを再開」), and
// snapshots each customer's transactions at batch start so transactions
// arriving mid-run are left for the next batch (SnapshotBefore).
func RunTMBatchEvaluation(ctx context.Context, deps TMBatchEvaluationDeps, candidateRunID string) error {
	batchStart := time.Now()

	runID, alreadyProcessed, err := ResumeOrCreateRun(ctx, deps.Runs, TMBatchEvaluationJobType, candidateRunID)
	if err != nil {
		return err
	}

	// Walk the customer book with keyset pages. This bounds memory and avoids
	// OFFSET degradation as the customer table grows; a page may be replayed
	// after a crash, and the existing idempotent checkpoint skips completed IDs.
	var after *domain.Cursor
	for {
		customers, pageErr := deps.Customers.ListByCursor(ctx, maxTMBatchCustomers, after)
		if pageErr != nil {
			return failRun(ctx, deps.Runs, runID, pageErr)
		}
		if len(customers) == 0 {
			break
		}
		if err := ProcessCustomersResumably(ctx, deps.Runs, runID, customers, alreadyProcessed, func(ctx context.Context, c *domain.Customer) error {
			return evaluateCustomerBatch(ctx, deps, c, batchStart, runID)
		}); err != nil {
			return failRun(ctx, deps.Runs, runID, err)
		}
		last := customers[len(customers)-1]
		after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		if len(customers) < maxTMBatchCustomers {
			break
		}
	}

	if err := deps.Runs.Complete(ctx, runID); err != nil {
		return fmt.Errorf("complete batch run %s: %w", runID, err)
	}
	return nil
}

func failRun(ctx context.Context, runs domain.BatchRunRepository, runID string, cause error) error {
	if err := runs.Fail(ctx, runID); err != nil {
		return errors.Join(cause, fmt.Errorf("mark batch run failed: %w", err))
	}
	return cause
}

func evaluateCustomerBatch(ctx context.Context, deps TMBatchEvaluationDeps, c *domain.Customer, batchStart time.Time, runID string) error {
	// the data model §1.1.2: closed customers stop TM evaluation entirely;
	// dormant customers are evaluated only "取引発生時" (at the moment a
	// transaction occurs via the realtime path), not on this periodic
	// schedule. frozen customers continue on existing data as usual.
	switch c.EffectiveStatus() {
	case domain.CustomerStatusClosed, domain.CustomerStatusDormant:
		return nil
	}

	txns, err := snapshotCustomerTransactions(ctx, deps.Transactions, c.ID, batchStart)
	if err != nil {
		return err
	}
	if len(txns) == 0 {
		return nil
	}

	riskTier := domain.RiskTierLow
	if c.RiskTier != nil {
		riskTier = *c.RiskTier
	}

	alerts, err := engine.EvaluateCompat(ctx, deps.Monitoring, engine.MonitoringRequest{CustomerID: c.ID, CustomerType: c.CustomerType, RiskTier: riskTier, Transactions: txns, Mode: engine.EvaluationModeBatch, EvaluatedAt: batchStart, ConfigDigests: copyDigests(deps.ConfigDigests)})
	if err != nil {
		return err
	}

	for i := range alerts {
		a := &alerts[i]
		a.ID = generateID()
		now := time.Now()
		a.CreatedAt = now
		a.UpdatedAt = now
		if a.DetectedAt.IsZero() {
			a.DetectedAt = now
		}
		windowStart := domain.DailyAggregationWindowStart(a.DetectedAt)
		a.AggregationWindowStart = &windowStart
		a.BatchRunID = runID

		created, existing, err := deps.Alerts.CreateIfNotDuplicate(ctx, a)
		if err != nil {
			metrics.AlertPersistenceFailuresTotal.WithLabelValues("create").Inc()
			return fmt.Errorf("persist batch alert for customer %s: %w", c.ID, err)
		}
		if !created {
			if existing != nil {
				if err := deps.Alerts.AnnotateBatchReviewed(ctx, existing.ID, runID); err != nil {
					metrics.AlertPersistenceFailuresTotal.WithLabelValues("annotate").Inc()
					return fmt.Errorf("annotate duplicate alert %s: %w", existing.ID, err)
				}
			}
			continue
		}
		if deps.Cases != nil {
			var err error
			if deps.CaseLifecycle != nil {
				_, err = casemgmt.ConsolidateAlertWithLifecycle(ctx, deps.Cases, deps.CaseLifecycle, a, casemgmt.DefaultConsolidationWindow)
			} else {
				_, err = casemgmt.ConsolidateAlert(ctx, deps.Cases, a, casemgmt.DefaultConsolidationWindow)
			}
			if err != nil {
				metrics.AlertPersistenceFailuresTotal.WithLabelValues("case_consolidation").Inc()
				return fmt.Errorf("consolidate alert %s: %w", a.ID, err)
			}
		}
	}
	return nil
}

// snapshotCustomerTransactions reads the complete history available at the
// batch snapshot. PostgreSQL and the in-memory store expose the event-time
// keyset capability; the cursor fallback keeps adapter compatibility while
// retaining the same cutoff semantics.
func snapshotCustomerTransactions(ctx context.Context, repo domain.TransactionRepository, customerID string, snapshot time.Time) ([]domain.Transaction, error) {
	return transactionhistory.ListCustomerTransactionsAsOf(ctx, repo, customerID, transactionhistory.Query{
		From:                   time.Time{},
		To:                     snapshot,
		CreatedThrough:         snapshot,
		CreatedBeforeExclusive: true,
	})
}
