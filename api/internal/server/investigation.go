package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

type caseWorkflowValidationError struct{ reason string }

func (e *caseWorkflowValidationError) Error() string { return e.reason }

func caseEventState(c domain.Case) map[string]any {
	return map[string]any{
		"status":                    c.Status,
		"priority":                  c.Priority,
		"assigned_to":               c.AssignedTo,
		"assigned_team":             c.AssignedTeam,
		"due_at":                    c.DueAt,
		"summary":                   c.Summary,
		"reopen_reason":             c.ReopenReason,
		"related_case_ids":          append([]string(nil), c.RelatedCaseIDs...),
		"investigation_disposition": c.InvestigationDisposition,
		"str_candidate":             c.STRCandidate,
		"str_report_id":             c.STRReportID,
		"str_filed_at":              c.STRFiledAt,
		"str_filed_by":              c.STRFiledBy,
		"str_filing_channel":        c.STRFilingChannel,
		"str_destination":           c.STRDestination,
		"str_external_reference":    c.STRExternalReference,
		"disposition_rationale":     c.DispositionRationale,
		"closed_at":                 c.ClosedAt,
	}
}

func evidenceEventState(e domain.CaseEvidence) map[string]any {
	return map[string]any{
		"id": e.ID, "case_id": e.CaseID, "root_id": e.RootID, "supersedes_id": e.SupersedesID, "description": e.Description, "source": e.Source,
		"evidence_type": e.EvidenceType, "collected_at": e.CollectedAt, "collected_by": e.CollectedBy,
		"integrity_hash": e.IntegrityHash, "version": e.Version, "created_at": e.CreatedAt,
	}
}

func workItemEventState(item domain.CaseWorkItem) map[string]any {
	return map[string]any{
		"id": item.ID, "case_id": item.CaseID, "title": item.Title, "description": item.Description,
		"status": item.Status, "assigned_to": item.AssignedTo, "due_at": item.DueAt,
		"completed_by": item.CompletedBy, "completed_at": item.CompletedAt, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
	}
}

func relationshipEventState(relationship domain.CaseRelationship) map[string]any {
	return map[string]any{
		"id": relationship.ID, "case_id": relationship.CaseID, "related_case_id": relationship.RelatedCaseID,
		"relationship_type": relationship.RelationshipType, "rationale": relationship.Rationale,
		"created_by": relationship.CreatedBy, "created_at": relationship.CreatedAt, "active": relationship.Active,
		"removed_by": relationship.RemovedBy, "removed_at": relationship.RemovedAt, "removal_reason": relationship.RemovalReason,
		"source": relationship.Source,
	}
}

func alertEventState(alert *domain.Alert) map[string]any {
	if alert == nil {
		return nil
	}
	return map[string]any{
		"alert_id": alert.ID, "customer_id": alert.CustomerID, "status": alert.Status,
		"resolved_at": alert.ResolvedAt, "resolved_by": alert.ResolvedBy,
		"disposition": alert.Disposition, "disposition_rationale": alert.DispositionRationale,
	}
}

func caseRationale(req updateCaseRequest) string {
	if req.Rationale != nil && strings.TrimSpace(*req.Rationale) != "" {
		return strings.TrimSpace(*req.Rationale)
	}
	if strings.TrimSpace(req.Reason) != "" {
		return strings.TrimSpace(req.Reason)
	}
	// Legacy status-only callers remain readable during the compatibility
	// window, but the server still records an explicit evidence value.
	return "legacy status transition"
}

func caseStateChanged(before, after map[string]any, key string) bool {
	return !reflect.DeepEqual(before[key], after[key])
}

func caseChangeReason(req updateCaseRequest) string {
	if req.Rationale != nil && strings.TrimSpace(*req.Rationale) != "" {
		return strings.TrimSpace(*req.Rationale)
	}
	if strings.TrimSpace(req.Reason) != "" {
		return strings.TrimSpace(req.Reason)
	}
	return "case field updated"
}

func nonEmptyIDs(id string) []string {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return []string{id}
}

