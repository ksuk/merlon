package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestCaseChangeEventsCaptureEveryChangedFieldAndCorrelation(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	caseRecord := &domain.Case{
		ID: "investigation-events-case", CustomerID: customer.ID, Status: domain.CaseStatusNew,
		Priority: domain.CasePriorityMedium, Summary: "initial summary", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatalf("create case: %v", err)
	}

	dueAt := now.Add(48 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseRecord.ID, strings.NewReader(`{"status":"investigating","priority":"critical","assigned_to":"analyst-2","assigned_team":"team-b","due_at":"`+dueAt+`","summary":"updated summary","investigation_disposition":"review","str_candidate":true,"rationale":"risk review started"}`))
	req.Header.Set("X-Request-ID", "wave2-case-change")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body: %s", rec.Code, rec.Body.String())
	}

	fileReq := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+caseRecord.ID+"/timeline", nil)
	fileRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, body: %s", fileRec.Code, fileRec.Body.String())
	}
	var file caseFileResponse
	if err := json.NewDecoder(fileRec.Body).Decode(&file); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}

	wantTypes := map[string]bool{
		"status_changed":       false,
		"assignment_changed":   false,
		"priority_changed":     false,
		"summary_changed":      false,
		"disposition_recorded": false,
	}
	for _, event := range file.Events {
		if _, wanted := wantTypes[event.EventType]; !wanted {
			continue
		}
		wantTypes[event.EventType] = true
		if event.CorrelationID != "wave2-case-change" {
			t.Errorf("%s correlation_id = %q, want wave2-case-change", event.EventType, event.CorrelationID)
		}
		if len(event.Before) == 0 || len(event.After) == 0 {
			t.Errorf("%s event omitted before/after state: %+v", event.EventType, event)
		}
	}
	for eventType, found := range wantTypes {
		if !found {
			t.Errorf("missing %s event; events = %+v", eventType, file.Events)
		}
	}
}

