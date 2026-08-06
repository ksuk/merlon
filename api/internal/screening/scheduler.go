package screening

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// TriggerType is the reason a rescreening batch was started (the screening workflow
// §CDDティア連動の再照合頻度: periodic schedule triggers plus four immediate
// triggers).
type TriggerType string

const (
	TriggerScheduledDaily   TriggerType = "scheduled_daily"
	TriggerScheduledWeekly  TriggerType = "scheduled_weekly"
	TriggerScheduledMonthly TriggerType = "scheduled_monthly"
	TriggerListUpdated      TriggerType = "list_updated"
	TriggerTierPromoted     TriggerType = "tier_promoted"
	TriggerCustomerChanged  TriggerType = "customer_changed"
	TriggerAccountOpened    TriggerType = "account_opened"
	TriggerAPIRequest       TriggerType = "api_request"
)

// RescreeningIntervalDays returns the default periodic rescreening interval,
// in days, for a CDD risk tier (the screening workflow §CDDティア連動の再照合頻度:
// High=daily, Medium=weekly, Low=monthly).
func RescreeningIntervalDays(tier domain.RiskTier) int {
	switch tier {
	case domain.RiskTierHigh:
		return 1
	case domain.RiskTierMedium:
		return 7
	case domain.RiskTierLow:
		return 30
	default:
		return 30
	}
}

// IsDueForRescreening reports whether a customer at tier, whose last
// screening happened elapsedDays ago, is due for periodic rescreening.
func IsDueForRescreening(tier domain.RiskTier, elapsedDays int) bool {
	return elapsedDays >= RescreeningIntervalDays(tier)
}

// scheduledTierFor maps a periodic-schedule trigger to the single risk tier
// it applies to. TriggerListUpdated and the single-customer immediate
// triggers are not periodic-schedule triggers and return ok=false (they
// apply across tiers or target one specific customer, not this fixed
// per-tier gating).
func scheduledTierFor(trigger TriggerType) (domain.RiskTier, bool) {
	switch trigger {
	case TriggerScheduledDaily:
		return domain.RiskTierHigh, true
	case TriggerScheduledWeekly:
		return domain.RiskTierMedium, true
	case TriggerScheduledMonthly:
		return domain.RiskTierLow, true
	default:
		return "", false
	}
}

// tierPriority orders customers High -> Medium -> Low -> (unscored) for
// list-update rescreening (the screening workflow "リスト更新時の再照合は...High → Medium
// → Low の優先順で実行").
func tierPriority(tier *domain.RiskTier) int {
	if tier == nil {
		return 3
	}
	switch *tier {
	case domain.RiskTierHigh:
		return 0
	case domain.RiskTierMedium:
		return 1
	case domain.RiskTierLow:
		return 2
	default:
		return 3
	}
}

// SchedulerDeps are the dependencies RunRescreeningBatch needs: the
// customer book, the screening engine's single-shot ScreenCustomer call
// (the existing api/internal/engine.ScreeningEngine, whose signature this
// WS does not change), and the persisted screening_results store.
type SchedulerDeps struct {
	Customers domain.CustomerRepository
	Screening interface {
		ScreenCustomer(ctx context.Context, customer *domain.Customer, listIDs []string) (*domain.ScreenResult, error)
	}
	Results  domain.ScreeningResultRepository
	Workflow domain.ScreeningWorkflowRepository
	// PersistWorkflow lets an API composition supply the same transaction
	// boundary used for audit/outbox evidence. Tests and older package-level
	// callers can continue to use Workflow directly.
	PersistWorkflow func(context.Context, *domain.ScreeningRun, []domain.ScreeningResultRecord) error
	ConfigDigests   map[string]string
	Actor           string
	ListIDs         []string

	// TargetCustomerID restricts the batch to a single customer, used for
	// the tier_promoted/customer_changed/account_opened/api_request
	// immediate triggers, which concern one specific customer rather than
	// the whole book (the screening workflow 即時再照合契機). Left empty for the
	// batch triggers (scheduled_daily/weekly/monthly, list_updated).
	TargetCustomerID string

	// BatchLimit caps how many customers Customers.List fetches for the
	// batch snapshot; defaults to 10000 when zero.
	BatchLimit int

	// Now returns the current time; defaults to time.Now. Tests inject a
	// fixed clock so batch-start-relative comparisons are deterministic.
	Now func() time.Time
}

