// Package casemgmt implements case-management side effects triggered by
// alert creation (the transaction-monitoring design「アラート統合ロジック」).
package casemgmt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

// eligible reports whether c can receive a new alert: unresolved, and
// created no earlier than windowStart. The lower bound is inclusive, so a
// case created exactly windowStart (e.g. exactly `window` before the
// alert's detection time) is still eligible.
func eligible(c *domain.Case, windowStart time.Time) bool {
	return domain.IsCaseUnresolved(c.Status) && !c.CreatedAt.Before(windowStart)
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
	return consolidateAlert(ctx, cases, nil, alert, window, nil)
}

// ConsolidateAlertWithLifecycle is the production consolidation path. Alert
// attachment and case creation are delegated to the lifecycle repository so
// a terminal/stale case can never receive a new link between selection and
// persistence.
func ConsolidateAlertWithLifecycle(
	ctx context.Context,
	cases domain.CaseRepository,
	lifecycle domain.CaseAlertLifecycleRepository,
	alert *domain.Alert,
	window time.Duration,
) (*domain.Case, error) {
	return consolidateAlert(ctx, cases, lifecycle, alert, window, nil)
}

// PriorityResolver supplies the versioned CDD-derived priority for a newly
// created case. It is a function so the consolidation package remains
// independent of a particular customer repository or HTTP server.
type PriorityResolver func(context.Context, string) (domain.CasePriority, error)

// ConsolidateAlertWithLifecycleAndPriority is the production path. Existing
// cases retain their stored priority; only a new case asks the resolver for a
// CDD-derived value.
func ConsolidateAlertWithLifecycleAndPriority(
	ctx context.Context,
	cases domain.CaseRepository,
	lifecycle domain.CaseAlertLifecycleRepository,
	alert *domain.Alert,
	window time.Duration,
	priorityResolver PriorityResolver,
) (*domain.Case, error) {
	return consolidateAlert(ctx, cases, lifecycle, alert, window, priorityResolver)
}

func consolidateAlert(
	ctx context.Context,
	cases domain.CaseRepository,
	lifecycle domain.CaseAlertLifecycleRepository,
	alert *domain.Alert,
	window time.Duration,
	priorityResolver PriorityResolver,
) (*domain.Case, error) {
	for attempt := 0; attempt < 3; attempt++ {
		result, retry, err := consolidateAlertOnce(ctx, cases, lifecycle, alert, window, priorityResolver)
		if err != nil {
			return nil, err
		}
		if !retry {
			return result, nil
		}
	}
	return nil, &domain.ErrConflict{Entity: "case", ID: alert.ID, Reason: "case consolidation remained concurrent after 3 attempts"}
}

func consolidateAlertOnce(
	ctx context.Context,
	cases domain.CaseRepository,
	lifecycle domain.CaseAlertLifecycleRepository,
	alert *domain.Alert,
	window time.Duration,
	priorityResolver PriorityResolver,
) (*domain.Case, bool, error) {
	existing, err := cases.ListByCustomer(ctx, alert.CustomerID)
	if err != nil {
		return nil, false, err
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
		if lifecycle != nil {
			updated, err := lifecycle.AppendAlerts(ctx, target.ID, target.UpdatedAt, []string{alert.ID})
			if err != nil {
				var conflict *domain.ErrConflict
				if errors.As(err, &conflict) {
					return nil, true, nil
				}
				return nil, false, err
			}
			return updated, false, nil
		}
		expectedUpdatedAt := target.UpdatedAt
		target.AlertIDs = append(target.AlertIDs, alert.ID)
		if err := cases.UpdateIfUnmodified(ctx, target, expectedUpdatedAt); err != nil {
			var conflict *domain.ErrConflict
			if errors.As(err, &conflict) {
				return nil, true, nil
			}
			return nil, false, err
		}
		return target, false, nil
	}

	now := time.Now()
	priority := domain.CasePriorityMedium
	if priorityResolver != nil {
		var err error
		priority, err = priorityResolver(ctx, alert.CustomerID)
		if err != nil {
			return nil, false, fmt.Errorf("derive case priority from CDD state: %w", err)
		}
	}
	newCase := &domain.Case{
		ID:         generateID(),
		CustomerID: alert.CustomerID,
		AlertIDs:   []string{alert.ID},
		Status:     domain.CaseStatusOpen,
		Priority:   priority,
		Summary:    "Auto-created from alert " + alert.ID + " (" + alert.ScenarioID + ")",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if lifecycle != nil {
		if err := lifecycle.CreateCaseWithAlerts(ctx, newCase); err != nil {
			return nil, false, err
		}
	} else if err := cases.Create(ctx, newCase); err != nil {
		return nil, false, err
	}
	return newCase, false, nil
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
