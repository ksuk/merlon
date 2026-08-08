package server

import (
	"context"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

// isMonitoringOperation reports whether an operation runs the TM engine, which
// is what decides both the eligibility rule and the side effects.
func isMonitoringOperation(operation string) bool {
	switch operation {
	case "batch_monitor", "monitor", "tm_batch_evaluation":
		return true
	default:
		return false
	}
}

// targetExclusionReason returns why a customer cannot be operated on, or ""
// when they can.
//
// The rules restate what the executing code already does, so the preview
// cannot promise work the run will skip: evaluatesUnder drops closed customers
// from every pass and dormant customers from a batch pass, and scoring a
// closed customer produces a tier nothing will act on.
func targetExclusionReason(c domain.Customer, operation string) string {
	status := c.EffectiveStatus()
	if isMonitoringOperation(operation) {
		if evaluatesUnder(status, engine.EvaluationModeBatch) {
			return ""
		}
		return string(status)
	}
	if status == domain.CustomerStatusClosed {
		return string(status)
	}
	return ""
}

// expectedSideEffects names what confirming a run will do, in the operator's
// terms rather than the engine's.
func expectedSideEffects(operation string) []string {
	if isMonitoringOperation(operation) {
		return []string{
			"evaluates transaction monitoring scenarios for every target",
			"may create alerts, and may consolidate new alerts into cases",
			"routes any customer the engine cannot evaluate to the pending-review queue",
		}
	}
	return []string{
		"recalculates the CDD risk score and tier for every target",
		"may open or close an EDD window when a target enters or leaves High tier",
		"writes a score history record for every target",
	}
}

// partitionEligibleTargets splits a resolved population into the customers a
// run will act on and a per-reason tally of the ones it would skip.
func (s *Server) partitionEligibleTargets(ctx context.Context, ids []string, operation string) (eligible []string, excluded map[string]int, err error) {
	excluded = map[string]int{}
	eligible = make([]string, 0, len(ids))
	for _, id := range ids {
		c, getErr := s.customers.Get(ctx, id)
		if getErr != nil {
			return nil, nil, getErr
		}
		if reason := targetExclusionReason(*c, operation); reason != "" {
			excluded[reason]++
			continue
		}
		eligible = append(eligible, id)
	}
	return eligible, excluded, nil
}

func totalExcluded(excluded map[string]int) int {
	total := 0
	for _, n := range excluded {
		total += n
	}
	return total
}
