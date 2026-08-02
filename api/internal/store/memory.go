package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// sortByCreatedAtDesc orders items by (created_at, id) descending, matching
// the default sort the HTTP API contract §1.1 specifies and the ORDER BY clause the
// Postgres repositories use. Go map iteration order is randomized per run,
// so anything read out of a map (as every Memory*Repo does) must be sorted
// before it can be paginated at all, whether by offset or cursor.
func sortByCreatedAtDesc[T any](items []T, createdAt func(T) time.Time, id func(T) string) {
	sort.Slice(items, func(i, j int) bool {
		ci, cj := createdAt(items[i]), createdAt(items[j])
		if !ci.Equal(cj) {
			return ci.After(cj)
		}
		return id(items[i]) > id(items[j])
	})
}

// pageByOffset slices a (created_at, id)-descending-sorted list the same way
// the Postgres OFFSET/LIMIT path does.
func pageByOffset[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

// sortAndPageByCursor orders items by (created_at, id) descending, drops
// everything at or after the cursor position, and trims the remainder to
// limit. It mirrors the keyset SQL `WHERE (created_at, id) < (?, ?) ORDER BY
// created_at DESC, id DESC LIMIT ?` used by the Postgres repositories, so
// in-memory and Postgres traversal produce the same result set.
func sortAndPageByCursor[T any](items []T, limit int, after *domain.Cursor, createdAt func(T) time.Time, id func(T) string) []T {
	sortByCreatedAtDesc(items, createdAt, id)

	if after != nil {
		filtered := make([]T, 0, len(items))
		for _, it := range items {
			if createdAt(it).Before(after.CreatedAt) || (createdAt(it).Equal(after.CreatedAt) && id(it) < after.ID) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	if limit >= 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

// sortAndPageByRiskCursor orders a queue by explicit risk rank, then by the
// API-03 deterministic created_at/id tie-breakers. The cursor carries the
// rank so keyset traversal cannot skip a lower-risk item after a page break.
func sortAndPageByRiskCursor[T any](items []T, limit int, after *domain.Cursor, rank func(T) int, createdAt func(T) time.Time, id func(T) string) []T {
	sort.Slice(items, func(i, j int) bool {
		ri, rj := rank(items[i]), rank(items[j])
		if ri != rj {
			return ri > rj
		}
		ci, cj := createdAt(items[i]), createdAt(items[j])
		if !ci.Equal(cj) {
			return ci.After(cj)
		}
		return id(items[i]) > id(items[j])
	})

	if after != nil {
		filtered := make([]T, 0, len(items))
		for _, it := range items {
			r := rank(it)
			createdBefore := createdAt(it).Before(after.CreatedAt)
			sameCreatedBeforeID := createdAt(it).Equal(after.CreatedAt) && id(it) < after.ID
			if r < after.Rank || (r == after.Rank && (createdBefore || sameCreatedBeforeID)) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	if limit >= 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

func sortByRiskDesc[T any](items []T, rank func(T) int, createdAt func(T) time.Time, id func(T) string) {
	sort.Slice(items, func(i, j int) bool {
		ri, rj := rank(items[i]), rank(items[j])
		if ri != rj {
			return ri > rj
		}
		ci, cj := createdAt(items[i]), createdAt(items[j])
		if !ci.Equal(cj) {
			return ci.After(cj)
		}
		return id(items[i]) > id(items[j])
	})
}

type MemoryCustomerRepo struct {
	mu       sync.RWMutex
	data     map[string]*domain.Customer
	external map[string]string // externalID -> id
	scores   map[string][]domain.ScoreRecord
}

func NewMemoryCustomerRepo() *MemoryCustomerRepo {
	return &MemoryCustomerRepo{
		data:     make(map[string]*domain.Customer),
		external: make(map[string]string),
		scores:   make(map[string][]domain.ScoreRecord),
	}
}

func (r *MemoryCustomerRepo) Get(_ context.Context, id string) (*domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer", ID: id}
	}
	cp := *c
	return &cp, nil
}

func (r *MemoryCustomerRepo) GetByExternalID(_ context.Context, externalID string) (*domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.external[externalID]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer", ID: externalID}
	}
	c := r.data[id]
	cp := *c
	return &cp, nil
}

func (r *MemoryCustomerRepo) List(_ context.Context, limit, offset int) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Customer
	for _, c := range r.data {
		all = append(all, *c)
	}
	sortByCreatedAtDesc(all,
		func(c domain.Customer) time.Time { return c.CreatedAt },
		func(c domain.Customer) string { return c.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

// DashboardRiskTierCounts returns a complete aggregate rather than relying
// on the dashboard's former 10,000-row list cap. A missing tier is exposed as
// "unscored", matching the dashboard's customer-level presentation.
func (r *MemoryCustomerRepo) DashboardRiskTierCounts(_ context.Context) (map[string]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[string]int)
	for _, c := range r.data {
		tier := "unscored"
		if c.RiskTier != nil {
			tier = string(*c.RiskTier)
		}
		counts[tier]++
	}
	return counts, nil
}

func (r *MemoryCustomerRepo) ListByCursor(_ context.Context, limit int, after *domain.Cursor) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Customer
	for _, c := range r.data {
		all = append(all, *c)
	}
	return sortAndPageByCursor(all, limit, after,
		func(c domain.Customer) time.Time { return c.CreatedAt },
		func(c domain.Customer) string { return c.ID },
	), nil
}

func customerMatchesSearch(c domain.Customer, search string) bool {
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.ID), needle) ||
		strings.Contains(strings.ToLower(c.ExternalID), needle) ||
		strings.Contains(strings.ToLower(c.CountryCode), needle) {
		return true
	}
	for key, value := range c.Attributes {
		if strings.Contains(strings.ToLower(key), needle) || strings.Contains(strings.ToLower(fmt.Sprint(value)), needle) {
			return true
		}
	}
	return false
}

func (r *MemoryCustomerRepo) ListSearch(_ context.Context, search string, limit, offset int) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Customer
	for _, c := range r.data {
		if customerMatchesSearch(*c, search) {
			all = append(all, *c)
		}
	}
	sortByCreatedAtDesc(all,
		func(c domain.Customer) time.Time { return c.CreatedAt },
		func(c domain.Customer) string { return c.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

func (r *MemoryCustomerRepo) ListByCursorSearch(_ context.Context, limit int, after *domain.Cursor, search string) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Customer
	for _, c := range r.data {
		if customerMatchesSearch(*c, search) {
			all = append(all, *c)
		}
	}
	return sortAndPageByCursor(all, limit, after,
		func(c domain.Customer) time.Time { return c.CreatedAt },
		func(c domain.Customer) string { return c.ID },
	), nil
}

func (r *MemoryCustomerRepo) Create(_ context.Context, c *domain.Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[c.ID] = c
	r.external[c.ExternalID] = c.ID
	return nil
}

func (r *MemoryCustomerRepo) Update(_ context.Context, c *domain.Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[c.ID]; !ok {
		return &domain.ErrNotFound{Entity: "customer", ID: c.ID}
	}
	c.UpdatedAt = time.Now()
	r.data[c.ID] = c
	return nil
}

func (r *MemoryCustomerRepo) UpdateStatus(_ context.Context, id string, status domain.CustomerStatus, _ string) (*domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer", ID: id}
	}
	c.Status = status
	c.UpdatedAt = time.Now()
	cp := *c
	return &cp, nil
}

func (r *MemoryCustomerRepo) ListEDDPending(_ context.Context) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.Customer
	for _, c := range r.data {
		if c.RiskTier != nil && *c.RiskTier == domain.RiskTierHigh && c.EddRequestedAt != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (r *MemoryCustomerRepo) SaveScoreRecord(_ context.Context, rec *domain.ScoreRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scores[rec.CustomerID] = append(r.scores[rec.CustomerID], *rec)
	return nil
}

func (r *MemoryCustomerRepo) ListScoreHistory(_ context.Context, customerID string, limit int) ([]domain.ScoreRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records := r.scores[customerID]
	if limit > 0 && limit < len(records) {
		return records[len(records)-limit:], nil
	}
	return records, nil
}

type MemoryTransactionRepo struct {
	mu             sync.RWMutex
	data           map[string]*domain.Transaction
	byCustomer     map[string][]string
	idempotencyKey map[string]string // idempotency key -> transaction ID
}

func NewMemoryTransactionRepo() *MemoryTransactionRepo {
	return &MemoryTransactionRepo{
		data:           make(map[string]*domain.Transaction),
		byCustomer:     make(map[string][]string),
		idempotencyKey: make(map[string]string),
	}
}

func (r *MemoryTransactionRepo) Get(_ context.Context, id string) (*domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "transaction", ID: id}
	}
	cp := *t
	return &cp, nil
}

func (r *MemoryTransactionRepo) ListByCustomer(_ context.Context, customerID string, limit, offset int) ([]domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Transaction
	for _, id := range r.byCustomer[customerID] {
		all = append(all, *r.data[id])
	}
	sortByCreatedAtDesc(all,
		func(t domain.Transaction) time.Time { return t.CreatedAt },
		func(t domain.Transaction) string { return t.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

func (r *MemoryTransactionRepo) ListByCustomerCursor(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Transaction
	for _, id := range r.byCustomer[customerID] {
		all = append(all, *r.data[id])
	}
	return sortAndPageByCursor(all, limit, after,
		func(t domain.Transaction) time.Time { return t.CreatedAt },
		func(t domain.Transaction) string { return t.ID },
	), nil
}

func (r *MemoryTransactionRepo) CountExecutedSince(_ context.Context, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, t := range r.data {
		if !t.ExecutedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (r *MemoryTransactionRepo) ListByCustomerEventRange(_ context.Context, customerID string, from, to, createdBefore time.Time, limit int, after *domain.TransactionEventCursor) ([]domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Transaction
	for _, id := range r.byCustomer[customerID] {
		t := *r.data[id]
		if !t.ExecutedAt.Before(from) && t.ExecutedAt.Before(to) && !t.CreatedAt.After(createdBefore) {
			if after == nil || t.ExecutedAt.After(after.ExecutedAt) || (t.ExecutedAt.Equal(after.ExecutedAt) && t.ID > after.ID) {
				all = append(all, t)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ExecutedAt.Equal(all[j].ExecutedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].ExecutedAt.Before(all[j].ExecutedAt)
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *MemoryTransactionRepo) Create(_ context.Context, t *domain.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.IdempotencyKey != nil {
		if _, exists := r.idempotencyKey[*t.IdempotencyKey]; exists {
			return &domain.ErrConflict{Entity: "transaction", ID: t.ID, Reason: "idempotency key already used"}
		}
		r.idempotencyKey[*t.IdempotencyKey] = t.ID
	}
	r.data[t.ID] = t
	r.byCustomer[t.CustomerID] = append(r.byCustomer[t.CustomerID], t.ID)
	return nil
}

type MemoryAlertRepo struct {
	mu         sync.RWMutex
	data       map[string]*domain.Alert
	byCustomer map[string][]string
}

func NewMemoryAlertRepo() *MemoryAlertRepo {
	return &MemoryAlertRepo{
		data:       make(map[string]*domain.Alert),
		byCustomer: make(map[string][]string),
	}
}

func (r *MemoryAlertRepo) Get(_ context.Context, id string) (*domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	cp := *a
	return &cp, nil
}

func (r *MemoryAlertRepo) ListByCustomer(_ context.Context, customerID string, limit, offset int) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Alert
	for _, id := range r.byCustomer[customerID] {
		all = append(all, *r.data[id])
	}
	sortByCreatedAtDesc(all,
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

func (r *MemoryAlertRepo) ListByCustomerRisk(_ context.Context, customerID string, limit, offset int) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Alert
	for _, id := range r.byCustomer[customerID] {
		all = append(all, *r.data[id])
	}
	sortByRiskDesc(all,
		func(a domain.Alert) int { return domain.AlertSeverityRank(a.Severity) },
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

func (r *MemoryAlertRepo) ListOpen(_ context.Context, limit, offset int) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Alert
	for _, a := range r.data {
		if a.Status == domain.AlertStatusOpen {
			open = append(open, *a)
		}
	}
	sortByCreatedAtDesc(open,
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	)
	return pageByOffset(open, limit, offset), nil
}

func (r *MemoryAlertRepo) ListOpenByRisk(_ context.Context, limit, offset int) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Alert
	for _, a := range r.data {
		if a.Status == domain.AlertStatusOpen {
			open = append(open, *a)
		}
	}
	sortByRiskDesc(open,
		func(a domain.Alert) int { return domain.AlertSeverityRank(a.Severity) },
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	)
	return pageByOffset(open, limit, offset), nil
}

func (r *MemoryAlertRepo) ListByCustomerCursor(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Alert
	for _, id := range r.byCustomer[customerID] {
		all = append(all, *r.data[id])
	}
	return sortAndPageByCursor(all, limit, after,
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	), nil
}

func (r *MemoryAlertRepo) ListByCustomerRiskCursor(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.Alert
	for _, id := range r.byCustomer[customerID] {
		all = append(all, *r.data[id])
	}
	return sortAndPageByRiskCursor(all, limit, after,
		func(a domain.Alert) int { return domain.AlertSeverityRank(a.Severity) },
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	), nil
}

func (r *MemoryAlertRepo) ListOpenByCursor(_ context.Context, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Alert
	for _, a := range r.data {
		if a.Status == domain.AlertStatusOpen {
			open = append(open, *a)
		}
	}
	return sortAndPageByCursor(open, limit, after,
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	), nil
}

func (r *MemoryAlertRepo) ListOpenByRiskCursor(_ context.Context, limit int, after *domain.Cursor) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Alert
	for _, a := range r.data {
		if a.Status == domain.AlertStatusOpen {
			open = append(open, *a)
		}
	}
	return sortAndPageByRiskCursor(open, limit, after,
		func(a domain.Alert) int { return domain.AlertSeverityRank(a.Severity) },
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	), nil
}

func (r *MemoryAlertRepo) DashboardUnresolvedCounts(_ context.Context) (map[string]int, map[string]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byStatus := make(map[string]int)
	bySeverity := make(map[string]int)
	for _, a := range r.data {
		switch a.Status {
		case domain.AlertStatusOpen, domain.AlertStatusInvestigating, domain.AlertStatusEscalated:
			byStatus[string(a.Status)]++
			bySeverity[string(a.Severity)]++
		}
	}
	return byStatus, bySeverity, nil
}

func (r *MemoryAlertRepo) ListByFilter(_ context.Context, f domain.AlertBulkFilter) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.Alert
	for _, a := range r.data {
		if f.ScenarioID != "" && a.ScenarioID != f.ScenarioID {
			continue
		}
		if f.Severity != "" && a.Severity != f.Severity {
			continue
		}
		if f.PeriodFrom != nil && a.DetectedAt.Before(*f.PeriodFrom) {
			continue
		}
		if f.PeriodTo != nil && a.DetectedAt.After(*f.PeriodTo) {
			continue
		}
		out = append(out, *a)
	}
	sortByCreatedAtDesc(out,
		func(a domain.Alert) time.Time { return a.CreatedAt },
		func(a domain.Alert) string { return a.ID },
	)
	return out, nil
}

func (r *MemoryAlertRepo) Create(_ context.Context, a *domain.Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[a.ID] = a
	r.byCustomer[a.CustomerID] = append(r.byCustomer[a.CustomerID], a.ID)
	return nil
}

// dedupConflict reports whether another alert already occupies a's
// (customer_id, scenario_id, aggregation_window_start) tuple, mirroring
// idx_alerts_dedup (migrations/012_alert_dedup.sql). A nil
// AggregationWindowStart is exempt (aggregation_window_start IS NOT NULL in
// the partial index), so it never conflicts. Caller must hold r.mu.
func (r *MemoryAlertRepo) dedupConflict(a *domain.Alert) *domain.Alert {
	if a.AggregationWindowStart == nil {
		return nil
	}
	for _, id := range r.byCustomer[a.CustomerID] {
		existing := r.data[id]
		if existing.ScenarioID == a.ScenarioID &&
			existing.AggregationWindowStart != nil &&
			existing.AggregationWindowStart.Equal(*a.AggregationWindowStart) {
			return existing
		}
	}
	return nil
}

func (r *MemoryAlertRepo) CreateIfNotDuplicate(_ context.Context, a *domain.Alert) (bool, *domain.Alert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.dedupConflict(a); existing != nil {
		cp := *existing
		return false, &cp, nil
	}
	r.data[a.ID] = a
	r.byCustomer[a.CustomerID] = append(r.byCustomer[a.CustomerID], a.ID)
	return true, nil, nil
}

func (r *MemoryAlertRepo) AnnotateBatchReviewed(_ context.Context, alertID string, batchRunID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.data[alertID]
	if !ok {
		return &domain.ErrNotFound{Entity: "alert", ID: alertID}
	}
	now := time.Now()
	a.BatchReviewedAt = &now
	if a.BatchRunID == "" {
		a.BatchRunID = batchRunID
	}
	a.UpdatedAt = now
	return nil
}

func (r *MemoryAlertRepo) UpdateStatus(_ context.Context, id string, status domain.AlertStatus, resolvedBy string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	a.Status = status
	a.ResolvedBy = resolvedBy
	now := time.Now()
	a.ResolvedAt = &now
	a.UpdatedAt = now
	return nil
}

// UpdateStatusIfUnmodified is UpdateStatus guarded by an optimistic-lock
// check against expectedUpdatedAt (the data model §3.9).
func (r *MemoryAlertRepo) UpdateStatusIfUnmodified(_ context.Context, id string, status domain.AlertStatus, resolvedBy string, expectedUpdatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	if !a.UpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "alert", ID: id, Reason: "updated_at mismatch"}
	}
	a.Status = status
	a.ResolvedBy = resolvedBy
	now := time.Now()
	a.ResolvedAt = &now
	a.UpdatedAt = now
	return nil
}

func (r *MemoryAlertRepo) EscalateSeverity(_ context.Context, id string, severity domain.AlertSeverity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "alert", ID: id}
	}
	a.Severity = severity
	a.UpdatedAt = time.Now()
	return nil
}

// MemoryAuditRepo

type MemoryAuditRepo struct {
	mu      sync.RWMutex
	entries []domain.AuditEntry
	nextID  int64
}

func NewMemoryAuditRepo() *MemoryAuditRepo {
	return &MemoryAuditRepo{nextID: 1}
}

func (r *MemoryAuditRepo) Create(_ context.Context, entry *domain.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.ID = r.nextID
	r.nextID++
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	r.entries = append(r.entries, *entry)
	return nil
}

// List serves ALD-001/002/004, mirroring PgAuditRepo.List's filter and
// (created_at, id) DESC keyset pagination semantics (filter.Limit = 0 means
// unlimited, used by the export endpoint).
func (r *MemoryAuditRepo) List(_ context.Context, filter domain.AuditListFilter) ([]domain.AuditEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var categoryTypes map[string]bool
	if filter.ActionCategory != "" {
		types := domain.ResourceTypesForCategory(filter.ActionCategory)
		if len(types) == 0 {
			return []domain.AuditEntry{}, nil
		}
		categoryTypes = make(map[string]bool, len(types))
		for _, t := range types {
			categoryTypes[t] = true
		}
	}

	var cursorID int64
	if filter.Cursor != nil {
		cursorID, _ = strconv.ParseInt(filter.Cursor.ID, 10, 64)
	}

	var filtered []domain.AuditEntry
	for i := len(r.entries) - 1; i >= 0; i-- {
		e := r.entries[i]
		if filter.ResourceType != "" && e.ResourceType != filter.ResourceType {
			continue
		}
		if filter.ResourceID != "" && e.ResourceID != filter.ResourceID {
			continue
		}
		if filter.UserID != "" && e.UserID != filter.UserID {
			continue
		}
		if categoryTypes != nil && !categoryTypes[e.ResourceType] {
			continue
		}
		if filter.Since != nil && e.CreatedAt.Before(*filter.Since) {
			continue
		}
		if filter.Until != nil && e.CreatedAt.After(*filter.Until) {
			continue
		}
		if filter.Cursor != nil && !auditEntryBeforeCursor(e, filter.Cursor.CreatedAt, cursorID) {
			continue
		}
		filtered = append(filtered, e)
		if filter.Limit > 0 && len(filtered) >= filter.Limit {
			break
		}
	}
	return filtered, nil
}

// auditEntryBeforeCursor reports whether e sorts strictly after the given
// (created_at, id) keyset cursor in (created_at, id) DESC order, i.e. the
// same "(created_at, id) < (cursor)" tuple comparison PgAuditRepo.List uses.
func auditEntryBeforeCursor(e domain.AuditEntry, cursorCreatedAt time.Time, cursorID int64) bool {
	if e.CreatedAt.Equal(cursorCreatedAt) {
		return e.ID < cursorID
	}
	return e.CreatedAt.Before(cursorCreatedAt)
}

// MemoryCaseRepo

type MemoryCaseRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.Case
}

func NewMemoryCaseRepo() *MemoryCaseRepo {
	return &MemoryCaseRepo{data: make(map[string]*domain.Case)}
}

func (r *MemoryCaseRepo) Get(_ context.Context, id string) (*domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "case", ID: id}
	}
	cp := *c
	return &cp, nil
}

func (r *MemoryCaseRepo) ListByCustomer(_ context.Context, customerID string) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Case
	for _, c := range r.data {
		if c.CustomerID == customerID {
			result = append(result, *c)
		}
	}
	sortByCreatedAtDesc(result,
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	)
	return result, nil
}

