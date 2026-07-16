// Package casemgmt implements case-management side effects triggered by
// alert creation (the transaction-monitoring design「アラート統合ロジック」).
package casemgmt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// DefaultConsolidationWindow is the default aggregation window for automatic
// case consolidation (the transaction-monitoring design「アラート統合ロジック」:
// デフォルト24時間、設定可能).
const DefaultConsolidationWindow = 24 * time.Hour

// investigatingOrLater reports whether status ranks at or above
// INVESTIGATING, so ConsolidateAlert can prefer such a case over a merely
// OPEN one (the transaction-monitoring design「既に INVESTIGATING 以降のケースが存在
// する場合は、そのケースに追加アラートとして紐づける」).
func investigatingOrLater(status domain.CaseStatus) bool {
	return status == domain.CaseStatusInvestigating || status == domain.CaseStatusEscalated
}

// eligible reports whether c can receive a new alert: not closed, and
// created no earlier than windowStart. The lower bound is inclusive, so a
// case created exactly windowStart (e.g. exactly `window` before the
// alert's detection time) is still eligible.
func eligible(c *domain.Case, windowStart time.Time) bool {
	return c.Status != domain.CaseStatusClosed && !c.CreatedAt.Before(windowStart)
}

// ConsolidateAlert joins alert to an existing, non-closed case for the same
// customer created within window of alert.DetectedAt, preferring a case
// already INVESTIGATING or later over a merely OPEN one
// (the transaction-monitoring design「アラート統合ロジック」). If no eligible case
// exists, it creates and returns a new one.
func ConsolidateAlert(
	ctx context.Context,
	cases domain.CaseRepository,
	alert *domain.Alert,
	window time.Duration,
) (*domain.Case, error) {
	existing, err := cases.ListByCustomer(ctx, alert.CustomerID)
	if err != nil {
		return nil, err
	}

	windowStart := alert.DetectedAt.Add(-window)

	var target *domain.Case
	for i := range existing {
		c := &existing[i]
		if !eligible(c, windowStart) {
			continue
		}
		if target == nil || (investigatingOrLater(c.Status) && !investigatingOrLater(target.Status)) {
			target = c
		}
	}

	if target != nil {
		target.AlertIDs = append(target.AlertIDs, alert.ID)
		target.UpdatedAt = time.Now()
		if err := cases.Update(ctx, target); err != nil {
			return nil, err
		}
		return target, nil
	}

	now := time.Now()
	newCase := &domain.Case{
		ID:         generateID(),
		CustomerID: alert.CustomerID,
		AlertIDs:   []string{alert.ID},
		Status:     domain.CaseStatusOpen,
		Priority:   priorityFromSeverity(alert.Severity),
		Summary:    "Auto-created from alert " + alert.ID + " (" + alert.ScenarioID + ")",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := cases.Create(ctx, newCase); err != nil {
		return nil, err
	}
	return newCase, nil
}

func priorityFromSeverity(s domain.AlertSeverity) domain.CasePriority {
	switch s {
	case domain.AlertSeverityCritical, domain.AlertSeverityHigh:
		return domain.CasePriorityHigh
	case domain.AlertSeverityMedium:
		return domain.CasePriorityMedium
	default:
		return domain.CasePriorityLow
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
