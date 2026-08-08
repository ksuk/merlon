package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryWave3Repo is the database-free implementation of the Wave 3 durable
// operator workflows.  A single mutex gives review/confirmation operations a
// compare-and-swap boundary equivalent to the PostgreSQL implementation.
type MemoryWave3Repo struct {
	mu      sync.RWMutex
	runs    map[string]*domain.ScreeningRun
	results map[string]*domain.ScreeningResultRecord
	history map[string][]domain.ScreeningResultHistoryEntry
	// snapshots holds unjudged importer facts; domain.ClassifyScreeningSource
	// turns them into an operational state at read time, so this store cannot
	// drift from the PostgreSQL one.
	snapshots map[string]domain.ScreeningSourceSnapshot
	metadata  map[string]*domain.BacktestMetadata
	manifests map[string]*domain.TargetManifest
	identity  map[string][]domain.CustomerIdentityHistoryEntry
	cases     *MemoryCaseRepo

	persistFailure error
}

func NewMemoryWave3Repo() *MemoryWave3Repo {
	return &MemoryWave3Repo{
		runs: make(map[string]*domain.ScreeningRun), results: make(map[string]*domain.ScreeningResultRecord),
		history: make(map[string][]domain.ScreeningResultHistoryEntry), snapshots: make(map[string]domain.ScreeningSourceSnapshot),
		metadata: make(map[string]*domain.BacktestMetadata), manifests: make(map[string]*domain.TargetManifest),
		identity: make(map[string][]domain.CustomerIdentityHistoryEntry),
	}
}

func wave3ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString(make([]byte, 16))
	}
	return hex.EncodeToString(b)
}

func cloneRun(in *domain.ScreeningRun) *domain.ScreeningRun {
	if in == nil {
		return nil
	}
	out := *in
	out.ListIDs = append([]string(nil), in.ListIDs...)
	out.DegradedSources = append([]string(nil), in.DegradedSources...)
	out.ConfigDigests = copyStringMap(in.ConfigDigests)
	return &out
}

func cloneScreeningRecord(in *domain.ScreeningResultRecord) *domain.ScreeningResultRecord {
	if in == nil {
		return nil
	}
	out := *in
	out.DegradedSources = append([]string(nil), in.DegradedSources...)
	if in.MatchEvidence != nil {
		out.MatchEvidence = copyAnyMap(in.MatchEvidence)
	}
	return &out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *MemoryWave3Repo) PersistScreeningRun(_ context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.persistFailure != nil {
		return r.persistFailure
	}
	if run == nil || run.ID == "" {
		return &domain.ErrConflict{Entity: "screening_run", Reason: "id is required"}
	}
	if _, exists := r.runs[run.ID]; exists {
		return &domain.ErrConflict{Entity: "screening_run", ID: run.ID, Reason: "already exists"}
	}
	now := time.Now().UTC()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = run.StartedAt
	}
	if run.Status == "" {
		run.Status = domain.ScreeningRunCompleted
	}
	if run.CompletedAt == nil {
		completed := now
		run.CompletedAt = &completed
	}
	run.ResultCount = len(results)
	r.runs[run.ID] = cloneRun(run)
	for i := range results {
		result := results[i]
		result.RunID = run.ID
		if result.ID == "" {
			result.ID = wave3ID()
		}
		if result.Version == 0 {
			result.Version = 1
		}
		if result.CreatedAt.IsZero() {
			result.CreatedAt = run.CreatedAt
		}
		if result.UpdatedAt.IsZero() {
			result.UpdatedAt = result.CreatedAt
		}
		if result.MatchEvidence == nil {
			result.MatchEvidence = map[string]any{}
		}
		r.results[result.ID] = cloneScreeningRecord(&result)
	}
	return nil
}

func (r *MemoryWave3Repo) GetScreeningRun(_ context.Context, id string) (*domain.ScreeningRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "screening_run", ID: id}
	}
	return cloneRun(run), nil
}

func (r *MemoryWave3Repo) GetScreeningResult(_ context.Context, id string) (*domain.ScreeningResultRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.results[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "screening_result", ID: id}
	}
	return cloneScreeningRecord(result), nil
}