func (r *MemoryCaseRepo) ListByCustomerOffset(_ context.Context, customerID string, limit, offset int) ([]domain.Case, error) {
	result, err := r.ListByCustomer(context.Background(), customerID)
	if err != nil {
		return nil, err
	}
	return pageByOffset(result, limit, offset), nil
}

func (r *MemoryCaseRepo) ListByCustomerRiskOffset(_ context.Context, customerID string, limit, offset int) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Case
	for _, c := range r.data {
		if c.CustomerID == customerID {
			result = append(result, *c)
		}
	}
	sortByRiskDesc(result,
		func(c domain.Case) int { return domain.CasePriorityRank(c.Priority) },
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	)
	return pageByOffset(result, limit, offset), nil
}

func (r *MemoryCaseRepo) ListByCustomerCursor(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Case
	for _, c := range r.data {
		if c.CustomerID == customerID {
			result = append(result, *c)
		}
	}
	return sortAndPageByCursor(result, limit, after,
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	), nil
}

func (r *MemoryCaseRepo) ListByCustomerRiskCursor(_ context.Context, customerID string, limit int, after *domain.Cursor) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Case
	for _, c := range r.data {
		if c.CustomerID == customerID {
			result = append(result, *c)
		}
	}
	return sortAndPageByRiskCursor(result, limit, after,
		func(c domain.Case) int { return domain.CasePriorityRank(c.Priority) },
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	), nil
}