func (s *Server) validateCaseSTRFiling(ctx context.Context, c *domain.Case, req updateCaseRequest) error {
	if strings.TrimSpace(req.STRReportID) == "" {
		return &caseWorkflowValidationError{reason: "a submitted str_report_id is required to file a case"}
	}
	if s.reports == nil {
		return &caseWorkflowValidationError{reason: "STR report storage is not configured"}
	}
	report, err := s.reports.Get(ctx, strings.TrimSpace(req.STRReportID))
	if err != nil {
		return err
	}
	if report.Status != domain.ReportStatusSubmitted {
		return &caseWorkflowValidationError{reason: "case can be filed only with a submitted STR report"}
	}
	if !domain.SameIdentifier(report.CaseID, c.ID) || !domain.SameIdentifier(report.CustomerID, c.CustomerID) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "STR report is linked to a different case or customer"}
	}
	linkedAlert := false
	for _, alertID := range c.AlertIDs {
		if domain.SameIdentifier(alertID, report.AlertID) {
			linkedAlert = true
			break
		}
	}
	if !linkedAlert {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "STR report source alert is not linked to the case"}
	}
	if strings.TrimSpace(req.STRFilingChannel) == "" || strings.TrimSpace(req.STRDestination) == "" || strings.TrimSpace(req.STRExternalReference) == "" {
		return &caseWorkflowValidationError{reason: "filing_channel, destination, and external_reference are required"}
	}
	return nil
}

func validateCaseSTRFilingWithReportRepo(ctx context.Context, reports domain.ReportRepository, c *domain.Case, req updateCaseRequest) error {
	if strings.TrimSpace(req.STRReportID) == "" {
		return &caseWorkflowValidationError{reason: "a submitted str_report_id is required to file a case"}
	}
	if reports == nil {
		return &caseWorkflowValidationError{reason: "STR report storage is not configured"}
	}
	report, err := reports.Get(ctx, domain.CanonicalIdentifier(req.STRReportID))
	if err != nil {
		return err
	}
	if report.Status != domain.ReportStatusSubmitted {
		return &caseWorkflowValidationError{reason: "case can be filed only with a submitted STR report"}
	}
	if !domain.SameIdentifier(report.CaseID, c.ID) || !domain.SameIdentifier(report.CustomerID, c.CustomerID) {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "STR report is linked to a different case or customer"}
	}
	linkedAlert := false
	for _, alertID := range c.AlertIDs {
		if domain.SameIdentifier(alertID, report.AlertID) {
			linkedAlert = true
			break
		}
	}
	if !linkedAlert {
		return &domain.ErrConflict{Entity: "case", ID: c.ID, Reason: "STR report source alert is not linked to the case"}
	}
	if strings.TrimSpace(req.STRFilingChannel) == "" || strings.TrimSpace(req.STRDestination) == "" || strings.TrimSpace(req.STRExternalReference) == "" {
		return &caseWorkflowValidationError{reason: "filing_channel, destination, and external_reference are required"}
	}
	return nil
}

type caseFileResponse struct {
	ExportVersion string                     `json:"export_version,omitempty"`
	ExportedAt    *time.Time                 `json:"exported_at,omitempty"`
	Case          domain.Case                `json:"case"`
	Events        []domain.CaseEvent         `json:"events"`
	Evidence      []domain.CaseEvidence      `json:"evidence"`
	Checklist     []domain.CaseChecklistItem `json:"checklist"`
	WorkItems     []domain.CaseWorkItem      `json:"work_items"`
	Relationships []domain.CaseRelationship  `json:"relationships"`
}

// caseTimelineResponse keeps the original case-file shape for unfiltered
// requests and adds pagination metadata only when the caller asks for a
// filtered/paged timeline. This preserves the additive contract of the
// existing /timeline endpoint while giving operators a bounded retrieval path.
type caseTimelineResponse struct {
	caseFileResponse
	EventPagination *PaginationMeta `json:"event_pagination,omitempty"`
}

var errTimelinePaginationUnavailable = errors.New("timeline pagination storage is not configured")

func caseEventCursor(event domain.CaseEvent) Cursor {
	return Cursor{CreatedAt: event.CreatedAt, ID: event.ID}
}

func eventAfterCursor(event domain.CaseEvent, cursor *Cursor) bool {
	if cursor == nil {
		return true
	}
	if event.CreatedAt.Equal(cursor.CreatedAt) {
		return event.ID > cursor.ID
	}
	return event.CreatedAt.After(cursor.CreatedAt)
}

