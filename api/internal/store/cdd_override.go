package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

const cddOverrideColumns = `id::text, customer_id::text, COALESCE(score_record_id,''), proposed_tier, computed_tier,
 computed_score, reason, supporting_documents, evidence, status, requested_by, requested_at,
 COALESCE(decided_by,''), decided_at, decision_rationale, version`

func scanCDDOverride(src workflowResultScanner) (*domain.CDDScoreOverride, error) {
	var out domain.CDDScoreOverride
	var evidence []byte
	if err := src.Scan(&out.ID, &out.CustomerID, &out.ScoreRecordID, &out.ProposedTier, &out.ComputedTier,
		&out.ComputedScore, &out.Reason, &out.SupportingDocuments, &evidence, &out.Status, &out.RequestedBy,
		&out.RequestedAt, &out.DecidedBy, &out.DecidedAt, &out.DecisionRationale, &out.Version); err != nil {
		return nil, err
	}
	out.ID = domain.CanonicalUUID(out.ID)
	out.CustomerID = domain.CanonicalUUID(out.CustomerID)
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &out.Evidence)
	}
	return &out, nil
}

func (r *PgCustomerRepo) CreateCDDScoreOverride(ctx context.Context, override *domain.CDDScoreOverride) error {
	if override == nil || override.CustomerID == "" {
		return &domain.ErrConflict{Entity: "cdd_score_override", Reason: "customer_id is required"}
	}
	if override.RequestedAt.IsZero() {
		override.RequestedAt = time.Now().UTC()
	}
	if override.Version == 0 {
		override.Version = 1
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cdd_score_overrides (id, customer_id, score_record_id, proposed_tier, computed_tier,
		 computed_score, reason, supporting_documents, evidence, status, requested_by, requested_at, version)
		 VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		override.ID, domain.CanonicalUUID(override.CustomerID), override.ScoreRecordID,
		string(override.ProposedTier), string(override.ComputedTier), override.ComputedScore,
		override.Reason, nonNilStrings(override.SupportingDocuments), wave3JSON(override.Evidence),
		string(override.Status), override.RequestedBy, override.RequestedAt, override.Version)
	return err
}

func (r *PgCustomerRepo) GetCDDScoreOverride(ctx context.Context, id string) (*domain.CDDScoreOverride, error) {
	override, err := scanCDDOverride(r.pool.QueryRow(ctx, `SELECT `+cddOverrideColumns+` FROM cdd_score_overrides WHERE id=$1 AND purge_marked_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "cdd_score_override", ID: id}
	}
	return override, err
}

func (r *PgCustomerRepo) ListCDDScoreOverrides(ctx context.Context, customerID string, limit int) ([]domain.CDDScoreOverride, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cddOverrideColumns+` FROM cdd_score_overrides WHERE customer_id=$1 AND purge_marked_at IS NULL ORDER BY requested_at DESC, id DESC LIMIT $2`,
		domain.CanonicalUUID(customerID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CDDScoreOverride{}
	for rows.Next() {
		override, err := scanCDDOverride(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *override)
	}
	return out, rows.Err()
}

// DecideCDDScoreOverride settles a proposal under a compare-and-swap, so two
// reviewers acting at once cannot both record a decision.
func (r *PgCustomerRepo) DecideCDDScoreOverride(ctx context.Context, id string, status domain.CDDOverrideStatus, decidedBy, rationale string, expectedVersion int) (*domain.CDDScoreOverride, error) {
	if expectedVersion <= 0 {
		return nil, &domain.ErrConflict{Entity: "cdd_score_override", ID: id, Reason: "expected version is required"}
	}
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx,
		`UPDATE cdd_score_overrides SET status=$2, decided_by=$3, decided_at=$4, decision_rationale=$5, version=version+1
		 WHERE id=$1 AND version=$6 AND status='pending_approval'`,
		id, string(status), decidedBy, now, rationale, expectedVersion)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		existing, getErr := r.GetCDDScoreOverride(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		return nil, &domain.ErrConflict{Entity: "cdd_score_override", ID: id, Reason: "override is already " + string(existing.Status)}
	}
	return r.GetCDDScoreOverride(ctx, id)
}

func (r *MemoryCustomerRepo) CreateCDDScoreOverride(_ context.Context, override *domain.CDDScoreOverride) error {
	if override == nil || override.CustomerID == "" {
		return &domain.ErrConflict{Entity: "cdd_score_override", Reason: "customer_id is required"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.overrides == nil {
		r.overrides = map[string]*domain.CDDScoreOverride{}
	}
	if override.RequestedAt.IsZero() {
		override.RequestedAt = time.Now().UTC()
	}
	if override.Version == 0 {
		override.Version = 1
	}
	stored := *override
	r.overrides[override.ID] = &stored
	return nil
}

func (r *MemoryCustomerRepo) GetCDDScoreOverride(_ context.Context, id string) (*domain.CDDScoreOverride, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	override, ok := r.overrides[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "cdd_score_override", ID: id}
	}
	out := *override
	return &out, nil
}

func (r *MemoryCustomerRepo) ListCDDScoreOverrides(_ context.Context, customerID string, limit int) ([]domain.CDDScoreOverride, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []domain.CDDScoreOverride{}
	for _, override := range r.overrides {
		if domain.SameIdentifier(override.CustomerID, customerID) {
			out = append(out, *override)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].RequestedAt.Equal(out[j].RequestedAt) {
			return out[i].RequestedAt.After(out[j].RequestedAt)
		}
		return out[i].ID > out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryCustomerRepo) DecideCDDScoreOverride(_ context.Context, id string, status domain.CDDOverrideStatus, decidedBy, rationale string, expectedVersion int) (*domain.CDDScoreOverride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	override, ok := r.overrides[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "cdd_score_override", ID: id}
	}
	if expectedVersion <= 0 || override.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "cdd_score_override", ID: id, Reason: "version mismatch"}
	}
	if override.Status != domain.CDDOverridePendingApproval {
		return nil, &domain.ErrConflict{Entity: "cdd_score_override", ID: id, Reason: "override is already " + string(override.Status)}
	}
	now := time.Now().UTC()
	override.Status = status
	override.DecidedBy = decidedBy
	override.DecidedAt = &now
	override.DecisionRationale = rationale
	override.Version++
	out := *override
	return &out, nil
}