func (r *MemoryCaseRepo) ListOpen(_ context.Context, limit, offset int) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Case
	for _, c := range r.data {
		if c.Status != domain.CaseStatusClosed {
			open = append(open, *c)
		}
	}
	sortByCreatedAtDesc(open,
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	)
	return pageByOffset(open, limit, offset), nil
}

func (r *MemoryCaseRepo) ListOpenByRisk(_ context.Context, limit, offset int) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Case
	for _, c := range r.data {
		if c.Status != domain.CaseStatusClosed {
			open = append(open, *c)
		}
	}
	sortByRiskDesc(open,
		func(c domain.Case) int { return domain.CasePriorityRank(c.Priority) },
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	)
	return pageByOffset(open, limit, offset), nil
}

func (r *MemoryCaseRepo) ListOpenByCursor(_ context.Context, limit int, after *domain.Cursor) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Case
	for _, c := range r.data {
		if c.Status != domain.CaseStatusClosed {
			open = append(open, *c)
		}
	}
	return sortAndPageByCursor(open, limit, after,
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	), nil
}

func (r *MemoryCaseRepo) ListOpenByRiskCursor(_ context.Context, limit int, after *domain.Cursor) ([]domain.Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var open []domain.Case
	for _, c := range r.data {
		if c.Status != domain.CaseStatusClosed {
			open = append(open, *c)
		}
	}
	return sortAndPageByRiskCursor(open, limit, after,
		func(c domain.Case) int { return domain.CasePriorityRank(c.Priority) },
		func(c domain.Case) time.Time { return c.CreatedAt },
		func(c domain.Case) string { return c.ID },
	), nil
}