func filterCaseTimelineEvents(events []domain.CaseEvent, r *http.Request) ([]domain.CaseEvent, *PaginationMeta, error) {
	query := r.URL.Query()
	allowedTypes := timelineEventTypes(r)
	filtered := make([]domain.CaseEvent, 0, len(events))
	for _, event := range events {
		if len(allowedTypes) > 0 {
			if _, ok := allowedTypes[event.EventType]; !ok {
				continue
			}
		}
		filtered = append(filtered, event)
	}

	// A plain event_type filter intentionally returns all matching events for
	// compatibility. Pagination is activated explicitly by limit, cursor, or
	// the deprecated offset parameter.
	if query.Get("limit") == "" && query.Get("cursor") == "" && query.Get("offset") == "" {
		return filtered, nil, nil
	}

	if query.Get("offset") != "" {
		limit, offset := parseOffsetLimit(r, defaultPageLimit)
		if offset > len(filtered) {
			offset = len(filtered)
		}
		end := offset + limit
		if end > len(filtered) {
			end = len(filtered)
		}
		page := filtered[offset:end]
		meta := PaginationMeta{HasMore: end < len(filtered)}
		if meta.HasMore && len(page) > 0 {
			meta.NextCursor = EncodeCursor(caseEventCursor(page[len(page)-1]))
		}
		return page, &meta, nil
	}

	pageRequest, err := ParsePageRequest(r)
	if err != nil {
		return nil, nil, &caseWorkflowValidationError{reason: "invalid timeline cursor"}
	}
	if pageRequest.Cursor != nil {
		paged := filtered[:0]
		for _, event := range filtered {
			if eventAfterCursor(event, pageRequest.Cursor) {
				paged = append(paged, event)
			}
		}
		filtered = paged
	}
	page, meta := BuildPaginationMeta(filtered, pageRequest.Limit, caseEventCursor)
	return page, &meta, nil
}

func timelineEventTypes(r *http.Request) map[string]struct{} {
	query := r.URL.Query()
	allowedTypes := make(map[string]struct{})
	for _, key := range []string{"event_type", "event_types"} {
		for _, raw := range query[key] {
			for _, value := range strings.Split(raw, ",") {
				if trimmed := strings.TrimSpace(value); trimmed != "" {
					allowedTypes[trimmed] = struct{}{}
				}
			}
		}
	}
	return allowedTypes
}

func (s *Server) caseFileBase(ctx context.Context, id string, includeInactive bool) (caseFileResponse, error) {
	if s.cases == nil {
		return caseFileResponse{}, errors.New("case management is not configured")
	}
	c, err := s.cases.Get(ctx, id)
	if err != nil {
		return caseFileResponse{}, err
	}
	file := caseFileResponse{Case: *c, Events: []domain.CaseEvent{}, Evidence: []domain.CaseEvidence{}, Checklist: []domain.CaseChecklistItem{}, WorkItems: []domain.CaseWorkItem{}, Relationships: []domain.CaseRelationship{}}
	if s.caseInvestigation != nil {
		if file.Evidence, err = s.caseInvestigation.ListEvidence(ctx, id); err != nil {
			return file, err
		}
		if file.Checklist, err = s.caseInvestigation.ListChecklist(ctx, id); err != nil {
			return file, err
		}
		if file.WorkItems, err = s.caseInvestigation.ListWorkItems(ctx, id); err != nil {
			return file, err
		}
		if file.Relationships, err = s.caseInvestigation.ListRelationships(ctx, id, includeInactive); err != nil {
			return file, err
		}
	}
	return file, nil
}

func (s *Server) caseFile(ctx context.Context, id string, includeInactive bool) (caseFileResponse, error) {
	file, err := s.caseFileBase(ctx, id, includeInactive)
	if err != nil {
		return file, err
	}
	if s.caseInvestigation != nil {
		file.Events, err = s.caseInvestigation.ListEvents(ctx, id)
		if err != nil {
			return file, err
		}
	}
	return file, nil
}

