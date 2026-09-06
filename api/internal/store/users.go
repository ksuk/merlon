package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
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

func (r *MemoryUserRepo) CreateIfEmpty(_ context.Context, u *domain.User) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.data) != 0 {
		return false, nil
	}
	r.data[u.ID] = u
	return true, nil
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

func (r *MemoryUserRepo) List(_ context.Context) ([]domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users := make([]domain.User, 0, len(r.data))
	for _, u := range r.data {
		users = append(users, *u)
	}
	sortByCreatedAtDesc(users, func(u domain.User) time.Time { return u.CreatedAt }, func(u domain.User) string { return u.ID })
	return users, nil
}

// MemoryRefreshTokenRepo

type MemoryRefreshTokenRepo struct {
	mu    sync.RWMutex
	data  map[string]*domain.RefreshToken // keyed by id
	audit domain.AuditRepository
}

func NewMemoryRefreshTokenRepo() *MemoryRefreshTokenRepo {
	return &MemoryRefreshTokenRepo{data: make(map[string]*domain.RefreshToken)}
}

func NewMemoryRefreshTokenRepoWithAudit(audit domain.AuditRepository) *MemoryRefreshTokenRepo {
	return &MemoryRefreshTokenRepo{data: make(map[string]*domain.RefreshToken), audit: audit}
}

func (r *MemoryRefreshTokenRepo) CreateSession(ctx context.Context, t *domain.RefreshToken, maxActiveFamilies int) ([]string, error) {
	return r.createSession(ctx, t, maxActiveFamilies, nil)
}

func (r *MemoryRefreshTokenRepo) CreateSessionWithAudit(ctx context.Context, t *domain.RefreshToken, maxActiveFamilies int, auditEntry *domain.AuditEntry) ([]string, error) {
	return r.createSession(ctx, t, maxActiveFamilies, auditEntry)
}

func (r *MemoryRefreshTokenRepo) createSession(ctx context.Context, t *domain.RefreshToken, maxActiveFamilies int, auditEntry *domain.AuditEntry) ([]string, error) {
	if maxActiveFamilies <= 0 {
		return nil, errors.New("max active refresh-token families must be positive")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	active := r.activeFamilyStartsLocked(t.UserID, t.CreatedAt)
	type familyStart struct {
		family    string
		createdAt time.Time
	}
	ordered := make([]familyStart, 0, len(active))
	for family, createdAt := range active {
		ordered = append(ordered, familyStart{family: family, createdAt: createdAt})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].createdAt.Equal(ordered[j].createdAt) {
			return ordered[i].family < ordered[j].family
		}
		return ordered[i].createdAt.Before(ordered[j].createdAt)
	})

	evictCount := len(ordered) - maxActiveFamilies + 1
	if evictCount < 0 {
		evictCount = 0
	}
	evicted := make([]string, 0, evictCount)
	for _, candidate := range ordered[:evictCount] {
		evicted = append(evicted, candidate.family)
	}
	if auditEntry != nil {
		if r.audit == nil {
			return nil, errors.New("audit repository is required for audited session creation")
		}
		if err := r.audit.Create(ctx, auditEntry); err != nil {
			return nil, err
		}
	}
	for _, family := range evicted {
		r.revokeFamilyLocked(family, t.CreatedAt)
	}

	cp := *t
	r.data[t.ID] = &cp
	return evicted, nil
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

func (r *MemoryRefreshTokenRepo) Rotate(_ context.Context, tokenHash string, replacement *domain.RefreshToken) (*domain.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var current *domain.RefreshToken
	for _, token := range r.data {
		if token.TokenHash == tokenHash {
			current = token
			break
		}
	}
	if current == nil {
		return nil, &domain.ErrNotFound{Entity: "refresh_token", ID: tokenHash}
	}
	currentCopy := *current
	if current.RevokedAt != nil {
		r.revokeFamilyLocked(current.TokenFamily, replacement.CreatedAt)
		return &currentCopy, domain.ErrRefreshTokenReuse
	}
	if !current.ExpiresAt.After(replacement.CreatedAt) {
		return &currentCopy, domain.ErrRefreshTokenExpired
	}

	revokedAt := replacement.CreatedAt
	current.RevokedAt = &revokedAt
	replacementCopy := *replacement
	replacementCopy.UserID = current.UserID
	replacementCopy.TokenFamily = current.TokenFamily
	replacementCopy.SessionRole = current.SessionRole
	r.data[replacementCopy.ID] = &replacementCopy
	return &currentCopy, nil
}

func (r *MemoryRefreshTokenRepo) RevokeFamily(_ context.Context, tokenFamily string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revokeFamilyLocked(tokenFamily, time.Now())
	return nil
}

