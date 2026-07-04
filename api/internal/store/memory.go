package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// sortByCreatedAtDesc orders items by (created_at, id) descending, matching
// the default sort api.md §1.1 specifies and the ORDER BY clause the
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
	mu   sync.RWMutex
	data map[string]*domain.Transaction
	byCustomer map[string][]string
}

func NewMemoryTransactionRepo() *MemoryTransactionRepo {
	return &MemoryTransactionRepo{
		data:       make(map[string]*domain.Transaction),
		byCustomer: make(map[string][]string),
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

func (r *MemoryTransactionRepo) Create(_ context.Context, t *domain.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[t.ID] = t
	r.byCustomer[t.CustomerID] = append(r.byCustomer[t.CustomerID], t.ID)
	return nil
}

type MemoryAlertRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.Alert
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

func (r *MemoryAlertRepo) Create(_ context.Context, a *domain.Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[a.ID] = a
	r.byCustomer[a.CustomerID] = append(r.byCustomer[a.CustomerID], a.ID)
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

func (r *MemoryAuditRepo) List(_ context.Context, resourceType, resourceID string, limit int) ([]domain.AuditEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []domain.AuditEntry
	for i := len(r.entries) - 1; i >= 0; i-- {
		e := r.entries[i]
		if resourceType != "" && e.ResourceType != resourceType {
			continue
		}
		if resourceID != "" && e.ResourceID != resourceID {
			continue
		}
		filtered = append(filtered, e)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
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
	return result, nil
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

func (r *MemoryRuleRepo) SetActive(_ context.Context, id string, active bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	vs := r.versions[id]
	if len(vs) == 0 {
		return &domain.ErrNotFound{Entity: "rule_definition", ID: id}
	}
	if active {
		deactivateAll(vs)
	}
	vs[len(vs)-1].IsActive = active
	return nil
}

// deactivateAll enforces the "at most one active version per rule name"
// invariant the engine's hot-reload (active rule set fetch) depends on.
func deactivateAll(versions []*domain.RuleDefinition) {
	for _, v := range versions {
		v.IsActive = false
	}
}