func (s *Server) handleCaseTimeline(w http.ResponseWriter, r *http.Request) {
	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	paged := r.URL.Query().Get("limit") != "" || r.URL.Query().Get("cursor") != "" || r.URL.Query().Get("offset") != ""
	var file caseFileResponse
	var err error
	if paged {
		file, err = s.caseFileBase(r.Context(), r.PathValue("id"), includeInactive)
	} else {
		file, err = s.caseFile(r.Context(), r.PathValue("id"), includeInactive)
	}
	if err != nil {
		writeCaseFileError(w, err)
		return
	}
	if r.URL.Query().Get("offset") != "" {
		setOffsetDeprecationHeaders(w)
	}
	if paged {
		pageRepo, ok := s.caseInvestigation.(domain.CaseEventPageRepository)
		if !ok {
			writeCaseFileError(w, errTimelinePaginationUnavailable)
			return
		}
		filter := domain.CaseEventPageFilter{}
		for eventType := range timelineEventTypes(r) {
			filter.EventTypes = append(filter.EventTypes, eventType)
		}
		sort.Strings(filter.EventTypes)
		var limit, offset int
		var after *domain.Cursor
		if r.URL.Query().Get("offset") != "" {
			limit, offset = parseOffsetLimit(r, defaultPageLimit)
		} else {
			pageRequest, parseErr := ParsePageRequest(r)
			if parseErr != nil {
				writeCaseFileError(w, &caseWorkflowValidationError{reason: "invalid timeline cursor"})
				return
			}
			limit = pageRequest.Limit
			after = toDomainCursor(pageRequest.Cursor)
		}
		events, pageErr := pageRepo.ListEventsPage(r.Context(), r.PathValue("id"), filter, limit+1, offset, after)
		if pageErr != nil {
			writeCaseFileError(w, pageErr)
			return
		}
		page, pagination := BuildPaginationMeta(events, limit, caseEventCursor)
		file.Events = page
		writeJSON(w, http.StatusOK, caseTimelineResponse{caseFileResponse: file, EventPagination: &pagination})
		return
	}
	events, pagination, err := filterCaseTimelineEvents(file.Events, r)
	if err != nil {
		writeCaseFileError(w, err)
		return
	}
	file.Events = events
	if pagination != nil {
		writeJSON(w, http.StatusOK, caseTimelineResponse{caseFileResponse: file, EventPagination: pagination})
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (s *Server) handleCaseFileExport(w http.ResponseWriter, r *http.Request) {
	file, err := s.caseFile(r.Context(), r.PathValue("id"), true)
	if err != nil {
		writeCaseFileError(w, err)
		return
	}
	exportedAt := time.Now().UTC()
	file.ExportVersion = "case-file-v1"
	file.ExportedAt = &exportedAt
	w.Header().Set("Content-Disposition", "attachment; filename=case_"+file.Case.ID+".json")
	writeJSON(w, http.StatusOK, file)
}

func writeCaseFileError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAtomicMutationUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	var notFound *domain.ErrNotFound
	if errors.As(err, &notFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
		return
	}
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
		return
	}
	var validation *caseWorkflowValidationError
	if errors.As(err, &validation) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, validation.Error())
		return
	}
	if errors.Is(err, errTimelinePaginationUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}

type createEvidenceRequest struct {
	Description   string     `json:"description"`
	Source        string     `json:"source"`
	EvidenceType  string     `json:"evidence_type"`
	CollectedAt   *time.Time `json:"collected_at"`
	CollectedBy   string     `json:"collected_by"`
	IntegrityHash string     `json:"integrity_hash"`
}

type correctEvidenceRequest struct {
	Description   string     `json:"description"`
	Source        string     `json:"source"`
	EvidenceType  string     `json:"evidence_type"`
	CollectedAt   *time.Time `json:"collected_at"`
	CollectedBy   string     `json:"collected_by"`
	IntegrityHash string     `json:"integrity_hash"`
	Reason        string     `json:"reason"`
}

func (s *Server) handleAddCaseEvidence(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	caseID := r.PathValue("id")
	if _, err := s.cases.Get(r.Context(), caseID); err != nil {
		writeCaseFileError(w, err)
		return
	}
	var req createEvidenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Description) == "" || strings.TrimSpace(req.Source) == "" || strings.TrimSpace(req.EvidenceType) == "" || strings.TrimSpace(req.CollectedBy) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "description, source, evidence_type, and collected_by are required")
		return
	}
	collectedAt := time.Now().UTC()
	if req.CollectedAt != nil {
		collectedAt = req.CollectedAt.UTC()
	}
	evidence := &domain.CaseEvidence{ID: generateID(), CaseID: caseID, Description: strings.TrimSpace(req.Description), Source: strings.TrimSpace(req.Source), EvidenceType: strings.TrimSpace(req.EvidenceType), CollectedAt: collectedAt, CollectedBy: strings.TrimSpace(req.CollectedBy), IntegrityHash: strings.TrimSpace(req.IntegrityHash), CreatedAt: time.Now().UTC()}
	var mutationErr error
	mutationErr = s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil {
			return errAtomicMutationUnavailable
		}
		if _, err := repos.Cases.Get(r.Context(), caseID); err != nil {
			return err
		}
		if err := repos.Investigation.AddEvidence(r.Context(), evidence); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: caseID, EventType: "evidence_added", After: evidenceEventState(*evidence),
		})
	})
	if mutationErr != nil {
		writeCaseFileError(w, mutationErr)
		return
	}
	writeJSON(w, http.StatusCreated, evidence)
}

