package domain

import (
	"context"
	"time"
)

// CustomerReviewStatus is the durable lifecycle of one periodic CDD review.
// A review is never deleted: completed and blocked rows are retained as
// evidence and a later cycle receives a new cycle number.
type CustomerReviewStatus string

const (
	CustomerReviewStatusScheduled  CustomerReviewStatus = "scheduled"
	CustomerReviewStatusDue        CustomerReviewStatus = "due"
	CustomerReviewStatusOverdue    CustomerReviewStatus = "overdue"
	CustomerReviewStatusInProgress CustomerReviewStatus = "in_progress"
	CustomerReviewStatusBlocked    CustomerReviewStatus = "blocked"
	CustomerReviewStatusCompleted  CustomerReviewStatus = "completed"
)

func (s CustomerReviewStatus) Valid() bool {
	switch s {
	case CustomerReviewStatusScheduled, CustomerReviewStatusDue,
		CustomerReviewStatusOverdue, CustomerReviewStatusInProgress,
		CustomerReviewStatusBlocked, CustomerReviewStatusCompleted:
		return true
	default:
		return false
	}
}

type CustomerReviewOutcome string

const (
	CustomerReviewOutcomeRatingUnchanged  CustomerReviewOutcome = "rating_unchanged"
	CustomerReviewOutcomeRatingChanged    CustomerReviewOutcome = "rating_changed"
	CustomerReviewOutcomeEscalatedToEDD   CustomerReviewOutcome = "escalated_to_edd"
	CustomerReviewOutcomeUnableToComplete CustomerReviewOutcome = "unable_to_complete"
)

func (o CustomerReviewOutcome) Valid() bool {
	switch o {
	case CustomerReviewOutcomeRatingUnchanged, CustomerReviewOutcomeRatingChanged,
		CustomerReviewOutcomeEscalatedToEDD, CustomerReviewOutcomeUnableToComplete:
		return true
	default:
		return false
	}
}

// CustomerReview is the append-only evidence record plus the mutable
// assignment/status projection for the current cycle. Updates increment
// Version; callers provide ExpectedVersion to make operator actions
// compare-and-swap operations.
type CustomerReview struct {
	ID               string                `json:"id"`
	CustomerID       string                `json:"customer_id"`
	Cycle            int                   `json:"cycle"`
	Status           CustomerReviewStatus  `json:"status"`
	Outcome          CustomerReviewOutcome `json:"outcome,omitempty"`
	Tier             RiskTier              `json:"tier"`
	PreviousTier     RiskTier              `json:"previous_tier,omitempty"`
	ResultingTier    RiskTier              `json:"resulting_tier,omitempty"`
	AssignedTo       string                `json:"assigned_to,omitempty"`
	AssignedTeam     string                `json:"assigned_team,omitempty"`
	Priority         CasePriority          `json:"priority,omitempty"`
	DueAt            time.Time             `json:"due_at"`
	GraceUntil       time.Time             `json:"grace_until"`
	OverdueAt        *time.Time            `json:"overdue_at,omitempty"`
	PolicyVersion    string                `json:"policy_version"`
	PolicyDigest     string                `json:"policy_digest"`
	Scope            map[string]any        `json:"scope,omitempty"`
	Rationale        string                `json:"rationale,omitempty"`
	EvidenceRefs     []string              `json:"evidence_refs,omitempty"`
	PreviousScoreID  string                `json:"previous_score_id,omitempty"`
	ResultingScoreID string                `json:"resulting_score_id,omitempty"`
	Actor            string                `json:"actor,omitempty"`
	ScheduledAt      time.Time             `json:"scheduled_at"`
	StartedAt        *time.Time            `json:"started_at,omitempty"`
	CompletedAt      *time.Time            `json:"completed_at,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
	Version          int64                 `json:"version"`
	ExpectedVersion  int64                 `json:"expected_version,omitempty"`
}

// IsDueAt keeps status transitions deterministic and testable. A scheduled
// review is due at DueAt and overdue after the policy grace period.
func (r CustomerReview) IsDueAt(now time.Time) bool {
	return !now.Before(r.DueAt)
}

func (r CustomerReview) IsOverdueAt(now time.Time) bool {
	return !now.Before(r.GraceUntil)
}

type CustomerReviewFilter struct {
	CustomerID   string
	Status       CustomerReviewStatus
	Statuses     []CustomerReviewStatus
	Tier         RiskTier
	AssignedTo   string
	AssignedTeam string
	DueBefore    *time.Time
	DueAfter     *time.Time
	AsOf         time.Time
	Cursor       *Cursor
	Limit        int
}

// CustomerReviewCompletion is the write payload consumed by the service.
// ResultingScoreID is intentionally not caller-controlled for score-changing
// outcomes; the service creates and links the score history record.
type CustomerReviewCompletion struct {
	Outcome         CustomerReviewOutcome `json:"outcome"`
	Rationale       string                `json:"rationale"`
	EvidenceRefs    []string              `json:"evidence_refs"`
	Scope           map[string]any        `json:"scope"`
	ExpectedVersion int64                 `json:"expected_version"`
	Actor           string                `json:"actor"`
	Role            string                `json:"role"`
	RuleSetID       string                `json:"rule_set_id,omitempty"`
}

// CustomerReviewRepository is deliberately separate from CustomerRepository:
// reviews have their own lifecycle, pagination and uniqueness constraint.
type CustomerReviewRepository interface {
	Get(ctx context.Context, id string) (*CustomerReview, error)
	List(ctx context.Context, filter CustomerReviewFilter) ([]CustomerReview, error)
	Create(ctx context.Context, review *CustomerReview) error
	Update(ctx context.Context, review *CustomerReview) error
	LatestByCustomer(ctx context.Context, customerID string) (*CustomerReview, error)
	GetByCustomerCycle(ctx context.Context, customerID string, cycle int) (*CustomerReview, error)
}

// CustomerReviewOptimisticRepository is an additive capability for stores
// that can enforce the expected version in one SQL/memory compare-and-swap.
// Legacy adapters may implement only CustomerReviewRepository; the service
// still validates the version before calling Update.
type CustomerReviewOptimisticRepository interface {
	UpdateIfUnmodified(ctx context.Context, review *CustomerReview, expectedVersion int64) error
}

// CustomerReviewProjectionRepository lets the scheduler update the compact
// customer projection without widening the stable CustomerRepository API.
type CustomerReviewProjectionRepository interface {
	UpdateReviewProjection(ctx context.Context, customerID string, next, last *time.Time, tier RiskTier, policyVersion, policyDigest string) error
}
