package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// MemorySTRReportRepo is the development/test implementation of the durable
// STR lifecycle. It deliberately mirrors the PostgreSQL repository's
// immutability and retry semantics instead of being a permissive map.
type MemorySTRReportRepo struct {
	mu           sync.RWMutex
	data         map[string]*domain.STRReport
	events       []domain.STRReportEvent
	eventFailure error
}

func NewMemorySTRReportRepo() *MemorySTRReportRepo {
	return &MemorySTRReportRepo{data: make(map[string]*domain.STRReport)}
}

func (r *MemorySTRReportRepo) SetReportEventFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventFailure = err
}

func cloneSTRReportEvent(event domain.STRReportEvent) domain.STRReportEvent {
	event.Before = cloneAnyMap(event.Before)
	event.After = cloneAnyMap(event.After)
	return event
}

func normalizeReportEventIdentifiers(event *domain.STRReportEvent) {
	if event == nil {
		return
	}
	event.ID = domain.CanonicalIdentifier(event.ID)
	event.ReportID = domain.CanonicalIdentifier(event.ReportID)
}

func (r *MemorySTRReportRepo) AppendReportEvent(_ context.Context, event *domain.STRReportEvent) error {
	if event == nil {
		return fmt.Errorf("STR report event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("STR report event id is required")
	}
	normalizeReportEventIdentifiers(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eventFailure != nil {
		return r.eventFailure
	}
	for _, existing := range r.events {
		if existing.ID == event.ID {
			return &domain.ErrConflict{Entity: "str_report_event", ID: event.ID, Reason: "event already exists"}
		}
	}
	copy := cloneSTRReportEvent(*event)
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	r.events = append(r.events, copy)
	return nil
}

func (r *MemorySTRReportRepo) ListReportEvents(_ context.Context, reportID string) ([]domain.STRReportEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.STRReportEvent, 0)
	reportID = domain.CanonicalIdentifier(reportID)
	for _, event := range r.events {
		if reportID == "" || domain.SameIdentifier(event.ReportID, reportID) {
			result = append(result, cloneSTRReportEvent(event))
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// NewMemoryReportRepo is the short constructor used by callers that do not
// need to spell out the concrete report type.
func NewMemoryReportRepo() *MemorySTRReportRepo {
	return NewMemorySTRReportRepo()
}

func (r *MemorySTRReportRepo) Get(_ context.Context, id string) (*domain.STRReport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id = domain.CanonicalIdentifier(id)
	report, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "str_report", ID: id}
	}
	return cloneSTRReport(report), nil
}

func (r *MemorySTRReportRepo) List(_ context.Context, filter domain.ReportListFilter) ([]domain.STRReport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	reports := make([]domain.STRReport, 0, len(r.data))
	for _, report := range r.data {
		if filter.Status != "" && report.Status != filter.Status {
			continue
		}
		if filter.CustomerID != "" && !domain.SameIdentifier(report.CustomerID, filter.CustomerID) {
			continue
		}
		if filter.AlertID != "" && !domain.SameIdentifier(report.AlertID, filter.AlertID) {
			continue
		}
		reports = append(reports, *cloneSTRReport(report))
	}
	sort.Slice(reports, func(i, j int) bool {
		if !reports[i].CreatedAt.Equal(reports[j].CreatedAt) {
			return reports[i].CreatedAt.After(reports[j].CreatedAt)
		}
		return reports[i].ID > reports[j].ID
	})

	if filter.Cursor != nil {
		reports = filterReportsAfterCursor(reports, filter.Cursor)
	}
	if filter.Offset > 0 {
		if filter.Offset >= len(reports) {
			return []domain.STRReport{}, nil
		}
		reports = reports[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(reports) {
		reports = reports[:filter.Limit]
	}
	return reports, nil
}

func filterReportsAfterCursor(reports []domain.STRReport, cursor *domain.Cursor) []domain.STRReport {
	filtered := make([]domain.STRReport, 0, len(reports))
	for _, report := range reports {
		if report.CreatedAt.Before(cursor.CreatedAt) || (report.CreatedAt.Equal(cursor.CreatedAt) && report.ID < cursor.ID) {
			filtered = append(filtered, report)
		}
	}
	return filtered
}

func (r *MemorySTRReportRepo) Create(_ context.Context, report *domain.STRReport) error {
	if report == nil {
		return fmt.Errorf("str_report is nil")
	}
	if err := prepareSTRReportForCreate(report); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[report.ID]; exists {
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "report already exists"}
	}
	r.data[report.ID] = cloneSTRReport(report)
	return nil
}

func (r *MemorySTRReportRepo) Update(_ context.Context, report *domain.STRReport) error {
	if report == nil {
		return fmt.Errorf("str_report is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.data[report.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "str_report", ID: report.ID}
	}
	if current.Status != domain.ReportStatusDraft {
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "submitted report is immutable"}
	}
	updated := cloneSTRReport(current)
	updated.SuspiciousPoint = report.SuspiciousPoint
	updated.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	r.data[report.ID] = updated
	*report = *cloneSTRReport(updated)
	return nil
}

func (r *MemorySTRReportRepo) UpdateIfUnmodified(_ context.Context, report *domain.STRReport, expectedUpdatedAt time.Time) error {
	if report == nil {
		return fmt.Errorf("str_report is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.data[report.ID]
	if !ok {
		return &domain.ErrNotFound{Entity: "str_report", ID: report.ID}
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "updated_at mismatch"}
	}
	if current.Status != domain.ReportStatusDraft {
		return &domain.ErrConflict{Entity: "str_report", ID: report.ID, Reason: "submitted report is immutable"}
	}
	updated := cloneSTRReport(current)
	updated.SuspiciousPoint = report.SuspiciousPoint
	updated.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	r.data[report.ID] = updated
	*report = *cloneSTRReport(updated)
	return nil
}

func (r *MemorySTRReportRepo) Submit(_ context.Context, id, submittedBy, submissionEvidence string) (*domain.STRReport, error) {
	submissionEvidence = strings.TrimSpace(submissionEvidence)
	if submissionEvidence == "" {
		return nil, fmt.Errorf("submission evidence is required")
	}
	submittedBy = strings.TrimSpace(submittedBy)
	r.mu.Lock()
	defer r.mu.Unlock()
	id = domain.CanonicalIdentifier(id)
	current, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "str_report", ID: id}
	}
	if current.Status == domain.ReportStatusSubmitted {
		if current.SubmissionEvidence == submissionEvidence {
			return cloneSTRReport(current), nil
		}
		return nil, &domain.ErrConflict{Entity: "str_report", ID: id, Reason: "submitted report has different submission evidence"}
	}
	if current.Status != domain.ReportStatusDraft {
		return nil, &domain.ErrInvalidStateTransition{Entity: "str_report", ID: id, From: string(current.Status), To: string(domain.ReportStatusSubmitted)}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	updated := cloneSTRReport(current)
	updated.Status = domain.ReportStatusSubmitted
	updated.SubmittedAt = &now
	updated.UpdatedAt = now
	updated.SubmittedBy = submittedBy
	updated.SubmissionEvidence = submissionEvidence
	r.data[id] = updated
	return cloneSTRReport(updated), nil
}

func cloneSTRReport(report *domain.STRReport) *domain.STRReport {
	if report == nil {
		return nil
	}
	cp := *report
	cp.TransactionIDs = make([]string, len(report.TransactionIDs))
	copy(cp.TransactionIDs, report.TransactionIDs)
	cp.AlertSnapshot.TransactionIDs = append([]string(nil), report.AlertSnapshot.TransactionIDs...)
	cp.TransactionSnapshot = make([]domain.STRTransactionSnapshot, len(report.TransactionSnapshot))
	for i, snapshot := range report.TransactionSnapshot {
		cp.TransactionSnapshot[i] = snapshot
		if snapshot.AccountID != nil {
			accountID := *snapshot.AccountID
			cp.TransactionSnapshot[i].AccountID = &accountID
		}
		if snapshot.Counterparty != nil {
			counterparty := *snapshot.Counterparty
			cp.TransactionSnapshot[i].Counterparty = &counterparty
		}
		if snapshot.Metadata != nil {
			cp.TransactionSnapshot[i].Metadata = cloneAnyMap(snapshot.Metadata)
		}
	}
	if report.SubmittedAt != nil {
		submittedAt := *report.SubmittedAt
		cp.SubmittedAt = &submittedAt
	}
	return &cp
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneAnyValue(value)
	}
	return output
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneAnyValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func prepareSTRReportForCreate(report *domain.STRReport) error {
	if strings.TrimSpace(report.ID) == "" {
		return fmt.Errorf("str_report id is required")
	}
	report.ID = domain.CanonicalIdentifier(report.ID)
	report.AlertID = domain.CanonicalUUID(report.AlertID)
	report.CustomerID = domain.CanonicalUUID(report.CustomerID)
	report.CaseID = domain.CanonicalIdentifier(report.CaseID)
	report.CorrectsReportID = domain.CanonicalIdentifier(report.CorrectsReportID)
	report.SupersedesReportID = domain.CanonicalIdentifier(report.SupersedesReportID)
	if report.ReportType == "" {
		report.ReportType = domain.ReportTypeSTR
	}
	if report.ReportType != domain.ReportTypeSTR {
		return fmt.Errorf("unsupported STR report type: %s", report.ReportType)
	}
	if report.Status == "" {
		report.Status = domain.ReportStatusDraft
	}
	if report.Status != domain.ReportStatusDraft && report.Status != domain.ReportStatusSubmitted {
		return fmt.Errorf("unsupported STR report status: %s", report.Status)
	}
	report.SubmittedBy = strings.TrimSpace(report.SubmittedBy)
	report.SubmissionEvidence = strings.TrimSpace(report.SubmissionEvidence)
	if report.Status == domain.ReportStatusSubmitted &&
		(report.SubmittedAt == nil || report.SubmissionEvidence == "") {
		return fmt.Errorf("submitted STR report requires submitted_at and submission evidence")
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	// PostgreSQL timestamp columns retain microsecond precision. Normalize the
	// memory adapter and the immediate POST response to that same precision so
	// POST -> GET and memory -> PostgreSQL contracts are byte-stable.
	report.CreatedAt = report.CreatedAt.UTC().Truncate(time.Microsecond)
	if report.UpdatedAt.IsZero() {
		report.UpdatedAt = report.CreatedAt
	}
	report.UpdatedAt = report.UpdatedAt.UTC().Truncate(time.Microsecond)
	if report.SubmittedAt != nil {
		submittedAt := report.SubmittedAt.UTC().Truncate(time.Microsecond)
		report.SubmittedAt = &submittedAt
	}
	report.TransactionIDs = nonNilStrings(report.TransactionIDs)
	report.TransactionSnapshot = nonNilSTRTransactionSnapshots(report.TransactionSnapshot)
	report.AlertSnapshot.ID = domain.CanonicalUUID(report.AlertSnapshot.ID)
	report.AlertSnapshot.CustomerID = domain.CanonicalUUID(report.AlertSnapshot.CustomerID)
	for i := range report.AlertSnapshot.TransactionIDs {
		report.AlertSnapshot.TransactionIDs[i] = domain.CanonicalUUID(report.AlertSnapshot.TransactionIDs[i])
	}
	report.CustomerSnapshot.ID = domain.CanonicalUUID(report.CustomerSnapshot.ID)
	for i := range report.TransactionIDs {
		report.TransactionIDs[i] = domain.CanonicalUUID(report.TransactionIDs[i])
	}
	for i := range report.TransactionSnapshot {
		report.TransactionSnapshot[i].ID = domain.CanonicalUUID(report.TransactionSnapshot[i].ID)
		if report.TransactionSnapshot[i].AccountID != nil {
			value := domain.CanonicalUUID(*report.TransactionSnapshot[i].AccountID)
			report.TransactionSnapshot[i].AccountID = &value
		}
	}
	return nil
}