func (r *MemoryCaseRepo) DashboardUnresolvedCounts(_ context.Context) (map[string]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[string]int)
	for _, c := range r.data {
		switch c.Status {
		case domain.CaseStatusClosed, domain.CaseStatusStrFiled:
			continue
		default:
			counts[string(c.Status)]++
		}
	}
	return counts, nil
}

func (r *MemoryCaseRepo) Create(_ context.Context, c *domain.Case) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[c.ID] = c
	return nil
}

func (r *MemoryCaseRepo) Update(_ context.Context, c *domain.Case) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[c.ID]; !ok {
		return &domain.ErrNotFound{Entity: "case", ID: c.ID}
	}
	c.UpdatedAt = time.Now()
	r.data[c.ID] = c
	return nil
}

// UpdateIfUnmodified is Update guarded by an optimistic-lock check against
// expectedUpdatedAt (the data model §3.9).
func (r *MemoryCaseRepo) UpdateIfUnmodified(_ context.Context, c *domain.Case, expectedUpdatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.data[c.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "case", ID: c.ID}
	}
	if !existing.UpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "updated_at mismatch"}
	}
	c.UpdatedAt = time.Now()
	r.data[c.ID] = c
	return nil
}

func (r *MemoryCaseRepo) AddNote(_ context.Context, caseID string, note *domain.CaseNote) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.data[caseID]
	if !ok {
		return &domain.ErrNotFound{Entity: "case", ID: caseID}
	}
	c.Notes = append(c.Notes, *note)
	c.UpdatedAt = time.Now()
	return nil
}

