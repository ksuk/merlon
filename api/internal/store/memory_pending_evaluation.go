package store

import (
	"context"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryPendingEvaluationRepo is the in-memory domain.PendingEvaluationRepository
// used for MERLON_DATABASE_URL-unset development/test runs (OPS-005 queueing).
type MemoryPendingEvaluationRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.PendingEvaluation
}

func NewMemoryPendingEvaluationRepo() *MemoryPendingEvaluationRepo {
	return &MemoryPendingEvaluationRepo{data: make(map[string]*domain.PendingEvaluation)}
}

func (r *MemoryPendingEvaluationRepo) Create(_ context.Context, pe *domain.PendingEvaluation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if pe.CreatedAt.IsZero() {
		pe.CreatedAt = now
	}
	pe.UpdatedAt = now
	cp := *pe
	r.data[pe.ID] = &cp
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
		if pe.CustomerID == customerID && pe.Status == status {
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
		wanted[id] = struct{}{}
	}
	var out []domain.PendingEvaluation
	for _, pe := range r.data {
		if _, ok := wanted[pe.CustomerID]; ok && pe.Status == status {
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
	pe.Status = status
	now := time.Now()
	pe.UpdatedAt = now
	if status == domain.PendingEvaluationStatusResolved {
		pe.ResolvedAt = &now
	}
	return nil
}

func (r *MemoryPendingEvaluationRepo) IncrementRetry(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pe, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "pending_evaluation", ID: id}
	}
	pe.RetryCount++
	pe.UpdatedAt = time.Now()
	return nil
}
