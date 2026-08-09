package domain

import "time"

type AlertSeverity string

const (
	AlertSeverityLow      AlertSeverity = "low"
	AlertSeverityMedium   AlertSeverity = "medium"
	AlertSeverityHigh     AlertSeverity = "high"
	AlertSeverityCritical AlertSeverity = "critical"
)

type AlertStatus string

const (
	AlertStatusOpen                AlertStatus = "open"
	AlertStatusInvestigating       AlertStatus = "investigating"
	AlertStatusEscalated           AlertStatus = "escalated"
	AlertStatusClosedTruePositive  AlertStatus = "closed_true_positive"
	AlertStatusClosedFalsePositive AlertStatus = "closed_false_positive"
	// AlertStatusSuppressed marks an alert withheld by an active whitelist
	// entry (WL-004, whitelist.md §3/§7.3). The spec's status enum uses
	// upper-case values (NEW/INVESTIGATING/.../SUPPRESSED), but existing
	// AlertStatus values here are lower-case; this follows the established
	// lower-case convention rather than the spec's casing (Contract
	// Stability: existing values are not renamed to match).
	AlertStatusSuppressed AlertStatus = "suppressed"
)

// IsAlertUnresolved reports whether an alert belongs in an operator's active
// queue. Keep this set explicit: unknown future values must not silently look
// unresolved (fail-alert is enforced by rejecting unsupported transitions).
func IsAlertUnresolved(status AlertStatus) bool {
	switch status {
	case AlertStatusOpen, AlertStatusInvestigating, AlertStatusEscalated:
		return true
	default:
		return false
	}
}

// AllAlertStatuses is every status an alert can hold, for lookups that must
// span the whole history rather than the operator's active queue. Callers that
// want the queue default must leave AlertQueueFilter.Statuses empty instead.
func AllAlertStatuses() []AlertStatus {
	return []AlertStatus{
		AlertStatusOpen, AlertStatusInvestigating, AlertStatusEscalated,
		AlertStatusClosedTruePositive, AlertStatusClosedFalsePositive,
		AlertStatusSuppressed,
	}
}

// IsAlertTerminal reports whether an alert has an operator disposition. The
// disposition itself is immutable history, but an explicit, reasoned reopen
// may move the current alert back into investigation. The decision event
// repository retains the terminal decision that was superseded.
func IsAlertTerminal(status AlertStatus) bool {
	switch status {
	case AlertStatusClosedTruePositive, AlertStatusClosedFalsePositive:
		return true
	default:
		return false
	}
}

// ValidAlertStatusTransition encodes the operator lifecycle. Direct open ->
// terminal close remains deliberately disabled. A terminal alert may be
// reopened only into investigating; the API requires a new rationale and
// confirmation for that reversal.
func ValidAlertStatusTransition(from, to AlertStatus) bool {
	switch from {
	case AlertStatusOpen:
		return to == AlertStatusInvestigating || to == AlertStatusEscalated
	case AlertStatusInvestigating:
		return to == AlertStatusEscalated || IsAlertTerminal(to)
	case AlertStatusEscalated:
		return to == AlertStatusInvestigating || IsAlertTerminal(to)
	case AlertStatusClosedTruePositive, AlertStatusClosedFalsePositive:
		return to == AlertStatusInvestigating
	default:
		return false
	}
}

// ValidAlertStatusTransitionForCaseFiling is the explicit workflow exception
// for a submitted STR being filed. Filing is an audited case disposition, not
// an operator's ordinary alert queue transition; it may close an unresolved
// linked alert directly, while all other callers retain the normal lifecycle.
func ValidAlertStatusTransitionForCaseFiling(from, to AlertStatus) bool {
	return IsAlertUnresolved(from) && to == AlertStatusClosedTruePositive
}

