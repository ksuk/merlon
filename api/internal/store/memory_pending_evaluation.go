package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryPendingEvaluationRepo is the in-memory domain.PendingEvaluationRepository
// used for MERLON_DATABASE_URL-unset development/test runs (OPS-005 queueing).
type MemoryPendingEvaluationRepo struct {
	mu      sync.RWMutex
	data    map[string]*domain.PendingEvaluation
	history map[string][]domain.PendingEvaluationHistoryEntry
}

func NewMemoryPendingEvaluationRepo() *MemoryPendingEvaluationRepo {
	return &MemoryPendingEvaluationRepo{data: make(map[string]*domain.PendingEvaluation), history: make(map[string][]domain.PendingEvaluationHistoryEntry)}
}

func (r *MemoryPendingEvaluationRepo) Create(_ context.Context, pe *domain.PendingEvaluation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if pe.CreatedAt.IsZero() {
		pe.CreatedAt = now
	}
	pe.UpdatedAt = now
	if pe.Version == 0 {
		pe.Version = 1
	}
	cp := *pe
	cp.TransactionIDs = append([]string(nil), pe.TransactionIDs...)
	cp.AlertIDs = append([]string(nil), pe.AlertIDs...)
	r.data[pe.ID] = &cp
	return nil
}

func pendingStatusMatch(statuses []domain.PendingEvaluationStatus, status domain.PendingEvaluationStatus) bool {
	if len(statuses) == 0 {
		return true
	}
	for _, candidate := range statuses {
		if candidate == status {
			return true
		}
	}
	return false
}

func (r *MemoryPendingEvaluationRepo) ListPendingEvaluations(_ context.Context, filter domain.PendingEvaluationFilter, limit int) ([]domain.PendingEvaluation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.PendingEvaluation, 0)
	for _, pe := range r.data {
		if !pendingStatusMatch(filter.Status, pe.Status) || (filter.CustomerID != "" && !domain.SameIdentifier(filter.CustomerID, pe.CustomerID)) {
			continue
		}
		if filter.BatchRunID != "" && (pe.BatchRunID == nil || !domain.SameIdentifier(filter.BatchRunID, *pe.BatchRunID)) {
			continue
		}
		if filter.CreatedFrom != nil && pe.CreatedAt.Before(*filter.CreatedFrom) {
			continue
		}
		if filter.CreatedTo != nil && pe.CreatedAt.After(*filter.CreatedTo) {
			continue
		}
		if filter.Cursor != nil && !(pe.CreatedAt.Before(filter.Cursor.CreatedAt) || (pe.CreatedAt.Equal(filter.Cursor.CreatedAt) && pe.ID < filter.Cursor.ID)) {
			continue
		}
		items = append(items, *pe)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID > items[j].ID)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *MemoryPendingEvaluationRepo) ListPendingHistory(_ context.Context, id string, limit int) ([]domain.PendingEvaluationHistoryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := append([]domain.PendingEvaluationHistoryEntry(nil), r.history[id]...)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (r *MemoryPendingEvaluationRepo) TransitionPendingEvaluation(_ context.Context, id, action, actor, reason string, expectedVersion int) (*domain.PendingEvaluation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pe, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "expected version is required"}
	}
	if pe.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "version mismatch"}
	}
	from := pe.Status
	now := time.Now().UTC()
	switch action {
	case "retry":
		if pe.Status == domain.PendingEvaluationStatusResolved {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "already resolved"}
		}
		pe.Status = domain.PendingEvaluationStatusPendingReview
		pe.RetryCount++
		pe.LastAttemptAt = &now
		next := now.Add(time.Duration(1<<minInt(pe.RetryCount, 8)) * time.Second)
		pe.NextRetryAt = &next
	case "process":
		if pe.Status != domain.PendingEvaluationStatusPendingReview {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "record is not pending review"}
		}
		pe.Status = domain.PendingEvaluationStatusProcessing
		pe.LastAttemptAt = &now
		pe.NextRetryAt = nil
	case "resolve":
		if pe.Status == domain.PendingEvaluationStatusResolved {
			return clonePending(pe), nil
		}
		pe.Status = domain.PendingEvaluationStatusResolved
		pe.ResolvedAt = &now
		pe.NextRetryAt = nil
	case "escalate":
		if pe.Status == domain.PendingEvaluationStatusResolved {
			return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "already resolved"}
		}
		pe.Status = domain.PendingEvaluationStatusFailed
		pe.EscalatedAt = &now
		pe.NextRetryAt = nil
	default:
		return nil, &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "unknown action"}
	}
	pe.UpdatedAt = now
	pe.Version++
	r.history[id] = append(r.history[id], domain.PendingEvaluationHistoryEntry{ID: wave3ID(), PendingEvaluationID: id, FromStatus: from, ToStatus: pe.Status, Action: action, Reason: reason, Actor: actor, RetryCount: pe.RetryCount, CreatedAt: now})
	return clonePending(pe), nil
}

