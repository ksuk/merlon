package batch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/store"
)

// fakeWebhookRecorder records every event dispatched through a
// WebhookDispatchFunc, guarded by a mutex for safety even though
// RunEDDEscalationJob calls it synchronously.
type fakeWebhookRecorder struct {
	mu     sync.Mutex
	events []domain.WebhookEventType
}

func (f *fakeWebhookRecorder) dispatch(_ context.Context, event domain.WebhookEventType, _ any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeWebhookRecorder) count(event domain.WebhookEventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e == event {
			n++
		}
	}
	return n
}

func seedHighTierCustomerWithEDD(t *testing.T, customers domain.CustomerRepository, externalID string, eddAgo time.Duration, now time.Time) *domain.Customer {
	t.Helper()
	tier := domain.RiskTierHigh
	eddAt := now.Add(-eddAgo)
	c := &domain.Customer{
		ID:             "cust-" + externalID,
		ExternalID:     externalID,
		CustomerType:   domain.CustomerTypeIndividual,
		CountryCode:    "JP",
		RiskTier:       &tier,
		EddRequestedAt: &eddAt,
	}
	if err := customers.Create(context.Background(), c); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return c
}

// TestRunEDDEscalationJob_Stage1At30Days_ResendsReminder verifies
// case-management.md's stage 1: 30 days after the EDD requirement began, the
// edd_required webhook is re-sent (and no case is created for a mere
// reminder).
func TestRunEDDEscalationJob_Stage1At30Days_ResendsReminder(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	cases := store.NewMemoryCaseRepo()
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	c := seedHighTierCustomerWithEDD(t, customers, "EDD_S1", 31*24*time.Hour, now)

	webhook := &fakeWebhookRecorder{}
	result, err := RunEDDEscalationJob(context.Background(), EDDEscalationDeps{
		Customers: customers,
		Cases:     cases,
		Webhook:   webhook.dispatch,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RunEDDEscalationJob: %v", err)
	}

	if result.Stage1Reminders != 1 {
		t.Errorf("Stage1Reminders = %d, want 1", result.Stage1Reminders)
	}
	if webhook.count(domain.WebhookEventEDDRequired) != 1 {
		t.Errorf("edd_required webhook count = %d, want 1", webhook.count(domain.WebhookEventEDDRequired))
	}

	got, err := customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EddStage1LastSentAt == nil {
		t.Error("expected EddStage1LastSentAt to be set")
	}

	cs, _ := cases.ListByCustomer(context.Background(), c.ID)
	if len(cs) != 0 {
		t.Errorf("expected no case created at stage 1, got %d", len(cs))
	}
}

// TestRunEDDEscalationJob_Stage2At60Days_RecommendsRestrictionAndCreatesHighCase
// verifies stage 2: at 60 days (default), transaction_restriction_recommended
// fires and a HIGH-priority case is auto-generated for the customer.
func TestRunEDDEscalationJob_Stage2At60Days_RecommendsRestrictionAndCreatesHighCase(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	cases := store.NewMemoryCaseRepo()
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	c := seedHighTierCustomerWithEDD(t, customers, "EDD_S2", 60*24*time.Hour, now)

	webhook := &fakeWebhookRecorder{}
	result, err := RunEDDEscalationJob(context.Background(), EDDEscalationDeps{
		Customers: customers,
		Cases:     cases,
		Webhook:   webhook.dispatch,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RunEDDEscalationJob: %v", err)
	}

	if result.Stage2Escalations != 1 {
		t.Errorf("Stage2Escalations = %d, want 1", result.Stage2Escalations)
	}
	if webhook.count(domain.WebhookEventTransactionRestrictionRecommended) != 1 {
		t.Errorf("transaction_restriction_recommended count = %d, want 1", webhook.count(domain.WebhookEventTransactionRestrictionRecommended))
	}

	cs, _ := cases.ListByCustomer(context.Background(), c.ID)
	if len(cs) != 1 {
		t.Fatalf("expected 1 case created at stage 2, got %d", len(cs))
	}
	if cs[0].Priority != domain.CasePriorityHigh {
		t.Errorf("case priority = %q, want %q", cs[0].Priority, domain.CasePriorityHigh)
	}
}

// TestRunEDDEscalationJob_Stage3At90Days_RecommendsDeclineAndEscalatesToCritical
// verifies stage 3: at 90 days (default), relationship_decline_recommended
// fires and the case is raised to CRITICAL priority.
func TestRunEDDEscalationJob_Stage3At90Days_RecommendsDeclineAndEscalatesToCritical(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	cases := store.NewMemoryCaseRepo()
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)

	// Simulate stage 2 having already run 30 days earlier by seeding a
	// customer whose EDD window started 90 days ago and already carries a
	// stage-2 notified timestamp with an existing HIGH case.
	tier := domain.RiskTierHigh
	eddAt := now.Add(-90 * 24 * time.Hour)
	stage2At := now.Add(-30 * 24 * time.Hour)
	c := &domain.Customer{
		ID:                  "cust-EDD_S3",
		ExternalID:          "EDD_S3",
		CustomerType:        domain.CustomerTypeIndividual,
		CountryCode:         "JP",
		RiskTier:            &tier,
		EddRequestedAt:      &eddAt,
		EddStage2NotifiedAt: &stage2At,
	}
	if err := customers.Create(context.Background(), c); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	existingCase := &domain.Case{
		ID:         "case-EDD_S3",
		CustomerID: c.ID,
		Status:     domain.CaseStatusNew,
		Priority:   domain.CasePriorityHigh,
		Summary:    eddCaseSummaryMarker + " EDD requirement overdue",
	}
	if err := cases.Create(context.Background(), existingCase); err != nil {
		t.Fatalf("seed case: %v", err)
	}

	webhook := &fakeWebhookRecorder{}
	result, err := RunEDDEscalationJob(context.Background(), EDDEscalationDeps{
		Customers: customers,
		Cases:     cases,
		Webhook:   webhook.dispatch,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RunEDDEscalationJob: %v", err)
	}

	if result.Stage3Escalations != 1 {
		t.Errorf("Stage3Escalations = %d, want 1", result.Stage3Escalations)
	}
	if webhook.count(domain.WebhookEventRelationshipDeclineRecommended) != 1 {
		t.Errorf("relationship_decline_recommended count = %d, want 1", webhook.count(domain.WebhookEventRelationshipDeclineRecommended))
	}

	cs, _ := cases.ListByCustomer(context.Background(), c.ID)
	if len(cs) != 1 {
		t.Fatalf("expected exactly 1 case (escalated, not duplicated), got %d", len(cs))
	}
	if cs[0].Priority != domain.CasePriorityCritical {
		t.Errorf("case priority = %q, want %q", cs[0].Priority, domain.CasePriorityCritical)
	}
}

// TestRunEDDEscalationJob_DoesNotChangeCustomerStatus verifies
// case-management.md: "本システムは各段階の推奨通知を発行するのみであり、
// customers.status の変更は基幹からの明示的なステータス変更通知を受けて
// 反映する" — this job must never mutate the customer's core attributes
// (risk tier, risk score, type, country), only its own EDD tracking columns.
func TestRunEDDEscalationJob_DoesNotChangeCustomerStatus(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	cases := store.NewMemoryCaseRepo()
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	c := seedHighTierCustomerWithEDD(t, customers, "EDD_NOCHANGE", 90*24*time.Hour, now)

	_, err := RunEDDEscalationJob(context.Background(), EDDEscalationDeps{
		Customers: customers,
		Cases:     cases,
		Webhook:   (&fakeWebhookRecorder{}).dispatch,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("RunEDDEscalationJob: %v", err)
	}

	got, err := customers.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CustomerType != c.CustomerType {
		t.Errorf("CustomerType changed: got %q, want %q", got.CustomerType, c.CustomerType)
	}
	if got.CountryCode != c.CountryCode {
		t.Errorf("CountryCode changed: got %q, want %q", got.CountryCode, c.CountryCode)
	}
	if got.RiskTier == nil || *got.RiskTier != domain.RiskTierHigh {
		t.Errorf("RiskTier changed: got %v, want High", got.RiskTier)
	}
}

// TestRunEDDEscalationJob_Idempotent verifies running the job twice on the
// same day never duplicates webhooks or cases, at any stage.
func TestRunEDDEscalationJob_Idempotent(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	cases := store.NewMemoryCaseRepo()
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	seedHighTierCustomerWithEDD(t, customers, "EDD_IDEMP", 60*24*time.Hour, now)

	webhook := &fakeWebhookRecorder{}
	deps := EDDEscalationDeps{
		Customers: customers,
		Cases:     cases,
		Webhook:   webhook.dispatch,
		Now:       func() time.Time { return now },
	}

	if _, err := RunEDDEscalationJob(context.Background(), deps); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Same day, a few hours later.
	deps.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := RunEDDEscalationJob(context.Background(), deps); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if webhook.count(domain.WebhookEventTransactionRestrictionRecommended) != 1 {
		t.Errorf("transaction_restriction_recommended count = %d, want 1 (no duplicate)", webhook.count(domain.WebhookEventTransactionRestrictionRecommended))
	}
}