type Alert struct {
	ID             string        `json:"id"`
	CustomerID     string        `json:"customer_id"`
	ScenarioID     string        `json:"scenario_id"`
	Severity       AlertSeverity `json:"severity"`
	Status         AlertStatus   `json:"status"`
	Score          float64       `json:"score"`
	Description    string        `json:"description"`
	TransactionIDs []string      `json:"transaction_ids"`
	DetectedAt     time.Time     `json:"detected_at"`
	ResolvedAt     *time.Time    `json:"resolved_at,omitempty"`
	ResolvedBy     string        `json:"resolved_by,omitempty"`
	// Suppressed and SuppressionReason record whitelist-driven suppression
	// (WL-004, whitelist.md §3.1/§7.3). SuppressionReason is "whitelist:{entry_id}"
	// when Suppressed is true.
	Suppressed        bool   `json:"suppressed"`
	SuppressionReason string `json:"suppression_reason,omitempty"`
	// AggregationWindowStart, BatchRunID, and BatchReviewedAt support alert
	// deduplication across the realtime/batch evaluation paths
	// (the transaction-monitoring design「アラート統合ロジック」/「バッチ/リアルタイム
	// 評価の重複アラート防止」). AggregationWindowStart is nil for scenarios
	// with no aggregation window (e.g. single-transaction realtime checks),
	// which are exempt from the dedup constraint.
	AggregationWindowStart *time.Time `json:"aggregation_window_start,omitempty"`
	BatchRunID             string     `json:"batch_run_id,omitempty"`
	BatchReviewedAt        *time.Time `json:"batch_reviewed_at,omitempty"`
	// Queue ownership and disposition evidence are first-class operator data.
	// Empty ownership means unassigned; terminal decisions retain their latest
	// rationale while the append-only decision repository keeps the full history.
	AssignedTo           string     `json:"assigned_to,omitempty"`
	AssignedTeam         string     `json:"assigned_team,omitempty"`
	DueAt                *time.Time `json:"due_at,omitempty"`
	Disposition          string     `json:"disposition,omitempty"`
	DispositionRationale string     `json:"disposition_rationale,omitempty"`
	// Provenance is the immutable record of what produced this detection. It
	// is nil on alerts generated before it was captured, which the API reports
	// as not_captured rather than filling in from current configuration.
	Provenance *AlertProvenance `json:"provenance,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// ProvenanceAvailability says whether the artifacts a provenance record points
// at can still be produced (PROV-01).
type ProvenanceAvailability string

const (
	// ProvenanceAvailable means the referenced rule version resolved.
	ProvenanceAvailable ProvenanceAvailability = "available"
	// ProvenanceRestricted means it resolved but its content is not returned:
	// the identifier and version travel, the body does not.
	ProvenanceRestricted ProvenanceAvailability = "restricted"
	// ProvenanceMissing means provenance was captured but the artifact it
	// names can no longer be resolved. This is reported, never reconstructed
	// from current configuration.
	ProvenanceMissing ProvenanceAvailability = "missing"
	// ProvenanceNotCaptured means the alert predates provenance capture. It is
	// a statement about the record, not about the rule.
	ProvenanceNotCaptured ProvenanceAvailability = "not_captured"
)

// AlertProvenance identifies the logic and inputs effective at detection time.
//
// It stores references, not copies. rule_definitions already holds immutable
// version rows, so duplicating a rule body here would create a second store
// with its own authorization and retention problems (ADR-0025, DR-19).
type AlertProvenance struct {
	// ScenarioID is the detection logic's own identifier, repeated here so the
	// provenance record is self-contained when read on its own.
	ScenarioID string `json:"scenario_id"`
	// ConfigDigests are the digests of every configuration document loaded by
	// the process that produced this alert.
	ConfigDigests map[string]string `json:"config_digests,omitempty"`
	EngineVersion string            `json:"engine_version,omitempty"`
	// EvaluationMode distinguishes a realtime detection from a batch or
	// recovery one; the same scenario can behave differently under each.
	EvaluationMode string     `json:"evaluation_mode,omitempty"`
	EvaluatedAt    *time.Time `json:"evaluated_at,omitempty"`
	WindowFrom     *time.Time `json:"window_from,omitempty"`
	WindowTo       *time.Time `json:"window_to,omitempty"`
	// AppliedThreshold is the single value that decided this detection for the
	// customer type and risk tier involved. It is public-safe: the alert
	// description already states it in prose. The rule body is not stored.
	AppliedThreshold *float64 `json:"applied_threshold,omitempty"`

	// The fields below are resolved at read time, never persisted, so a rule
	// version that is deleted or restricted after the fact is reported as it is
	// now rather than as it was when the alert was written.
	RuleName     string                 `json:"rule_name,omitempty"`
	RuleVersion  *int                   `json:"rule_version,omitempty"`
	RuleDigest   string                 `json:"rule_digest,omitempty"`
	Availability ProvenanceAvailability `json:"availability"`
}
