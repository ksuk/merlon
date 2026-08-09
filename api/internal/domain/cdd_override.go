package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CDDOverrideStatus is the maker-checker state of a proposed tier override.
type CDDOverrideStatus string

const (
	CDDOverridePendingApproval CDDOverrideStatus = "pending_approval"
	CDDOverrideApproved        CDDOverrideStatus = "approved"
	CDDOverrideRejected        CDDOverrideStatus = "rejected"
)

// CDDScoreOverride is a proposal to record a risk tier other than the one the
// engine computed. It exists because the computed tier decides EDD, monitoring
// thresholds and rescreening frequency, so overriding it is a control decision
// that needs two people -- the same rule whitelist entries already follow.
type CDDScoreOverride struct {
	ID                  string            `json:"id"`
	CustomerID          string            `json:"customer_id"`
	ScoreRecordID       string            `json:"score_record_id,omitempty"`
	ProposedTier        RiskTier          `json:"proposed_tier"`
	ComputedTier        RiskTier          `json:"computed_tier"`
	ComputedScore       float64           `json:"computed_score"`
	Reason              string            `json:"reason"`
	SupportingDocuments []string          `json:"supporting_documents,omitempty"`
	Evidence            map[string]any    `json:"evidence,omitempty"`
	Status              CDDOverrideStatus `json:"status"`
	RequestedBy         string            `json:"requested_by"`
	RequestedAt         time.Time         `json:"requested_at"`
	DecidedBy           string            `json:"decided_by,omitempty"`
	DecidedAt           *time.Time        `json:"decided_at,omitempty"`
	DecisionRationale   string            `json:"decision_rationale,omitempty"`
	Version             int               `json:"version"`
}

// overrideEvidenceKeys is the closed set an override_evidence object may
// carry. An unknown key is rejected rather than stored: evidence nobody can
// interpret is not evidence, and silently accepting it invites a client to
// invent a field that looks authoritative and is read by nothing.
var overrideEvidenceKeys = map[string]bool{
	"reason":               true,
	"proposed_tier":        true,
	"supporting_documents": true,
}

// ParseOverrideEvidence validates the shape of an override_evidence object and
// extracts the fields the override record needs.
func ParseOverrideEvidence(evidence map[string]any) (reason string, proposedTier RiskTier, documents []string, err error) {
	if len(evidence) == 0 {
		return "", "", nil, fmt.Errorf("override_evidence must not be empty")
	}
	for key := range evidence {
		if !overrideEvidenceKeys[key] {
			return "", "", nil, fmt.Errorf("override_evidence has unknown field %q", key)
		}
	}
	raw, ok := evidence["reason"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", "", nil, fmt.Errorf("override_evidence.reason is required")
	}
	reason = strings.TrimSpace(raw)

	tierRaw, ok := evidence["proposed_tier"].(string)
	if !ok || strings.TrimSpace(tierRaw) == "" {
		return "", "", nil, fmt.Errorf("override_evidence.proposed_tier is required")
	}
	// Accepted case-insensitively: the tier constants are lower case, and a
	// client that sends "LOW" means the same thing.
	proposedTier = RiskTier(strings.ToLower(strings.TrimSpace(tierRaw)))
	switch proposedTier {
	case RiskTierLow, RiskTierMedium, RiskTierHigh:
	default:
		return "", "", nil, fmt.Errorf("override_evidence.proposed_tier must be one of %s, %s, %s", RiskTierLow, RiskTierMedium, RiskTierHigh)
	}

	if docs, present := evidence["supporting_documents"]; present {
		list, ok := docs.([]any)
		if !ok {
			return "", "", nil, fmt.Errorf("override_evidence.supporting_documents must be an array of strings")
		}
		for _, item := range list {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return "", "", nil, fmt.Errorf("override_evidence.supporting_documents must be an array of non-empty strings")
			}
			documents = append(documents, strings.TrimSpace(text))
		}
	}
	return reason, proposedTier, documents, nil
}

// CDDScoreOverrideRepository is additive: an adapter that does not implement
// it cannot accept overrides at all, which is the safe default.
type CDDScoreOverrideRepository interface {
	CreateCDDScoreOverride(ctx context.Context, override *CDDScoreOverride) error
	GetCDDScoreOverride(ctx context.Context, id string) (*CDDScoreOverride, error)
	ListCDDScoreOverrides(ctx context.Context, customerID string, limit int) ([]CDDScoreOverride, error)
	DecideCDDScoreOverride(ctx context.Context, id string, status CDDOverrideStatus, decidedBy, rationale string, expectedVersion int) (*CDDScoreOverride, error)
}
