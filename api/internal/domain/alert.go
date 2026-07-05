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
	Suppressed        bool      `json:"suppressed"`
	SuppressionReason string    `json:"suppression_reason,omitempty"`
	// AggregationWindowStart, BatchRunID, and BatchReviewedAt support alert
	// deduplication across the realtime/batch evaluation paths
	// (transaction-monitoring.md「アラート統合ロジック」/「バッチ/リアルタイム
	// 評価の重複アラート防止」). AggregationWindowStart is nil for scenarios
	// with no aggregation window (e.g. single-transaction realtime checks),
	// which are exempt from the dedup constraint.
	AggregationWindowStart *time.Time `json:"aggregation_window_start,omitempty"`
	BatchRunID             string     `json:"batch_run_id,omitempty"`
	BatchReviewedAt        *time.Time `json:"batch_reviewed_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}