func (r *MemoryWave3Repo) ListScreeningRuns(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.ScreeningRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]domain.ScreeningRun, 0)
	for _, run := range r.runs {
		if customerID != "" && !domain.SameIdentifier(customerID, run.CustomerID) {
			continue
		}
		if after != nil && !beforeCursor(run.CreatedAt, run.ID, after) {
			continue
		}
		all = append(all, *cloneRun(run))
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt) || (all[i].CreatedAt.Equal(all[j].CreatedAt) && all[i].ID > all[j].ID)
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func beforeCursor(at time.Time, id string, after *domain.Cursor) bool {
	return at.Before(after.CreatedAt) || (at.Equal(after.CreatedAt) && id < after.ID)
}

func (r *MemoryWave3Repo) ListScreeningResults(_ context.Context, filter domain.ScreeningResultFilter, limit int) ([]domain.ScreeningResultRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]domain.ScreeningResultRecord, 0)
	for _, result := range r.results {
		if filter.CustomerID != "" && !domain.SameIdentifier(filter.CustomerID, result.CustomerID) {
			continue
		}
		if filter.Status != "" && result.Status != filter.Status {
			continue
		}
		if filter.ListID != "" && result.ListID != filter.ListID {
			continue
		}
		if filter.From != nil && result.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && result.CreatedAt.After(*filter.To) {
			continue
		}
		if filter.Suppressed != nil && result.Suppressed != *filter.Suppressed {
			continue
		}
		if filter.Cursor != nil && !beforeCursor(result.CreatedAt, result.ID, filter.Cursor) {
			continue
		}
		all = append(all, *cloneScreeningRecord(result))
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt) || (all[i].CreatedAt.Equal(all[j].CreatedAt) && all[i].ID > all[j].ID)
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *MemoryWave3Repo) CountByCustomer(_ context.Context, customerID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, result := range r.results {
		if domain.SameIdentifier(customerID, result.CustomerID) {
			count++
		}
	}
	return count, nil
}

func (r *MemoryWave3Repo) ReviewScreeningResult(_ context.Context, id string, to domain.ScreeningResultStatus, reason, actor string, expectedVersion int) (*domain.ScreeningReviewOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result, ok := r.results[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "screening_result", ID: id}
	}
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "screening_result", ID: id, Reason: "expected version is required"}
	}
	if result.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "screening_result", ID: id, Reason: "version mismatch"}
	}
	from := result.Status
	previous := *cloneScreeningRecord(result)
	if err := result.ApplyStatusTransition(to, reason); err != nil {
		return nil, err
	}
	caseID := result.CaseID
	caseCreated := false
	// The case id and the caseCreated flag are only claimed when a case was
	// actually written. Minting an id with no case repository wired told the
	// caller a critical case existed for a confirmed sanctions hit when none
	// did -- the one outcome a screening review must never report falsely.
	if to == domain.ScreeningResultStatusTruePositive && caseID == "" && r.cases != nil {
		caseID = wave3ID()
		now := time.Now().UTC()
		caseRecord := &domain.Case{ID: caseID, CustomerID: result.CustomerID, Status: domain.CaseStatusNew, Priority: domain.CasePriorityCritical, Summary: "Screening true positive: " + result.MatchedName + " matched " + result.ListID + " (" + result.ListType + ")", CreatedAt: now, UpdatedAt: now}
		if err := r.cases.Create(context.Background(), caseRecord); err != nil {
			*result = previous
			return nil, err
		}
		caseCreated = true
	}
	now := time.Now().UTC()
	result.ReviewedBy = actor
	result.ReviewedAt = &now
	result.Version++
	result.UpdatedAt = now
	history := domain.ScreeningResultHistoryEntry{ID: wave3ID(), ScreeningResultID: id, FromStatus: from, ToStatus: to, Rationale: reason, Actor: actor, Version: result.Version, CreatedAt: now}
	r.history[id] = append(r.history[id], history)
	result.CaseID = caseID
	out := &domain.ScreeningReviewOutcome{Result: cloneScreeningRecord(result), CaseID: caseID, CaseCreated: caseCreated}
	return out, nil
}

