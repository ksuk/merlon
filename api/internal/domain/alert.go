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
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
