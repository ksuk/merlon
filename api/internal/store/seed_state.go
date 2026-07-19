package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

type PgSeedStateRepo struct{ db DBTX }

func NewPgSeedStateRepo(db DBTX) *PgSeedStateRepo { return &PgSeedStateRepo{db: db} }

func (r *PgSeedStateRepo) Get(ctx context.Context) (*domain.SeedState, error) {
	state := &domain.SeedState{}
	err := r.db.QueryRow(ctx, `SELECT dataset_kind, completed_at FROM seed_state WHERE id = 'initial'`).Scan(&state.DatasetKind, &state.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (r *PgSeedStateRepo) MarkCompleted(ctx context.Context, kind domain.SeedDatasetKind) error {
	_, err := r.db.Exec(ctx, `INSERT INTO seed_state (id, dataset_kind) VALUES ('initial', $1)`, kind)
	return err
}

// MemorySeedStateRepo mirrors the singleton database row for in-memory
// development and unit tests.
type MemorySeedStateRepo struct {
	mu    sync.RWMutex
	state *domain.SeedState
}

func NewMemorySeedStateRepo() *MemorySeedStateRepo { return &MemorySeedStateRepo{} }

func (r *MemorySeedStateRepo) Get(_ context.Context) (*domain.SeedState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state == nil {
		return nil, nil
	}
	cp := *r.state
	return &cp, nil
}

func (r *MemorySeedStateRepo) MarkCompleted(_ context.Context, kind domain.SeedDatasetKind) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != nil {
		return errors.New("seed state already completed")
	}
	r.state = &domain.SeedState{DatasetKind: kind, CompletedAt: time.Now().UTC()}
	return nil
}

var _ domain.SeedStateRepository = (*PgSeedStateRepo)(nil)
var _ domain.SeedStateRepository = (*MemorySeedStateRepo)(nil)
