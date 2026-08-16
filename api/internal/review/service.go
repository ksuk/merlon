// Package review owns the durable CDD periodic-review lifecycle. The policy
// package calculates dates; this package persists cycles, assignment and
// evidence and coordinates score/audit/outbox side effects.
package review

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/policy"
)

var (
	ErrNotConfigured = errors.New("CDD review service is not configured")
	ErrInvalid       = errors.New("invalid CDD review request")
)

type Dependencies struct {
	Reviews   domain.CustomerReviewRepository
	Customers domain.CustomerRepository
	Scoring   engine.ScoringEngine
	Audit     domain.AuditRepository
	Outbox    domain.EventOutboxRepository
	Atomic    domain.AtomicMutationRepository
	Policy    *policy.CDDReviewPolicy
	Clock     func() time.Time
	RuleSetID string
}

type Service struct {
	reviews   domain.CustomerReviewRepository
	customers domain.CustomerRepository
	scoring   engine.ScoringEngine
	audit     domain.AuditRepository
	outbox    domain.EventOutboxRepository
	atomic    domain.AtomicMutationRepository
	policy    *policy.CDDReviewPolicy
	clock     func() time.Time
	ruleSetID string
}

func NewService(deps Dependencies) *Service {
	clock := deps.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	policyValue := deps.Policy
	if policyValue == nil {
		policyValue = policy.DefaultCDDReviewPolicy()
	}
	return &Service{reviews: deps.Reviews, customers: deps.Customers, scoring: deps.Scoring,
		audit: deps.Audit, outbox: deps.Outbox, atomic: deps.Atomic, policy: policyValue,
		clock: clock, ruleSetID: deps.RuleSetID}
}

func (s *Service) configured() error {
	if s == nil || s.reviews == nil || s.customers == nil {
		return ErrNotConfigured
	}
	return nil
}

func (s *Service) Policy() *policy.CDDReviewPolicy { return s.policy }

// Repository exposes the durable review store to a transaction composition
// root. It is intentionally read-only at the service boundary; callers still
// use CustomerReviewRepository methods.
func (s *Service) Repository() domain.CustomerReviewRepository {
	if s == nil {
		return nil
	}
	return s.reviews
}

// SetScoring completes late engine wiring in the API composition root. The
// store and policy are available before the native engine is loaded.
func (s *Service) SetScoring(scoring engine.ScoringEngine) {
	if s != nil {
		s.scoring = scoring
	}
}

type SweepResult struct {
	Scanned   int `json:"scanned"`
	Scheduled int `json:"scheduled"`
	Updated   int `json:"updated"`
	Due       int `json:"due"`
	Overdue   int `json:"overdue"`
	Skipped   int `json:"skipped"`
}

