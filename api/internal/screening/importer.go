package screening

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// ListStore persists the current, last-successfully-imported content of a
// sanctions/PEP list keyed by list ID. RunImportJob only ever calls
// SaveList after a successful fetch (full replace); on fetch failure the
// previously saved list is left untouched so matching continues against it
// (the screening workflow "リスト取得に失敗した場合、前回成功時のリストで照合を継続する").
type ListStore interface {
	SaveList(ctx context.Context, data *RawListData) error
	GetList(ctx context.Context, listID string) (*RawListData, error)
}

// ListConsumer atomically replaces the screening lists used for matching.
// Implementations must treat the supplied lists as a complete snapshot.
type ListConsumer interface {
	ReplaceScreeningLists([]RawListData)
}

// FailureTracker counts consecutive fetch failures per list so RunImportJob
// can flag a list for an operational alert once the failure streak reaches
// staleFailureThreshold (the screening workflow, default 3 consecutive days).
type FailureTracker interface {
	RecordSuccess(ctx context.Context, listID string) error
	RecordFailure(ctx context.Context, listID string) (consecutiveFailures int, err error)
	ConsecutiveFailures(ctx context.Context, listID string) (int, error)
	// LastSuccessAt returns the timestamp of the most recent RecordSuccess
	// call for listID, used to publish merlon_screening_list_stale_days
	// (freshness.go).
	LastSuccessAt(ctx context.Context, listID string) (time.Time, error)
}

// FailureStatus is the safe, operator-facing status snapshot for one
// configured source.  It is deliberately additive to FailureTracker so older
// adapters remain source-compatible while the Wave 3 directory can expose
// last-attempt and safe diagnostics without scraping implementation details.
type FailureStatus struct {
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	ConsecutiveFailures int
	Diagnostic          string
}

// FailureStatusReader is an optional capability implemented by durable
// failure trackers.  Implementations must return a redacted diagnostic; raw
// upstream URLs, response bodies, and credentials must never reach the API.
type FailureStatusReader interface {
	FailureStatus(ctx context.Context, listID string) (FailureStatus, error)
}

// staleFailureThreshold is the default number of consecutive fetch failures
// after which an operational alert is required (the screening workflow "連続 N 日間
// （デフォルト：3 日）取得失敗した場合、運用アラート...を発行").
const staleFailureThreshold = 3

// ListImportOutcome records what RunImportJob did for one configured list
// adapter during a single run.
type ListImportOutcome struct {
	ListID                string
	Imported              bool
	Skipped               bool
	SkipReason            string
	Err                   error
	ConsecutiveFailures   int
	NeedsOperationalAlert bool
}

// ImportResult is the aggregate outcome of one RunImportJob invocation
// across all configured list adapters.
type ImportResult struct {
	Outcomes []ListImportOutcome
}