func (r *MemoryWave3Repo) ListScreeningResultHistory(_ context.Context, id string, limit int) ([]domain.ScreeningResultHistoryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := append([]domain.ScreeningResultHistoryEntry(nil), r.history[id]...)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (r *MemoryWave3Repo) ListScreeningSources(_ context.Context, configuredIDs []string, thresholdFor func(string) time.Duration) ([]domain.ScreeningSourceStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now().UTC()
	out := make([]domain.ScreeningSourceStatus, 0, len(configuredIDs))
	for _, id := range configuredIDs {
		snapshot := r.snapshots[id]
		snapshot.ListID = id
		if _, imported := r.snapshots[id]; !imported {
			snapshot.SnapshotMissing = true
		}
		out = append(out, domain.ClassifyScreeningSource(snapshot, resolveSourceThreshold(thresholdFor, id), now))
	}
	return out, nil
}

// resolveSourceThreshold keeps the pre-policy 72-hour default reachable for
// callers that have no policy to consult.
func resolveSourceThreshold(thresholdFor func(string) time.Duration, listID string) time.Duration {
	if thresholdFor == nil {
		return 72 * time.Hour
	}
	if d := thresholdFor(listID); d > 0 {
		return d
	}
	return 72 * time.Hour
}

// RecordScreeningSource is used by tests and by the memory import adapter to
// update the same source directory that the API reads. It records what
// happened, never what it means: the operational state is derived on read.
func (r *MemoryWave3Repo) RecordScreeningSource(id, listType string, success bool, diagnostic string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	current := r.snapshots[id]
	current.ListID = id
	current.ListType = listType
	current.LastAttemptAt = &now
	current.Diagnostic = strings.TrimSpace(diagnostic)
	current.SnapshotMissing = false
	if success {
		current.LastSuccessAt = &now
		current.ConsecutiveFailures = 0
	} else {
		current.ConsecutiveFailures++
		current.LastFailureAt = &now
	}
	r.snapshots[id] = current
}

// MarkScreeningSourceUnreadable records that a source body exists but cannot
// be read, the state the memory store previously had no way to express.
func (r *MemoryWave3Repo) MarkScreeningSourceUnreadable(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.snapshots[id]
	current.ListID = id
	current.SnapshotUnreadable = true
	r.snapshots[id] = current
}

// SetScreeningSourceSnapshot writes an importer state directly, including
// timestamps in the past. RecordScreeningSource can only ever say "just now",
// which is not enough to reproduce a stale source.
func (r *MemoryWave3Repo) SetScreeningSourceSnapshot(snapshot domain.ScreeningSourceSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots[snapshot.ListID] = snapshot
}

func (r *MemoryWave3Repo) SaveBacktestMetadata(_ context.Context, metadata *domain.BacktestMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if metadata == nil || metadata.JobID == "" {
		return &domain.ErrConflict{Entity: "backtest_metadata", Reason: "job_id is required"}
	}
	if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = time.Now().UTC()
	}
	cp := *metadata
	cp.CohortPreview = copyAnyMap(metadata.CohortPreview)
	cp.BaselineSnapshot = copyAnyMap(metadata.BaselineSnapshot)
	cp.CandidateSnapshot = copyAnyMap(metadata.CandidateSnapshot)
	r.metadata[metadata.JobID] = &cp
	return nil
}

func (r *MemoryWave3Repo) GetBacktestMetadata(_ context.Context, jobID string) (*domain.BacktestMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.metadata[jobID]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "backtest_metadata", ID: jobID}
	}
	cp := *m
	cp.CohortPreview = copyAnyMap(m.CohortPreview)
	cp.BaselineSnapshot = copyAnyMap(m.BaselineSnapshot)
	cp.CandidateSnapshot = copyAnyMap(m.CandidateSnapshot)
	return &cp, nil
}

func (r *MemoryWave3Repo) CreateTargetManifest(_ context.Context, manifest *domain.TargetManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if manifest == nil || manifest.ID == "" {
		return &domain.ErrConflict{Entity: "target_manifest", Reason: "id is required"}
	}
	if _, ok := r.manifests[manifest.ID]; ok {
		return &domain.ErrConflict{Entity: "target_manifest", ID: manifest.ID, Reason: "already exists"}
	}
	if manifest.IdempotencyKey != "" {
		for _, existing := range r.manifests {
			if existing.Operation == manifest.Operation && existing.IdempotencyKey == manifest.IdempotencyKey {
				return &domain.ErrConflict{Entity: "target_manifest", ID: manifest.ID, Reason: "idempotency key already used"}
			}
		}
	}
	if manifest.Version == 0 {
		manifest.Version = 1
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = time.Now().UTC()
	}
	cp := *manifest
	cp.CustomerIDs = append([]string(nil), manifest.CustomerIDs...)
	cp.SampleCustomerIDs = append([]string(nil), manifest.SampleCustomerIDs...)
	cp.Filter = copyAnyMap(manifest.Filter)
	cp.ConfigDigests = copyStringMap(manifest.ConfigDigests)
	r.manifests[manifest.ID] = &cp
	return nil
}