// MemoryAPIKeyRepo

type MemoryAPIKeyRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.APIKey // keyed by hash
}

func NewMemoryAPIKeyRepo() *MemoryAPIKeyRepo {
	return &MemoryAPIKeyRepo{data: make(map[string]*domain.APIKey)}
}

func (r *MemoryAPIKeyRepo) GetByHash(_ context.Context, keyHash string) (*domain.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.data[keyHash]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "api_key", ID: keyHash}
	}
	cp := *k
	return &cp, nil
}

func (r *MemoryAPIKeyRepo) Create(_ context.Context, key *domain.APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[key.KeyHash] = key
	return nil
}

func (r *MemoryAPIKeyRepo) List(_ context.Context) ([]domain.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var keys []domain.APIKey
	for _, k := range r.data {
		keys = append(keys, *k)
	}
	return keys, nil
}

func (r *MemoryAPIKeyRepo) Revoke(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.data {
		if k.ID == id {
			k.Active = false
			return nil
		}
	}
	return &domain.ErrNotFound{Entity: "api_key", ID: id}
}

// MemoryWebhookRepo

type MemoryWebhookRepo struct {
	mu         sync.RWMutex
	webhooks   map[string]*domain.Webhook
	deliveries []domain.WebhookDelivery
	dlq        []domain.DLQEntry
}

func NewMemoryWebhookRepo() *MemoryWebhookRepo {
	return &MemoryWebhookRepo{webhooks: make(map[string]*domain.Webhook)}
}