func clonePending(pe *domain.PendingEvaluation) *domain.PendingEvaluation {
	if pe == nil {
		return nil
	}
	cp := *pe
	cp.TransactionIDs = append([]string(nil), pe.TransactionIDs...)
	cp.AlertIDs = append([]string(nil), pe.AlertIDs...)
	return &cp
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (r *MemoryPendingEvaluationRepo) SetPendingEvaluationAlertIDs(_ context.Context, id string, alertIDs []string, expectedVersion int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pe, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	if expectedVersion <= 0 || pe.Version != expectedVersion {
		return &domain.ErrConflict{Entity: "pending_evaluation", ID: id, Reason: "version mismatch"}
	}
	now := time.Now().UTC()
	from := pe.Status
	pe.AlertIDs = append([]string(nil), alertIDs...)
	pe.UpdatedAt = now
	pe.Version++
	r.history[id] = append(r.history[id], domain.PendingEvaluationHistoryEntry{
		ID: wave3ID(), PendingEvaluationID: id, FromStatus: from, ToStatus: from,
		Action: "link_alerts", Reason: "recovery alert links persisted", Actor: "system:pending-recovery",
		RetryCount: pe.RetryCount, CreatedAt: now,
	})
	return nil
}

func (r *MemoryPendingEvaluationRepo) Get(_ context.Context, id string) (*domain.PendingEvaluation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pe, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	cp := *pe
	return &cp, nil
}

func (r *MemoryPendingEvaluationRepo) ListByStatus(_ context.Context, status domain.PendingEvaluationStatus, limit, offset int) ([]domain.PendingEvaluation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.PendingEvaluation
	for _, pe := range r.data {
		if pe.Status == status {
			all = append(all, *pe)
		}
	}
	sortByCreatedAtDesc(all,
		func(pe domain.PendingEvaluation) time.Time { return pe.CreatedAt },
		func(pe domain.PendingEvaluation) string { return pe.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

// ListPendingByCustomer supports idempotent fail-alert queueing when realtime
// and batch passes observe the same engine outage concurrently.
func (r *MemoryPendingEvaluationRepo) ListPendingByCustomer(_ context.Context, customerID string, status domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.PendingEvaluation
	for _, pe := range r.data {
		if domain.SameIdentifier(pe.CustomerID, customerID) && pe.Status == status {
			out = append(out, *pe)
		}
	}
	return out, nil
}

func (r *MemoryPendingEvaluationRepo) ListPendingByCustomers(_ context.Context, customerIDs []string, status domain.PendingEvaluationStatus) ([]domain.PendingEvaluation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wanted := make(map[string]struct{}, len(customerIDs))
	for _, id := range customerIDs {
		wanted[domain.CanonicalUUID(id)] = struct{}{}
	}
	var out []domain.PendingEvaluation
	for _, pe := range r.data {
		if _, ok := wanted[domain.CanonicalUUID(pe.CustomerID)]; ok && pe.Status == status {
			out = append(out, *pe)
		}
	}
	return out, nil
}

func (r *MemoryPendingEvaluationRepo) UpdateStatus(_ context.Context, id string, status domain.PendingEvaluationStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pe, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	from := pe.Status
	pe.Status = status
	now := time.Now().UTC()
	pe.UpdatedAt = now
	if status == domain.PendingEvaluationStatusResolved {
		pe.ResolvedAt = &now
	}
	pe.Version++
	r.history[id] = append(r.history[id], domain.PendingEvaluationHistoryEntry{ID: wave3ID(), PendingEvaluationID: id, FromStatus: from, ToStatus: status, Action: "status", CreatedAt: now, RetryCount: pe.RetryCount})
	return nil
}

func (r *MemoryPendingEvaluationRepo) IncrementRetry(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pe, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	from := pe.Status
	pe.RetryCount++
	now := time.Now().UTC()
	pe.LastAttemptAt = &now
	pe.UpdatedAt = now
	pe.Version++
	r.history[id] = append(r.history[id], domain.PendingEvaluationHistoryEntry{ID: wave3ID(), PendingEvaluationID: id, FromStatus: from, ToStatus: pe.Status, Action: "retry", CreatedAt: now, RetryCount: pe.RetryCount})
	return nil
}
