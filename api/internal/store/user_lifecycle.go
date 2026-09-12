package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ksuk/merlon/api/internal/domain"
)

const userLifecycleLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended('merlon.user-lifecycle', 0))`

// MemoryUserLifecycleRepo is the database-free equivalent of the PostgreSQL
// lifecycle transaction. It locks the three affected repositories in a fixed
// order and validates every failure before changing any state.
type MemoryUserLifecycleRepo struct {
	users  *MemoryUserRepo
	tokens *MemoryRefreshTokenRepo
	audit  *MemoryAuditRepo
}

func NewMemoryUserLifecycleRepo(users *MemoryUserRepo, tokens *MemoryRefreshTokenRepo, audit *MemoryAuditRepo) *MemoryUserLifecycleRepo {
	return &MemoryUserLifecycleRepo{users: users, tokens: tokens, audit: audit}
}

func (r *MemoryUserLifecycleRepo) configured() bool {
	return r != nil && r.users != nil && r.tokens != nil && r.audit != nil
}

func appendMemoryAuditLocked(audit *MemoryAuditRepo, entry *domain.AuditEntry) error {
	if audit.createFailure != nil {
		return audit.createFailure
	}
	entry.ID = audit.nextID
	audit.nextID++
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	audit.entries = append(audit.entries, *entry)
	return nil
}

func (r *MemoryUserLifecycleRepo) CreateUser(_ context.Context, user *domain.User, entry *domain.AuditEntry) error {
	if !r.configured() {
		return errors.New("user lifecycle repository is not configured")
	}
	r.users.mu.Lock()
	defer r.users.mu.Unlock()
	r.audit.mu.Lock()
	defer r.audit.mu.Unlock()

	for _, existing := range r.users.data {
		if strings.EqualFold(existing.Email, user.Email) {
			return &domain.ErrConflict{Entity: "user", ID: user.Email, Reason: "email already exists"}
		}
	}
	if _, exists := r.users.data[user.ID]; exists {
		return &domain.ErrConflict{Entity: "user", ID: user.ID, Reason: "identifier already exists"}
	}
	if err := appendMemoryAuditLocked(r.audit, entry); err != nil {
		return err
	}
	copyUser := *user
	r.users.data[user.ID] = &copyUser
	return nil
}

func (r *MemoryUserLifecycleRepo) UpdateAuthority(_ context.Context, id string, role domain.Role, active bool, updatedAt time.Time, entry *domain.AuditEntry) (*domain.User, []string, error) {
	if !r.configured() {
		return nil, nil, errors.New("user lifecycle repository is not configured")
	}
	r.users.mu.Lock()
	defer r.users.mu.Unlock()
	r.tokens.mu.Lock()
	defer r.tokens.mu.Unlock()
	r.audit.mu.Lock()
	defer r.audit.mu.Unlock()

	current, ok := r.users.data[id]
	if !ok {
		return nil, nil, &domain.ErrNotFound{Entity: "user", ID: id}
	}
	if current.Active && current.Role == domain.RoleAdmin && (!active || role != domain.RoleAdmin) {
		activeAdmins := 0
		for _, user := range r.users.data {
			if user.Active && user.Role == domain.RoleAdmin {
				activeAdmins++
			}
		}
		if activeAdmins == 1 {
			return nil, nil, &domain.ErrConflict{Entity: "user", ID: id, Reason: "last active administrator must remain active"}
		}
	}
	if current.Role == role && current.Active == active {
		result := *current
		return &result, nil, nil
	}

	families := memoryActiveFamiliesLocked(r.tokens, id, updatedAt)
	if err := appendMemoryAuditLocked(r.audit, entry); err != nil {
		return nil, nil, err
	}
	updated := *current
	updated.Role = role
	updated.Active = active
	updated.UpdatedAt = updatedAt
	r.users.data[id] = &updated
	revokeMemoryFamiliesLocked(r.tokens, families, updatedAt)
	result := updated
	return &result, families, nil
}

func (r *MemoryUserLifecycleRepo) ResetPassword(_ context.Context, id, passwordHash string, updatedAt time.Time, entry *domain.AuditEntry) (*domain.User, []string, error) {
	if !r.configured() {
		return nil, nil, errors.New("user lifecycle repository is not configured")
	}
	r.users.mu.Lock()
	defer r.users.mu.Unlock()
	r.tokens.mu.Lock()
	defer r.tokens.mu.Unlock()
	r.audit.mu.Lock()
	defer r.audit.mu.Unlock()

	current, ok := r.users.data[id]
	if !ok {
		return nil, nil, &domain.ErrNotFound{Entity: "user", ID: id}
	}
	families := memoryActiveFamiliesLocked(r.tokens, id, updatedAt)
	if err := appendMemoryAuditLocked(r.audit, entry); err != nil {
		return nil, nil, err
	}
	updated := *current
	updated.PasswordHash = passwordHash
	updated.UpdatedAt = updatedAt
	r.users.data[id] = &updated
	revokeMemoryFamiliesLocked(r.tokens, families, updatedAt)
	result := updated
	return &result, families, nil
}

func memoryActiveFamiliesLocked(tokens *MemoryRefreshTokenRepo, userID string, now time.Time) []string {
	set := make(map[string]struct{})
	for _, token := range tokens.data {
		if token.UserID == userID && token.RevokedAt == nil && token.ExpiresAt.After(now) {
			set[token.TokenFamily] = struct{}{}
		}
	}
	families := make([]string, 0, len(set))
	for family := range set {
		families = append(families, family)
	}
	sort.Strings(families)
	return families
}

func revokeMemoryFamiliesLocked(tokens *MemoryRefreshTokenRepo, families []string, now time.Time) {
	set := make(map[string]struct{}, len(families))
	for _, family := range families {
		set[family] = struct{}{}
	}
	for _, token := range tokens.data {
		if _, ok := set[token.TokenFamily]; ok && token.RevokedAt == nil {
			revokedAt := now
			token.RevokedAt = &revokedAt
		}
	}
}

// PgUserLifecycleRepo owns the PostgreSQL transaction for each account
// mutation. A global transaction-scoped lock serializes the last-Admin check.
type PgUserLifecycleRepo struct{ pool DBTX }

func NewPgUserLifecycleRepo(pool DBTX) *PgUserLifecycleRepo {
	return &PgUserLifecycleRepo{pool: pool}
}

func (r *PgUserLifecycleRepo) CreateUser(ctx context.Context, user *domain.User, entry *domain.AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, userLifecycleLockSQL); err != nil {
		return err
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE LOWER(email) = LOWER($1))`, user.Email).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return &domain.ErrConflict{Entity: "user", ID: user.Email, Reason: "email already exists"}
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, role, active, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		user.ID, user.Email, user.PasswordHash, string(user.Role), user.Active, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return &domain.ErrConflict{Entity: "user", ID: user.Email, Reason: "email already exists"}
		}
		return err
	}
	if err := NewPgAuditRepo(tx).Create(ctx, entry); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgUserLifecycleRepo) UpdateAuthority(ctx context.Context, id string, role domain.Role, active bool, updatedAt time.Time, entry *domain.AuditEntry) (*domain.User, []string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, userLifecycleLockSQL); err != nil {
		return nil, nil, err
	}
	current, err := getUserForUpdate(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if current.Active && current.Role == domain.RoleAdmin && (!active || role != domain.RoleAdmin) {
		var activeAdmins int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE active = TRUE AND role = 'admin'`).Scan(&activeAdmins); err != nil {
			return nil, nil, err
		}
		if activeAdmins == 1 {
			return nil, nil, &domain.ErrConflict{Entity: "user", ID: id, Reason: "last active administrator must remain active"}
		}
	}
	if current.Role == role && current.Active == active {
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return current, nil, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role = $2, active = $3, updated_at = $4 WHERE id = $1`, id, string(role), active, updatedAt); err != nil {
		return nil, nil, err
	}
	families, err := revokePgUserFamilies(ctx, tx, id, updatedAt)
	if err != nil {
		return nil, nil, err
	}
	if err := NewPgAuditRepo(tx).Create(ctx, entry); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	current.Role = role
	current.Active = active
	current.UpdatedAt = updatedAt
	return current, families, nil
}