func (s *Server) handleCorrectCaseEvidence(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	caseID, evidenceID := r.PathValue("id"), r.PathValue("evidence")
	if _, err := s.cases.Get(r.Context(), caseID); err != nil {
		writeCaseFileError(w, err)
		return
	}
	var req correctEvidenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "reason is required for an evidence correction")
		return
	}
	evidence, err := s.caseEvidenceByID(r.Context(), caseID, evidenceID)
	if err != nil {
		writeCaseFileError(w, err)
		return
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = evidence.Description
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = evidence.Source
	}
	evidenceType := strings.TrimSpace(req.EvidenceType)
	if evidenceType == "" {
		evidenceType = evidence.EvidenceType
	}
	collector := strings.TrimSpace(req.CollectedBy)
	if collector == "" {
		collector = evidence.CollectedBy
	}
	if description == "" || source == "" || evidenceType == "" || collector == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "description, source, evidence_type, and collected_by are required")
		return
	}
	collectedAt := evidence.CollectedAt
	if req.CollectedAt != nil {
		collectedAt = req.CollectedAt.UTC()
	}
	version := evidence.Version + 1
	if version <= 1 {
		version = 2
	}
	now := time.Now().UTC()
	replacement := &domain.CaseEvidence{
		ID: generateID(), CaseID: caseID, RootID: evidence.RootID, SupersedesID: evidence.ID, Description: description, Source: source, EvidenceType: evidenceType,
		CollectedAt: collectedAt, CollectedBy: collector, IntegrityHash: strings.TrimSpace(req.IntegrityHash),
		Version: version, CreatedAt: now,
	}
	_, ok := s.caseInvestigation.(domain.EvidenceCorrectionRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "evidence correction storage is not configured")
		return
	}
	var mutationErr error
	mutationErr = s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		txCorrection, ok := repos.Investigation.(domain.EvidenceCorrectionRepository)
		if !ok {
			return errAtomicMutationUnavailable
		}
		returnErr := txCorrection.CorrectEvidence(r.Context(), replacement, evidence.ID)
		if returnErr != nil {
			return returnErr
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{
			CaseID: caseID, EventType: "evidence_corrected", Reason: strings.TrimSpace(req.Reason),
			Before: evidenceEventState(*evidence), After: evidenceEventState(*replacement),
		})
	})
	if mutationErr != nil {
		writeCaseFileError(w, mutationErr)
		return
	}
	writeJSON(w, http.StatusCreated, replacement)
}

func (s *Server) caseEvidenceByID(ctx context.Context, caseID, evidenceID string) (*domain.CaseEvidence, error) {
	evidence, err := s.caseInvestigation.ListEvidence(ctx, caseID)
	if err != nil {
		return nil, err
	}
	for i := range evidence {
		if evidence[i].ID == evidenceID {
			copy := evidence[i]
			return &copy, nil
		}
	}
	return nil, &domain.ErrNotFound{Entity: "case_evidence", ID: evidenceID}
}

type checklistRequest struct {
	Label     string `json:"label"`
	Completed bool   `json:"completed"`
}

func (s *Server) handleUpdateCaseChecklist(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	caseID, key := r.PathValue("id"), r.PathValue("item")
	var req checklistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(req.Label) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "item and label are required")
		return
	}
	var item *domain.CaseChecklistItem
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if _, err := repos.Cases.Get(r.Context(), caseID); err != nil {
			return err
		}
		var before map[string]any
		items, err := repos.Investigation.ListChecklist(r.Context(), caseID)
		if err != nil {
			return err
		}
		itemID := caseID + ":" + key
		for i := range items {
			if items[i].Key == key {
				itemID = items[i].ID
				before = map[string]any{"item": items[i].Key, "label": items[i].Label, "completed": items[i].Completed, "version": items[i].Version}
				break
			}
		}
		now := time.Now().UTC()
		item = &domain.CaseChecklistItem{ID: itemID, CaseID: caseID, Key: key, Label: strings.TrimSpace(req.Label), Completed: req.Completed, CreatedAt: now, UpdatedAt: now}
		if req.Completed {
			item.CompletedBy = resolveAuditUserID(r)
			item.CompletedAt = &now
		}
		if err := repos.Investigation.UpsertChecklist(r.Context(), item); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{CaseID: caseID, EventType: "checklist_updated", Before: before, After: map[string]any{"item": key, "completed": req.Completed, "version": item.Version}})
	}); err != nil {
		writeCaseFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workItemRequest struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	AssignedTo  string     `json:"assigned_to"`
	DueAt       *time.Time `json:"due_at"`
}

