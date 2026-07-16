package batch

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// eddStage1Days is the fixed reminder threshold (the case-management workflow §EDD
// 未実施継続時の段階的措置: "EDD要求から30日経過"). Unlike stages 2/3, this
// is not configurable in the spec.
const eddStage1Days = 30

// defaultEDDStage2Days/defaultEDDStage3Days back Stage2Days/Stage3Days when
// EDDEscalationDeps leaves them at zero (the case-management workflow: "60日経過（デフォ
// ルト、設定可）", "90日経過（デフォルト、設定可）").
const (
	defaultEDDStage2Days = 60
	defaultEDDStage3Days = 90
)

// eddCaseSummaryMarker tags cases RunEDDEscalationJob creates so a later run
// can find and escalate the same case instead of creating duplicates. It
// carries no PII (customer_id only, via the webhook/case payload).
const eddCaseSummaryMarker = "[EDD escalation]"

// WebhookDispatchFunc emits a webhook event. It mirrors server.Server's
// unexported dispatchWebhook so this package (which cannot import server,
// to avoid an import cycle) can still trigger webhook delivery; main.go
// wires it to server.Server.DispatchWebhook.
type WebhookDispatchFunc func(ctx context.Context, event domain.WebhookEventType, data any)

// EDDEscalationDeps groups RunEDDEscalationJob's dependencies.
type EDDEscalationDeps struct {
	Customers domain.CustomerRepository
	Cases     domain.CaseRepository
	Webhook   WebhookDispatchFunc
	// Now overrides time.Now for deterministic tests. Nil uses time.Now.
	Now func() time.Time
	// Stage2Days/Stage3Days override the default 60/90-day thresholds
	// (the case-management workflow: "デフォルト、設定可"). Zero uses the default.
	Stage2Days int
	Stage3Days int
}

// EDDEscalationResult reports what RunEDDEscalationJob did on a single run.
type EDDEscalationResult struct {
	Stage1Reminders   int
	Stage2Escalations int
	Stage3Escalations int
}

// eddNotifyPayload is the webhook payload for EDD escalation events. It
// carries only the customer ID, never PII (notifications.md §1).
type eddNotifyPayload struct {
	CustomerID string `json:"customer_id"`
}

// RunEDDEscalationJob implements the 3-stage EDD escalation
// (the case-management workflow §EDD未実施継続時の段階的措置). For each High-tier
// customer with an open EDD requirement (EddRequestedAt set), it checks how
// many days have elapsed and fires at most one stage per run:
//
//   - Stage 1 (>=30 days): re-send the edd_required webhook. Repeats at most
//     once per calendar day (the UI task itself stays visible independent of
//     this job).
//   - Stage 2 (>=60 days, default): fire transaction_restriction_recommended
//     once and ensure a HIGH-priority case exists for the customer.
//   - Stage 3 (>=90 days, default): fire relationship_decline_recommended
//     once and raise that case's priority to CRITICAL.
//
// The actual restriction/decline decision and execution remains the core
// system's responsibility (CONST-002); this job only detects elapsed time
// and notifies. It never modifies any customer field the core system owns
// (e.g. an eventual customers.status) — only its own EDD tracking columns.
func RunEDDEscalationJob(ctx context.Context, deps EDDEscalationDeps) (EDDEscalationResult, error) {
	var result EDDEscalationResult

	now := time.Now
	if deps.Now != nil {
		now = deps.Now
	}
	stage2Days := deps.Stage2Days
	if stage2Days <= 0 {
		stage2Days = defaultEDDStage2Days
	}
	stage3Days := deps.Stage3Days
	if stage3Days <= 0 {
		stage3Days = defaultEDDStage3Days
	}

	customers, err := deps.Customers.ListEDDPending(ctx)
	if err != nil {
		return result, err
	}

	nowTime := now()

	for i := range customers {
		c := customers[i]
		if c.EddRequestedAt == nil {
			continue
		}
		elapsedDays := int(nowTime.Sub(*c.EddRequestedAt).Hours() / 24)

		dirty := false
		switch {
		case elapsedDays >= stage3Days && c.EddStage3NotifiedAt == nil:
			if err := escalateEDDStage(ctx, deps, &c, domain.WebhookEventRelationshipDeclineRecommended, domain.CasePriorityCritical); err != nil {
				slog.ErrorContext(ctx, "EDD stage 3 escalation failed", "customer_id", c.ID, "error", err)
				break
			}
			stamp := nowTime
			c.EddStage3NotifiedAt = &stamp
			result.Stage3Escalations++
			dirty = true

		case elapsedDays >= stage2Days && c.EddStage2NotifiedAt == nil:
			if err := escalateEDDStage(ctx, deps, &c, domain.WebhookEventTransactionRestrictionRecommended, domain.CasePriorityHigh); err != nil {
				slog.ErrorContext(ctx, "EDD stage 2 escalation failed", "customer_id", c.ID, "error", err)
				break
			}
			stamp := nowTime
			c.EddStage2NotifiedAt = &stamp
			result.Stage2Escalations++
			dirty = true

		case elapsedDays >= eddStage1Days && (c.EddStage1LastSentAt == nil || !sameDay(*c.EddStage1LastSentAt, nowTime)):
			if deps.Webhook != nil {
				deps.Webhook(ctx, domain.WebhookEventEDDRequired, eddNotifyPayload{CustomerID: c.ID})
			}
			stamp := nowTime
			c.EddStage1LastSentAt = &stamp
			result.Stage1Reminders++
			dirty = true
		}

		if dirty {
			if err := deps.Customers.Update(ctx, &c); err != nil {
				slog.ErrorContext(ctx, "EDD escalation state update failed", "customer_id", c.ID, "error", err)
			}
		}
	}

	return result, nil
}