// Sweep is safe to run repeatedly. A customer/cycle uniqueness constraint and
// the repository's compare-and-swap update make concurrent schedulers
// converge without duplicate queue rows.
func (s *Service) Sweep(ctx context.Context, asOf time.Time) (SweepResult, error) {
	if err := s.configured(); err != nil {
		return SweepResult{}, err
	}
	if asOf.IsZero() {
		asOf = s.clock()
	}
	result := SweepResult{}
	var cursor *domain.Cursor
	for {
		customers, err := s.customers.ListByCursor(ctx, 200, cursor)
		if err != nil {
			return result, err
		}
		result.Scanned += len(customers)
		for i := range customers {
			if err := s.ensureCustomer(ctx, &customers[i], asOf, &result); err != nil {
				return result, err
			}
		}
		if len(customers) < 200 {
			break
		}
		last := customers[len(customers)-1]
		cursor = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return result, nil
}

func (s *Service) ensureCustomer(ctx context.Context, customer *domain.Customer, asOf time.Time, result *SweepResult) error {
	latest, err := s.reviews.LatestByCustomer(ctx, customer.ID)
	if err != nil {
		var nf *domain.ErrNotFound
		if !errors.As(err, &nf) {
			return err
		}
	}
	if latest != nil && latest.Status != domain.CustomerReviewStatusCompleted {
		before := latest.Status
		updated := *latest
		status := effectiveStatus(updated, asOf)
		if status != before && (before == domain.CustomerReviewStatusScheduled || before == domain.CustomerReviewStatusDue) {
			updated.Status = status
			if status == domain.CustomerReviewStatusOverdue && updated.OverdueAt == nil {
				at := asOf.UTC()
				updated.OverdueAt = &at
			}
			updated.ExpectedVersion = latest.Version
			if err := s.updateReview(ctx, &updated, latest.Version); err != nil {
				return err
			}
			result.Updated++
		}
		if status == domain.CustomerReviewStatusDue {
			result.Due++
		} else if status == domain.CustomerReviewStatusOverdue {
			result.Overdue++
		}
		// The active cycle has no CompletedAt by definition. Preserve the
		// projection of the most recently completed cycle instead of erasing it.
		return s.project(ctx, customer.ID, &updated, customer.LastReviewAt)
	}

	previousTier := domain.RiskTier("")
	cycle := 1
	var completedAt *time.Time
	if latest != nil {
		cycle = latest.Cycle + 1
		previousTier = latest.Tier
		if latest.ResultingTier != "" {
			previousTier = latest.ResultingTier
		}
		completedAt = latest.CompletedAt
	}
	input := policy.CDDReviewInput{CustomerID: customer.ID, Tier: valueOrTier(customer.RiskTier), PreviousTier: previousTier,
		LastCompletedReview: completedAt, LastScoredAt: customer.LastScoredAt, CustomerCreatedAt: customer.CreatedAt, AsOf: asOf}
	schedule := s.policy.Schedule(input)
	status := domain.CustomerReviewStatusScheduled
	if !asOf.Before(schedule.GraceUntil) {
		status = domain.CustomerReviewStatusOverdue
	} else if !asOf.Before(schedule.NextReviewAt) {
		status = domain.CustomerReviewStatusDue
	}
	review := &domain.CustomerReview{ID: newID(), CustomerID: customer.ID, Cycle: cycle, Status: status,
		Tier: schedule.Tier, PreviousTier: previousTier, Priority: priorityForTier(schedule.Tier), DueAt: schedule.NextReviewAt,
		GraceUntil: schedule.GraceUntil, PolicyVersion: schedule.PolicyVersion, PolicyDigest: schedule.PolicyDigest,
		Scope:       map[string]any{"policy_anchor": string(schedule.Anchor), "cold_start_offset_days": schedule.ColdStartOffset},
		ScheduledAt: asOf.UTC(), CreatedAt: asOf.UTC(), UpdatedAt: asOf.UTC(), Version: 1}
	if err := s.reviews.Create(ctx, review); err != nil {
		var conflict *domain.ErrConflict
		if errors.As(err, &conflict) {
			result.Skipped++
			return nil
		}
		return err
	}
	result.Scheduled++
	if status == domain.CustomerReviewStatusDue {
		result.Due++
	} else if status == domain.CustomerReviewStatusOverdue {
		result.Overdue++
	}
	return s.project(ctx, customer.ID, review, completedAt)
}

func valueOrTier(tier *domain.RiskTier) domain.RiskTier {
	if tier == nil {
		return domain.RiskTierHigh
	}
	return *tier
}

func priorityForTier(tier domain.RiskTier) domain.CasePriority {
	switch tier {
	case domain.RiskTierHigh:
		return domain.CasePriorityHigh
	case domain.RiskTierMedium:
		return domain.CasePriorityMedium
	default:
		return domain.CasePriorityLow
	}
}

func effectiveStatus(review domain.CustomerReview, asOf time.Time) domain.CustomerReviewStatus {
	if review.Status != domain.CustomerReviewStatusScheduled && review.Status != domain.CustomerReviewStatusDue {
		return review.Status
	}
	if !asOf.Before(review.GraceUntil) {
		return domain.CustomerReviewStatusOverdue
	}
	if !asOf.Before(review.DueAt) {
		return domain.CustomerReviewStatusDue
	}
	return domain.CustomerReviewStatusScheduled
}

func (s *Service) project(ctx context.Context, customerID string, review *domain.CustomerReview, last *time.Time) error {
	projection, ok := s.reviews.(domain.CustomerReviewProjectionRepository)
	if ok {
		return projection.UpdateReviewProjection(ctx, customerID, &review.DueAt, last, review.Tier, review.PolicyVersion, review.PolicyDigest)
	}
	customer, err := s.customers.Get(ctx, customerID)
	if err != nil {
		return err
	}
	customer.NextReviewAt = &review.DueAt
	customer.LastReviewAt = last
	tier := review.Tier
	customer.ReviewTier = &tier
	customer.ReviewPolicyVersion, customer.ReviewPolicyDigest = review.PolicyVersion, review.PolicyDigest
	return s.customers.Update(ctx, customer)
}

func (s *Service) updateReview(ctx context.Context, review *domain.CustomerReview, expected int64) error {
	if expected > 0 {
		if optimistic, ok := s.reviews.(domain.CustomerReviewOptimisticRepository); ok {
			return optimistic.UpdateIfUnmodified(ctx, review, expected)
		}
	}
	review.ExpectedVersion = expected
	return s.reviews.Update(ctx, review)
}

func (s *Service) Get(ctx context.Context, id string) (*domain.CustomerReview, error) {
	if err := s.configured(); err != nil {
		return nil, err
	}
	return s.reviews.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, filter domain.CustomerReviewFilter) ([]domain.CustomerReview, error) {
	if err := s.configured(); err != nil {
		return nil, err
	}
	return s.reviews.List(ctx, filter)
}

func (s *Service) Assign(ctx context.Context, id, assignedTo, assignedTeam, actor string, expectedVersion int64) (*domain.CustomerReview, error) {
	review, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if expectedVersion > 0 && review.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "customer_review", ID: id, Reason: "version does not match the version read by the client"}
	}
	review.AssignedTo, review.AssignedTeam, review.Actor = strings.TrimSpace(assignedTo), strings.TrimSpace(assignedTeam), actor
	if review.Status == domain.CustomerReviewStatusCompleted {
		return nil, &domain.ErrConflict{Entity: "customer_review", ID: id, Reason: "completed review is immutable"}
	}
	if err := s.updateReview(ctx, review, versionOrExpected(expectedVersion, review.Version)); err != nil {
		return nil, err
	}
	return review, nil
}