func (r *MemoryWebhookRepo) Get(_ context.Context, id string) (*domain.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.webhooks[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "webhook", ID: id}
	}
	cp := *w
	return &cp, nil
}

func (r *MemoryWebhookRepo) List(_ context.Context) ([]domain.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Webhook
	for _, w := range r.webhooks {
		result = append(result, *w)
	}
	return result, nil
}

func (r *MemoryWebhookRepo) ListByEvent(_ context.Context, event domain.WebhookEventType) ([]domain.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.Webhook
	for _, w := range r.webhooks {
		if !w.Active {
			continue
		}
		for _, e := range w.Events {
			if e == event {
				result = append(result, *w)
				break
			}
		}
	}
	return result, nil
}

func (r *MemoryWebhookRepo) Create(_ context.Context, w *domain.Webhook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.webhooks[w.ID] = w
	return nil
}

func (r *MemoryWebhookRepo) Update(_ context.Context, w *domain.Webhook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.webhooks[w.ID]; !ok {
		return &domain.ErrNotFound{Entity: "webhook", ID: w.ID}
	}
	w.UpdatedAt = time.Now()
	r.webhooks[w.ID] = w
	return nil
}

func (r *MemoryWebhookRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.webhooks[id]; !ok {
		return &domain.ErrNotFound{Entity: "webhook", ID: id}
	}
	delete(r.webhooks, id)
	return nil
}

func (r *MemoryWebhookRepo) CreateDelivery(_ context.Context, d *domain.WebhookDelivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries = append(r.deliveries, *d)
	return nil
}

