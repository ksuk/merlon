package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ksuk/merlon/api/internal/domain"
)

type PgCustomerReviewRepo struct{ pool DBTX }

func NewPgCustomerReviewRepo(pool DBTX) *PgCustomerReviewRepo {
	return &PgCustomerReviewRepo{pool: pool}
}

const customerReviewColumns = `id, customer_id::text, cycle, status, outcome, tier, previous_tier, resulting_tier,
 assigned_to, assigned_team, priority, due_at, grace_until, overdue_at, policy_version, policy_digest,
 scope, rationale, evidence_refs, previous_score_id, resulting_score_id, actor, scheduled_at, started_at,
 completed_at, created_at, updated_at, version`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCustomerReview(row rowScanner) (*domain.CustomerReview, error) {
	var review domain.CustomerReview
	var outcome, tier, previousTier, resultingTier *string
	var scope, evidence []byte
	if err := row.Scan(
		&review.ID, &review.CustomerID, &review.Cycle, &review.Status, &outcome,
		&tier, &previousTier, &resultingTier, &review.AssignedTo, &review.AssignedTeam,
		&review.Priority, &review.DueAt, &review.GraceUntil, &review.OverdueAt,
		&review.PolicyVersion, &review.PolicyDigest, &scope, &review.Rationale, &evidence,
		&review.PreviousScoreID, &review.ResultingScoreID, &review.Actor, &review.ScheduledAt,
		&review.StartedAt, &review.CompletedAt, &review.CreatedAt, &review.UpdatedAt, &review.Version,
	); err != nil {
		return nil, err
	}
	review.CustomerID = domain.CanonicalIdentifier(review.CustomerID)
	if tier != nil {
		review.Tier = domain.RiskTier(*tier)
	}
	if previousTier != nil {
		review.PreviousTier = domain.RiskTier(*previousTier)
	}
	if resultingTier != nil {
		review.ResultingTier = domain.RiskTier(*resultingTier)
	}
	if outcome != nil {
		review.Outcome = domain.CustomerReviewOutcome(*outcome)
	}
	if len(scope) > 0 {
		if err := json.Unmarshal(scope, &review.Scope); err != nil {
			return nil, fmt.Errorf("decode review scope: %w", err)
		}
	}
	if len(evidence) > 0 {
		if err := json.Unmarshal(evidence, &review.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode review evidence_refs: %w", err)
		}
	}
	return &review, nil
}

func (r *PgCustomerReviewRepo) Get(ctx context.Context, id string) (*domain.CustomerReview, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+customerReviewColumns+` FROM customer_reviews WHERE id=$1`, id)
	review, err := scanCustomerReview(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "customer_review", ID: id}
	}
	return review, err
}

func (r *PgCustomerReviewRepo) GetByCustomerCycle(ctx context.Context, customerID string, cycle int) (*domain.CustomerReview, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+customerReviewColumns+` FROM customer_reviews WHERE customer_id=$1 AND cycle=$2`, domain.CanonicalIdentifier(customerID), cycle)
	review, err := scanCustomerReview(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "customer_review", ID: customerID}
	}
	return review, err
}

func (r *PgCustomerReviewRepo) LatestByCustomer(ctx context.Context, customerID string) (*domain.CustomerReview, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+customerReviewColumns+` FROM customer_reviews WHERE customer_id=$1 ORDER BY cycle DESC LIMIT 1`, domain.CanonicalIdentifier(customerID))
	review, err := scanCustomerReview(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "customer_review", ID: customerID}
	}
	return review, err
}

func reviewStatusValues(filter domain.CustomerReviewFilter) []domain.CustomerReviewStatus {
	if len(filter.Statuses) > 0 {
		return filter.Statuses
	}
	if filter.Status != "" {
		return []domain.CustomerReviewStatus{filter.Status}
	}
	return nil
}

