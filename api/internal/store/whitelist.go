package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
)

// MemoryWhitelistRepo is the in-memory domain.WhitelistRepository
// implementation (dev/test), mirroring the invariants PgWhitelistRepo
// enforces at the database level: at most one active entry per customer
// (whitelist.md §3.1 partial unique index), and optimistic locking on
// UpdateWithVersion.
type MemoryWhitelistRepo struct {
	mu      sync.RWMutex
	entries map[string]*domain.WhitelistEntry
	reviews map[string][]*domain.WhitelistReview
}

func NewMemoryWhitelistRepo() *MemoryWhitelistRepo {
	return &MemoryWhitelistRepo{
		entries: make(map[string]*domain.WhitelistEntry),
		reviews: make(map[string][]*domain.WhitelistReview),
	}
}

func (r *MemoryWhitelistRepo) Get(_ context.Context, id string) (*domain.WhitelistEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "whitelist_entry", ID: id}
	}
	cp := *e
	return &cp, nil
}

func (r *MemoryWhitelistRepo) GetActiveByCustomer(_ context.Context, customerID string) (*domain.WhitelistEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.CustomerID == customerID && e.Status == domain.WhitelistEntryStatusActive {
			cp := *e
			return &cp, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "whitelist_entry", ID: customerID}
}

func (r *MemoryWhitelistRepo) List(_ context.Context, status domain.WhitelistEntryStatus, limit, offset int) ([]domain.WhitelistEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []domain.WhitelistEntry
	for _, e := range r.entries {
		if status != "" && e.Status != status {
			continue
		}
		all = append(all, *e)
	}
	sortByCreatedAtDesc(all,
		func(e domain.WhitelistEntry) time.Time { return e.CreatedAt },
		func(e domain.WhitelistEntry) string { return e.ID },
	)
	return pageByOffset(all, limit, offset), nil
}

func (r *MemoryWhitelistRepo) ListExpiringSoon(_ context.Context, withinDays int) ([]domain.WhitelistEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	threshold := time.Now().Add(time.Duration(withinDays) * 24 * time.Hour)
	var out []domain.WhitelistEntry
	for _, e := range r.entries {
		if e.Status == domain.WhitelistEntryStatusActive && e.ValidUntil.Before(threshold) {
			out = append(out, *e)
		}
	}
	sortByCreatedAtDesc(out,
		func(e domain.WhitelistEntry) time.Time { return e.CreatedAt },
		func(e domain.WhitelistEntry) string { return e.ID },
	)
	return out, nil
}

// activeConflict reports whether another entry (different from excludeID)
// is already active for customerID, enforcing whitelist.md §3.1's "at most
// one active entry per customer" invariant. Caller must hold r.mu.
func (r *MemoryWhitelistRepo) activeConflict(customerID, excludeID string) bool {
	for _, e := range r.entries {
		if e.ID != excludeID && e.CustomerID == customerID && e.Status == domain.WhitelistEntryStatusActive {
			return true
		}
	}
	return false
}

func (r *MemoryWhitelistRepo) Create(_ context.Context, e *domain.WhitelistEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Status == domain.WhitelistEntryStatusActive && r.activeConflict(e.CustomerID, e.ID) {
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: e.ID, Reason: "an active entry already exists for this customer"}
	}
	if e.Version == 0 {
		e.Version = 1
	}
	cp := *e
	r.entries[e.ID] = &cp
	return nil
}

func (r *MemoryWhitelistRepo) UpdateWithVersion(_ context.Context, e *domain.WhitelistEntry, expectedVersion int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.entries[e.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "whitelist_entry", ID: e.ID}
	}
	if existing.Version != expectedVersion {
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: e.ID, Reason: "version mismatch"}
	}
	if e.Status == domain.WhitelistEntryStatusActive && r.activeConflict(e.CustomerID, e.ID) {
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: e.ID, Reason: "an active entry already exists for this customer"}
	}
	e.Version = expectedVersion + 1
	e.UpdatedAt = time.Now()
	cp := *e
	r.entries[e.ID] = &cp
	return nil
}

func (r *MemoryWhitelistRepo) CreateReview(_ context.Context, rev *domain.WhitelistReview) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *rev
	r.reviews[rev.WhitelistEntryID] = append(r.reviews[rev.WhitelistEntryID], &cp)
	return nil
}

