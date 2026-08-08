package domain

import (
	"context"
	"time"
)

// CaseEvent is the append-only investigation timeline. Before/After are
// intentionally JSON-shaped so a new workflow field can be recorded without
// changing the event schema; callers must never mutate an existing event.
type CaseEvent struct {
	ID               string         `json:"id"`
	CaseID           string         `json:"case_id"`
	EventType        string         `json:"event_type"`
	Actor            string         `json:"actor"`
	Reason           string         `json:"reason,omitempty"`
	Before           map[string]any `json:"before,omitempty"`
	After            map[string]any `json:"after,omitempty"`
	RelatedAlertIDs  []string       `json:"related_alert_ids,omitempty"`
	RelatedCaseIDs   []string       `json:"related_case_ids,omitempty"`
	RelatedReportIDs []string       `json:"related_report_ids,omitempty"`
	CorrelationID    string         `json:"correlation_id,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

type CaseEvidence struct {
	ID            string    `json:"id"`
	CaseID        string    `json:"case_id"`
	RootID        string    `json:"root_id"`
	SupersedesID  string    `json:"supersedes_id,omitempty"`
	Description   string    `json:"description"`
	Source        string    `json:"source"`
	EvidenceType  string    `json:"evidence_type"`
	CollectedAt   time.Time `json:"collected_at"`
	CollectedBy   string    `json:"collected_by"`
	IntegrityHash string    `json:"integrity_hash,omitempty"`
	Version       int       `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
}

