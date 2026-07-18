package server

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// applyWhitelistSuppression marks alert as suppressed if customerID holds an
// active whitelist entry covering alert.ScenarioID (WL-004, whitelist.md
// §3.1). It is called from every alert-generation path (currently
// handleBatchMonitor) immediately before the alert is persisted; the
// engine itself keeps evaluating every customer/scenario unchanged
// (whitelist.md §3.1 evaluation flow), so suppression is applied here in the
// Go API layer only.
func (s *Server) applyWhitelistSuppression(ctx context.Context, alert *domain.Alert) (*domain.Alert, error) {
	if s.whitelist == nil {
		return alert, nil
	}

	entry, err := s.whitelist.GetActiveByCustomer(ctx, alert.CustomerID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			return alert, nil
		}
		return nil, err
	}

	// A daily expiry job (Task 4) is responsible for transitioning
	// status=active entries past valid_until to status=expired; until that
	// runs, check valid_until directly so an overdue entry doesn't keep
	// suppressing alerts.
	if entry.ValidUntil.Before(time.Now()) {
		return alert, nil
	}

	if len(entry.ExcludedRuleIDs) > 0 && !slices.Contains(entry.ExcludedRuleIDs, alert.ScenarioID) {
		return alert, nil
	}

	alert.Suppressed = true
	alert.SuppressionReason = "whitelist:" + entry.ID
	alert.Status = domain.AlertStatusSuppressed
	return alert, nil
}