func (r *PgCustomerReviewRepo) List(ctx context.Context, filter domain.CustomerReviewFilter) ([]domain.CustomerReview, error) {
	query := `SELECT ` + customerReviewColumns + ` FROM customer_reviews`
	conditions := []string{"1=1"}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filter.CustomerID != "" {
		add("customer_id=$%d", domain.CanonicalIdentifier(filter.CustomerID))
	}
	if filter.Tier != "" {
		add("tier=$%d", filter.Tier)
	}
	if filter.AssignedTo != "" {
		add("assigned_to=$%d", filter.AssignedTo)
	}
	if filter.AssignedTeam != "" {
		add("assigned_team=$%d", filter.AssignedTeam)
	}
	statuses := reviewStatusValues(filter)
	if len(statuses) > 0 {
		asOf := filter.AsOf
		if asOf.IsZero() {
			asOf = time.Now().UTC()
		}
		statusConditions := make([]string, 0, len(statuses))
		for _, status := range statuses {
			switch status {
			case domain.CustomerReviewStatusDue:
				args = append(args, status, domain.CustomerReviewStatusScheduled, asOf, asOf)
				statusConditions = append(statusConditions, fmt.Sprintf("(status=$%d OR (status=$%d AND due_at <= $%d AND grace_until > $%d))", len(args)-3, len(args)-2, len(args)-1, len(args)))
			case domain.CustomerReviewStatusOverdue:
				args = append(args, status, domain.CustomerReviewStatusScheduled, domain.CustomerReviewStatusDue, asOf)
				statusConditions = append(statusConditions, fmt.Sprintf("(status=$%d OR ((status=$%d OR status=$%d) AND grace_until <= $%d))", len(args)-3, len(args)-2, len(args)-1, len(args)))
			default:
				args = append(args, status)
				statusConditions = append(statusConditions, fmt.Sprintf("status=$%d", len(args)))
			}
		}
		conditions = append(conditions, "("+strings.Join(statusConditions, " OR ")+")")
	}
	if filter.DueBefore != nil {
		add("due_at <= $%d", *filter.DueBefore)
	}
	if filter.DueAfter != nil {
		add("due_at >= $%d", *filter.DueAfter)
	}
	if filter.Cursor != nil {
		args = append(args, filter.Cursor.CreatedAt, filter.Cursor.ID)
		conditions = append(conditions, fmt.Sprintf("(due_at,id) > ($%d,$%d)", len(args)-1, len(args)))
	}
	query += " WHERE " + strings.Join(conditions, " AND ") + " ORDER BY due_at ASC, id ASC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CustomerReview{}
	for rows.Next() {
		review, err := scanCustomerReview(rows)
		if err != nil {
			return nil, err
		}
		if filter.AsOf.IsZero() {
			filter.AsOf = time.Now().UTC()
		}
		if review.Status == domain.CustomerReviewStatusScheduled || review.Status == domain.CustomerReviewStatusDue {
			if !filter.AsOf.Before(review.GraceUntil) {
				review.Status = domain.CustomerReviewStatusOverdue
			} else if !filter.AsOf.Before(review.DueAt) {
				review.Status = domain.CustomerReviewStatusDue
			}
		}
		out = append(out, *review)
	}
	return out, rows.Err()
}

func (r *PgCustomerReviewRepo) Create(ctx context.Context, review *domain.CustomerReview) error {
	if review.ID == "" {
		review.ID = domain.CanonicalIdentifier(newReviewID())
	}
	if review.Version <= 0 {
		review.Version = 1
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = time.Now().UTC()
	}
	if review.UpdatedAt.IsZero() {
		review.UpdatedAt = review.CreatedAt
	}
	scope, _ := json.Marshal(review.Scope)
	evidence, _ := json.Marshal(review.EvidenceRefs)
	_, err := r.pool.Exec(ctx, `INSERT INTO customer_reviews
		(id,customer_id,cycle,status,outcome,tier,previous_tier,resulting_tier,assigned_to,assigned_team,priority,due_at,grace_until,overdue_at,policy_version,policy_digest,scope,rationale,evidence_refs,previous_score_id,resulting_score_id,actor,scheduled_at,started_at,completed_at,created_at,updated_at,version)
		VALUES($1,$2,$3,$4,NULLIF($5,''),$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)`,
		review.ID, domain.CanonicalIdentifier(review.CustomerID), review.Cycle, review.Status, review.Outcome,
		review.Tier, review.PreviousTier, review.ResultingTier, review.AssignedTo, review.AssignedTeam,
		review.Priority, review.DueAt, review.GraceUntil, review.OverdueAt, review.PolicyVersion, review.PolicyDigest,
		scope, review.Rationale, evidence, review.PreviousScoreID, review.ResultingScoreID, review.Actor,
		review.ScheduledAt, review.StartedAt, review.CompletedAt, review.CreatedAt, review.UpdatedAt, review.Version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return &domain.ErrConflict{Entity: "customer_review", ID: review.CustomerID, Reason: "customer cycle already exists"}
		}
		return err
	}
	return nil
}

