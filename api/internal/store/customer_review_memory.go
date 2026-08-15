package store

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryCustomerReviewRepo mirrors the PostgreSQL customer_reviews contract.
// The mutex covers the uniqueness check and version update together so a
// scheduler retry cannot create two rows for the same customer/cycle.
type MemoryCustomerReviewRepo struct {
	mu    sync.RWMutex
	data  map[string]*domain.CustomerReview
	byKey map[string]string
}

func NewMemoryCustomerReviewRepo() *MemoryCustomerReviewRepo {
	return &MemoryCustomerReviewRepo{data: map[string]*domain.CustomerReview{}, byKey: map[string]string{}}
}

func reviewKey(customerID string, cycle int) string {
	return domain.CanonicalIdentifier(customerID) + ":" + strconvItoa(cycle)
}

func strconvItoa(value int) string {
	// Keeping this helper local avoids exposing formatting concerns through the
	// domain package and makes reviewKey allocation-free for common cycles.
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func cloneCustomerReview(in *domain.CustomerReview) *domain.CustomerReview {
	if in == nil {
		return nil
	}
	out := *in
	out.Scope = cloneReviewAnyMap(in.Scope)
	out.EvidenceRefs = append([]string(nil), in.EvidenceRefs...)
	return &out
}

func cloneReviewAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (r *MemoryCustomerReviewRepo) Get(_ context.Context, id string) (*domain.CustomerReview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	review, ok := r.data[domain.CanonicalIdentifier(id)]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer_review", ID: id}
	}
	return cloneCustomerReview(review), nil
}

func (r *MemoryCustomerReviewRepo) GetByCustomerCycle(_ context.Context, customerID string, cycle int) (*domain.CustomerReview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byKey[reviewKey(customerID, cycle)]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer_review", ID: reviewKey(customerID, cycle)}
	}
	return cloneCustomerReview(r.data[id]), nil
}

func (r *MemoryCustomerReviewRepo) LatestByCustomer(_ context.Context, customerID string) (*domain.CustomerReview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *domain.CustomerReview
	for _, review := range r.data {
		if !domain.SameIdentifier(review.CustomerID, customerID) {
			continue
		}
		if latest == nil || review.Cycle > latest.Cycle ||
			(review.Cycle == latest.Cycle && review.UpdatedAt.After(latest.UpdatedAt)) {
			latest = review
		}
	}
	if latest == nil {
		return nil, &domain.ErrNotFound{Entity: "customer_review", ID: customerID}
	}
	return cloneCustomerReview(latest), nil
}

func (r *MemoryCustomerReviewRepo) Create(_ context.Context, review *domain.CustomerReview) error {
	if review == nil || strings.TrimSpace(review.CustomerID) == "" {
		return &domain.ErrConflict{Entity: "customer_review", Reason: "customer_id is required"}
	}
	if !review.Status.Valid() {
		return &domain.ErrConflict{Entity: "customer_review", Reason: "invalid status"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if review.ID == "" {
		review.ID = wave3ID()
	}
	review.ID = domain.CanonicalIdentifier(review.ID)
	review.CustomerID = domain.CanonicalIdentifier(review.CustomerID)
	if review.Cycle <= 0 {
		review.Cycle = 1
	}
	key := reviewKey(review.CustomerID, review.Cycle)
	if _, exists := r.byKey[key]; exists {
		return &domain.ErrConflict{Entity: "customer_review", ID: key, Reason: "customer cycle already exists"}
	}
	now := time.Now().UTC()
	if review.CreatedAt.IsZero() {
		review.CreatedAt = now
	}
	if review.UpdatedAt.IsZero() {
		review.UpdatedAt = review.CreatedAt
	}
	if review.Version <= 0 {
		review.Version = 1
	}
	stored := cloneCustomerReview(review)
	r.data[stored.ID] = stored
	r.byKey[key] = stored.ID
	*review = *cloneCustomerReview(stored)
	return nil
}

func (r *MemoryCustomerReviewRepo) Update(ctx context.Context, review *domain.CustomerReview) error {
	if review == nil {
		return &domain.ErrConflict{Entity: "customer_review", Reason: "review is required"}
	}
	return r.update(ctx, review, review.ExpectedVersion)
}

func (r *MemoryCustomerReviewRepo) UpdateIfUnmodified(_ context.Context, review *domain.CustomerReview, expectedVersion int64) error {
	return r.update(context.Background(), review, expectedVersion)
}

func (r *MemoryCustomerReviewRepo) update(_ context.Context, review *domain.CustomerReview, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := domain.CanonicalIdentifier(review.ID)
	current, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "customer_review", ID: id}
	}
	if expectedVersion > 0 && current.Version != expectedVersion {
		return &domain.ErrConflict{Entity: "customer_review", ID: id, Reason: "version does not match the version read by the client"}
	}
	if current.Status == domain.CustomerReviewStatusCompleted && review.Status != current.Status {
		return &domain.ErrConflict{Entity: "customer_review", ID: id, Reason: "completed review is immutable"}
	}
	updated := cloneCustomerReview(review)
	updated.ID = id
	updated.CustomerID = current.CustomerID
	updated.Cycle = current.Cycle
	updated.CreatedAt = current.CreatedAt
	updated.Version = current.Version + 1
	updated.UpdatedAt = time.Now().UTC()
	r.data[id] = updated
	*review = *cloneCustomerReview(updated)
	return nil
}

