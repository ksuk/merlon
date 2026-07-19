package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgScreeningResultRepo implements domain.ScreeningResultRepository against
// screening_results (migrations/011_screening_results.sql).
type PgScreeningResultRepo struct {
	pool DBTX
}

func NewPgScreeningResultRepo(pool DBTX) *PgScreeningResultRepo {
	return &PgScreeningResultRepo{pool: pool}
}

const screeningResultColumns = "id, customer_id, list_id, list_type, entry_id, matched_name, similarity, status, false_positive_reason, reviewed_by, reviewed_at, screened_at, created_at"

func scanScreeningResult(row pgx.Row) (*domain.ScreeningResultRecord, error) {
	var sr domain.ScreeningResultRecord
	var falsePositiveReason, reviewedBy *string

	err := row.Scan(
		&sr.ID, &sr.CustomerID, &sr.ListID, &sr.ListType, &sr.EntryID, &sr.MatchedName,
		&sr.Similarity, &sr.Status, &falsePositiveReason, &reviewedBy, &sr.ReviewedAt,
		&sr.ScreenedAt, &sr.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if falsePositiveReason != nil {
		sr.FalsePositiveReason = *falsePositiveReason
	}
	if reviewedBy != nil {
		sr.ReviewedBy = *reviewedBy
	}
	return &sr, nil
}

func (r *PgScreeningResultRepo) Get(ctx context.Context, id string) (*domain.ScreeningResultRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+screeningResultColumns+` FROM screening_results WHERE id = $1 AND purge_marked_at IS NULL`,
		id,
	)
	sr, err := scanScreeningResult(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "screening_result", ID: id}
		}
		return nil, err
	}
	return sr, nil
}

func (r *PgScreeningResultRepo) Create(ctx context.Context, sr *domain.ScreeningResultRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO screening_results (id, customer_id, list_id, list_type, entry_id, matched_name, similarity, status, false_positive_reason, reviewed_by, reviewed_at, screened_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		sr.ID, sr.CustomerID, sr.ListID, sr.ListType, sr.EntryID, sr.MatchedName, sr.Similarity,
		sr.Status, nullableString(sr.FalsePositiveReason), nullableString(sr.ReviewedBy), sr.ReviewedAt,
		sr.ScreenedAt, sr.CreatedAt,
	)
	return err
}

func (r *PgScreeningResultRepo) Update(ctx context.Context, sr *domain.ScreeningResultRecord) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE screening_results SET status = $2, false_positive_reason = $3, reviewed_by = $4, reviewed_at = $5
		WHERE id = $1`,
		sr.ID, sr.Status, nullableString(sr.FalsePositiveReason), nullableString(sr.ReviewedBy), sr.ReviewedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "screening_result", ID: sr.ID}
	}
	return nil
}

func (r *PgScreeningResultRepo) listScreeningResults(ctx context.Context, query string, args ...any) ([]domain.ScreeningResultRecord, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ScreeningResultRecord
	for rows.Next() {
		sr, err := scanScreeningResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sr)
	}
	return out, rows.Err()
}

func (r *PgScreeningResultRepo) ListByCustomer(ctx context.Context, customerID string, limit, offset int) ([]domain.ScreeningResultRecord, error) {
	return r.listScreeningResults(ctx,
		`SELECT `+screeningResultColumns+` FROM screening_results WHERE customer_id = $1 AND purge_marked_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		customerID, limit, offset,
	)
}

func (r *PgScreeningResultRepo) ListByStatus(ctx context.Context, status domain.ScreeningResultStatus, limit, offset int) ([]domain.ScreeningResultRecord, error) {
	return r.listScreeningResults(ctx,
		`SELECT `+screeningResultColumns+` FROM screening_results WHERE status = $1 AND purge_marked_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		status, limit, offset,
	)
}

// ListPastFalsePositives lets a reviewer see prior False Positive
// determinations against the same list entry (the screening workflow "同一リストエントリへの
// 再ヒット時に過去の False Positive 判定を参照可能とする"), regardless of which
// customer triggered the earlier hit.
func (r *PgScreeningResultRepo) ListPastFalsePositives(ctx context.Context, entryID string) ([]domain.ScreeningResultRecord, error) {
	return r.listScreeningResults(ctx,
		`SELECT `+screeningResultColumns+` FROM screening_results WHERE entry_id = $1 AND status = $2 AND purge_marked_at IS NULL ORDER BY created_at DESC, id DESC`,
		entryID, domain.ScreeningResultStatusFalsePositive,
	)
}