func (r *PgCustomerReviewRepo) Update(ctx context.Context, review *domain.CustomerReview) error {
	if review.ExpectedVersion > 0 {
		return r.UpdateIfUnmodified(ctx, review, review.ExpectedVersion)
	}
	return r.update(ctx, review, 0)
}

func (r *PgCustomerReviewRepo) UpdateIfUnmodified(ctx context.Context, review *domain.CustomerReview, expectedVersion int64) error {
	return r.update(ctx, review, expectedVersion)
}

func (r *PgCustomerReviewRepo) update(ctx context.Context, review *domain.CustomerReview, expectedVersion int64) error {
	scope, _ := json.Marshal(review.Scope)
	evidence, _ := json.Marshal(review.EvidenceRefs)
	newVersion := review.Version
	if newVersion <= 0 {
		newVersion = expectedVersion + 1
		if newVersion <= 0 {
			newVersion = 1
		}
	}
	query := `UPDATE customer_reviews SET status=$2,outcome=NULLIF($3,''),tier=$4,previous_tier=NULLIF($5,''),resulting_tier=NULLIF($6,''),assigned_to=$7,assigned_team=$8,priority=$9,due_at=$10,grace_until=$11,overdue_at=$12,policy_version=$13,policy_digest=$14,scope=$15,rationale=$16,evidence_refs=$17,previous_score_id=$18,resulting_score_id=$19,actor=$20,scheduled_at=$21,started_at=$22,completed_at=$23,updated_at=now(),version=$24 WHERE id=$1`
	args := []any{review.ID, review.Status, review.Outcome, review.Tier, review.PreviousTier, review.ResultingTier, review.AssignedTo, review.AssignedTeam, review.Priority, review.DueAt, review.GraceUntil, review.OverdueAt, review.PolicyVersion, review.PolicyDigest, scope, review.Rationale, evidence, review.PreviousScoreID, review.ResultingScoreID, review.Actor, review.ScheduledAt, review.StartedAt, review.CompletedAt, newVersion}
	if expectedVersion > 0 {
		query += " AND version=$25"
		args = append(args, expectedVersion)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		current, getErr := r.Get(ctx, review.ID)
		if getErr != nil {
			return getErr
		}
		if expectedVersion > 0 && current.Version != expectedVersion {
			return &domain.ErrConflict{Entity: "customer_review", ID: review.ID, Reason: "version does not match the version read by the client"}
		}
		return &domain.ErrNotFound{Entity: "customer_review", ID: review.ID}
	}
	review.Version = newVersion
	review.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *PgCustomerReviewRepo) UpdateReviewProjection(ctx context.Context, customerID string, next, last *time.Time, tier domain.RiskTier, policyVersion, policyDigest string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE customers SET next_review_at=$2,last_review_at=$3,review_tier=NULLIF($4,''),review_policy_version=$5,review_policy_digest=$6,updated_at=now() WHERE id=$1`, domain.CanonicalIdentifier(customerID), next, last, tier, policyVersion, policyDigest)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &domain.ErrNotFound{Entity: "customer", ID: customerID}
	}
	return nil
}

func newReviewID() string { return wave3ID() }

var _ domain.CustomerReviewRepository = (*PgCustomerReviewRepo)(nil)
var _ domain.CustomerReviewOptimisticRepository = (*PgCustomerReviewRepo)(nil)
var _ domain.CustomerReviewProjectionRepository = (*PgCustomerReviewRepo)(nil)
