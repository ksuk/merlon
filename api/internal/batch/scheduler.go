package batch

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
)

// DefaultTMBatchSchedule is transaction-monitoring.md's default daily run
// time for TM batch evaluation ("バッチ評価のスケジューリング": デフォルト実行時刻は
// 毎日02:00、システム設定のタイムゾーンに従う。設定で変更可能).
const DefaultTMBatchSchedule = "02:00"

// TMBatchEvaluationJobType is the job_type recorded on batch_runs
// (migrations/013_batch_runs.sql) for the daily TM batch evaluation.
const TMBatchEvaluationJobType = "tm_batch_evaluation"

// Scheduler runs a job once per day at a fixed HH:MM time. A full cron
// syntax is intentionally not supported: daily-at-fixed-time is the only
// schedule transaction-monitoring.md specifies for TM batch evaluation, so a
// time.Timer computing the delay until the next occurrence is sufficient
// without adding a third-party cron dependency.
type Scheduler struct {
	// Location is the timezone hour/minute are interpreted in
	// (transaction-monitoring.md「システム設定のタイムゾーンに従う」). Defaults to
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
		hour, minute, _ = parseHHMM(DefaultTMBatchSchedule)
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
// killed process, overview.md §4.4「再起動時は未処理分のみを再開」), its ID and
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
// a kill) skips it (overview.md §4.4). It stops and returns the first error
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
// (transaction-monitoring.md「バッチ評価のスケジューリング」). Task 7 extends this
// job's alert-creation step to route through
// domain.AlertRepository.CreateIfNotDuplicate and casemgmt.ConsolidateAlert
// instead of a plain Create, so it shares the dedup constraint with the
// realtime evaluation path.
type TMBatchEvaluationDeps struct {
	Runs         domain.BatchRunRepository
	Customers    domain.CustomerRepository
	Transactions domain.TransactionRepository
	Monitoring   engine.MonitoringEngine
	Alerts       domain.AlertRepository
}

// maxTMBatchCustomers bounds how many customers/transactions a single batch
// pass loads at once, mirroring server.maxBatchCustomers.
const maxTMBatchCustomers = 1000

// RunTMBatchEvaluation is the daily TM batch evaluation job body
// (TMBatchEvaluationJobType). It resumes an interrupted previous run
// instead of starting over (overview.md §4.4「再起動時は未処理分のみを再開」), and
// snapshots each customer's transactions at batch start so transactions
// arriving mid-run are left for the next batch (SnapshotBefore).
func RunTMBatchEvaluation(ctx context.Context, deps TMBatchEvaluationDeps, candidateRunID string) error {
	batchStart := time.Now()

	runID, alreadyProcessed, err := ResumeOrCreateRun(ctx, deps.Runs, TMBatchEvaluationJobType, candidateRunID)
	if err != nil {
		return err
	}

	customers, err := deps.Customers.List(ctx, maxTMBatchCustomers, 0)
	if err != nil {
		_ = deps.Runs.Fail(ctx, runID)
		return err
	}

	err = ProcessCustomersResumably(ctx, deps.Runs, runID, customers, alreadyProcessed, func(ctx context.Context, c *domain.Customer) error {
		return evaluateCustomerBatch(ctx, deps, c, batchStart)
	})
	if err != nil {
		_ = deps.Runs.Fail(ctx, runID)
		return err
	}

	return deps.Runs.Complete(ctx, runID)
}

func evaluateCustomerBatch(ctx context.Context, deps TMBatchEvaluationDeps, c *domain.Customer, batchStart time.Time) error {
	txns, err := deps.Transactions.ListByCustomer(ctx, c.ID, maxTMBatchCustomers, 0)
	if err != nil {
		return err
	}
	txns = SnapshotBefore(txns, batchStart)
	if len(txns) == 0 {
		return nil
	}

	riskTier := domain.RiskTierLow
	if c.RiskTier != nil {
		riskTier = *c.RiskTier
	}

	alerts, err := deps.Monitoring.EvaluateTransactions(ctx, c.ID, riskTier, txns, nil)
	if err != nil {
		return err
	}

	for i := range alerts {
		a := &alerts[i]
		a.ID = generateID()
		now := time.Now()
		a.CreatedAt = now
		a.UpdatedAt = now
		_ = deps.Alerts.Create(ctx, a)
	}
	return nil
}

// SnapshotBefore filters transactions to those ingested strictly before
// batchStart, so transactions arriving mid-run are excluded from the
// current batch and left for the next one
// (transaction-monitoring.md「バッチ実行中に到着した新規取引は次回バッチの対象とする」).
// Transaction.CreatedAt is set once at intake (server.handleCreateTransaction)
// and never modified afterward, so it serves directly as the ingestion
// timestamp without a dedicated ingested_at column.
func SnapshotBefore(transactions []domain.Transaction, batchStart time.Time) []domain.Transaction {
	out := make([]domain.Transaction, 0, len(transactions))
	for _, t := range transactions {
		if t.CreatedAt.Before(batchStart) {
			out = append(out, t)
		}
	}
	return out
}
