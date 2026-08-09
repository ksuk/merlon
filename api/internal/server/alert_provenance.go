package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/ksuk/merlon/api/internal/domain"
)

// resolveAlertProvenance fills in what can be resolved now and states what
// cannot.
//
// The stored record deliberately keeps references rather than copies, so the
// rule version an alert names is looked up at read time. That means the answer
// reflects the store as it is today: a version that has since been removed
// reports missing rather than being reconstructed from current configuration,
// which would produce something that looks like evidence and is not
// (ADR-0025, DR-19).
//
// An alert with no stored provenance is not an error and not an omission. It
// predates capture, and saying so is the whole point: current configuration is
// never backfilled as historical fact.
func (s *Server) resolveAlertProvenance(ctx context.Context, a *domain.Alert) {
	if a == nil {
		return
	}
	if a.Provenance == nil {
		a.Provenance = &domain.AlertProvenance{
			ScenarioID:   a.ScenarioID,
			Availability: domain.ProvenanceNotCaptured,
		}
		return
	}

	a.Provenance.ScenarioID = a.ScenarioID
	if s.rules == nil || a.ScenarioID == "" {
		// Nothing to resolve against. The captured facts stand on their own;
		// the rule reference is the part we cannot answer.
		a.Provenance.Availability = domain.ProvenanceMissing
		return
	}

	rule, err := s.rules.Get(ctx, a.ScenarioID)
	if err != nil || rule == nil {
		a.Provenance.Availability = domain.ProvenanceMissing
		return
	}

	a.Provenance.RuleName = rule.Name
	version := rule.Version
	a.Provenance.RuleVersion = &version
	a.Provenance.RuleDigest = ruleDefinitionDigest(rule)
	// The rule body itself is never returned here. The identifier, version and
	// digest are what let an authorized reviewer fetch the artifact through the
	// rule API, where its own authorization applies.
	a.Provenance.Availability = domain.ProvenanceRestricted
}

// ruleDefinitionDigest is a content address for a stored rule version, so a
// reviewer can tell whether the artifact they fetched is the one the alert
// names.
func ruleDefinitionDigest(rule *domain.RuleDefinition) string {
	if rule == nil || len(rule.Definition) == 0 {
		return ""
	}
	sum := sha256.Sum256(rule.Definition)
	return hex.EncodeToString(sum[:])
}

// resolveAlertProvenanceAll applies the resolution to a page of alerts.
func (s *Server) resolveAlertProvenanceAll(ctx context.Context, alerts []domain.Alert) {
	for i := range alerts {
		s.resolveAlertProvenance(ctx, &alerts[i])
	}
}