func (r *MemoryRefreshTokenRepo) revokeFamilyLocked(tokenFamily string, now time.Time) {
	for _, t := range r.data {
		if t.TokenFamily == tokenFamily && t.RevokedAt == nil {
			revokedAt := now
			t.RevokedAt = &revokedAt
		}
	}
}

func (r *MemoryRefreshTokenRepo) RevokeUserSessions(ctx context.Context, userID string) ([]string, error) {
	return r.revokeUserSessions(ctx, userID, nil)
}

func (r *MemoryRefreshTokenRepo) RevokeUserSessionsWithAudit(ctx context.Context, userID string, auditEntry *domain.AuditEntry) ([]string, error) {
	return r.revokeUserSessions(ctx, userID, auditEntry)
}

func (r *MemoryRefreshTokenRepo) revokeUserSessions(ctx context.Context, userID string, auditEntry *domain.AuditEntry) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	active := r.activeFamilyStartsLocked(userID, now)
	families := make([]string, 0, len(active))
	for family := range active {
		families = append(families, family)
	}
	sort.Strings(families)
	if auditEntry != nil {
		if r.audit == nil {
			return nil, errors.New("audit repository is required for audited session revocation")
		}
		if err := r.audit.Create(ctx, auditEntry); err != nil {
			return nil, err
		}
	}
	for _, family := range families {
		r.revokeFamilyLocked(family, now)
	}
	return families, nil
}

func (r *MemoryRefreshTokenRepo) IsFamilyActive(_ context.Context, tokenFamily string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	for _, token := range r.data {
		if token.TokenFamily == tokenFamily && token.RevokedAt == nil && token.ExpiresAt.After(now) {
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryRefreshTokenRepo) activeFamilyStartsLocked(userID string, now time.Time) map[string]time.Time {
	active := make(map[string]bool)
	for _, token := range r.data {
		if token.UserID == userID && token.RevokedAt == nil && token.ExpiresAt.After(now) {
			active[token.TokenFamily] = true
		}
	}
	starts := make(map[string]time.Time, len(active))
	for _, token := range r.data {
		if !active[token.TokenFamily] {
			continue
		}
		start, ok := starts[token.TokenFamily]
		if !ok || token.CreatedAt.Before(start) {
			starts[token.TokenFamily] = token.CreatedAt
		}
	}
	return starts
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

const initialAdministratorLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended('merlon.initial-administrator', 0))`

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

func (r *PgUserRepo) CreateIfEmpty(ctx context.Context, u *domain.User) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// All initial-setup callers share this transaction-scoped lock. The
	// explicit READ COMMITTED isolation above ensures the emptiness query gets
	// a fresh snapshot after a winning setup transaction commits while this
	// caller waits for the lock.
	if _, err := tx.Exec(ctx, initialAdministratorLockSQL); err != nil {
		return false, err
	}

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, role, active, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.Active, u.CreatedAt, u.UpdatedAt); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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

func (r *PgUserRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, email, password_hash, role, active, created_at, updated_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Active, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// PgRefreshTokenRepo

type PgRefreshTokenRepo struct {
	pool *pgxpool.Pool
}

const (
	refreshTokenUserLockSQL   = `SELECT pg_advisory_xact_lock(hashtextextended($1, 1))`
	refreshTokenFamilyLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1, 2))`
)

func NewPgRefreshTokenRepo(pool *pgxpool.Pool) *PgRefreshTokenRepo {
	return &PgRefreshTokenRepo{pool: pool}
}

func (r *PgRefreshTokenRepo) CreateSession(ctx context.Context, t *domain.RefreshToken, maxActiveFamilies int) ([]string, error) {
	return r.createSession(ctx, t, maxActiveFamilies, nil)
}

func (r *PgRefreshTokenRepo) CreateSessionWithAudit(ctx context.Context, t *domain.RefreshToken, maxActiveFamilies int, auditEntry *domain.AuditEntry) ([]string, error) {
	return r.createSession(ctx, t, maxActiveFamilies, auditEntry)
}

func (r *PgRefreshTokenRepo) createSession(ctx context.Context, t *domain.RefreshToken, maxActiveFamilies int, auditEntry *domain.AuditEntry) ([]string, error) {
	if maxActiveFamilies <= 0 {
		return nil, errors.New("max active refresh-token families must be positive")
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, refreshTokenUserLockSQL, t.UserID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT token_family, MIN(created_at) AS family_created_at
		FROM refresh_tokens
		WHERE user_id = $1
		GROUP BY token_family
		HAVING BOOL_OR(revoked_at IS NULL AND expires_at > $2)
		ORDER BY family_created_at ASC, token_family ASC`, t.UserID, t.CreatedAt)
	if err != nil {
		return nil, err
	}
	var activeFamilies []string
	for rows.Next() {
		var family string
		var createdAt time.Time
		if err := rows.Scan(&family, &createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		activeFamilies = append(activeFamilies, family)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	evictCount := len(activeFamilies) - maxActiveFamilies + 1
	if evictCount < 0 {
		evictCount = 0
	}
	evicted := append([]string(nil), activeFamilies[:evictCount]...)
	for _, family := range evicted {
		if _, err := tx.Exec(ctx, refreshTokenFamilyLockSQL, family); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE token_family = $1 AND revoked_at IS NULL`, family, t.CreatedAt); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, token_family, session_role, expires_at, revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		t.ID, t.UserID, t.TokenHash, t.TokenFamily, string(t.SessionRole), t.ExpiresAt, t.RevokedAt, t.CreatedAt); err != nil {
		return nil, err
	}
	if auditEntry != nil {
		if err := NewPgAuditRepo(tx).Create(ctx, auditEntry); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return evicted, nil
}

func (r *PgRefreshTokenRepo) GetByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	var t domain.RefreshToken
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, token_family, session_role, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash = $1`, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.TokenFamily, &t.SessionRole, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "refresh_token", ID: tokenHash}
		}
		return nil, err
	}
	return &t, nil
}

func (r *PgRefreshTokenRepo) Rotate(ctx context.Context, tokenHash string, replacement *domain.RefreshToken) (*domain.RefreshToken, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var family string
	if err := tx.QueryRow(ctx, `SELECT token_family FROM refresh_tokens WHERE token_hash = $1`, tokenHash).Scan(&family); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "refresh_token", ID: tokenHash}
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, refreshTokenFamilyLockSQL, family); err != nil {
		return nil, err
	}

	var current domain.RefreshToken
	if err := tx.QueryRow(ctx,
		`SELECT id, user_id, token_hash, token_family, session_role, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash = $1`, tokenHash,
	).Scan(&current.ID, &current.UserID, &current.TokenHash, &current.TokenFamily, &current.SessionRole, &current.ExpiresAt, &current.RevokedAt, &current.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "refresh_token", ID: tokenHash}
		}
		return nil, err
	}

	if current.RevokedAt != nil {
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE token_family = $1 AND revoked_at IS NULL`, current.TokenFamily, replacement.CreatedAt); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &current, domain.ErrRefreshTokenReuse
	}
	if !current.ExpiresAt.After(replacement.CreatedAt) {
		return &current, domain.ErrRefreshTokenExpired
	}

	tag, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, current.ID, replacement.CreatedAt)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, domain.ErrRefreshTokenReuse
	}
	replacement.UserID = current.UserID
	replacement.TokenFamily = current.TokenFamily
	replacement.SessionRole = current.SessionRole
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, token_family, session_role, expires_at, revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		replacement.ID, replacement.UserID, replacement.TokenHash, replacement.TokenFamily, string(replacement.SessionRole), replacement.ExpiresAt, replacement.RevokedAt, replacement.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &current, nil
}

