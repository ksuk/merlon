package native

import (
	"time"

	"github.com/ksuk/merlon/api/internal/buildinfo"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

// provenanceContext carries what the caller told the engine about the
// configuration effective for this evaluation.
//
// The monitoring request has carried ConfigDigests, EvaluatedAt and the window
// since Wave 1, and the engine read none of them. An alert therefore recorded
// that a scenario had fired but not which version of it, so once a rule changed
// nobody could show what had been effective at detection time (#84).
//
// A zero value means the caller supplied nothing, which is a real state: the
// legacy EvaluateTransactionsBatch entry point has no request. The alert then
// carries no provenance and is reported as not_captured, rather than being
// filled in from whatever the process happens to be running now.
type provenanceContext struct {
	configDigests map[string]string
	evaluatedAt   *time.Time
	windowFrom    *time.Time
	windowTo      *time.Time
	captured      bool
}

func provenanceContextFrom(req engine.MonitoringRequest) provenanceContext {
	ctx := provenanceContext{
		configDigests: req.ConfigDigests,
		windowFrom:    req.WindowFrom,
		windowTo:      req.WindowTo,
		captured:      true,
	}
	if !req.EvaluatedAt.IsZero() {
		at := req.EvaluatedAt.UTC()
		ctx.evaluatedAt = &at
	}
	return ctx
}

// digests reports the engine's own configuration digests, which identify the
// documents this process actually loaded. They are merged under the caller's
// digests rather than over them: the caller knows about configuration the
// engine never sees, and the engine knows its own roots exactly.
func (e *Engine) digests() map[string]string {
	out := map[string]string{}
	if e == nil {
		return out
	}
	if e.cddDigest != "" {
		out["engine:cdd_weights"] = e.cddDigest
	}
	if e.tmDigest != "" {
		out["engine:tm_scenarios"] = e.tmDigest
	}
	if e.screeningDigest != "" {
		out["engine:screening_lists"] = e.screeningDigest
	}
	return out
}

// forScenario builds the immutable record attached to one alert.
//
// It stores references and the single threshold that decided the detection, not
// the rule body: rule_definitions already holds immutable version rows, and
// copying content into every alert would create a second store with its own
// authorization and purge problems (ADR-0025, DR-19).
func (p provenanceContext) forScenario(s scenario, customerType, tier, mode string, engineDigests map[string]string) *domain.AlertProvenance {
	if !p.captured {
		return nil
	}

	digests := make(map[string]string, len(p.configDigests)+len(engineDigests))
	for name, digest := range engineDigests {
		digests[name] = digest
	}
	for name, digest := range p.configDigests {
		digests[name] = digest
	}

	record := &domain.AlertProvenance{
		ScenarioID:     s.ID,
		ConfigDigests:  digests,
		EngineVersion:  buildinfo.Version,
		EvaluationMode: mode,
		EvaluatedAt:    p.evaluatedAt,
		WindowFrom:     p.windowFrom,
		WindowTo:       p.windowTo,
		Availability:   domain.ProvenanceAvailable,
	}

	if threshold, ok := appliedThreshold(s, customerType, tier); ok {
		record.AppliedThreshold = &threshold
	}
	return record
}

// appliedThreshold resolves the value that actually governed this customer
// type and risk tier. A scenario with no per-tier table has no such number, and
// reporting one from a neighbouring tier would be a fabricated explanation.
func appliedThreshold(s scenario, customerType, tier string) (float64, bool) {
	byTier, ok := s.Thresholds[customerType]
	if !ok {
		byTier, ok = s.Thresholds[""]
	}
	if !ok {
		return 0, false
	}
	if value, ok := byTier[tier]; ok {
		return value, true
	}
	if value, ok := byTier[""]; ok {
		return value, true
	}
	return 0, false
}
