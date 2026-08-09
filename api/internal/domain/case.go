package domain

import (
	"context"
	"time"
)

type CaseStatus string

const (
	// CaseStatusOpen is the legacy initial status. Contract Stability keeps
	// it accepted for 12 months as an alias of CaseStatusNew (the case-management workflow
	// §ケースのステータス遷移); normalizeCaseStatus treats the two identically.
	CaseStatusOpen          CaseStatus = "open"
	CaseStatusNew           CaseStatus = "new"
	CaseStatusInvestigating CaseStatus = "investigating"
	CaseStatusEscalated     CaseStatus = "escalated"
	CaseStatusClosed        CaseStatus = "closed"
	CaseStatusReopened      CaseStatus = "reopened"
	CaseStatusStrFiled      CaseStatus = "str_filed"
)

// normalizeCaseStatus collapses the legacy "open" alias to "new" so transition
// rules only need to be defined once.
func normalizeCaseStatus(s CaseStatus) CaseStatus {
	if s == CaseStatusOpen {
		return CaseStatusNew
	}
	return s
}

// caseStatusTransitions encodes the case-management workflow's status transition
// diagram. str_filed has no outgoing edges: it is a terminal state, and any
// new alert on the same customer becomes a separate case that references
// this one rather than reopening it.
var caseStatusTransitions = map[CaseStatus][]CaseStatus{
	CaseStatusNew:           {CaseStatusInvestigating},
	CaseStatusInvestigating: {CaseStatusEscalated, CaseStatusClosed, CaseStatusStrFiled},
	CaseStatusEscalated:     {CaseStatusInvestigating},
	CaseStatusClosed:        {CaseStatusReopened},
	CaseStatusReopened:      {CaseStatusInvestigating},
	CaseStatusStrFiled:      {},
}

// ValidCaseStatusTransition reports whether a case may move from "from" to
// "to" per the status transition diagram (the case-management workflow). The legacy
// "open" status is treated as equivalent to "new" on both sides.
func ValidCaseStatusTransition(from, to CaseStatus) bool {
	from = normalizeCaseStatus(from)
	to = normalizeCaseStatus(to)
	for _, allowed := range caseStatusTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// IsCaseUnresolved reports whether a case belongs in the active operator
// queue. The legacy "open" value is retained as an active alias of "new".
func IsCaseUnresolved(status CaseStatus) bool {
	switch normalizeCaseStatus(status) {
	case CaseStatusNew, CaseStatusInvestigating, CaseStatusEscalated, CaseStatusReopened:
		return true
	default:
		return false
	}
}

// IsCaseTerminal reports whether a case has reached a terminal disposition.
func IsCaseTerminal(status CaseStatus) bool {
	return status == CaseStatusClosed || status == CaseStatusStrFiled
}

// CompatibleCaseAlertState is the public storage invariant for a case and
// one of its linked alerts. Terminal cases may reference only terminal
// alerts. Active cases may retain terminal alerts as immutable investigation
// history after a reopen, as well as reference unresolved alerts.
func CompatibleCaseAlertState(caseStatus CaseStatus, alertStatus AlertStatus) bool {
	if IsCaseTerminal(caseStatus) {
		return IsAlertTerminal(alertStatus)
	}
	if !IsCaseUnresolved(caseStatus) {
		return false
	}
	return IsAlertUnresolved(alertStatus) || IsAlertTerminal(alertStatus)
}

// CanAttachAlertToCase is deliberately stricter than the storage invariant:
// a new link may be added only while both records are active. Terminal alerts
// can remain linked when a closed case is reopened, but cannot be introduced
// as new work.
func CanAttachAlertToCase(caseStatus CaseStatus, alertStatus AlertStatus) bool {
	return IsCaseUnresolved(caseStatus) && IsAlertUnresolved(alertStatus)
}

type CasePriority string

const (
	CasePriorityLow    CasePriority = "low"
	CasePriorityMedium CasePriority = "medium"
	CasePriorityHigh   CasePriority = "high"
	// CasePriorityCritical is used by EDD stage-3 escalation (the case-management workflow
	// §EDD未実施継続時の段階的措置: "ケースをCRITICALに引き上げる") and is
	// available for other WS's severity=CRITICAL case generation as well.
	CasePriorityCritical CasePriority = "critical"
)

type Case struct {
	ID             string       `json:"id"`
	CustomerID     string       `json:"customer_id"`
	AlertIDs       []string     `json:"alert_ids"`
	Status         CaseStatus   `json:"status"`
	Priority       CasePriority `json:"priority"`
	AssignedTo     string       `json:"assigned_to,omitempty"`
	AssignedTeam   string       `json:"assigned_team,omitempty"`
	DueAt          *time.Time   `json:"due_at,omitempty"`
	Summary        string       `json:"summary"`
	Notes          []CaseNote   `json:"notes,omitempty"`
	ReopenReason   string       `json:"reopen_reason,omitempty"`
	RelatedCaseIDs []string     `json:"related_case_ids,omitempty"`
	// InvestigationDisposition is deliberately separate from severity/priority:
	// an alert may be high-risk without an operator deciding that an STR is
	// warranted. STRCandidate is the review-stage flag; str_filed is reserved
	// for a validated submitted report.
	InvestigationDisposition string     `json:"investigation_disposition,omitempty"`
	STRCandidate             bool       `json:"str_candidate,omitempty"`
	DispositionRationale     string     `json:"disposition_rationale,omitempty"`
	STRReportID              string     `json:"str_report_id,omitempty"`
	STRFiledAt               *time.Time `json:"str_filed_at,omitempty"`
	STRFiledBy               string     `json:"str_filed_by,omitempty"`
	STRFilingChannel         string     `json:"str_filing_channel,omitempty"`
	STRDestination           string     `json:"str_destination,omitempty"`
	STRExternalReference     string     `json:"str_external_reference,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	ClosedAt                 *time.Time `json:"closed_at,omitempty"`
}

type CaseNote struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type CaseRepository interface {
	Get(ctx context.Context, id string) (*Case, error)
	ListByCustomer(ctx context.Context, customerID string) ([]Case, error)
	ListByCustomerOffset(ctx context.Context, customerID string, limit, offset int) ([]Case, error)
	ListByCustomerCursor(ctx context.Context, customerID string, limit int, after *Cursor) ([]Case, error)
	ListOpen(ctx context.Context, limit, offset int) ([]Case, error)
	ListOpenByCursor(ctx context.Context, limit int, after *Cursor) ([]Case, error)
	Create(ctx context.Context, c *Case) error
	Update(ctx context.Context, c *Case) error
	// UpdateIfUnmodified applies the same update as Update, but only if the
	// case's stored updated_at still equals expectedUpdatedAt (optimistic
	// locking, the data model §3.9: two concurrent updates must not silently
	// lose one to the other). Returns *ErrConflict on mismatch.
	UpdateIfUnmodified(ctx context.Context, c *Case, expectedUpdatedAt time.Time) error
	AddNote(ctx context.Context, caseID string, note *CaseNote) error
}
