package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/store"
)

func reviewCustomer(t *testing.T, repo domain.CustomerRepository, now time.Time) *domain.Customer {
	t.Helper()
	c := &domain.Customer{ID: "customer-1", ExternalID: "ext-1", CustomerType: domain.CustomerTypeIndividual,
		Status: domain.CustomerStatusActive, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour), Attributes: map[string]any{}}
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSweepIsIdempotentAndProjectsCustomer(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	c := reviewCustomer(t, customers, now)
	p := policy.DefaultCDDReviewPolicy()
	p.Intervals[domain.RiskTierHigh] = 1
	p.GraceDays = 1
	s := NewService(Dependencies{Reviews: reviews, Customers: customers, Policy: p, Clock: func() time.Time { return now }})
	first, err := s.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scheduled != 1 {
		t.Fatalf("scheduled = %d, want 1", first.Scheduled)
	}
	second, err := s.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if second.Scheduled != 0 {
		t.Fatalf("second scheduled = %d, want 0", second.Scheduled)
	}
	latest, err := reviews.LatestByCustomer(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextReviewAt == nil || !got.NextReviewAt.Equal(latest.DueAt) {
		t.Fatalf("next review projection = %v, want %v", got.NextReviewAt, latest.DueAt)
	}
}

func TestSweepMarksDueAndOverdueWithoutHidingReview(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	reviewCustomer(t, customers, now.Add(-365*24*time.Hour))
	p := policy.DefaultCDDReviewPolicy()
	p.Intervals[domain.RiskTierHigh] = 1
	p.GraceDays = 1
	s := NewService(Dependencies{Reviews: reviews, Customers: customers, Policy: p, Clock: func() time.Time { return now }})
	if _, err := s.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(context.Background(), domain.CustomerReviewFilter{Status: domain.CustomerReviewStatusOverdue, AsOf: now.Add(3 * 24 * time.Hour), Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != domain.CustomerReviewStatusOverdue {
		t.Fatalf("overdue queue = %#v", items)
	}
}

func TestCompleteRatingChangedWritesEvidenceAndLinksScore(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	audit := store.NewMemoryAuditRepo()
	outbox := store.NewMemoryEventOutboxRepo()
	c := reviewCustomer(t, customers, now)
	p := policy.DefaultCDDReviewPolicy()
	s := NewService(Dependencies{Reviews: reviews, Customers: customers, Scoring: &engine.MockScoringEngine{Score: 88, Tier: domain.RiskTierHigh}, Audit: audit, Outbox: outbox, Policy: p, Clock: func() time.Time { return now }})
	if _, err := s.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	item, _ := reviews.LatestByCustomer(context.Background(), c.ID)
	completed, err := s.Complete(context.Background(), item.ID, domain.CustomerReviewCompletion{Outcome: domain.CustomerReviewOutcomeRatingChanged, Rationale: "income evidence changed", EvidenceRefs: []string{"doc:income-1"}, Scope: map[string]any{"fields": []any{"income"}}, ExpectedVersion: item.Version, Actor: "analyst-1", Role: "analyst"})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.CustomerReviewStatusCompleted || completed.ResultingScoreID == "" {
		t.Fatalf("completed review = %#v", completed)
	}
	history, err := customers.ListScoreHistory(context.Background(), c.ID, 10)
	if err != nil || len(history) != 1 || history[0].ID != completed.ResultingScoreID {
		t.Fatalf("score history = %#v, err=%v", history, err)
	}
	pending, err := outbox.ListPending(context.Background(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("outbox = %#v, err=%v", pending, err)
	}
}

func TestCompleteFailureDoesNotMarkReviewCompleted(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	audit := store.NewMemoryAuditRepo()
	audit.SetCreateFailure(errors.New("audit unavailable"))
	c := reviewCustomer(t, customers, now)
	s := NewService(Dependencies{Reviews: reviews, Customers: customers, Scoring: &engine.MockScoringEngine{Score: 88, Tier: domain.RiskTierHigh}, Audit: audit, Outbox: store.NewMemoryEventOutboxRepo(), Clock: func() time.Time { return now }})
	if _, err := s.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	item, _ := reviews.LatestByCustomer(context.Background(), c.ID)
	if _, err := s.Complete(context.Background(), item.ID, domain.CustomerReviewCompletion{Outcome: domain.CustomerReviewOutcomeRatingUnchanged, Rationale: "checked", EvidenceRefs: []string{"doc:1"}, Scope: map[string]any{"fields": []any{"all"}}, ExpectedVersion: item.Version, Actor: "analyst-1", Role: "analyst"}); err == nil {
		t.Fatal("expected audit failure")
	}
	after, _ := reviews.Get(context.Background(), item.ID)
	if after.Status == domain.CustomerReviewStatusCompleted {
		t.Fatalf("review marked completed after audit failure: %#v", after)
	}
}

func TestUnableToCompleteBlocksWithoutAdvancingProjection(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	audit := store.NewMemoryAuditRepo()
	outbox := store.NewMemoryEventOutboxRepo()
	c := reviewCustomer(t, customers, now)
	p := policy.DefaultCDDReviewPolicy()
	s := NewService(Dependencies{Reviews: reviews, Customers: customers, Audit: audit, Outbox: outbox, Policy: p, Clock: func() time.Time { return now }})
	if _, err := s.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	item, _ := reviews.LatestByCustomer(context.Background(), c.ID)
	before, err := customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := s.Complete(context.Background(), item.ID, domain.CustomerReviewCompletion{
		Outcome:         domain.CustomerReviewOutcomeUnableToComplete,
		Rationale:       "customer unreachable",
		EvidenceRefs:    []string{"case-note:1"},
		Scope:           map[string]any{"contact_attempts": 3},
		ExpectedVersion: item.Version,
		Actor:           "analyst-1",
		Role:            "analyst",
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != domain.CustomerReviewStatusBlocked || blocked.CompletedAt != nil {
		t.Fatalf("blocked review = %#v", blocked)
	}
	after, err := customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameReviewProjection(before, after) {
		t.Fatalf("blocked review advanced projection: before=%#v after=%#v", before, after)
	}
	entries, err := audit.List(context.Background(), domain.AuditListFilter{ResourceType: "customer_reviews", Limit: 10})
	if err != nil || len(entries) != 1 || entries[0].Action != "customer_review.blocked" {
		t.Fatalf("blocked audit entries = %#v, err=%v", entries, err)
	}
	pending, err := outbox.ListPending(context.Background(), 10)
	if err != nil || len(pending) != 1 || pending[0].Topic != "customer.review.blocked" {
		t.Fatalf("blocked outbox = %#v, err=%v", pending, err)
	}
}

func TestSweepKeepsLastCompletedReviewWhenNextCycleIsActive(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	c := reviewCustomer(t, customers, now)
	s := NewService(Dependencies{
		Reviews: reviews, Customers: customers,
		Audit: store.NewMemoryAuditRepo(), Outbox: store.NewMemoryEventOutboxRepo(),
		Clock: func() time.Time { return now },
	})
	if _, err := s.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	first, _ := reviews.LatestByCustomer(context.Background(), c.ID)
	if _, err := s.Complete(context.Background(), first.ID, domain.CustomerReviewCompletion{
		Outcome: domain.CustomerReviewOutcomeRatingUnchanged, Rationale: "checked",
		EvidenceRefs: []string{"doc:1"}, Scope: map[string]any{"fields": []any{"all"}},
		ExpectedVersion: first.Version, Actor: "analyst", Role: "analyst",
	}); err != nil {
		t.Fatal(err)
	}
	completedProjection, _ := customers.Get(context.Background(), c.ID)
	if completedProjection.LastReviewAt == nil {
		t.Fatal("completion did not set last_review_at")
	}
	if _, err := s.Sweep(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sweep(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, _ := customers.Get(context.Background(), c.ID)
	if after.LastReviewAt == nil || !after.LastReviewAt.Equal(*completedProjection.LastReviewAt) {
		t.Fatalf("last_review_at = %v, want %v", after.LastReviewAt, completedProjection.LastReviewAt)
	}
}

func sameReviewProjection(left, right *domain.Customer) bool {
	if left == nil || right == nil {
		return left == right
	}
	if !timePtrEqual(left.NextReviewAt, right.NextReviewAt) || !timePtrEqual(left.LastReviewAt, right.LastReviewAt) {
		return false
	}
	if left.ReviewPolicyVersion != right.ReviewPolicyVersion || left.ReviewPolicyDigest != right.ReviewPolicyDigest {
		return false
	}
	if left.ReviewTier == nil || right.ReviewTier == nil {
		return left.ReviewTier == right.ReviewTier
	}
	return *left.ReviewTier == *right.ReviewTier
}

func timePtrEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

func TestCompleteAtomicRollsBackScoreWhenOutboxFails(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	customers := store.NewMemoryCustomerRepo()
	reviews := store.NewMemoryCustomerReviewRepo()
	audit := store.NewMemoryAuditRepo()
	outbox := store.NewMemoryEventOutboxRepo()
	outbox.SetEnqueueFailure(errors.New("outbox unavailable"))
	c := reviewCustomer(t, customers, now)
	atomic, err := store.NewMemoryAtomicMutationRepo(domain.AtomicMutationRepositories{Customers: customers, Audit: audit, EventOutbox: outbox, CustomerReviews: reviews})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(Dependencies{Reviews: reviews, Customers: customers, Scoring: &engine.MockScoringEngine{Score: 90, Tier: domain.RiskTierHigh}, Audit: audit, Outbox: outbox, Atomic: atomic, Clock: func() time.Time { return now }})
	if _, err := s.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	item, _ := reviews.LatestByCustomer(context.Background(), c.ID)
	if _, err := s.Complete(context.Background(), item.ID, domain.CustomerReviewCompletion{Outcome: domain.CustomerReviewOutcomeRatingChanged, Rationale: "changed", EvidenceRefs: []string{"doc:1"}, Scope: map[string]any{"fields": []any{"risk"}}, ExpectedVersion: item.Version, Actor: "analyst", Role: "analyst"}); err == nil {
		t.Fatal("expected outbox failure")
	}
	after, _ := reviews.Get(context.Background(), item.ID)
	if after.Status == domain.CustomerReviewStatusCompleted {
		t.Fatal("review completed after rollback")
	}
	history, _ := customers.ListScoreHistory(context.Background(), c.ID, 10)
	if len(history) != 0 {
		t.Fatalf("score history after rollback = %#v", history)
	}
}