func (r *MemoryWhitelistRepo) ListReviews(_ context.Context, entryID string) ([]domain.WhitelistReview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.WhitelistReview
	for _, rev := range r.reviews[entryID] {
		out = append(out, *rev)
	}
	return out, nil
}

func (r *MemoryWhitelistRepo) CreateReviewAndApply(_ context.Context, review *domain.WhitelistReview, entry *domain.WhitelistEntry, expectedVersion int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.entries[entry.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "whitelist_entry", ID: entry.ID}
	}
	if existing.Version != expectedVersion {
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: entry.ID, Reason: "version mismatch"}
	}

	entry.Version = expectedVersion + 1
	entry.UpdatedAt = time.Now()
	entryCp := *entry
	r.entries[entry.ID] = &entryCp

	reviewCp := *review
	r.reviews[review.WhitelistEntryID] = append(r.reviews[review.WhitelistEntryID], &reviewCp)
	return nil
}

// PgWhitelistRepo implements domain.WhitelistRepository against
// whitelist_entries/whitelist_reviews (migrations/010_whitelist.sql).
type PgWhitelistRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresWhitelistRepo(pool *pgxpool.Pool) *PgWhitelistRepo {
	return &PgWhitelistRepo{pool: pool}
}

const whitelistColumns = "id, customer_id, status, reason, excluded_rule_ids, valid_from, valid_until, requested_by, approved_by, approved_at, revoked_by, revoked_at, version, created_at, updated_at"

func scanWhitelistEntry(row pgx.Row) (*domain.WhitelistEntry, error) {
	var e domain.WhitelistEntry
	err := row.Scan(
		&e.ID, &e.CustomerID, &e.Status, &e.Reason, &e.ExcludedRuleIDs,
		&e.ValidFrom, &e.ValidUntil, &e.RequestedBy,
		&e.ApprovedBy, &e.ApprovedAt, &e.RevokedBy, &e.RevokedAt,
		&e.Version, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *PgWhitelistRepo) Get(ctx context.Context, id string) (*domain.WhitelistEntry, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+whitelistColumns+` FROM whitelist_entries WHERE id = $1`, id)
	e, err := scanWhitelistEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "whitelist_entry", ID: id}
		}
		return nil, err
	}
	return e, nil
}

func (r *PgWhitelistRepo) GetActiveByCustomer(ctx context.Context, customerID string) (*domain.WhitelistEntry, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+whitelistColumns+` FROM whitelist_entries WHERE customer_id = $1 AND status = 'active'`,
		customerID,
	)
	e, err := scanWhitelistEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "whitelist_entry", ID: customerID}
		}
		return nil, err
	}
	return e, nil
}