func (r *MemoryWebhookRepo) ListDeliveries(_ context.Context, webhookID string, limit int) ([]domain.WebhookDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.WebhookDelivery
	for i := len(r.deliveries) - 1; i >= 0; i-- {
		d := r.deliveries[i]
		if d.WebhookID == webhookID {
			result = append(result, d)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (r *MemoryWebhookRepo) UpdateDelivery(_ context.Context, d *domain.WebhookDelivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.deliveries {
		if r.deliveries[i].ID == d.ID {
			r.deliveries[i] = *d
			return nil
		}
	}
	return &domain.ErrNotFound{Entity: "webhook_delivery", ID: d.ID}
}

// ListPendingRetries returns failed deliveries whose NextAttemptAt is due
// (webhook_retry.go's background worker polls this).
func (r *MemoryWebhookRepo) ListPendingRetries(_ context.Context, before time.Time) ([]domain.WebhookDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []domain.WebhookDelivery
	for _, d := range r.deliveries {
		if d.NextAttemptAt != nil && !d.NextAttemptAt.After(before) {
			result = append(result, d)
		}
	}
	return result, nil
}

func (r *MemoryWebhookRepo) CreateDLQEntry(_ context.Context, entry *domain.DLQEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dlq = append(r.dlq, *entry)
	return nil
}

func (r *MemoryWebhookRepo) GetDLQEntry(_ context.Context, id string) (*domain.DLQEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.dlq {
		if e.ID == id {
			cp := e
			return &cp, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "webhook_dlq_entry", ID: id}
}

// ListDLQEntries returns all DLQ entries, oldest first, including already
// reprocessed ones (ReprocessedAt distinguishes them for the UI).
func (r *MemoryWebhookRepo) ListDLQEntries(_ context.Context) ([]domain.DLQEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.DLQEntry, len(r.dlq))
	copy(result, r.dlq)
	return result, nil
}

// CountDLQEntries counts entries not yet reprocessed, for the depth metric
// and 80% capacity warning (Task 4).
func (r *MemoryWebhookRepo) CountDLQEntries(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, e := range r.dlq {
		if e.ReprocessedAt == nil {
			count++
		}
	}
	return count, nil
}

func (r *MemoryWebhookRepo) MarkDLQEntryReprocessed(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.dlq {
		if r.dlq[i].ID == id {
			r.dlq[i].ReprocessedAt = &at
			return nil
		}
	}
	return &domain.ErrNotFound{Entity: "webhook_dlq_entry", ID: id}
}

// MemoryRuleRepo stores rule definitions keyed by name, each holding every
// version ever created (append-only; Auditability First). "id" in the
// RuleRepository interface refers to this name, not a per-row UUID: the
// rule_definitions PRIMARY KEY is regenerated on every version INSERT
// (migrations/001_init.sql), so name is the only value stable enough for
// GET /api/v1/rules/{id}?version=N to keep resolving after a PUT creates a
// new row.
type MemoryRuleRepo struct {
	mu       sync.RWMutex
	versions map[string][]*domain.RuleDefinition
}

func NewMemoryRuleRepo() *MemoryRuleRepo {
	return &MemoryRuleRepo{versions: make(map[string][]*domain.RuleDefinition)}
}

func (r *MemoryRuleRepo) Get(_ context.Context, id string) (*domain.RuleDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vs := r.versions[id]
	if len(vs) == 0 {
		return nil, &domain.ErrNotFound{Entity: "rule_definition", ID: id}
	}
	cp := *vs[len(vs)-1]
	return &cp, nil
}

func (r *MemoryRuleRepo) GetActive(_ context.Context, id string) (*domain.RuleDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vs := r.versions[id]
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].IsActive {
			cp := *vs[i]
			return &cp, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "active_rule_definition", ID: id}
}

func (r *MemoryRuleRepo) GetVersion(_ context.Context, id string, version int) (*domain.RuleDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rd := range r.versions[id] {
		if rd.Version == version {
			cp := *rd
			return &cp, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "rule_definition", ID: id}
}

func (r *MemoryRuleRepo) List(_ context.Context, ruleType domain.RuleType, activeOnly bool, limit int, after *domain.Cursor) ([]domain.RuleDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []domain.RuleDefinition
	for _, vs := range r.versions {
		for _, rd := range vs {
			if ruleType != "" && rd.Type != ruleType {
				continue
			}
			if activeOnly && !rd.IsActive {
				continue
			}
			all = append(all, *rd)
		}
	}

	return sortAndPageByCursor(all, limit, after,
		func(rd domain.RuleDefinition) time.Time { return rd.CreatedAt },
		func(rd domain.RuleDefinition) string { return rd.ID },
	), nil
}

func (r *MemoryRuleRepo) Create(_ context.Context, rd *domain.RuleDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.versions[rd.Name]) > 0 {
		return fmt.Errorf("rule %q already exists", rd.Name)
	}
	rd.Version = 1
	cp := *rd
	r.versions[rd.Name] = []*domain.RuleDefinition{&cp}
	return nil
}

// CreateNewVersion never mutates an existing row: it appends a new one with
// version = max(existing versions) + 1 (Auditability First, no UPDATE/overwrite).
func (r *MemoryRuleRepo) CreateNewVersion(_ context.Context, rd *domain.RuleDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := r.versions[rd.Name]
	if len(existing) == 0 {
		return &domain.ErrNotFound{Entity: "rule_definition", ID: rd.Name}
	}
	rd.Version = existing[len(existing)-1].Version + 1
	if rd.IsActive {
		deactivateAll(existing)
	}
	cp := *rd
	r.versions[rd.Name] = append(existing, &cp)
	return nil
}

func (r *MemoryRuleRepo) SetActive(_ context.Context, id string, active bool, actor string) (*domain.RuleStateChange, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	vs := r.versions[id]
	if len(vs) == 0 {
		return nil, &domain.ErrNotFound{Entity: "rule_definition", ID: id}
	}

	latest := vs[len(vs)-1]
	target := latest
	if !active {
		target = nil
		for i := len(vs) - 1; i >= 0; i-- {
			if vs[i].IsActive {
				target = vs[i]
				break
			}
		}
		if target == nil {
			current := *latest
			return &domain.RuleStateChange{Current: &current, TargetVersion: latest.Version, TargetCreatedBy: latest.CreatedBy}, nil
		}
	}

	if actor == "" {
		return nil, separationOfDutiesError(target, "approver identity is missing")
	}
	if target.CreatedBy == "" {
		return nil, separationOfDutiesError(target, "rule creator identity is missing")
	}
	if target.CreatedBy == actor {
		return nil, separationOfDutiesError(target, "the rule author cannot change its active state")
	}

	changed := target.IsActive != active
	deactivateAll(vs)
	if active {
		latest.IsActive = true
	}
	current := *latest
	return &domain.RuleStateChange{
		Current:         &current,
		TargetVersion:   target.Version,
		TargetCreatedBy: target.CreatedBy,
		Changed:         changed,
	}, nil
}

func separationOfDutiesError(target *domain.RuleDefinition, reason string) error {
	return &domain.ErrSeparationOfDuties{
		RuleName: target.Name, Version: target.Version, RuleCreatedBy: target.CreatedBy, Reason: reason,
	}
}

// deactivateAll enforces the "at most one active version per rule name"
// invariant the engine's hot-reload (active rule set fetch) depends on.
func deactivateAll(versions []*domain.RuleDefinition) {
	for _, v := range versions {
		v.IsActive = false
	}
}

// MemoryRetentionRepo is the dev/test-only RetentionRepository
// (RET-001/RET-002), pre-seeded with the same configurable defaults as the
// database migrations so tests don't depend on Postgres.
type MemoryRetentionRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.RetentionPolicy
}

func NewMemoryRetentionRepo() *MemoryRetentionRepo {
	now := time.Now()
	seed := func(category string, retentionDays int, minRetentionDays *int) *domain.RetentionPolicy {
		return &domain.RetentionPolicy{
			ID:               category,
			DataCategory:     category,
			RetentionDays:    retentionDays,
			MinRetentionDays: minRetentionDays,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
	}
	return &MemoryRetentionRepo{
		data: map[string]*domain.RetentionPolicy{
			"customer_data":     seed("customer_data", 2555, nil),
			"transaction_data":  seed("transaction_data", 2555, nil),
			"alert_case_data":   seed("alert_case_data", 2555, nil),
			"cdd_score_history": seed("cdd_score_history", 2555, nil),
			"audit_log":         seed("audit_log", 3650, nil),
		},
	}
}

func (r *MemoryRetentionRepo) List(_ context.Context) ([]domain.RetentionPolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.RetentionPolicy, 0, len(r.data))
	for _, p := range r.data {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DataCategory < out[j].DataCategory })
	return out, nil
}

func (r *MemoryRetentionRepo) Get(_ context.Context, dataCategory string) (*domain.RetentionPolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.data[dataCategory]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "retention_policy", ID: dataCategory}
	}
	cp := *p
	return &cp, nil
}

func (r *MemoryRetentionRepo) Update(_ context.Context, dataCategory string, retentionDays int, updatedBy string) (*domain.RetentionPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.data[dataCategory]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "retention_policy", ID: dataCategory}
	}
	if retentionDays <= 0 {
		return nil, &domain.ErrInvalidRetentionDays{Days: retentionDays}
	}
	if p.MinRetentionDays != nil && retentionDays < *p.MinRetentionDays {
		return nil, &domain.ErrRetentionShorten{DataCategory: dataCategory, RequestedDays: retentionDays, MinDays: *p.MinRetentionDays}
	}
	p.RetentionDays = retentionDays
	p.UpdatedBy = updatedBy
	p.UpdatedAt = time.Now()
	cp := *p
	return &cp, nil
}