// RunImportJob fetches every configured list adapter and replaces the
// corresponding entry in store on success. A fetch error leaves the
// previously stored list untouched and increments that list's consecutive
// failure count; ErrPEPNotConfigured is treated as an intentional skip
// (audit-logged, not counted as a failure) rather than an error, per
// the screening workflow's PEP-not-configured handling.
func RunImportJob(ctx context.Context, adapters map[string]ListAdapter, store ListStore, failureTracker FailureTracker) (ImportResult, error) {
	listIDs := make([]string, 0, len(adapters))
	for id := range adapters {
		listIDs = append(listIDs, id)
	}
	sort.Strings(listIDs)

	var result ImportResult
	for _, listID := range listIDs {
		outcome, err := importOne(ctx, listID, adapters[listID], store, failureTracker)
		if err != nil {
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}

	recordFreshnessMetrics(ctx, listIDs, store, failureTracker)
	return result, nil
}

// recordFreshnessMetrics publishes merlon_screening_list_stale_days
// (freshness.go) for every configured list that has succeeded at least
// once; a list that has never succeeded has no known freshness yet and is
// left unreported rather than misreported as freshly imported.
func recordFreshnessMetrics(ctx context.Context, listIDs []string, listStore ListStore, failureTracker FailureTracker) {
	statuses := make([]ListImportStatus, 0, len(listIDs))
	for _, listID := range listIDs {
		data, err := listStore.GetList(ctx, listID)
		if err != nil {
			continue
		}
		lastSuccess, err := failureTracker.LastSuccessAt(ctx, listID)
		if err != nil {
			continue
		}
		statuses = append(statuses, ListImportStatus{ListID: listID, ListType: data.ListType, LastSuccessAt: lastSuccess})
	}
	RecordListFreshnessMetrics(ComputeListFreshness(statuses))
}

func importOne(ctx context.Context, listID string, adapter ListAdapter, store ListStore, failureTracker FailureTracker) (ListImportOutcome, error) {
	data, err := adapter.FetchList(ctx)

	switch {
	case errors.Is(err, ErrPEPNotConfigured):
		slog.Warn("screening list import skipped: provider not configured",
			"list_id", listID, "reason", "pep_not_configured")
		return ListImportOutcome{ListID: listID, Skipped: true, SkipReason: "pep_not_configured"}, nil

	case err != nil:
		n, trackErr := failureTracker.RecordFailure(ctx, listID)
		if trackErr != nil {
			return ListImportOutcome{}, fmt.Errorf("record failure for list %q: %w", listID, trackErr)
		}
		needsAlert := n >= staleFailureThreshold
		slog.Error("screening list import failed, continuing with previously imported list",
			"list_id", listID, "error", err,
			"consecutive_failures", n, "needs_operational_alert", needsAlert)
		return ListImportOutcome{
			ListID:                listID,
			Err:                   err,
			ConsecutiveFailures:   n,
			NeedsOperationalAlert: needsAlert,
		}, nil

	default:
		if err := store.SaveList(ctx, data); err != nil {
			return ListImportOutcome{}, fmt.Errorf("save list %q: %w", listID, err)
		}
		if err := failureTracker.RecordSuccess(ctx, listID); err != nil {
			return ListImportOutcome{}, fmt.Errorf("record success for list %q: %w", listID, err)
		}
		return ListImportOutcome{ListID: listID, Imported: true}, nil
	}
}

// RunImportJobPeriodically runs RunImportJob immediately, then again every
// interval, until ctx is cancelled. the screening workflow's default fetch schedule
// is daily at 03:00 JST; a precise time-of-day cron trigger is left as a
// future enhancement, so this exposes a tunable interval instead (wired
// from MERLON_SCREENING_IMPORT_INTERVAL in main.go).
func RunImportJobPeriodically(ctx context.Context, interval time.Duration, adapters map[string]ListAdapter, store ListStore, failureTracker FailureTracker) {
	RunImportJobPeriodicallyWithConsumer(ctx, interval, adapters, store, failureTracker, nil)
}

// RunImportJobPeriodicallyWithConsumer is the durable import loop plus an
// optional atomic consumer update (the native engine uses this to swap a
// last-good list snapshot without restarting the API process).
func RunImportJobPeriodicallyWithConsumer(ctx context.Context, interval time.Duration, adapters map[string]ListAdapter, store ListStore, failureTracker FailureTracker, consumer ListConsumer) {
	runOnce := func() {
		result, err := RunImportJob(ctx, adapters, store, failureTracker)
		if err != nil {
			slog.Error("screening list import job failed", "error", err)
		} else if consumer != nil {
			replaceConsumerSnapshot(ctx, adapters, store, result, consumer)
		}
	}
	runOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// replaceConsumerSnapshot reconstructs and atomically swaps the complete
// last-good snapshot. A partial snapshot must never be installed: an
// unexpected store read error retains the consumer's previous snapshot.
// The sole allowed absence is a PEP adapter intentionally skipped because it
// has never been configured; if a previous PEP snapshot exists it is retained.
func replaceConsumerSnapshot(ctx context.Context, adapters map[string]ListAdapter, store ListStore, result ImportResult, consumer ListConsumer) {
	intentionallyMissing := make(map[string]bool)
	for _, outcome := range result.Outcomes {
		if outcome.Skipped && outcome.SkipReason == "pep_not_configured" {
			intentionallyMissing[outcome.ListID] = true
		}
	}

	ids := make([]string, 0, len(adapters))
	for id := range adapters {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	lists := make([]RawListData, 0, len(ids))
	for _, id := range ids {
		data, err := store.GetList(ctx, id)
		if err != nil {
			if intentionallyMissing[id] && errors.Is(err, errListNotFound) {
				continue
			}
			slog.Error("screening list consumer snapshot rebuild failed; retaining previous snapshot",
				"list_id", id,
				"error", err,
				"needs_operational_alert", true)
			return
		}
		lists = append(lists, *data)
	}
	consumer.ReplaceScreeningLists(lists)
}

// MemoryListStore is the dev/test-only ListStore, mirroring the store
// package's Memory*Repo naming convention.
type MemoryListStore struct {
	mu   sync.RWMutex
	data map[string]*RawListData
}

func NewMemoryListStore() *MemoryListStore {
	return &MemoryListStore{data: make(map[string]*RawListData)}
}

func (s *MemoryListStore) SaveList(_ context.Context, data *RawListData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *data
	s.data[data.ListID] = &cp
	return nil
}

func (s *MemoryListStore) GetList(_ context.Context, listID string) (*RawListData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[listID]
	if !ok {
		return nil, fmt.Errorf("list %q: %w", listID, errListNotFound)
	}
	cp := *data
	return &cp, nil
}

// ErrListNotFound lets the API distinguish an expected never-imported source
// from a storage or decode failure without exposing store-specific errors.
var ErrListNotFound = errors.New("screening list not found")

// Keep the package-private name for existing package tests and callers.
var errListNotFound = ErrListNotFound

var errNoSuccessYet = errors.New("list has never been successfully imported")

// MemoryFailureTracker is the dev/test-only FailureTracker.
type MemoryFailureTracker struct {
	mu          sync.Mutex
	counts      map[string]int
	lastOK      map[string]time.Time
	lastAttempt map[string]time.Time
	lastFailure map[string]time.Time
	diagnostic  map[string]string
}

func NewMemoryFailureTracker() *MemoryFailureTracker {
	return &MemoryFailureTracker{
		counts: make(map[string]int), lastOK: make(map[string]time.Time),
		lastAttempt: make(map[string]time.Time), lastFailure: make(map[string]time.Time),
		diagnostic: make(map[string]string),
	}
}

func (t *MemoryFailureTracker) RecordSuccess(_ context.Context, listID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UTC()
	t.counts[listID] = 0
	t.lastOK[listID] = now
	t.lastAttempt[listID] = now
	delete(t.lastFailure, listID)
	delete(t.diagnostic, listID)
	return nil
}

func (t *MemoryFailureTracker) LastSuccessAt(_ context.Context, listID string) (time.Time, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ts, ok := t.lastOK[listID]
	if !ok {
		return time.Time{}, fmt.Errorf("list %q: %w", listID, errNoSuccessYet)
	}
	return ts, nil
}

func (t *MemoryFailureTracker) RecordFailure(_ context.Context, listID string) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UTC()
	t.counts[listID]++
	t.lastAttempt[listID] = now
	t.lastFailure[listID] = now
	t.diagnostic[listID] = "source fetch failed"
	return t.counts[listID], nil
}

func (t *MemoryFailureTracker) FailureStatus(_ context.Context, listID string) (FailureStatus, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	status := FailureStatus{ConsecutiveFailures: t.counts[listID], Diagnostic: t.diagnostic[listID]}
	if ts, ok := t.lastAttempt[listID]; ok {
		copy := ts
		status.LastAttemptAt = &copy
	}
	if ts, ok := t.lastOK[listID]; ok {
		copy := ts
		status.LastSuccessAt = &copy
	}
	if ts, ok := t.lastFailure[listID]; ok {
		copy := ts
		status.LastFailureAt = &copy
	}
	return status, nil
}

func (t *MemoryFailureTracker) ConsecutiveFailures(_ context.Context, listID string) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counts[listID], nil
}
