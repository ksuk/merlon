package domain

import (
	"context"
	"time"
)

type ReportType string

const (
	ReportTypeSTR ReportType = "str"
)

type ReportStatus string

const (
	ReportStatusDraft     ReportStatus = "draft"
	ReportStatusSubmitted ReportStatus = "submitted"
)

// STRTransactionSnapshot is the immutable transaction data copied into an STR
// at creation time. The source transaction remains linked by ID, but later
// edits or retention processing cannot change what the operator reviewed.
type STRTransactionSnapshot struct {
	ID                  string               `json:"id"`
	ExternalID          string               `json:"external_id"`
	Amount              float64              `json:"amount"`
	Currency            string               `json:"currency"`
	Direction           TransactionDirection `json:"direction"`
	CounterpartyID      string               `json:"counterparty_id,omitempty"`
	CounterpartyCountry string               `json:"counterparty_country,omitempty"`
	Channel             string               `json:"channel,omitempty"`
	AccountID           *string              `json:"account_id,omitempty"`
	Counterparty        *Counterparty        `json:"counterparty,omitempty"`
	Metadata            map[string]any       `json:"metadata,omitempty"`
	ExecutedAt          time.Time            `json:"executed_at"`
	CreatedAt           time.Time            `json:"created_at"`
}

// STRAlertSnapshot and STRCustomerSnapshot pin the source identity used by
// an operator when the durable report was created. The live alert/customer
// rows remain available for referential checks, but an export must not change
// merely because a later queue action edits those rows.
type STRAlertSnapshot struct {
	ID             string        `json:"id"`
	CustomerID     string        `json:"customer_id"`
	ScenarioID     string        `json:"scenario_id"`
	Severity       AlertSeverity `json:"severity"`
	Status         AlertStatus   `json:"status"`
	Score          float64       `json:"score"`
	Description    string        `json:"description"`
	TransactionIDs []string      `json:"transaction_ids"`
	DetectedAt     time.Time     `json:"detected_at"`
}

type STRCustomerSnapshot struct {
	ID           string       `json:"id"`
	ExternalID   string       `json:"external_id"`
	CustomerType CustomerType `json:"customer_type"`
	CountryCode  string       `json:"country_code"`
}

type STRReport struct {
	ID                  string                   `json:"id"`
	AlertID             string                   `json:"alert_id"`
	CustomerID          string                   `json:"customer_id"`
	CaseID              string                   `json:"case_id,omitempty"`
	CorrectsReportID    string                   `json:"corrects_report_id,omitempty"`
	SupersedesReportID  string                   `json:"supersedes_report_id,omitempty"`
	ReportType          ReportType               `json:"report_type"`
	Status              ReportStatus             `json:"status"`
	SuspiciousPoint     string                   `json:"suspicious_point"`
	AlertSnapshot       STRAlertSnapshot         `json:"alert_snapshot"`
	CustomerSnapshot    STRCustomerSnapshot      `json:"customer_snapshot"`
	TransactionIDs      []string                 `json:"transaction_ids"`
	TransactionSnapshot []STRTransactionSnapshot `json:"transaction_snapshot"`
	TotalAmount         float64                  `json:"total_amount"`
	Currency            string                   `json:"currency"`
	CreatedAt           time.Time                `json:"created_at"`
	UpdatedAt           time.Time                `json:"updated_at"`
	SubmittedAt         *time.Time               `json:"submitted_at,omitempty"`
	CreatedBy           string                   `json:"created_by"`
	SubmittedBy         string                   `json:"submitted_by,omitempty"`
	SubmissionEvidence  string                   `json:"submission_evidence,omitempty"`
}

// STRReportEvent is the immutable submission/correction history row. The
// current report is a projection for the API; lifecycle history is never
// reconstructed from the mutable row.
type STRReportEvent struct {
	ID            string         `json:"id"`
	ReportID      string         `json:"report_id"`
	EventType     string         `json:"event_type"`
	Actor         string         `json:"actor"`
	Reason        string         `json:"reason"`
	Before        map[string]any `json:"before,omitempty"`
	After         map[string]any `json:"after,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ReportListFilter is the stable read contract for the STR list endpoint.
// Cursor/Limit use the same (created_at, id) keyset convention as other
// operator lists; Offset remains available during the API migration window.
type ReportListFilter struct {
	Status     ReportStatus
	CustomerID string
	AlertID    string
	Cursor     *Cursor
	Limit      int
	Offset     int
}

type ReportRepository interface {
	Get(ctx context.Context, id string) (*STRReport, error)
	List(ctx context.Context, filter ReportListFilter) ([]STRReport, error)
	Create(ctx context.Context, report *STRReport) error
	// Update changes draft-only editable fields. Source links and snapshots
	// remain immutable; submitted reports cannot be overwritten.
	Update(ctx context.Context, report *STRReport) error
	// Submit performs the only draft -> submitted transition. Submission
	// evidence is the configured filing-process reference and is also the
	// idempotency key for a retry of the same submit request.
	Submit(ctx context.Context, id, submittedBy, submissionEvidence string) (*STRReport, error)
}

type ReportOptimisticLockRepository interface {
	UpdateIfUnmodified(ctx context.Context, report *STRReport, expectedUpdatedAt time.Time) error
}

type STRReportHistoryRepository interface {
	AppendReportEvent(ctx context.Context, event *STRReportEvent) error
}

func ValidReportStatusTransition(from, to ReportStatus) bool {
	return from == ReportStatusDraft && to == ReportStatusSubmitted
}