func cloneManifest(in *domain.TargetManifest) *domain.TargetManifest {
	if in == nil {
		return nil
	}
	out := *in
	out.CustomerIDs = append([]string(nil), in.CustomerIDs...)
	out.SampleCustomerIDs = append([]string(nil), in.SampleCustomerIDs...)
	out.Filter = copyAnyMap(in.Filter)
	out.ConfigDigests = copyStringMap(in.ConfigDigests)
	return &out
}

func (r *MemoryWave3Repo) GetTargetManifest(_ context.Context, id string) (*domain.TargetManifest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.manifests[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "target_manifest", ID: id}
	}
	return cloneManifest(m), nil
}

func (r *MemoryWave3Repo) ConfirmTargetManifest(_ context.Context, id, token, actor, rationale, idempotencyKey string, expectedVersion int) (*domain.TargetManifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.manifests[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "target_manifest", ID: id}
	}
	if token != "" && token != m.Token {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "stale token"}
	}
	if (m.Status == "confirmed" || m.Status == "consumed") && idempotencyKey != "" && idempotencyKey == m.IdempotencyKey {
		return cloneManifest(m), nil
	}
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "expected version is required"}
	}
	if expectedVersion != m.Version {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "version mismatch"}
	}
	if !m.ExpiresAt.IsZero() && time.Now().UTC().After(m.ExpiresAt) {
		m.Status = "expired"
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "manifest expired"}
	}
	if m.Status == "confirmed" || m.Status == "consumed" {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "already confirmed"}
	}
	m.Status = "confirmed"
	m.Version++
	m.CreatedBy = actor
	if rationale != "" {
		m.Rationale = rationale
	}
	if idempotencyKey != "" {
		m.IdempotencyKey = idempotencyKey
	}
	now := time.Now().UTC()
	m.ConfirmedAt = &now
	return cloneManifest(m), nil
}

func (r *MemoryWave3Repo) ClaimTargetManifest(_ context.Context, id, runID string, expectedVersion int) (*domain.TargetManifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.manifests[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "target_manifest", ID: id}
	}
	if expectedVersion <= 0 || expectedVersion != m.Version {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "version mismatch"}
	}
	if m.Status != "confirmed" {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "target manifest is not available"}
	}
	if runID == "" {
		return nil, &domain.ErrConflict{Entity: "target_manifest", ID: id, Reason: "run id is required"}
	}
	m.Status = "consumed"
	m.RunID = runID
	m.Version++
	return cloneManifest(m), nil
}

func (r *MemoryWave3Repo) SetCaseRepository(cases *MemoryCaseRepo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cases = cases
}

// SetPersistFailure makes PersistScreeningRun fail, the way MemoryAuditRepo's
// SetCreateFailure does, so callers can be tested against a durable store that
// is refusing writes.
func (r *MemoryWave3Repo) SetPersistFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.persistFailure = err
}

func (r *MemoryWave3Repo) AppendCustomerIdentityHistory(_ context.Context, entry *domain.CustomerIdentityHistoryEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.ID == "" {
		entry.ID = wave3ID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	cp := *entry
	cp.ChangedFields = copyAnyMap(entry.ChangedFields)
	r.identity[entry.CustomerID] = append(r.identity[entry.CustomerID], cp)
	return nil
}
func (r *MemoryWave3Repo) ListCustomerIdentityHistory(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.CustomerIdentityHistoryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := append([]domain.CustomerIdentityHistoryEntry(nil), r.identity[customerID]...)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if after != nil {
		filtered := items[:0]
		for _, item := range items {
			if beforeCursor(item.CreatedAt, item.ID, after) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