func (d SchedulerDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d SchedulerDeps) batchLimit() int {
	if d.BatchLimit > 0 {
		return d.BatchLimit
	}
	return 10000
}

// CustomerScreenOutcome records what RunRescreeningBatch did for one
// customer in the batch.
type CustomerScreenOutcome struct {
	CustomerID string `json:"customer_id"`
	Screened   bool   `json:"screened"`
	Skipped    bool   `json:"skipped"`
	SkipReason string `json:"skip_reason,omitempty"`
	Err        error  `json:"error,omitempty"`
}

// BatchResult is the aggregate outcome of one RunRescreeningBatch call.
type BatchResult struct {
	Trigger  TriggerType             `json:"trigger"`
	Outcomes []CustomerScreenOutcome `json:"outcomes"`
}

// RunRescreeningBatch screens either a single targeted customer
// (deps.TargetCustomerID, for the immediate triggers) or the customer book
// as of the moment this function is called (for the periodic/list-update
// triggers). The customer set is snapshotted once at the start and never
// re-queried mid-batch (the screening workflow "対象顧客セットのスナップショット...
// バッチ開始後に追加された新規顧客は...当該バッチの対象には含めない").
func RunRescreeningBatch(ctx context.Context, deps SchedulerDeps, trigger TriggerType) (BatchResult, error) {
	batchStart := deps.now()
	result := BatchResult{Trigger: trigger}

	if deps.TargetCustomerID != "" {
		c, err := deps.Customers.Get(ctx, deps.TargetCustomerID)
		if err != nil {
			return result, fmt.Errorf("get target customer %q: %w", deps.TargetCustomerID, err)
		}
		result.Outcomes = append(result.Outcomes, screenOneForBatch(ctx, deps, c, batchStart))
		return result, nil
	}

	all, err := deps.Customers.List(ctx, deps.batchLimit(), 0)
	if err != nil {
		return result, fmt.Errorf("list customers: %w", err)
	}

	snapshot := selectCustomersForTrigger(ctx, deps, all, trigger)

	for i := range snapshot {
		result.Outcomes = append(result.Outcomes, screenOneForBatch(ctx, deps, &snapshot[i], batchStart))
	}
	return result, nil
}

// selectCustomersForTrigger filters and orders the batch snapshot: a
// periodic-schedule trigger only includes customers at its designated tier
// who are actually due (elapsed >= the tier's interval); list_updated
// includes every customer regardless of tier or due-ness, ordered
// High -> Medium -> Low (the screening workflow).
func selectCustomersForTrigger(ctx context.Context, deps SchedulerDeps, all []domain.Customer, trigger TriggerType) []domain.Customer {
	tier, isScheduled := scheduledTierFor(trigger)

	filtered := make([]domain.Customer, 0, len(all))
	for _, c := range all {
		// the data model §1.1.2: closed customers stop periodic rescreening;
		// dormant continues (undetected sanctions listing during dormancy is
		// exactly the risk this rescreening cadence exists to catch).
		if c.EffectiveStatus() == domain.CustomerStatusClosed {
			continue
		}
		if isScheduled {
			if c.RiskTier == nil || *c.RiskTier != tier {
				continue
			}
			if !isDueByLastScreening(ctx, deps, c.ID, tier) {
				continue
			}
		}
		filtered = append(filtered, c)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return tierPriority(filtered[i].RiskTier) < tierPriority(filtered[j].RiskTier)
	})
	return filtered
}

func isDueByLastScreening(ctx context.Context, deps SchedulerDeps, customerID string, tier domain.RiskTier) bool {
	if deps.Results == nil {
		return true
	}
	recent, err := deps.Results.ListByCustomer(ctx, customerID, 1, 0)
	if err != nil || len(recent) == 0 {
		return true // never screened: due immediately
	}
	elapsedDays := int(deps.now().Sub(recent[0].ScreenedAt).Hours() / 24)
	return IsDueForRescreening(tier, elapsedDays)
}