func (r *PgWhitelistRepo) List(ctx context.Context, status domain.WhitelistEntryStatus, limit, offset int) ([]domain.WhitelistEntry, error) {
	query := `SELECT ` + whitelistColumns + ` FROM whitelist_entries`
	var args []any
	argN := 1
	if status != "" {
		query += fmt.Sprintf(" WHERE status = $%d", argN)
		args = append(args, status)
		argN++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.WhitelistEntry
	for rows.Next() {
		e, err := scanWhitelistEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *PgWhitelistRepo) ListExpiringSoon(ctx context.Context, withinDays int) ([]domain.WhitelistEntry, error) {
	threshold := time.Now().Add(time.Duration(withinDays) * 24 * time.Hour)
	rows, err := r.pool.Query(ctx,
		`SELECT `+whitelistColumns+` FROM whitelist_entries WHERE status = 'active' AND valid_until < $1 ORDER BY valid_until ASC`,
		threshold,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.WhitelistEntry
	for rows.Next() {
		e, err := scanWhitelistEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *PgWhitelistRepo) Create(ctx context.Context, e *domain.WhitelistEntry) error {
	if e.Version == 0 {
		e.Version = 1
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO whitelist_entries (`+whitelistColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		e.ID, e.CustomerID, string(e.Status), e.Reason, e.ExcludedRuleIDs,
		e.ValidFrom, e.ValidUntil, e.RequestedBy,
		e.ApprovedBy, e.ApprovedAt, e.RevokedBy, e.RevokedAt,
		e.Version, e.CreatedAt, e.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: e.ID, Reason: "an active entry already exists for this customer"}
	}
	return err
}

// UpdateWithVersion enforces optimistic locking via the WHERE version =
// expectedVersion clause (whitelist.md §3.1). A partial unique index
// violation on (customer_id) WHERE status = 'active' is translated to
// domain.ErrConflict (approval-time race between two entries for the same
// customer).
func (r *PgWhitelistRepo) UpdateWithVersion(ctx context.Context, e *domain.WhitelistEntry, expectedVersion int) error {
	e.UpdatedAt = time.Now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE whitelist_entries SET
			status = $3, reason = $4, excluded_rule_ids = $5, valid_from = $6, valid_until = $7,
			approved_by = $8, approved_at = $9, revoked_by = $10, revoked_at = $11,
			version = $12, updated_at = $13
		WHERE id = $1 AND version = $2`,
		e.ID, expectedVersion,
		string(e.Status), e.Reason, e.ExcludedRuleIDs, e.ValidFrom, e.ValidUntil,
		e.ApprovedBy, e.ApprovedAt, e.RevokedBy, e.RevokedAt,
		expectedVersion+1, e.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: e.ID, Reason: "an active entry already exists for this customer"}
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, getErr := r.Get(ctx, e.ID); getErr != nil {
			return &domain.ErrNotFound{Entity: "whitelist_entry", ID: e.ID}
		}
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: e.ID, Reason: "version mismatch"}
	}
	e.Version = expectedVersion + 1
	return nil
}

func (r *PgWhitelistRepo) CreateReview(ctx context.Context, rev *domain.WhitelistReview) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO whitelist_reviews (id, whitelist_entry_id, reviewed_by, decision, review_notes, next_review_date, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rev.ID, rev.WhitelistEntryID, rev.ReviewedBy, string(rev.Decision),
		nullableString(rev.ReviewNotes), rev.NextReviewDate, rev.CreatedAt,
	)
	return err
}

func (r *PgWhitelistRepo) ListReviews(ctx context.Context, entryID string) ([]domain.WhitelistReview, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, whitelist_entry_id, reviewed_by, decision, review_notes, next_review_date, created_at
		FROM whitelist_reviews WHERE whitelist_entry_id = $1 ORDER BY created_at ASC`,
		entryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.WhitelistReview
	for rows.Next() {
		var rev domain.WhitelistReview
		var notes *string
		if err := rows.Scan(&rev.ID, &rev.WhitelistEntryID, &rev.ReviewedBy, &rev.Decision, &notes, &rev.NextReviewDate, &rev.CreatedAt); err != nil {
			return nil, err
		}
		if notes != nil {
			rev.ReviewNotes = *notes
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// CreateReviewAndApply inserts the review and updates the reviewed entry in
// one pgx.Tx (whitelist.md §7.2, Task 5): without a transaction a crash
// between the two writes could record a review whose decision was never
// actually applied to the entry (or vice versa). This is the first
// transaction usage in store/postgres.go; prior repositories only ever
// needed single-statement writes.
func (r *PgWhitelistRepo) CreateReviewAndApply(ctx context.Context, review *domain.WhitelistReview, entry *domain.WhitelistEntry, expectedVersion int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO whitelist_reviews (id, whitelist_entry_id, reviewed_by, decision, review_notes, next_review_date, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		review.ID, review.WhitelistEntryID, review.ReviewedBy, string(review.Decision),
		nullableString(review.ReviewNotes), review.NextReviewDate, review.CreatedAt,
	); err != nil {
		return err
	}

	entry.UpdatedAt = time.Now()
	tag, err := tx.Exec(ctx,
		`UPDATE whitelist_entries SET status = $3, valid_until = $4, version = $5, updated_at = $6
		WHERE id = $1 AND version = $2`,
		entry.ID, expectedVersion,
		string(entry.Status), entry.ValidUntil, expectedVersion+1, entry.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, getErr := r.Get(ctx, entry.ID); getErr != nil {
			return &domain.ErrNotFound{Entity: "whitelist_entry", ID: entry.ID}
		}
		return &domain.ErrConflict{Entity: "whitelist_entry", ID: entry.ID, Reason: "version mismatch"}
	}
	entry.Version = expectedVersion + 1

	return tx.Commit(ctx)
}

// isUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505), the error class the whitelist_entries partial unique
// index raises when a second active entry is created for the same customer.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