// escalateEDDStage fires the given webhook event and ensures a case exists
// for the customer at (at least) the given priority, creating one if
// necessary or raising an existing EDD case's priority.
func escalateEDDStage(ctx context.Context, deps EDDEscalationDeps, c *domain.Customer, event domain.WebhookEventType, priority domain.CasePriority) error {
	if deps.Webhook != nil {
		deps.Webhook(ctx, event, eddNotifyPayload{CustomerID: c.ID})
	}
	return ensureEDDCase(ctx, deps, c, priority)
}

// ensureEDDCase finds the customer's open EDD-tagged case (if any) and
// raises its priority when priority outranks the current one, or creates a
// new case at priority when none exists yet.
func ensureEDDCase(ctx context.Context, deps EDDEscalationDeps, c *domain.Customer, priority domain.CasePriority) error {
	if deps.Cases == nil {
		return nil
	}

	existing, err := deps.Cases.ListByCustomer(ctx, c.ID)
	if err != nil {
		return err
	}
	for i := range existing {
		e := existing[i]
		if !strings.Contains(e.Summary, eddCaseSummaryMarker) {
			continue
		}
		if e.Status == domain.CaseStatusClosed || e.Status == domain.CaseStatusStrFiled {
			continue
		}
		if casePriorityRank(priority) <= casePriorityRank(e.Priority) {
			return nil
		}
		e.Priority = priority
		return deps.Cases.Update(ctx, &e)
	}

	now := time.Now()
	if deps.Now != nil {
		now = deps.Now()
	}
	newCase := &domain.Case{
		ID:         generateID(),
		CustomerID: c.ID,
		Status:     domain.CaseStatusNew,
		Priority:   priority,
		Summary:    eddCaseSummaryMarker + " EDD requirement overdue",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return deps.Cases.Create(ctx, newCase)
}

func casePriorityRank(p domain.CasePriority) int {
	switch p {
	case domain.CasePriorityLow:
		return 0
	case domain.CasePriorityMedium:
		return 1
	case domain.CasePriorityHigh:
		return 2
	case domain.CasePriorityCritical:
		return 3
	default:
		return -1
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// StartEDDEscalationTicker runs RunEDDEscalationJob on a fixed interval until
// ctx is cancelled. A daily-scale interval is expected in production since
// the job's finest granularity is one calendar day (stage 1 dedup); a
// shorter interval is harmless since the job is idempotent.
func StartEDDEscalationTicker(ctx context.Context, deps EDDEscalationDeps, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := RunEDDEscalationJob(ctx, deps); err != nil {
					slog.ErrorContext(ctx, "EDD escalation job failed", "error", err)
				}
			}
		}
	}()
}