// screenOneForBatch screens a single customer, first checking whether an
// immediate rescreen (e.g. a name change) already screened this customer
// after batchStart; if so, the batch defers to it instead of duplicating
// the work (the screening workflow "氏名変更との排他制御...判定は screening_results.screened_at
// のタイムスタンプ比較で行う").
func screenOneForBatch(ctx context.Context, deps SchedulerDeps, c *domain.Customer, batchStart time.Time) CustomerScreenOutcome {
	if deps.Results != nil {
		recent, err := deps.Results.ListByCustomer(ctx, c.ID, 1, 0)
		if err == nil && len(recent) > 0 && recent[0].ScreenedAt.After(batchStart) {
			slog.Info("rescreening batch: skipping customer, an immediate rescreen already superseded this batch",
				"customer_id", c.ID, "reason", "immediate_rescreen_duplicate")
			return CustomerScreenOutcome{CustomerID: c.ID, Skipped: true, SkipReason: "immediate_rescreen_duplicate"}
		}
	}

	if deps.Screening == nil {
		return persistScreeningFailure(ctx, deps, c, errors.New("screening engine not configured"))
	}

	screenResult, err := deps.Screening.ScreenCustomer(ctx, c, deps.ListIDs)
	if err != nil {
		return persistScreeningFailure(ctx, deps, c, err)
	}

	records := make([]domain.ScreeningResultRecord, 0, len(screenResult.Matches))
	for _, m := range screenResult.Matches {
		records = append(records, domain.ScreeningResultRecord{
			ID: newScreeningResultID(), CustomerID: c.ID, ListID: m.ListID, ListType: m.ListType,
			EntryID: m.EntryID, MatchedName: m.MatchedName, Similarity: m.Similarity,
			Status: domain.ScreeningResultStatusNew, ScreenedAt: screenResult.ScreenedAt, CreatedAt: screenResult.ScreenedAt,
			MatchEvidence: map[string]any{"source": m.Source},
		})
	}
	if deps.PersistWorkflow != nil || deps.Workflow != nil {
		runAt := screenResult.ScreenedAt
		run := &domain.ScreeningRun{ID: newScreeningResultID(), CustomerID: c.ID, ListIDs: append([]string(nil), deps.ListIDs...), ConfigDigests: copyStringMap(deps.ConfigDigests), Status: domain.ScreeningRunCompleted, StartedAt: runAt, CreatedAt: runAt, Actor: deps.Actor}
		persist := deps.PersistWorkflow
		if persist == nil {
			persist = deps.Workflow.PersistScreeningRun
		}
		if err := persist(ctx, run, records); err != nil {
			return CustomerScreenOutcome{CustomerID: c.ID, Err: fmt.Errorf("persist screening run: %w", err)}
		}
	} else if deps.Results != nil {
		for i := range records {
			rec := records[i]
			if err := deps.Results.Create(ctx, &rec); err != nil {
				slog.Error("rescreening batch: failed to persist screening result", "customer_id", c.ID, "list_id", rec.ListID, "entry_id", rec.EntryID, "error", err)
			}
		}
	}
	return CustomerScreenOutcome{CustomerID: c.ID, Screened: true}
}

func persistScreeningFailure(ctx context.Context, deps SchedulerDeps, c *domain.Customer, cause error) CustomerScreenOutcome {
	outcome := CustomerScreenOutcome{CustomerID: c.ID, Err: cause}
	persist := deps.PersistWorkflow
	if persist == nil && deps.Workflow != nil {
		persist = deps.Workflow.PersistScreeningRun
	}
	if persist == nil {
		return outcome
	}
	now := deps.now().UTC()
	run := &domain.ScreeningRun{
		ID: newScreeningResultID(), CustomerID: c.ID, ListIDs: append([]string(nil), deps.ListIDs...),
		ConfigDigests: copyStringMap(deps.ConfigDigests), Status: domain.ScreeningRunFailed,
		Error: cause.Error(), Actor: deps.Actor, StartedAt: now, CompletedAt: &now, CreatedAt: now,
	}
	if err := persist(ctx, run, nil); err != nil {
		outcome.Err = fmt.Errorf("%w (failed-run persistence: %v)", cause, err)
	}
	return outcome
}

