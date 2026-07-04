package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

// MemoryUserRepo

type MemoryUserRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.User // keyed by id
}

func NewMemoryUserRepo() *MemoryUserRepo {
	return &MemoryUserRepo{data: make(map[string]*domain.User)}
}

func (r *MemoryUserRepo) Get(_ context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "user", ID: id}
	}
	cp := *u
	return &cp, nil
}

func (r *MemoryUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.data {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "user", ID: email}
}

func (r *MemoryUserRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[u.ID] = u
	return nil
}

func (r *MemoryUserRepo) Update(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[u.ID]; !ok {
		return &domain.ErrNotFound{Entity: "user", ID: u.ID}
	}
	r.data[u.ID] = u
	return nil
}

func (r *MemoryUserRepo) Count(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.data), nil
}

// MemoryRefreshTokenRepo

type MemoryRefreshTokenRepo struct {
	mu   sync.RWMutex
	data map[string]*domain.RefreshToken // keyed by id
}

func NewMemoryRefreshTokenRepo() *MemoryRefreshTokenRepo {
	return &MemoryRefreshTokenRepo{data: make(map[string]*domain.RefreshToken)}
}

func (r *MemoryRefreshTokenRepo) Create(_ context.Context, t *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[t.ID] = t
	return nil
}

func (r *MemoryRefreshTokenRepo) GetByHash(_ context.Context, tokenHash string) (*domain.RefreshToken, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.data {
		if t.TokenHash == tokenHash {
			cp := *t
			return &cp, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "refresh_token", ID: tokenHash}
}

func (r *MemoryRefreshTokenRepo) RevokeFamily(_ context.Context, tokenFamily string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, t := range r.data {
		if t.TokenFamily == tokenFamily && t.RevokedAt == nil {
			revokedAt := now
			t.RevokedAt = &revokedAt
		}
	}
	return nil
}

func (r *MemoryRefreshTokenRepo) Revoke(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.data[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "refresh_token", ID: id}
	}
	if t.RevokedAt == nil {
		now := time.Now()
		t.RevokedAt = &now
	}
	return nil
}

func (r *MemoryRefreshTokenRepo) CountActiveByUser(_ context.Context, userID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, t := range r.data {
		if t.UserID == userID && t.RevokedAt == nil && t.ExpiresAt.After(now) {
			count++
		}
	}
	return count, nil
}

func (r *MemoryRefreshTokenRepo) ListActiveByUser(_ context.Context, userID string) ([]domain.RefreshToken, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	var active []domain.RefreshToken
	for _, t := range r.data {
		if t.UserID == userID && t.RevokedAt == nil && t.ExpiresAt.After(now) {
			active = append(active, *t)
		}
	}
	return active, nil
}

// PgUserRepo

type PgUserRepo struct {
	pool *pgxpool.Pool
}

func NewPgUserRepo(pool *pgxpool.Pool) *PgUserRepo {
	return &PgUserRepo{pool: pool}
}

func (r *PgUserRepo) scanUser(ctx context.Context, query string, args ...any) (*domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Active, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "user", ID: ""}
		}
		return nil, err
	}
	return &u, nil
}

func (r *PgUserRepo) Get(ctx context.Context, id string) (*domain.User, error) {
	return r.scanUser(ctx,
		`SELECT id, email, password_hash, role, active, created_at, updated_at FROM users WHERE id = $1`, id)
}

func (r *PgUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.scanUser(ctx,
		`SELECT id, email, password_hash, role, active, created_at, updated_at FROM users WHERE email = $1`, email)
}

func (r *PgUserRepo) Create(ctx context.Context, u *domain.User) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, role, active, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.Active, u.CreatedAt, u.UpdatedAt)
	return err
}

func (r *PgUserRepo) Update(ctx context.Context, u *domain.User) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET email = $2, password_hash = $3, role = $4, active = $5, updated_at = $6 WHERE id = $1`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.Active, u.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "user", ID: u.ID}
	}
	return nil
}

func (r *PgUserRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// PgRefreshTokenRepo

type PgRefreshTokenRepo struct {
	pool *pgxpool.Pool
}

func NewPgRefreshTokenRepo(pool *pgxpool.Pool) *PgRefreshTokenRepo {
	return &PgRefreshTokenRepo{pool: pool}
}

func (r *PgRefreshTokenRepo) Create(ctx context.Context, t *domain.RefreshToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, token_family, expires_at, revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, t.UserID, t.TokenHash, t.TokenFamily, t.ExpiresAt, t.RevokedAt, t.CreatedAt)
	return err
}

func (r *PgRefreshTokenRepo) GetByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	var t domain.RefreshToken
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, token_family, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash = $1`, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.TokenFamily, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "refresh_token", ID: tokenHash}
		}
		return nil, err
	}
	return &t, nil
}

func (r *PgRefreshTokenRepo) RevokeFamily(ctx context.Context, tokenFamily string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_family = $1 AND revoked_at IS NULL`, tokenFamily)
	return err
}

func (r *PgRefreshTokenRepo) Revoke(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "refresh_token", ID: id}
	}
	return nil
}

func (r *PgRefreshTokenRepo) CountActiveByUser(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()`, userID,
	).Scan(&count)
	return count, err
}

func (r *PgRefreshTokenRepo) ListActiveByUser(ctx context.Context, userID string) ([]domain.RefreshToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, token_hash, token_family, expires_at, revoked_at, created_at FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW() ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []domain.RefreshToken
	for rows.Next() {
		var t domain.RefreshToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.TokenFamily, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
