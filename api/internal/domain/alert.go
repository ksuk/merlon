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
	AlertStatusOpen              AlertStatus = "open"
	AlertStatusInvestigating     AlertStatus = "investigating"
	AlertStatusEscalated         AlertStatus = "escalated"
	AlertStatusClosedTruePositive  AlertStatus = "closed_true_positive"
	AlertStatusClosedFalsePositive AlertStatus = "closed_false_positive"
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
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}