func (r *PgUserLifecycleRepo) ResetPassword(ctx context.Context, id, passwordHash string, updatedAt time.Time, entry *domain.AuditEntry) (*domain.User, []string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)
	current, err := getUserForUpdate(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1`, id, passwordHash, updatedAt); err != nil {
		return nil, nil, err
	}
	families, err := revokePgUserFamilies(ctx, tx, id, updatedAt)
	if err != nil {
		return nil, nil, err
	}
	if err := NewPgAuditRepo(tx).Create(ctx, entry); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	current.PasswordHash = passwordHash
	current.UpdatedAt = updatedAt
	return current, families, nil
}

func getUserForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.User, error) {
	var user domain.User
	err := tx.QueryRow(ctx, `SELECT id, email, password_hash, role, active, created_at, updated_at FROM users WHERE id = $1 FOR UPDATE`, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Active, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "user", ID: id}
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func revokePgUserFamilies(ctx context.Context, tx pgx.Tx, userID string, now time.Time) ([]string, error) {
	if _, err := tx.Exec(ctx, refreshTokenUserLockSQL, userID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT token_family FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2 ORDER BY token_family`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var families []string
	for rows.Next() {
		var family string
		if err := rows.Scan(&family); err != nil {
			return nil, err
		}
		families = append(families, family)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, family := range families {
		if _, err := tx.Exec(ctx, refreshTokenFamilyLockSQL, family); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE token_family = $1 AND revoked_at IS NULL`, family, now); err != nil {
			return nil, err
		}
	}
	return families, nil
}