func (s *Service) Start(ctx context.Context, id, actor string, expectedVersion int64) (*domain.CustomerReview, error) {
	review, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if expectedVersion > 0 && review.Version != expectedVersion {
		return nil, &domain.ErrConflict{Entity: "customer_review", ID: id, Reason: "version does not match the version read by the client"}
	}
	switch review.Status {
	case domain.CustomerReviewStatusScheduled, domain.CustomerReviewStatusDue, domain.CustomerReviewStatusOverdue, domain.CustomerReviewStatusBlocked, domain.CustomerReviewStatusInProgress:
	default:
		return nil, &domain.ErrInvalidStateTransition{Entity: "customer_review", ID: id, From: string(review.Status), To: string(domain.CustomerReviewStatusInProgress)}
	}
	now := s.clock()
	review.Status, review.Actor = domain.CustomerReviewStatusInProgress, actor
	if review.StartedAt == nil {
		review.StartedAt = &now
	}
	if err := s.updateReview(ctx, review, versionOrExpected(expectedVersion, review.Version)); err != nil {
		return nil, err
	}
	return review, nil
}

func versionOrExpected(expected, current int64) int64 {
	if expected > 0 {
		return expected
	}
	return current
}

func (s *Service) Complete(ctx context.Context, id string, completion domain.CustomerReviewCompletion) (*domain.CustomerReview, error) {
	if err := s.configured(); err != nil {
		return nil, err
	}
	review, err := s.reviews.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if review.Status == domain.CustomerReviewStatusCompleted {
		// Idempotent retry: the same completed evidence is returned without
		// publishing another event.
		return review, nil
	}
	if completion.ExpectedVersion > 0 && completion.ExpectedVersion != review.Version {
		return nil, &domain.ErrConflict{Entity: "customer_review", ID: id, Reason: "version does not match the version read by the client"}
	}
	if !completion.Outcome.Valid() {
		return nil, fmt.Errorf("%w: unsupported outcome", ErrInvalid)
	}
	if err := s.policy.ValidateCompletion(completion.Role, completion.Rationale); err != nil {
		return nil, err
	}
	if len(completion.EvidenceRefs) == 0 {
		return nil, fmt.Errorf("%w: at least one evidence reference is required", ErrInvalid)
	}
	if completion.Scope == nil {
		return nil, fmt.Errorf("%w: scope is required", ErrInvalid)
	}
	if completion.Actor == "" {
		completion.Actor = completion.Role
	}
	customer, err := s.customers.Get(ctx, review.CustomerID)
	if err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	review.Outcome, review.Rationale, review.EvidenceRefs, review.Scope = completion.Outcome, completion.Rationale, append([]string(nil), completion.EvidenceRefs...), completion.Scope
	review.Actor, review.CompletedAt = completion.Actor, &now
	lastScoreID := review.PreviousScoreID
	if lastScoreID == "" {
		// The review row is scheduled from the customer projection, which only
		// carries the score timestamp/tier. Resolve the score-history ID at the
		// completion boundary so unchanged and changed outcomes both retain an
		// explicit predecessor link.
		history, historyErr := s.customers.ListScoreHistory(ctx, customer.ID, 1)
		if historyErr != nil {
			return nil, historyErr
		}
		if len(history) > 0 {
			lastScoreID = history[0].ID
		}
	}
	review.PreviousScoreID = lastScoreID
	var resultingTier = valueOrTier(customer.RiskTier)
	var score *domain.ScoreRecord
	if completion.Outcome == domain.CustomerReviewOutcomeRatingChanged {
		if s.scoring == nil {
			return nil, fmt.Errorf("%w: scoring engine is not configured", ErrNotConfigured)
		}
		ruleSetID := s.ruleSetID
		if strings.TrimSpace(completion.RuleSetID) != "" {
			ruleSetID = completion.RuleSetID
		}
		score, err = s.scoring.ScoreCustomer(ctx, customer, ruleSetID)
		if err != nil {
			return nil, fmt.Errorf("score customer for review: %w", err)
		}
		if score == nil {
			return nil, fmt.Errorf("score customer for review: empty score")
		}
		if strings.TrimSpace(score.ID) == "" {
			score.ID = newID()
		}
		if score.ScoredAt.IsZero() {
			score.ScoredAt = now
		}
		lastScoreID, resultingTier = score.ID, score.Tier
	} else if completion.Outcome == domain.CustomerReviewOutcomeEscalatedToEDD {
		resultingTier = domain.RiskTierHigh
		customer.RiskTier = &resultingTier
		customer.EddRequestedAt = &now
	}
	review.ResultingScoreID = lastScoreID
	review.ResultingTier = resultingTier
	review.Status = domain.CustomerReviewStatusCompleted
	if completion.Outcome == domain.CustomerReviewOutcomeUnableToComplete {
		review.Status = domain.CustomerReviewStatusBlocked
		review.CompletedAt = nil
	}
	if score != nil {
		review.ResultingScoreID = score.ID
	}
	var nextReviewAt *time.Time
	if review.Status == domain.CustomerReviewStatusCompleted {
		next := s.policy.Schedule(policy.CDDReviewInput{CustomerID: customer.ID, Tier: resultingTier, PreviousTier: review.Tier,
			LastCompletedReview: &now, LastScoredAt: customer.LastScoredAt, CustomerCreatedAt: customer.CreatedAt, AsOf: now})
		nextReviewAt = &next.NextReviewAt
		customer.LastReviewAt = &now
		customer.NextReviewAt = nextReviewAt
		customer.ReviewTier = &resultingTier
		customer.ReviewPolicyVersion, customer.ReviewPolicyDigest = review.PolicyVersion, review.PolicyDigest
	}

	payload, _ := json.Marshal(map[string]any{"review_id": review.ID, "customer_id": review.CustomerID, "cycle": review.Cycle, "outcome": review.Outcome, "actor": review.Actor, "resulting_score_id": lastScoreID})
	auditAction := "customer_review.completed"
	outboxTopic := "customer.review.completed"
	if review.Status == domain.CustomerReviewStatusBlocked {
		auditAction = "customer_review.blocked"
		outboxTopic = "customer.review.blocked"
	}
	reviewUpdated := false
	mutate := func(repos domain.AtomicMutationRepositories) error {
		if score != nil {
			if err := repos.Customers.SaveScoreRecord(ctx, score); err != nil {
				return err
			}
			customer.RiskScore, customer.RiskTier, customer.LastScoredAt = &score.Score, &score.Tier, &score.ScoredAt
		}
		if review.Status == domain.CustomerReviewStatusCompleted || completion.Outcome == domain.CustomerReviewOutcomeEscalatedToEDD || score != nil {
			if err := repos.Customers.Update(ctx, customer); err != nil {
				return err
			}
		}
		if repos.Audit == nil || repos.EventOutbox == nil {
			return ErrNotConfigured
		}
		if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: review.Actor, Action: auditAction, ResourceType: "customer_reviews", ResourceID: review.ID, Details: map[string]string{"outcome": string(review.Outcome), "rationale": review.Rationale, "evidence_count": fmt.Sprint(len(review.EvidenceRefs))}, CreatedAt: now}); err != nil {
			return err
		}
		if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: "customer-review:" + review.ID + ":" + fmt.Sprint(review.Version+1), Topic: outboxTopic, Payload: payload, ChainID: review.ID, CreatedAt: now}); err != nil {
			return err
		}
		if repos.CustomerReviews != nil {
			expected := completion.ExpectedVersion
			if expected <= 0 {
				expected = review.Version
			}
			if optimistic, ok := repos.CustomerReviews.(domain.CustomerReviewOptimisticRepository); ok {
				if err := optimistic.UpdateIfUnmodified(ctx, review, expected); err != nil {
					return err
				}
			} else if err := repos.CustomerReviews.Update(ctx, review); err != nil {
				return err
			}
			reviewUpdated = true
		}
		return nil
	}
	if s.atomic != nil {
		if err := s.atomic.RunAtomic(ctx, mutate); err != nil {
			return nil, err
		}
	} else {
		if err := mutate(domain.AtomicMutationRepositories{Customers: s.customers, Audit: s.audit, EventOutbox: s.outbox}); err != nil {
			return nil, err
		}
	}
	if !reviewUpdated {
		if err := s.updateReview(ctx, review, versionOrExpected(completion.ExpectedVersion, review.Version)); err != nil {
			return nil, err
		}
	}
	if review.Status == domain.CustomerReviewStatusBlocked {
		return review, nil
	}
	return review, nil
}

// RunDailySweep runs immediately and then at the requested interval. It is
// intentionally small so deployments can use the same service in a CLI job.
func (s *Service) RunDailySweep(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if _, err := s.Sweep(ctx, s.clock()); err != nil && ctx.Err() == nil {
		slog.Error("CDD review daily sweep failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Sweep(ctx, s.clock()); err != nil && ctx.Err() == nil {
				slog.Error("CDD review daily sweep failed", "error", err)
			}
		}
	}
}

func newID() string {
	// Customer score history uses UUID columns in PostgreSQL. Generate the
	// same compact hexadecimal representation used by the other wave-3
	// services so a score created by a review can be persisted by either the
	// memory or PostgreSQL repository.
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	// crypto/rand should be available in supported deployments; retain a
	// deterministic shape if the OS entropy source is temporarily unavailable.
	return fmt.Sprintf("%032x", time.Now().UnixNano())
}