func TestCaseTimelineFiltersEventsAndPaginatesWithStableCursor(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	caseRecord := &domain.Case{
		ID: "timeline-filter-case", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating,
		Priority: domain.CasePriorityMedium, Summary: "timeline filter case",
		CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
	}
	if err := s.cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatalf("create case: %v", err)
	}
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	for _, event := range []domain.CaseEvent{
		{ID: "timeline-event-1", CaseID: caseRecord.ID, EventType: "status_changed", Actor: "analyst-1", CreatedAt: base, RelatedAlertIDs: []string{"alert-1"}},
		{ID: "timeline-event-2", CaseID: caseRecord.ID, EventType: "evidence_added", Actor: "analyst-1", CreatedAt: base.Add(time.Minute), RelatedCaseIDs: []string{"case-related-1"}},
		{ID: "timeline-event-3", CaseID: caseRecord.ID, EventType: "evidence_added", Actor: "analyst-2", CreatedAt: base.Add(2 * time.Minute), RelatedReportIDs: []string{"report-1"}},
	} {
		copy := event
		if err := s.caseInvestigation.AppendEvent(context.Background(), &copy); err != nil {
			t.Fatalf("append event %s: %v", event.ID, err)
		}
	}

	requestPage := func(cursor string) ([]domain.CaseEvent, PaginationMeta) {
		path := "/api/v1/cases/" + caseRecord.ID + "/timeline?event_type=evidence_added&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("timeline page status = %d, body: %s", rec.Code, rec.Body.String())
		}
		var response struct {
			Events          []domain.CaseEvent `json:"events"`
			EventPagination PaginationMeta     `json:"event_pagination"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode timeline page: %v", err)
		}
		return response.Events, response.EventPagination
	}

	page1, meta1 := requestPage("")
	if len(page1) != 1 || page1[0].ID != "timeline-event-2" || !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatalf("page1 = %+v, pagination = %+v, want first matching event and cursor", page1, meta1)
	}
	page2, meta2 := requestPage(meta1.NextCursor)
	if len(page2) != 1 || page2[0].ID != "timeline-event-3" || meta2.HasMore {
		t.Fatalf("page2 = %+v, pagination = %+v, want final matching event", page2, meta2)
	}

	offsetReq := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+caseRecord.ID+"/timeline?event_type=evidence_added&limit=1&offset=1", nil)
	offsetRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(offsetRec, offsetReq)
	if offsetRec.Code != http.StatusOK || offsetRec.Header().Get("Deprecation") != "true" || offsetRec.Header().Get("Sunset") == "" {
		t.Fatalf("offset response = status %d, deprecation=%q, sunset=%q; want deprecated pagination headers", offsetRec.Code, offsetRec.Header().Get("Deprecation"), offsetRec.Header().Get("Sunset"))
	}
}

func TestCaseEvidenceCorrectionAppendsVersionAndTimelineEvent(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	caseRecord := &domain.Case{
		ID: "evidence-correction-case", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating,
		Priority: domain.CasePriorityMedium, Summary: "evidence correction case", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatalf("create case: %v", err)
	}

	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseRecord.ID+"/evidence", strings.NewReader(`{"description":"original statement","source":"bank-api","evidence_type":"statement","collected_by":"analyst-1"}`))
	addRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add evidence status = %d, body: %s", addRec.Code, addRec.Body.String())
	}
	var original domain.CaseEvidence
	if err := json.NewDecoder(addRec.Body).Decode(&original); err != nil {
		t.Fatalf("decode original evidence: %v", err)
	}

	missingReason := httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseRecord.ID+"/evidence/"+original.ID+"/corrections", strings.NewReader(`{"description":"corrected statement"}`))
	missingReasonRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(missingReasonRec, missingReason)
	if missingReasonRec.Code != http.StatusBadRequest {
		t.Fatalf("missing correction reason status = %d, body: %s", missingReasonRec.Code, missingReasonRec.Body.String())
	}

	correctReq := httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseRecord.ID+"/evidence/"+original.ID+"/corrections", strings.NewReader(`{"description":"corrected statement","reason":"replaced by verified source"}`))
	correctRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(correctRec, correctReq)
	if correctRec.Code != http.StatusCreated {
		t.Fatalf("correct evidence status = %d, body: %s", correctRec.Code, correctRec.Body.String())
	}
	var corrected domain.CaseEvidence
	if err := json.NewDecoder(correctRec.Body).Decode(&corrected); err != nil {
		t.Fatalf("decode corrected evidence: %v", err)
	}
	if corrected.ID == original.ID || corrected.Version != original.Version+1 || corrected.Description != "corrected statement" {
		t.Fatalf("corrected evidence = %+v, want a new incremented version", corrected)
	}

	file, err := s.caseFile(context.Background(), caseRecord.ID, false)
	if err != nil {
		t.Fatalf("load case file: %v", err)
	}
	if len(file.Evidence) != 2 || file.Evidence[0].ID != original.ID || file.Evidence[0].Description != original.Description {
		t.Fatalf("evidence history = %+v, want original and corrected records", file.Evidence)
	}
	var foundCorrection bool
	for _, event := range file.Events {
		if event.EventType != "evidence_corrected" {
			continue
		}
		foundCorrection = true
		if event.Reason != "replaced by verified source" || event.Before["id"] != original.ID || event.After["id"] != corrected.ID {
			t.Fatalf("correction event = %+v, want before/after references and reason", event)
		}
	}
	if !foundCorrection {
		t.Fatalf("case events = %+v, want evidence_corrected", file.Events)
	}
	auditEntries, err := s.audit.List(context.Background(), domain.AuditListFilter{ResourceType: "cases", ResourceID: caseRecord.ID, Limit: 100})
	if err != nil {
		t.Fatalf("list case audit: %v", err)
	}
	var correlated bool
	for _, entry := range auditEntries {
		if entry.Action == "case_event_evidence_corrected" && entry.Details["event_id"] != "" {
			correlated = true
			break
		}
	}
	if !correlated {
		t.Fatalf("case audit entries = %+v, want correlated evidence correction event", auditEntries)
	}
}

func TestCaseFileExportV1GoldenPreservesEventOrderAndReferences(t *testing.T) {
	s := testServerFull()
	customer := domain.Customer{
		ID: "customer-golden", ExternalID: "EXT-GOLDEN", CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", ProductTypes: []string{}, Attributes: map[string]any{},
		CreatedAt: time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC),
	}
	if err := s.customers.Create(context.Background(), &customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	caseRecord := &domain.Case{
		ID: "case-file-golden", CustomerID: customer.ID, AlertIDs: []string{"alert-golden"},
		RelatedCaseIDs: []string{"case-related-golden"}, Status: domain.CaseStatusInvestigating,
		Priority: domain.CasePriorityHigh, Summary: "golden case file",
		CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC),
	}
	if err := s.cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatalf("create case: %v", err)
	}
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	for _, event := range []domain.CaseEvent{
		{ID: "case-file-event-1", CaseID: caseRecord.ID, EventType: "created", Actor: "analyst-1", Reason: "case opened", CreatedAt: base, RelatedAlertIDs: []string{"alert-golden"}},
		{ID: "case-file-event-2", CaseID: caseRecord.ID, EventType: "str_filed", Actor: "analyst-2", Reason: "filing confirmed", CreatedAt: base.Add(time.Minute), RelatedCaseIDs: []string{"case-related-golden"}, RelatedReportIDs: []string{"report-golden"}},
	} {
		copy := event
		if err := s.caseInvestigation.AppendEvent(context.Background(), &copy); err != nil {
			t.Fatalf("append event %s: %v", event.ID, err)
		}
	}
	collectedAt := base.Add(3 * time.Minute)
	if err := s.caseInvestigation.AddEvidence(context.Background(), &domain.CaseEvidence{
		ID: "case-file-evidence-1", CaseID: caseRecord.ID, Description: "bank statement",
		Source: "bank-api", EvidenceType: "transaction_record", CollectedAt: collectedAt,
		CollectedBy: "analyst-1", IntegrityHash: "sha256:golden", Version: 1, CreatedAt: collectedAt,
	}); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	if err := s.caseInvestigation.UpsertChecklist(context.Background(), &domain.CaseChecklistItem{
		ID: "case-file-check-1", CaseID: caseRecord.ID, Key: "cdd", Label: "CDD reviewed", Completed: true,
		CompletedBy: "analyst-1", CompletedAt: &collectedAt, Version: 1, CreatedAt: collectedAt, UpdatedAt: collectedAt,
	}); err != nil {
		t.Fatalf("add checklist: %v", err)
	}
	if err := s.caseInvestigation.CreateWorkItem(context.Background(), &domain.CaseWorkItem{
		ID: "case-file-work-1", CaseID: caseRecord.ID, Title: "Confirm filing receipt", Status: "completed",
		AssignedTo: "analyst-2", CompletedBy: "analyst-2", CompletedAt: &collectedAt, CreatedAt: collectedAt, UpdatedAt: collectedAt,
	}); err != nil {
		t.Fatalf("add work item: %v", err)
	}
	if err := s.caseInvestigation.AddRelationship(context.Background(), &domain.CaseRelationship{
		ID: "case-file-relationship-1", CaseID: caseRecord.ID, RelatedCaseID: "case-related-golden",
		RelationshipType: "same_customer", Rationale: "same customer review", CreatedBy: "analyst-1",
		CreatedAt: collectedAt, Active: true, Source: "manual",
	}); err != nil {
		t.Fatalf("add relationship: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+caseRecord.ID+"/export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("case file export status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode case file export: %v", err)
	}
	payload["exported_at"] = "<dynamic>"
	got, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal normalized case file: %v", err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/case_file_v1.json.golden")
	if err != nil {
		t.Fatalf("read case file golden: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("case file golden mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestCaseWorkItemCompletionPreservesCreationAndRejectsUnknownStatus(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	caseRecord := &domain.Case{
		ID: "work-item-case", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating,
		Priority: domain.CasePriorityMedium, Summary: "work item case", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.cases.Create(context.Background(), caseRecord); err != nil {
		t.Fatalf("create case: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseRecord.ID+"/work-items", strings.NewReader(`{"title":"Collect statement","status":"open"}`))
	createRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create work item status = %d, body: %s", createRec.Code, createRec.Body.String())
	}
	var created domain.CaseWorkItem
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created work item: %v", err)
	}

	badReq := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseRecord.ID+"/work-items/"+created.ID, strings.NewReader(`{"title":"Collect statement","status":"unknown"}`))
	badRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("unknown status = %d, want %d, body: %s", badRec.Code, http.StatusBadRequest, badRec.Body.String())
	}

	completeReq := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseRecord.ID+"/work-items/"+created.ID, strings.NewReader(`{"title":"Collect statement","status":"completed"}`))
	completeRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete work item status = %d, body: %s", completeRec.Code, completeRec.Body.String())
	}
	var completed domain.CaseWorkItem
	if err := json.NewDecoder(completeRec.Body).Decode(&completed); err != nil {
		t.Fatalf("decode completed work item: %v", err)
	}
	if completed.CreatedAt.IsZero() || !completed.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at = %v, want original %v", completed.CreatedAt, created.CreatedAt)
	}
	if completed.Status != "completed" || completed.CompletedAt == nil || completed.CompletedBy == "" {
		t.Fatalf("completion metadata = %+v", completed)
	}
}

func TestCaseQueueFiltersCandidateAndAge(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	now := time.Now().UTC()
	fixtures := []domain.Case{
		{ID: "queue-candidate-old", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, STRCandidate: true, Summary: "candidate old", CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour)},
		{ID: "queue-non-candidate-old", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, Summary: "not candidate old", CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour)},
		{ID: "queue-candidate-new", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, STRCandidate: true, Summary: "candidate new", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
	}
	for i := range fixtures {
		if err := s.cases.Create(context.Background(), &fixtures[i]); err != nil {
			t.Fatalf("create fixture %s: %v", fixtures[i].ID, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cases?str_candidate=true&min_age_days=2&limit=20", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue status = %d, body: %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeListResponse[domain.Case](t, rec.Body)
	if len(items) != 1 || items[0].ID != "queue-candidate-old" {
		t.Fatalf("queue items = %+v, want only old candidate", items)
	}
}

func TestCaseQueueCursorPaginationRetainsFiltersAcrossPages(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fixtures := []domain.Case{
		{ID: "queue-case-newest", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, STRCandidate: true, Summary: "matching case", CreatedAt: base, UpdatedAt: base},
		{ID: "queue-case-middle", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, STRCandidate: true, Summary: "matching case", CreatedAt: base.Add(-time.Hour), UpdatedAt: base.Add(-time.Hour)},
		{ID: "queue-case-oldest", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, STRCandidate: true, Summary: "matching case", CreatedAt: base.Add(-2 * time.Hour), UpdatedAt: base.Add(-2 * time.Hour)},
		{ID: "queue-case-excluded", CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, Summary: "must be filtered", CreatedAt: base, UpdatedAt: base},
	}
	for i := range fixtures {
		if err := s.cases.Create(context.Background(), &fixtures[i]); err != nil {
			t.Fatalf("create case %s: %v", fixtures[i].ID, err)
		}
	}

	requestPage := func(cursor string) ([]domain.Case, PaginationMeta) {
		path := "/api/v1/cases?customer_id=" + url.QueryEscape(customer.ID) + "&active=true&str_candidate=true&limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("queue page status = %d, body: %s", rec.Code, rec.Body.String())
		}
		return decodeListResponse[domain.Case](t, rec.Body)
	}

	page1, meta1 := requestPage("")
	if len(page1) != 2 || !meta1.HasMore || meta1.NextCursor == "" {
		t.Fatalf("page1 = %+v, pagination = %+v, want two matching cases and a cursor", page1, meta1)
	}
	page2, meta2 := requestPage(meta1.NextCursor)
	if len(page2) != 1 || meta2.HasMore {
		t.Fatalf("page2 = %+v, pagination = %+v, want final matching case", page2, meta2)
	}
	seen := map[string]bool{}
	for _, kase := range append(page1, page2...) {
		if seen[kase.ID] {
			t.Fatalf("cursor pagination duplicated case %s", kase.ID)
		}
		seen[kase.ID] = true
	}
	for _, id := range []string{"queue-case-newest", "queue-case-middle", "queue-case-oldest"} {
		if !seen[id] {
			t.Errorf("cursor pagination omitted matching case %s", id)
		}
	}
	if seen["queue-case-excluded"] {
		t.Error("cursor pagination returned a case outside the composed filters")
	}
}

func TestCaseDispositionRationaleIsPersistedAndAlertDecisionIsRecorded(t *testing.T) {
	s := testServerFull()
	customer, alert := createTestCustomerAndAlert(t, s)
	caseRecord := &domain.Case{
		ID: "investigation-disposition-case", CustomerID: customer.ID, AlertIDs: []string{alert.ID},
		Status: domain.CaseStatusNew, Priority: domain.CasePriorityMedium, Summary: "disposition case",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.caseAlertLifecycle.CreateCaseWithAlerts(context.Background(), caseRecord); err != nil {
		t.Fatalf("create case with alert: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseRecord.ID, strings.NewReader(`{"status":"investigating","investigation_disposition":"review","str_candidate":true,"rationale":"linked alert requires enhanced review"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body: %s", rec.Code, rec.Body.String())
	}

	updated, err := s.cases.Get(context.Background(), caseRecord.ID)
	if err != nil {
		t.Fatalf("get updated case: %v", err)
	}
	if updated.DispositionRationale != "linked alert requires enhanced review" {
		t.Fatalf("disposition rationale = %q, want persisted rationale", updated.DispositionRationale)
	}

	decisions, err := s.alertDecisions.ListDecisions(context.Background(), alert.ID)
	if err != nil {
		t.Fatalf("list alert decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].FromStatus != domain.AlertStatusOpen || decisions[0].ToStatus != domain.AlertStatusInvestigating {
		t.Fatalf("alert decisions = %+v, want one open-to-investigating decision", decisions)
	}
}