func newScreeningResultID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// runFunc matches RunRescreeningBatch's signature; Scheduler depends on
// this narrower type rather than the function directly so tests can inject
// a fake with controlled timing/concurrency.
type runFunc func(ctx context.Context, deps SchedulerDeps, trigger TriggerType) (BatchResult, error)

// Scheduler serializes rescreening batch executions. Only one batch ever
// runs at a time; a trigger that arrives while a batch is already running
// is recorded as pending (collapsing any number of triggers that arrive
// meanwhile into a single follow-up run) rather than starting a second,
// concurrent batch (the screening workflow "実行中のバッチは中断せず完了まで継続する。新しい
// 更新は完了後にキューイングされ、連続して実行する（多重実行はしない）").
type Scheduler struct {
	deps SchedulerDeps
	run  runFunc

	mu      sync.Mutex
	running bool
	pending *TriggerType
}

// NewScheduler builds a Scheduler backed by RunRescreeningBatch and deps.
func NewScheduler(deps SchedulerDeps) *Scheduler {
	return &Scheduler{deps: deps, run: RunRescreeningBatch}
}

// newTestScheduler is test-only: it injects a fake run function so tests
// can control batch timing/concurrency without a real SchedulerDeps.
func newTestScheduler(run runFunc) *Scheduler {
	return &Scheduler{run: run}
}

// Trigger requests a rescreening batch for trigger. If no batch is
// currently running, this call runs it (and any collapsed follow-up
// batches) synchronously on the calling goroutine. If a batch is already
// running (on another goroutine), this call just records trigger as
// pending and returns immediately without blocking or starting a second
// batch.
func (s *Scheduler) Trigger(ctx context.Context, trigger TriggerType) {
	s.mu.Lock()
	if s.running {
		s.pending = &trigger
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.runLoop(ctx, trigger)
}

func (s *Scheduler) runLoop(ctx context.Context, trigger TriggerType) {
	for {
		if _, err := s.run(ctx, s.deps, trigger); err != nil {
			slog.Error("rescreening batch failed", "trigger", trigger, "error", err)
		}

		s.mu.Lock()
		if s.pending == nil {
			s.running = false
			s.mu.Unlock()
			return
		}
		trigger = *s.pending
		s.pending = nil
		s.mu.Unlock()
	}
}

// RunPeriodic triggers the tier-appropriate scheduled rescreening batch
// (High=daily/Medium=weekly/Low=monthly, the screening workflow §CDDティア連動の再照合
// 頻度) by polling every checkInterval for a UTC day/week/month boundary
// crossing, until ctx is cancelled. Because Trigger already serializes
// execution, a batch still running when a boundary is crossed is not
// interrupted; the new trigger simply queues behind it
// (the screening workflow バッチ実行中の排他制御).
func (s *Scheduler) RunPeriodic(ctx context.Context, checkInterval time.Duration) {
	now := time.Now().UTC()
	lastDay := now.YearDay()
	lastYear := now.Year()
	lastWeekYear, lastWeek := now.ISOWeek()
	lastMonth := now.Month()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			if now.YearDay() != lastDay || now.Year() != lastYear {
				lastDay = now.YearDay()
				s.Trigger(ctx, TriggerScheduledDaily)
			}
			if y, w := now.ISOWeek(); y != lastWeekYear || w != lastWeek {
				lastWeekYear, lastWeek = y, w
				s.Trigger(ctx, TriggerScheduledWeekly)
			}
			if now.Month() != lastMonth || now.Year() != lastYear {
				lastMonth = now.Month()
				s.Trigger(ctx, TriggerScheduledMonthly)
			}
			lastYear = now.Year()
		}
	}
}
