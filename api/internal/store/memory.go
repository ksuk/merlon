package store

import (
	"context"
	"sync"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

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
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
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
	ids := r.byCustomer[customerID]
	if offset >= len(ids) {
		return nil, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	var result []domain.Transaction
	for _, id := range ids[offset:end] {
		result = append(result, *r.data[id])
	}
	return result, nil
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
	ids := r.byCustomer[customerID]
	if offset >= len(ids) {
		return nil, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	var result []domain.Alert
	for _, id := range ids[offset:end] {
		result = append(result, *r.data[id])
	}
	return result, nil
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
	if offset >= len(open) {
		return nil, nil
	}
	end := offset + limit
	if end > len(open) {
		end = len(open)
	}
	return open[offset:end], nil
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