type CaseChecklistItem struct {
	ID          string     `json:"id"`
	CaseID      string     `json:"case_id"`
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Completed   bool       `json:"completed"`
	CompletedBy string     `json:"completed_by,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Version     int        `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CaseWorkItem struct {
	ID          string     `json:"id"`
	CaseID      string     `json:"case_id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`
	AssignedTo  string     `json:"assigned_to,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	CompletedBy string     `json:"completed_by,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CaseRelationship struct {
	ID               string     `json:"id"`
	CaseID           string     `json:"case_id"`
	RelatedCaseID    string     `json:"related_case_id"`
	RelationshipType string     `json:"relationship_type"`
	Rationale        string     `json:"rationale"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	Active           bool       `json:"active"`
	RemovedBy        string     `json:"removed_by,omitempty"`
	RemovedAt        *time.Time `json:"removed_at,omitempty"`
	RemovalReason    string     `json:"removal_reason,omitempty"`
	Source           string     `json:"source"` // auto or manual
}

// CaseRelationshipEvent is the immutable history row for manual relationship
// add/remove/correction operations. The current relationship table is a
// projection; this event stream is the audit source of truth.
type CaseRelationshipEvent struct {
	ID             string         `json:"id"`
	RelationshipID string         `json:"relationship_id"`
	CaseID         string         `json:"case_id"`
	RelatedCaseID  string         `json:"related_case_id"`
	EventType      string         `json:"event_type"`
	Actor          string         `json:"actor"`
	Reason         string         `json:"reason"`
	Before         map[string]any `json:"before,omitempty"`
	After          map[string]any `json:"after,omitempty"`
	CorrelationID  string         `json:"correlation_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type AlertDecisionEvent struct {
	ID           string      `json:"id"`
	AlertID      string      `json:"alert_id"`
	FromStatus   AlertStatus `json:"from_status"`
	ToStatus     AlertStatus `json:"to_status"`
	Outcome      string      `json:"outcome"`
	Rationale    string      `json:"rationale"`
	Actor        string      `json:"actor"`
	SupersedesID string      `json:"supersedes_id,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

// CaseEventRepository is the minimal timeline capability. The larger
// investigation repository embeds it and backs evidence, checklist, work,
// and relationship sections of the case file.
type CaseEventRepository interface {
	AppendEvent(ctx context.Context, event *CaseEvent) error
	ListEvents(ctx context.Context, caseID string) ([]CaseEvent, error)
}

type CaseEventPageFilter struct {
	EventTypes []string
}

// CaseEventPageRepository is required for explicit timeline pagination. The
// storage adapter applies filtering, keyset/offset bounds, and ordering in
// the database (or the bounded memory equivalent); the HTTP layer must not
// fetch an unbounded event stream and slice it after the fact.
type CaseEventPageRepository interface {
	ListEventsPage(ctx context.Context, caseID string, filter CaseEventPageFilter, limit, offset int, after *Cursor) ([]CaseEvent, error)
}

type CaseInvestigationRepository interface {
	CaseEventRepository
	AddEvidence(ctx context.Context, evidence *CaseEvidence) error
	ListEvidence(ctx context.Context, caseID string) ([]CaseEvidence, error)
	UpsertChecklist(ctx context.Context, item *CaseChecklistItem) error
	ListChecklist(ctx context.Context, caseID string) ([]CaseChecklistItem, error)
	CreateWorkItem(ctx context.Context, item *CaseWorkItem) error
	UpdateWorkItem(ctx context.Context, item *CaseWorkItem) error
	ListWorkItems(ctx context.Context, caseID string) ([]CaseWorkItem, error)
	AddRelationship(ctx context.Context, relationship *CaseRelationship) error
	ListRelationships(ctx context.Context, caseID string, includeInactive bool) ([]CaseRelationship, error)
	RemoveRelationship(ctx context.Context, id, actor, reason string) error
	ReplaceRelationship(ctx context.Context, currentID string, replacement *CaseRelationship, actor, reason string) error
}

// EvidenceCorrectionRepository provides compare-and-swap correction semantics
// without widening the base investigation adapter contract for older plugins.
type EvidenceCorrectionRepository interface {
	CorrectEvidence(ctx context.Context, evidence *CaseEvidence, expectedCurrentID string) error
}

type CaseRelationshipHistoryRepository interface {
	AppendRelationshipEvent(ctx context.Context, event *CaseRelationshipEvent) error
}

type AlertDecisionRepository interface {
	CreateDecision(ctx context.Context, event *AlertDecisionEvent) error
	ListDecisions(ctx context.Context, alertID string) ([]AlertDecisionEvent, error)
}

type AlertDispositionRepository interface {
	UpdateStatusWithRationale(ctx context.Context, id string, status AlertStatus, resolvedBy, rationale string, expectedUpdatedAt *time.Time) error
}

// AlertBulkDispositionRepository is the explicitly scoped exception for the
// bulk false-positive workflow. A bulk review may close an OPEN alert in one
// audited operation, while the individual status endpoint still requires the
// normal open -> investigating/escalated -> terminal transition.
type AlertBulkDispositionRepository interface {
	CloseFalsePositiveWithRationale(ctx context.Context, id, resolvedBy, rationale string, expectedUpdatedAt *time.Time) error
}

type AlertQueueFilter struct {
	CustomerID string
	ScenarioID string
	// TransactionID restricts the queue to alerts carrying this transaction in
	// transaction_ids. It is what makes "what did this transaction trigger?"
	// answerable server-side instead of by filtering a customer's page client
	// side, which loses records as soon as the customer has more than one page.
	TransactionID string
	Statuses      []AlertStatus
	Assignee      string
	Team          string
	Unassigned    bool
	Severity      AlertSeverity
	Search        string
	Overdue       bool
	MinAgeDays    int
	MaxAgeDays    int
	AsOf          time.Time
}

type AlertQueueRepository interface {
	ListQueue(ctx context.Context, filter AlertQueueFilter, limit, offset int) ([]Alert, error)
}

// AlertQueueCursorRepository is the stable keyset variant of the operator
// queue. Queue pages are risk-ranked, so the cursor must carry the severity
// rank in addition to updated_at/id.
type AlertQueueCursorRepository interface {
	ListQueueCursor(ctx context.Context, filter AlertQueueFilter, limit int, after *Cursor) ([]Alert, error)
}

type AlertQueueMutationRepository interface {
	UpdateQueue(ctx context.Context, id string, assignedTo, assignedTeam *string, dueAt *time.Time, expectedUpdatedAt *time.Time) error
}

type CaseQueueFilter struct {
	CustomerID string
	Statuses   []CaseStatus
	// AlertIDs restricts the queue to cases linked to any of these alerts.
	// A transaction reaches its cases through the alerts it raised, so the
	// HTTP layer resolves transaction_id into alert ids and passes them here
	// rather than every store re-implementing that join.
	AlertIDs     []string
	Assignee     string
	Team         string
	Unassigned   bool
	Priority     CasePriority
	Disposition  string
	STRCandidate *bool
	Search       string
	Overdue      bool
	MinAgeDays   int
	MaxAgeDays   int
	AsOf         time.Time
}

type CaseQueueRepository interface {
	ListQueue(ctx context.Context, filter CaseQueueFilter, limit, offset int) ([]Case, error)
}

// CaseQueueCursorRepository is the stable keyset variant of the operator
// queue. Queue pages are priority-ranked, so the cursor must carry the
// priority rank in addition to updated_at/id.
type CaseQueueCursorRepository interface {
	ListQueueCursor(ctx context.Context, filter CaseQueueFilter, limit int, after *Cursor) ([]Case, error)
}
