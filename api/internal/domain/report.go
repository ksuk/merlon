package domain

import "time"

type ReportType string

const (
	ReportTypeSTR ReportType = "str"
)

type ReportStatus string

const (
	ReportStatusDraft     ReportStatus = "draft"
	ReportStatusSubmitted ReportStatus = "submitted"
)

type STRReport struct {
	ID              string       `json:"id"`
	AlertID         string       `json:"alert_id"`
	CustomerID      string       `json:"customer_id"`
	ReportType      ReportType   `json:"report_type"`
	Status          ReportStatus `json:"status"`
	SuspiciousPoint string       `json:"suspicious_point"`
	TransactionIDs  []string     `json:"transaction_ids"`
	TotalAmount     float64      `json:"total_amount"`
	Currency        string       `json:"currency"`
	CreatedAt       time.Time    `json:"created_at"`
	SubmittedAt     *time.Time   `json:"submitted_at,omitempty"`
	CreatedBy       string       `json:"created_by"`
}