func effectiveReviewStatus(review domain.CustomerReview, asOf time.Time) domain.CustomerReviewStatus {
	if review.Status != domain.CustomerReviewStatusScheduled && review.Status != domain.CustomerReviewStatusDue {
		return review.Status
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	if !asOf.Before(review.GraceUntil) {
		return domain.CustomerReviewStatusOverdue
	}
	if !asOf.Before(review.DueAt) {
		return domain.CustomerReviewStatusDue
	}
	return review.Status
}

func reviewStatusMatches(review domain.CustomerReview, filter domain.CustomerReviewFilter) bool {
	status := effectiveReviewStatus(review, filter.AsOf)
	if filter.Status != "" && status != filter.Status {
		return false
	}
	if len(filter.Statuses) > 0 {
		matched := false
		for _, wanted := range filter.Statuses {
			if status == wanted {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (r *MemoryCustomerReviewRepo) List(_ context.Context, filter domain.CustomerReviewFilter) ([]domain.CustomerReview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.CustomerReview, 0, len(r.data))
	for _, stored := range r.data {
		if filter.CustomerID != "" && !domain.SameIdentifier(stored.CustomerID, filter.CustomerID) {
			continue
		}
		if !reviewStatusMatches(*stored, filter) ||
			(filter.Tier != "" && stored.Tier != filter.Tier) ||
			(filter.AssignedTo != "" && stored.AssignedTo != filter.AssignedTo) ||
			(filter.AssignedTeam != "" && stored.AssignedTeam != filter.AssignedTeam) {
			continue
		}
		if filter.DueBefore != nil && stored.DueAt.After(*filter.DueBefore) {
			continue
		}
		if filter.DueAfter != nil && stored.DueAt.Before(*filter.DueAfter) {
			continue
		}
		copy := *cloneCustomerReview(stored)
		copy.Status = effectiveReviewStatus(copy, filter.AsOf)
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].DueAt.Equal(items[j].DueAt) {
			return items[i].DueAt.Before(items[j].DueAt)
		}
		return items[i].ID < items[j].ID
	})
	if filter.Cursor != nil {
		cursor := filter.Cursor
		items = filterReviewCursor(items, cursor)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func filterReviewCursor(items []domain.CustomerReview, cursor *domain.Cursor) []domain.CustomerReview {
	filtered := items[:0]
	for _, item := range items {
		if item.DueAt.After(cursor.CreatedAt) || (item.DueAt.Equal(cursor.CreatedAt) && item.ID > cursor.ID) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

var _ domain.CustomerReviewRepository = (*MemoryCustomerReviewRepo)(nil)
var _ domain.CustomerReviewOptimisticRepository = (*MemoryCustomerReviewRepo)(nil)