func (r *PgRefreshTokenRepo) RevokeFamily(ctx context.Context, tokenFamily string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, refreshTokenFamilyLockSQL, tokenFamily); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_family = $1 AND revoked_at IS NULL`, tokenFamily); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgRefreshTokenRepo) RevokeUserSessions(ctx context.Context, userID string) ([]string, error) {
	return r.revokeUserSessions(ctx, userID, nil)
}

func (r *PgRefreshTokenRepo) RevokeUserSessionsWithAudit(ctx context.Context, userID string, auditEntry *domain.AuditEntry) ([]string, error) {
	return r.revokeUserSessions(ctx, userID, auditEntry)
}

func (r *PgRefreshTokenRepo) revokeUserSessions(ctx context.Context, userID string, auditEntry *domain.AuditEntry) ([]string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, refreshTokenUserLockSQL, userID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT token_family FROM refresh_tokens
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY token_family`, userID)
	if err != nil {
		return nil, err
	}
	var families []string
	for rows.Next() {
		var family string
		if err := rows.Scan(&family); err != nil {
			rows.Close()
			return nil, err
		}
		families = append(families, family)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, family := range families {
		if _, err := tx.Exec(ctx, refreshTokenFamilyLockSQL, family); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_family = $1 AND revoked_at IS NULL`, family); err != nil {
			return nil, err
		}
	}
	if auditEntry != nil {
		if err := NewPgAuditRepo(tx).Create(ctx, auditEntry); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return families, nil
}

func (r *PgRefreshTokenRepo) IsFamilyActive(ctx context.Context, tokenFamily string) (bool, error) {
	var active bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM refresh_tokens
		WHERE token_family = $1 AND revoked_at IS NULL AND expires_at > NOW()
	)`, tokenFamily).Scan(&active)
	return active, err
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
		`SELECT id, user_id, token_hash, token_family, session_role, expires_at, revoked_at, created_at FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW() ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []domain.RefreshToken
	for rows.Next() {
		var t domain.RefreshToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.TokenFamily, &t.SessionRole, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