// MemoryScreeningResultRepo is the dev/test-only ScreeningResultRepository
// (the screening workflow §スクリーニングヒット後の調査ワークフロー).
type MemoryScreeningResultRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.ScreeningResultRecord
}

func NewMemoryScreeningResultRepo() *MemoryScreeningResultRepo {
	return &MemoryScreeningResultRepo{data: make(map[string]*domain.ScreeningResultRecord)}
}

func (r *MemoryScreeningResultRepo) Get(_ context.Context, id string) (*domain.ScreeningResultRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sr, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "screening_result", ID: id}
	}
	cp := *sr
	return &cp, nil
}

func (r *MemoryScreeningResultRepo) Create(_ context.Context, sr *domain.ScreeningResultRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *sr
	r.data[sr.ID] = &cp
	return nil
}

func (r *MemoryScreeningResultRepo) Update(_ context.Context, sr *domain.ScreeningResultRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[sr.ID]; !ok {
		return &domain.ErrNotFound{Entity: "screening_result", ID: sr.ID}
	}
	cp := *sr
	r.data[sr.ID] = &cp
	return nil
}

func (r *MemoryScreeningResultRepo) ListByCustomer(_ context.Context, customerID string, limit, offset int) ([]domain.ScreeningResultRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.ScreeningResultRecord
	for _, sr := range r.data {
		if sr.CustomerID == customerID {
			all = append(all, *sr)
		}
	}
	sortByCreatedAtDesc(all,
		func(sr domain.ScreeningResultRecord) time.Time { return sr.CreatedAt },
		func(sr domain.ScreeningResultRecord) string { return sr.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

func (r *MemoryScreeningResultRepo) ListByStatus(_ context.Context, status domain.ScreeningResultStatus, limit, offset int) ([]domain.ScreeningResultRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.ScreeningResultRecord
	for _, sr := range r.data {
		if sr.Status == status {
			all = append(all, *sr)
		}
	}
	sortByCreatedAtDesc(all,
		func(sr domain.ScreeningResultRecord) time.Time { return sr.CreatedAt },
		func(sr domain.ScreeningResultRecord) string { return sr.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

// ListPastFalsePositives lets a reviewer see prior False Positive
// determinations against the same list entry (the screening workflow "同一リストエントリへの
// 再ヒット時に過去の False Positive 判定を参照可能とする"), regardless of which
// customer triggered the earlier hit.
func (r *MemoryScreeningResultRepo) ListPastFalsePositives(_ context.Context, entryID string) ([]domain.ScreeningResultRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.ScreeningResultRecord
	for _, sr := range r.data {
		if sr.EntryID == entryID && sr.Status == domain.ScreeningResultStatusFalsePositive {
			all = append(all, *sr)
		}
	}
	sortByCreatedAtDesc(all,
		func(sr domain.ScreeningResultRecord) time.Time { return sr.CreatedAt },
		func(sr domain.ScreeningResultRecord) string { return sr.ID },
	)
	return all, nil
}

// MemoryAccountRepo (WS-11 Task 4, the data model §1.1.3). RepresentativeRiskScore
// reads customerRepo directly (same package) rather than duplicating
// risk_score storage, mirroring PgAccountRepo's JOIN against customers.
type MemoryAccountRepo struct {
	mu           sync.RWMutex
	accounts     map[string]*domain.Account
	customers    map[string][]domain.AccountCustomer // accountID -> links
	customerRepo *MemoryCustomerRepo
}

func NewMemoryAccountRepo(customerRepo *MemoryCustomerRepo) *MemoryAccountRepo {
	return &MemoryAccountRepo{
		accounts:     make(map[string]*domain.Account),
		customers:    make(map[string][]domain.AccountCustomer),
		customerRepo: customerRepo,
	}
}

func (r *MemoryAccountRepo) Create(_ context.Context, a *domain.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	a.CreatedAt = now
	a.UpdatedAt = now
	cp := *a
	r.accounts[a.ID] = &cp
	return nil
}

func (r *MemoryAccountRepo) Get(_ context.Context, id string) (*domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.accounts[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "account", ID: id}
	}
	cp := *a
	return &cp, nil
}

func (r *MemoryAccountRepo) AddCustomer(_ context.Context, accountID, customerID string, role domain.AccountRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.accounts[accountID]; !ok {
		return &domain.ErrNotFound{Entity: "account", ID: accountID}
	}
	r.customers[accountID] = append(r.customers[accountID], domain.AccountCustomer{
		AccountID:  accountID,
		CustomerID: customerID,
		Role:       role,
	})
	return nil
}

func (r *MemoryAccountRepo) ListCustomers(_ context.Context, accountID string) ([]domain.AccountCustomer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.AccountCustomer, len(r.customers[accountID]))
	copy(out, r.customers[accountID])
	return out, nil
}

func (r *MemoryAccountRepo) RepresentativeRiskScore(ctx context.Context, accountID string) (*float64, error) {
	r.mu.RLock()
	links := make([]domain.AccountCustomer, len(r.customers[accountID]))
	copy(links, r.customers[accountID])
	r.mu.RUnlock()

	var max *float64
	for _, link := range links {
		c, err := r.customerRepo.Get(ctx, link.CustomerID)
		if err != nil || c.RiskScore == nil {
			continue
		}
		if max == nil || *c.RiskScore > *max {
			max = c.RiskScore
		}
	}
	return max, nil
}