var validCaseWorkItemStatuses = map[string]bool{
	"open":        true,
	"in_progress": true,
	"completed":   true,
	"cancelled":   true,
}

func validCaseWorkItemStatus(status string) bool {
	return validCaseWorkItemStatuses[status]
}

func (s *Server) handleCreateCaseWorkItem(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	caseID := r.PathValue("id")
	var req workItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "title is required")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "open"
	}
	if !validCaseWorkItemStatus(status) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "status must be open, in_progress, completed, or cancelled")
		return
	}
	var item *domain.CaseWorkItem
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if _, err := repos.Cases.Get(r.Context(), caseID); err != nil {
			return err
		}
		now := time.Now().UTC()
		item = &domain.CaseWorkItem{ID: generateID(), CaseID: caseID, Title: strings.TrimSpace(req.Title), Description: strings.TrimSpace(req.Description), Status: status, AssignedTo: strings.TrimSpace(req.AssignedTo), DueAt: req.DueAt, CreatedAt: now, UpdatedAt: now}
		if status == "completed" {
			item.CompletedBy = resolveAuditUserID(r)
			item.CompletedAt = &now
		}
		if err := repos.Investigation.CreateWorkItem(r.Context(), item); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{CaseID: caseID, EventType: "work_item_created", After: workItemEventState(*item)})
	}); err != nil {
		writeCaseFileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateCaseWorkItem(w http.ResponseWriter, r *http.Request) {
	if s.caseInvestigation == nil || s.atomic == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "case investigation is not configured")
		return
	}
	caseID, itemID := r.PathValue("id"), r.PathValue("item")
	var req workItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "title is required")
		return
	}
	var item *domain.CaseWorkItem
	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Cases == nil || repos.Investigation == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if _, err := repos.Cases.Get(r.Context(), caseID); err != nil {
			return err
		}
		items, err := repos.Investigation.ListWorkItems(r.Context(), caseID)
		if err != nil {
			return err
		}
		var beforeItem *domain.CaseWorkItem
		for i := range items {
			if items[i].ID == itemID {
				copy := items[i]
				beforeItem = &copy
				break
			}
		}
		if beforeItem == nil {
			return &domain.ErrNotFound{Entity: "case_work_item", ID: itemID}
		}
		now := time.Now().UTC()
		updated := *beforeItem
		updated.Title = strings.TrimSpace(req.Title)
		updated.Description = strings.TrimSpace(req.Description)
		updated.AssignedTo = strings.TrimSpace(req.AssignedTo)
		updated.DueAt = req.DueAt
		if strings.TrimSpace(req.Status) != "" {
			updated.Status = strings.TrimSpace(req.Status)
		}
		if !validCaseWorkItemStatus(updated.Status) {
			return &caseWorkflowValidationError{reason: "status must be open, in_progress, completed, or cancelled"}
		}
		if updated.Status == "completed" {
			if beforeItem.Status != "completed" || beforeItem.CompletedAt == nil {
				updated.CompletedBy = resolveAuditUserID(r)
				updated.CompletedAt = &now
			}
		} else {
			updated.CompletedBy = ""
			updated.CompletedAt = nil
		}
		updated.UpdatedAt = now
		item = &updated
		if err := repos.Investigation.UpdateWorkItem(r.Context(), item); err != nil {
			return err
		}
		return appendRequiredCaseEvent(r.Context(), r, repos, &domain.CaseEvent{CaseID: caseID, EventType: "work_item_updated", Before: workItemEventState(*beforeItem), After: workItemEventState(*item)})
	}); err != nil {
		writeCaseFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleListAlertDecisions(w http.ResponseWriter, r *http.Request) {
	if s.alertDecisions == nil || s.alerts == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert decision history is not configured")
		return
	}
	if _, err := s.alerts.Get(r.Context(), r.PathValue("id")); err != nil {
		writeCaseFileError(w, err)
		return
	}
	decisions, err := s.alertDecisions.ListDecisions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCaseFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decisions)
}
