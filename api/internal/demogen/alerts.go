package demogen

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
)

// evaluateAlerts runs txns (one customer's transactions — either their
// ordinary background history, or one isolated story/FP incident block)
// through the real native engine's realtime AND batch evaluation paths and
// returns the union, deduplicated by (scenario_id, transaction_ids): a
// scenario whose evaluation_mode is "both" runs under both calls and would
// otherwise be double-counted, since each call independently reconstructs
// the same alert from the same input.
//
// This is demogen's primary self-check (a) mechanism: rather than
// separately re-deriving whether a seeded transaction block "should"
// breach a threshold, every alert in this dataset is something the actual
// engine.Evaluate code path produced from the seeded transactions.
func evaluateAlerts(ctx context.Context, eng *native.Engine, customerID string, tier domain.RiskTier, txns []domain.Transaction) ([]domain.Alert, error) {
	if len(txns) == 0 {
		return nil, nil
	}
	batch, err := eng.EvaluateTransactionsBatch(ctx, customerID, tier, txns, nil)
	if err != nil {
		return nil, fmt.Errorf("evaluate batch for %s: %w", customerID, err)
	}
	realtime, err := eng.EvaluateTransactions(ctx, customerID, tier, txns, nil)
	if err != nil {
		return nil, fmt.Errorf("evaluate realtime for %s: %w", customerID, err)
	}
	seen := map[string]bool{}
	var out []domain.Alert
	for _, a := range append(batch, realtime...) {
		key := a.ScenarioID + "|" + strings.Join(a.TransactionIDs, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out, nil
}

// severityBySeenario is the flat scenario-level severity from each TM
// scenario YAML's `severity:` field (T1-W2 instructions: "severityはシナリオ
// YAML値" — deliberately not the engine's dynamically-computed per-alert
// severity, e.g. rapid_movement's ratio-based medium/high/critical split).
func severityBySeenario(cfgs map[string]scenarioConfig, scenarioID string) domain.AlertSeverity {
	if cfg, ok := cfgs[scenarioID]; ok && cfg.Severity != "" {
		return domain.AlertSeverity(cfg.Severity)
	}
	return domain.AlertSeverityMedium
}

// alertBuildContext accumulates alerts across every customer/incident so
// the final pass can assign sequential IDs and enforce the
// (customer_id, scenario_id, aggregation_window_start) uniqueness self-check
// in one place.
type alertBuildContext struct {
	anchor  time.Time
	cfgs    map[string]scenarioConfig
	txnTime map[string]time.Time // transaction ID -> executed_at, for DetectedAt derivation
	alerts  []domain.Alert
	seen    map[string]bool // customer_id|scenario_id|aggregation_window_start
}

func newAlertBuildContext(anchor time.Time, cfgs map[string]scenarioConfig, allTxns []domain.Transaction) *alertBuildContext {
	txnTime := make(map[string]time.Time, len(allTxns))
	for _, t := range allTxns {
		txnTime[t.ID] = t.ExecutedAt
	}
	return &alertBuildContext{anchor: anchor, cfgs: cfgs, txnTime: txnTime, seen: map[string]bool{}}
}

// add converts raw engine alerts into dataset alerts: DetectedAt is the
// latest transaction time among the alert's own transaction_ids (the moment
// the pattern was actually complete, not time.Now()), severity is
// overridden per severityBySeenario, and the (customer_id, scenario_id,
// aggregation_window_start) uniqueness constraint (migrations/012) is
// enforced by skipping any duplicate rather than emitting a row that would
// fail the real constraint. It returns [start,end) indices into ctx.alerts
// for whatever was actually appended (post-dedup), so the caller can set
// Status (and, for story 4, force Severity to critical) on exactly those
// entries via ctx.alerts[start:end] — mutating through the slice index
// affects the stored copy, unlike a separately returned []domain.Alert
// would.
func (ctx *alertBuildContext) add(customerID string, raw []domain.Alert) (start, end int) {
	start = len(ctx.alerts)
	for _, a := range raw {
		detected := ctx.anchor
		first := true
		for _, tid := range a.TransactionIDs {
			if t, ok := ctx.txnTime[tid]; ok {
				if first || t.After(detected) {
					detected = t
					first = false
				}
			}
		}
		windowStart := domain.DailyAggregationWindowStart(detected)
		key := customerID + "|" + a.ScenarioID + "|" + windowStart.Format(time.RFC3339)
		if ctx.seen[key] {
			continue
		}
		ctx.seen[key] = true

		a.CustomerID = customerID
		a.Severity = severityBySeenario(ctx.cfgs, a.ScenarioID)
		a.DetectedAt = detected
		a.AggregationWindowStart = &windowStart
		a.CreatedAt = detected
		a.UpdatedAt = detected
		a.Status = domain.AlertStatusOpen // default; callers normally override
		ctx.alerts = append(ctx.alerts, a)
	}
	end = len(ctx.alerts)
	return
}

// fpStatusWeights realizes A7's overall status distribution
// (closed_false_positive 55% / closed_true_positive 5% / open 22% /
// investigating 12% / escalated 6%) given that the TP alerts (assigned
// their own specific statuses by the caller — see demogen.go) already
// account for the closed_true_positive/open/investigating share: the
// remaining FP-alert pool is weighted so the population-wide total lands
// close to the A7 percentages (worked out by hand against the actual A6/A7
// counts: 8 TP + 86 FP ≈ 94 total; see the T1-W2 report for the exact
// arithmetic).
func fpStatusWeights(rng *rand.Rand) domain.AlertStatus {
	statuses := []string{"closed_false_positive", "open", "investigating", "escalated"}
	weights := []int{60, 24, 9, 7}
	return domain.AlertStatus(weightedPick(rng, statuses, weights))
}

// finalizeAlerts assigns sequential IDs in ctx.alerts' current order (a
// pure function of the deterministic order alerts were add()-ed in).
func finalizeAlerts(ctx *alertBuildContext) []domain.Alert {
	for i := range ctx.alerts {
		ctx.alerts[i].ID = fmt.Sprintf("demo-alert-%05d", i+1)
	}
	return ctx.alerts
}
