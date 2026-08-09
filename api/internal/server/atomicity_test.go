package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestCaseCreateRollsBackWhenTimelineAppendFails(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	investigation := s.caseInvestigation.(*store.MemoryCaseInvestigationRepo)
	investigation.SetAppendEventFailure(errors.New("case_events unavailable"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(`{"customer_id":"`+customer.ID+`","summary":"atomicity fixture"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("timeline failure returned success: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.cases.Get(req.Context(), ""); err == nil {
		t.Fatal("empty case id unexpectedly found")
	}
	cases, err := s.cases.ListByCustomer(req.Context(), customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0 {
		t.Fatalf("case rows after timeline failure = %d, want 0", len(cases))
	}
	events, err := investigation.ListEvents(req.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("timeline rows after failed create = %d, want 0", len(events))
	}
}

func TestCaseCreateRollsBackWhenAuditInsertFails(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	audit := s.audit.(*store.MemoryAuditRepo)
	audit.SetCreateFailure(errors.New("audit_logs unavailable"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases", strings.NewReader(`{"customer_id":"`+customer.ID+`","summary":"atomicity fixture"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("audit failure returned success: %d %s", rec.Code, rec.Body.String())
	}
	cases, err := s.cases.ListByCustomer(req.Context(), customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0 {
		t.Fatalf("case rows after audit failure = %d, want 0", len(cases))
	}
}

func TestAlertDecisionFailureRollsBackAlertStatus(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	alert := seedAlert(t, s, customer.ID)
	decisions := s.alertDecisions.(*store.MemoryAlertDecisionRepo)
	decisions.SetCreateFailure(errors.New("alert_decision_events unavailable"))

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alert.ID, strings.NewReader(`{"status":"investigating"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("decision failure returned success: %d %s", rec.Code, rec.Body.String())
	}
	current, err := s.alerts.Get(req.Context(), alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.AlertStatusOpen {
		t.Fatalf("alert status after decision failure = %s, want open", current.Status)
	}
	got, err := decisions.ListDecisions(req.Context(), alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("decision rows after failure = %d, want 0", len(got))
	}
}

func TestEvidenceCorrectionUsesCASAndPreservesLineage(t *testing.T) {
	repo := store.NewMemoryCaseInvestigationRepo()
	now := time.Now().UTC()
	v1 := &domain.CaseEvidence{ID: "evidence-v1", CaseID: "case-1", Description: "v1", Source: "source", EvidenceType: "document", CollectedAt: now, CollectedBy: "analyst", CreatedAt: now}
	if err := repo.AddEvidence(nil, v1); err != nil {
		t.Fatal(err)
	}
	v2 := &domain.CaseEvidence{ID: "evidence-v2", CaseID: "case-1", RootID: v1.RootID, SupersedesID: v1.ID, Version: 2, Description: "v2", Source: "source", EvidenceType: "document", CollectedAt: now, CollectedBy: "analyst", CreatedAt: now}
	if err := repo.CorrectEvidence(nil, v2, v1.ID); err != nil {
		t.Fatal(err)
	}
	loser := *v2
	loser.ID = "evidence-v2-loser"
	if err := repo.CorrectEvidence(nil, &loser, v1.ID); err == nil {
		t.Fatal("concurrent correction with stale current id succeeded")
	}
	items, err := repo.ListEvidence(nil, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Version != 1 || items[1].Version != 2 || items[1].SupersedesID != v1.ID || items[1].RootID != v1.ID {
		t.Fatalf("evidence lineage = %+v", items)
	}
}

func TestInvestigationMutationsRollbackTheirProjectionWhenTimelineFails(t *testing.T) {
	s := testServerFull()
	customer, _ := createTestCustomerAndAlert(t, s)
	now := time.Now().UTC()
	caseID := "atomic-investigation-" + generateID()
	if err := s.cases.Create(nil, &domain.Case{ID: caseID, CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityMedium, Summary: "atomic investigation", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	investigation := s.caseInvestigation.(*store.MemoryCaseInvestigationRepo)
	investigation.SetAppendEventFailure(errors.New("case_events unavailable"))

	checklistReq := httptest.NewRequest(http.MethodPut, "/api/v1/cases/"+caseID+"/checklist/source", strings.NewReader(`{"label":"Source reviewed","completed":true}`))
	checklistRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(checklistRec, checklistReq)
	if checklistRec.Code < 400 {
		t.Fatalf("checklist failure returned success: %d", checklistRec.Code)
	}
	items, err := investigation.ListChecklist(nil, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("checklist after failed timeline append = %+v", items)
	}

	workReq := httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseID+"/work-items", strings.NewReader(`{"title":"Call customer"}`))
	workRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(workRec, workReq)
	if workRec.Code < 400 {
		t.Fatalf("work-item failure returned success: %d", workRec.Code)
	}
	workItems, err := investigation.ListWorkItems(nil, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workItems) != 0 {
		t.Fatalf("work items after failed timeline append = %+v", workItems)
	}
}

func TestRelationshipMutationRollsBackWhenHistoryAppendFails(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	now := time.Now().UTC()
	caseID := "atomic-related-a-" + generateID()
	relatedID := "atomic-related-b-" + generateID()
	for _, id := range []string{caseID, relatedID} {
		if err := s.cases.Create(nil, &domain.Case{ID: id, CustomerID: customer.ID, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityMedium, Summary: id, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	investigation := s.caseInvestigation.(*store.MemoryCaseInvestigationRepo)
	investigation.SetAppendEventFailure(errors.New("relationship history unavailable"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cases/"+caseID+"/related", strings.NewReader(`{"related_case_id":"`+relatedID+`","rationale":"same controller"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("relationship failure returned success: %d", rec.Code)
	}
	stored, err := s.cases.Get(nil, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.RelatedCaseIDs) != 0 {
		t.Fatalf("related projection after history failure = %v", stored.RelatedCaseIDs)
	}
	relationships, err := investigation.ListRelationships(nil, caseID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 0 {
		t.Fatalf("relationship rows after history failure = %+v", relationships)
	}
}

func TestSTRSubmitAndFileRollbackWhenRequiredHistoryFails(t *testing.T) {
	s := testServerFull()
	customer, alert := createTestCustomerAndAlert(t, s)
	now := time.Now().UTC()
	caseID := "atomic-str-" + generateID()
	if err := s.cases.Create(nil, &domain.Case{ID: caseID, CustomerID: customer.ID, AlertIDs: []string{alert.ID}, Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityMedium, STRCandidate: true, Summary: "STR candidate", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(`{"alert_id":"`+alert.ID+`","case_id":"`+caseID+`","suspicious_point":"suspicious activity"}`))
	createRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create report status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var report domain.STRReport
	if err := json.NewDecoder(createRec.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}

	reports := s.reports.(*store.MemorySTRReportRepo)
	reports.SetReportEventFailure(errors.New("str report history unavailable"))
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str/"+report.ID+"/submit", strings.NewReader(`{"submission_evidence":"receipt-atomic"}`))
	submitRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(submitRec, submitReq)
	if submitRec.Code < 400 {
		t.Fatalf("submit history failure returned success: %d", submitRec.Code)
	}
	stored, err := reports.Get(nil, report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.ReportStatusDraft || stored.SubmittedAt != nil {
		t.Fatalf("report after failed submit = %+v, want draft", stored)
	}
	reports.SetReportEventFailure(nil)
	if _, err := reports.ListReportEvents(nil, report.ID); err != nil {
		t.Fatal(err)
	}

	// A submitted report can be filed, but a failed case timeline append must
	// roll back both the case and its linked alert transitions.
	submitReq = httptest.NewRequest(http.MethodPost, "/api/v1/reports/str/"+report.ID+"/submit", strings.NewReader(`{"submission_evidence":"receipt-atomic"}`))
	submitRec = httptest.NewRecorder()
	s.Handler().ServeHTTP(submitRec, submitReq)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit retry status = %d, body = %s", submitRec.Code, submitRec.Body.String())
	}
	investigation := s.caseInvestigation.(*store.MemoryCaseInvestigationRepo)
	investigation.SetAppendEventFailure(errors.New("case timeline unavailable"))
	fileReq := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+caseID, strings.NewReader(`{"status":"str_filed","str_report_id":"`+report.ID+`","rationale":"filed with regulator","confirm":true,"filing_channel":"api","destination":"jafic","external_reference":"receipt-atomic"}`))
	fileRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(fileRec, fileReq)
	if fileRec.Code < 400 {
		t.Fatalf("file timeline failure returned success: %d", fileRec.Code)
	}
	caseAfter, err := s.cases.Get(nil, caseID)
	if err != nil {
		t.Fatal(err)
	}
	alertAfter, err := s.alerts.Get(nil, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if caseAfter.Status != domain.CaseStatusInvestigating || alertAfter.Status != domain.AlertStatusOpen {
		t.Fatalf("file failure partially mutated case=%s alert=%s", caseAfter.Status, alertAfter.Status)
	}
}
