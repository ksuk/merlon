// Package handlers holds events.Bus subscription handlers that react to
// domain events (Task 8: CDD tier-change -> TM re-evaluation).
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/events"
	"github.com/merlon-aml/merlon/api/internal/metrics"
)

// maxChainHops bounds how many times a single event_chain_id may re-trigger
// CDD rescoring before the automatic chain is truncated in favor of manual
// review (cdd-scoring.md safety valve 4: circular dependency prevention,
// default 3).
const maxChainHops = 3

// retroactiveWindow is how far back transaction-monitoring.md's in-flight
// tier consistency rule looks for already-evaluated transactions to
// re-evaluate after a tier upgrade.
const retroactiveWindow = 24 * time.Hour

// TierChangeEvent is the Payload of a "cdd.tier_changed" events.Event.
type TierChangeEvent struct {
	CustomerID string           `json:"customer_id"`
	OldTier    *domain.RiskTier `json:"old_tier,omitempty"`
	NewTier    domain.RiskTier  `json:"new_tier"`
	ChainID    string           `json:"chain_id"`
	ScoredAt   time.Time        `json:"scored_at"`
}

// NewTierChangeHandler builds an events.Bus subscription handler for
// "cdd.tier_changed" events. On a MEDIUM/LOW -> HIGH upgrade, it
// re-evaluates the customer's transactions from the last 24 hours under the
// new tier's thresholds and persists any newly-generated alerts
// (transaction-monitoring.md in-flight tier consistency). Downgrades are
// never retroactively re-evaluated (Fail-Alert principle: prefer false
// positives over missed detections, so existing alerts are never
// retroactively invalidated). Once the event's chain hop count exceeds
// maxChainHops, the handler stops processing and increments
// merlon_cdd_event_chain_truncated_total instead (cdd-scoring.md safety
// valve 4).
func NewTierChangeHandler(
	transactions domain.TransactionRepository,
	monitoring engine.MonitoringEngine,
	alerts domain.AlertRepository,
) func(events.Event) {
	return func(e events.Event) {
		if e.ChainHopCount >= maxChainHops {
			metrics.CDDEventChainTruncatedTotal.Inc()
			return
		}

		var tc TierChangeEvent
		if err := json.Unmarshal(e.Payload, &tc); err != nil {
			return
		}

		if !isUpgradeToHigh(tc.OldTier, tc.NewTier) {
			return
		}

		ctx := context.Background()

		// api/internal/domain.Transaction has no separate "evaluated_at"
		// timestamp; CreatedAt (ingestion time) is used as the proxy for
		// "evaluated within the window", since batch/inline monitoring
		// runs shortly after ingestion.
		cutoff := tc.ScoredAt.Add(-retroactiveWindow)
		all, err := transactions.ListByCustomer(ctx, tc.CustomerID, 1000, 0)
		if err != nil {
			return
		}

		var recent []domain.Transaction
		for _, t := range all {
			if t.CreatedAt.After(cutoff) {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			return
		}

		newAlerts, err := monitoring.EvaluateTransactions(ctx, tc.CustomerID, tc.NewTier, recent, nil)
		if err != nil {
			return
		}

		for _, a := range newAlerts {
			a.ID = generateID()
			now := time.Now()
			a.CreatedAt = now
			a.UpdatedAt = now
			_ = alerts.Create(ctx, &a)
		}
	}
}

// isUpgradeToHigh reports whether the transition is a MEDIUM/LOW -> HIGH
// upgrade (transaction-monitoring.md in-flight tier consistency). A nil
// oldTier (first-ever scoring) is not treated as an upgrade.
func isUpgradeToHigh(oldTier *domain.RiskTier, newTier domain.RiskTier) bool {
	if newTier != domain.RiskTierHigh || oldTier == nil {
		return false
	}
	return *oldTier == domain.RiskTierMedium || *oldTier == domain.RiskTierLow
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
