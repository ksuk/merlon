package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestSTRReportExportV1Golden(t *testing.T) {
	submittedAt := time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC)
	report := &domain.STRReport{
		ID: "report-golden-001", AlertID: "alert-golden-001", CustomerID: "customer-golden-001", CaseID: "case-golden-001",
		ReportType: domain.ReportTypeSTR, Status: domain.ReportStatusSubmitted, SuspiciousPoint: "golden suspicious point",
		AlertSnapshot: domain.STRAlertSnapshot{
			ID: "alert-golden-001", CustomerID: "customer-golden-001", ScenarioID: "scenario-golden", Severity: domain.AlertSeverityHigh,
			Status: domain.AlertStatusInvestigating, Score: 87.5, Description: "golden alert", TransactionIDs: []string{"transaction-golden-001"},
			DetectedAt: time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC),
		},
		CustomerSnapshot: domain.STRCustomerSnapshot{ID: "customer-golden-001", ExternalID: "EXT-GOLDEN-001", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP"},
		TransactionIDs:   []string{"transaction-golden-001"},
		TransactionSnapshot: []domain.STRTransactionSnapshot{{
			ID: "transaction-golden-001", ExternalID: "TX-GOLDEN-001", Amount: 1234.5, Currency: "JPY",
			Direction: domain.DirectionInbound, Channel: "web", ExecutedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
			CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
		}},
		TotalAmount: 1234.5, Currency: "JPY", CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: submittedAt, SubmittedAt: &submittedAt, SubmittedBy: "analyst01", SubmissionEvidence: "receipt-001",
	}

	t.Run("csv", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		testServerFull().exportSTRReportCSV(recorder, report, nil, nil)
		rows, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
		if err != nil {
			t.Fatalf("read CSV export: %v", err)
		}
		rows[1][len(rows[1])-1] = "<dynamic>"
		var normalized bytes.Buffer
		writer := csv.NewWriter(&normalized)
		if err := writer.WriteAll(rows); err != nil {
			t.Fatalf("normalize CSV export: %v", err)
		}
		assertSTRExportGolden(t, "testdata/str_export_v1.csv.golden", normalized.Bytes())
	})

	t.Run("json", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		testServerFull().exportSTRReportJSON(recorder, report, nil, nil)
		var exported map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &exported); err != nil {
			t.Fatalf("read JSON export: %v", err)
		}
		exported["exported_at"] = "<dynamic>"
		normalized, err := json.MarshalIndent(exported, "", "  ")
		if err != nil {
			t.Fatalf("normalize JSON export: %v", err)
		}
		normalized = append(normalized, '\n')
		assertSTRExportGolden(t, "testdata/str_export_v1.json.golden", normalized)
	})
}

func assertSTRExportGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("export differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

// The export contract is snapshot-stable: repeated exports of one report have
// identical reviewed content. exported_at is intentionally the only volatile
// field and is excluded from this comparison.
func normalizedSTRExport(t *testing.T, format string, body []byte) []byte {
	t.Helper()
	if format == "json" {
		var exported map[string]any
		if err := json.Unmarshal(body, &exported); err != nil {
			t.Fatalf("decode JSON export for stability check: %v", err)
		}
		delete(exported, "exported_at")
		normalized, err := json.Marshal(exported)
		if err != nil {
			t.Fatalf("normalize JSON export: %v", err)
		}
		return normalized
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("decode CSV export for stability check: %v", err)
	}
	if len(rows) != 2 || len(rows[1]) == 0 {
		t.Fatalf("CSV export rows = %d, want one data row", len(rows))
	}
	rows[1][len(rows[1])-1] = "<dynamic>"
	var normalized bytes.Buffer
	writer := csv.NewWriter(&normalized)
	if err := writer.WriteAll(rows); err != nil {
		t.Fatalf("normalize CSV export: %v", err)
	}
	return normalized.Bytes()
}

func TestSTRReportLifecycleAPI(t *testing.T) {
	s := testServerFull()
	_, alert := createTestCustomerAndAlert(t, s)
	now := time.Now().UTC()
	if err := s.cases.Create(context.Background(), &domain.Case{
		ID: "report-case-1", CustomerID: alert.CustomerID, AlertIDs: []string{alert.ID},
		Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh,
		STRCandidate: true,
		Summary:      "report lifecycle fixture", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create source case: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(
		`{"alert_id":"`+alert.ID+`","case_id":"report-case-1","suspicious_point":"initial rationale","created_by":"analyst01"}`,
	))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var report domain.STRReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if report.CaseID == "" || len(report.TransactionSnapshot) != 1 {
		t.Fatalf("create response = %+v, want case and transaction snapshot", report)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/"+report.ID, nil)
	getRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var fetched domain.STRReport
	if err := json.NewDecoder(getRec.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if fetched.ID != report.ID || fetched.AlertID != alert.ID {
		t.Fatalf("fetched = %+v, want report %s linked to %s", fetched, report.ID, alert.ID)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str?status=draft&limit=1", nil)
	listRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(listRec, list)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	listed, _ := decodeListResponse[domain.STRReport](t, listRec.Body)
	if len(listed) != 1 || listed[0].ID != report.ID {
		t.Fatalf("listed = %+v, want %s", listed, report.ID)
	}

	update := httptest.NewRequest(http.MethodPut, "/api/v1/reports/str/"+report.ID, strings.NewReader(
		`{"suspicious_point":"reviewed rationale"}`,
	))
	updateRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(updateRec, update)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRec.Code, updateRec.Body.String())
	}

	submit := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str/"+report.ID+"/submit", strings.NewReader(
		`{"submission_evidence":"filing-ref-62"}`,
	))
	submitRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(submitRec, submit)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit status = %d, body = %s", submitRec.Code, submitRec.Body.String())
	}
	var submitted domain.STRReport
	if err := json.NewDecoder(submitRec.Body).Decode(&submitted); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if submitted.Status != domain.ReportStatusSubmitted || submitted.SubmittedAt == nil || submitted.SubmissionEvidence != "filing-ref-62" {
		t.Fatalf("submitted = %+v", submitted)
	}

	// Same evidence makes a retry safe; a changed filing reference is a conflict.
	retry := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str/"+report.ID+"/submit", strings.NewReader(
		`{"submission_evidence":"filing-ref-62"}`,
	))
	retryRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(retryRec, retry)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("idempotent submit status = %d, body = %s", retryRec.Code, retryRec.Body.String())
	}

	conflicting := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str/"+report.ID+"/submit", strings.NewReader(
		`{"submission_evidence":"different-ref"}`,
	))
	conflictingRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(conflictingRec, conflicting)
	if conflictingRec.Code != http.StatusConflict {
		t.Fatalf("conflicting submit status = %d, body = %s", conflictingRec.Code, conflictingRec.Body.String())
	}

	// A correction is a new immutable report in the same case/customer/alert
	// lineage. The submitted original remains readable and is never overwritten.
	correctionCreate := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(
		`{"alert_id":"`+alert.ID+`","case_id":"report-case-1","suspicious_point":"corrected rationale","corrects_report_id":"`+report.ID+`","created_by":"analyst01"}`,
	))
	correctionCreateRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(correctionCreateRec, correctionCreate)
	if correctionCreateRec.Code != http.StatusCreated {
		t.Fatalf("correction create status = %d, body = %s", correctionCreateRec.Code, correctionCreateRec.Body.String())
	}
	var correction domain.STRReport
	if err := json.NewDecoder(correctionCreateRec.Body).Decode(&correction); err != nil {
		t.Fatalf("decode correction create response: %v", err)
	}
	if correction.CorrectsReportID != report.ID || correction.SupersedesReportID != report.ID {
		t.Fatalf("correction lineage = %+v, want parent %s", correction, report.ID)
	}
	correctionSubmit := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str/"+correction.ID+"/submit", strings.NewReader(
		`{"submission_evidence":"filing-ref-correction"}`,
	))
	correctionSubmitRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(correctionSubmitRec, correctionSubmit)
	if correctionSubmitRec.Code != http.StatusOK {
		t.Fatalf("correction submit status = %d, body = %s", correctionSubmitRec.Code, correctionSubmitRec.Body.String())
	}
	originalAfterCorrection, err := s.reports.Get(context.Background(), report.ID)
	if err != nil {
		t.Fatalf("read original after correction: %v", err)
	}
	if originalAfterCorrection.SuspiciousPoint != "reviewed rationale" || originalAfterCorrection.Status != domain.ReportStatusSubmitted {
		t.Fatalf("original after correction = %+v, want unchanged submitted history", originalAfterCorrection)
	}

	currentCase, err := s.cases.Get(context.Background(), report.CaseID)
	if err != nil {
		t.Fatalf("read case before filing: %v", err)
	}
	fileReq := httptest.NewRequest(http.MethodPatch, "/api/v1/cases/"+report.CaseID, strings.NewReader(
		`{"status":"str_filed","str_report_id":"`+correction.ID+`","rationale":"filed with regulator","confirm":true,"filing_channel":"api","destination":"jafic","external_reference":"receipt-64","expected_updated_at":"`+currentCase.UpdatedAt.Format(time.RFC3339Nano)+`"}`,
	))
	fileRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("file status = %d, body = %s", fileRec.Code, fileRec.Body.String())
	}
	var filedCase domain.Case
	if err := json.NewDecoder(fileRec.Body).Decode(&filedCase); err != nil {
		t.Fatalf("decode filed case: %v", err)
	}
	if filedCase.Status != domain.CaseStatusStrFiled || filedCase.STRReportID != correction.ID || filedCase.STRFiledAt == nil {
		t.Fatalf("filed case = %+v", filedCase)
	}
	if submitted.SubmittedAt == nil || filedCase.STRFiledAt.Equal(*submitted.SubmittedAt) {
		t.Fatalf("submitted_at and str_filed_at must represent distinct lifecycle moments: submitted=%v filed=%v", submitted.SubmittedAt, filedCase.STRFiledAt)
	}
	filedGet := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+report.CaseID, nil)
	filedGetRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(filedGetRec, filedGet)
	if filedGetRec.Code != http.StatusOK {
		t.Fatalf("filed case GET status = %d, body = %s", filedGetRec.Code, filedGetRec.Body.String())
	}
	var reloadedFiledCase domain.Case
	if err := json.NewDecoder(filedGetRec.Body).Decode(&reloadedFiledCase); err != nil {
		t.Fatalf("decode reloaded filed case: %v", err)
	}
	if reloadedFiledCase.STRFiledAt == nil || !reloadedFiledCase.STRFiledAt.Equal(*filedCase.STRFiledAt) {
		t.Fatalf("filed timestamp changed between PATCH and GET: response=%v get=%v", filedCase.STRFiledAt, reloadedFiledCase.STRFiledAt)
	}
	filedAlert, err := s.alerts.Get(context.Background(), alert.ID)
	if err != nil {
		t.Fatalf("read filed alert: %v", err)
	}
	if filedAlert.Status != domain.AlertStatusClosedTruePositive || filedAlert.ResolvedBy != "case-filing" {
		t.Fatalf("filed alert = %+v, want closed true-positive receipt", filedAlert)
	}
	decisions, ok := s.alertDecisions.(domain.AlertDecisionRepository)
	if !ok {
		t.Fatal("test server alert decision repository does not expose history")
	}
	decisionHistory, err := decisions.ListDecisions(context.Background(), alert.ID)
	if err != nil {
		t.Fatalf("read filing decision history: %v", err)
	}
	if len(decisionHistory) == 0 || decisionHistory[len(decisionHistory)-1].ToStatus != domain.AlertStatusClosedTruePositive {
		t.Fatalf("decision history after filing = %+v", decisionHistory)
	}

	timelineReq := httptest.NewRequest(http.MethodGet, "/api/v1/cases/"+report.CaseID+"/timeline", nil)
	timelineRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(timelineRec, timelineReq)
	if timelineRec.Code != http.StatusOK {
		t.Fatalf("report timeline status = %d, body = %s", timelineRec.Code, timelineRec.Body.String())
	}
	var timeline caseFileResponse
	if err := json.NewDecoder(timelineRec.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode report timeline: %v", err)
	}
	wantReportEvents := map[string]bool{
		"str_report_created":   false,
		"str_report_updated":   false,
		"str_report_submitted": false,
	}
	for _, event := range timeline.Events {
		if _, wanted := wantReportEvents[event.EventType]; !wanted {
			continue
		}
		wantReportEvents[event.EventType] = true
		reportReferenceOK := len(event.RelatedReportIDs) == 1 && (event.RelatedReportIDs[0] == report.ID || event.RelatedReportIDs[0] == correction.ID)
		if !reportReferenceOK || len(event.RelatedAlertIDs) != 1 || event.RelatedAlertIDs[0] != alert.ID {
			t.Errorf("%s related IDs = report %v alert %v, want report %s or correction %s and alert %s", event.EventType, event.RelatedReportIDs, event.RelatedAlertIDs, report.ID, correction.ID, alert.ID)
		}
	}
	for eventType, found := range wantReportEvents {
		if !found {
			t.Errorf("report timeline missing %s: %+v", eventType, timeline.Events)
		}
	}

	updateSubmitted := httptest.NewRequest(http.MethodPut, "/api/v1/reports/str/"+report.ID, strings.NewReader(
		`{"suspicious_point":"silent overwrite"}`,
	))
	updateSubmittedRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(updateSubmittedRec, updateSubmitted)
	if updateSubmittedRec.Code != http.StatusConflict {
		t.Fatalf("submitted update status = %d, body = %s", updateSubmittedRec.Code, updateSubmittedRec.Body.String())
	}

	notFound := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/missing-report", nil)
	notFoundRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(notFoundRec, notFound)
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("not-found status = %d, body = %s", notFoundRec.Code, notFoundRec.Body.String())
	}

	auditEntries, err := s.audit.List(context.Background(), domain.AuditListFilter{ResourceID: report.ID, Limit: 100})
	if err != nil {
		t.Fatalf("list report audit entries: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range auditEntries {
		if entry.ResourceType == "reports" {
			actions[entry.Action] = true
		}
	}
	if !actions["create_str"] || !actions["submit_str"] {
		t.Fatalf("report audit actions = %v, want create_str and submit_str", actions)
	}

}

func TestSTRReportExportUsesDurableSnapshot(t *testing.T) {
	s := testServerFull()
	customer, alert := createTestCustomerAndAlert(t, s)
	now := time.Now().UTC()
	if err := s.cases.Create(context.Background(), &domain.Case{
		ID: "report-export-case", CustomerID: customer.ID, AlertIDs: []string{alert.ID},
		Status: domain.CaseStatusInvestigating, Priority: domain.CasePriorityHigh, STRCandidate: true,
		Summary: "report export fixture", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create export source case: %v", err)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/v1/reports/str", strings.NewReader(
		`{"alert_id":"`+alert.ID+`","case_id":"report-export-case","suspicious_point":"snapshot rationale","created_by":"analyst01"}`,
	))
	createRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(createRec, create)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var report domain.STRReport
	if err := json.NewDecoder(createRec.Body).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}

	// A report export is keyed by the durable report, not by the current queue
	// projection. Change the live rows after creation and ensure both formats
	// still contain the evidence the operator originally reviewed.
	if err := s.alerts.UpdateStatus(context.Background(), alert.ID, domain.AlertStatusInvestigating, ""); err != nil {
		t.Fatalf("mutate live alert: %v", err)
	}
	customer.ExternalID = "STR001-MUTATED"
	if err := s.customers.Update(context.Background(), &customer); err != nil {
		t.Fatalf("mutate live customer: %v", err)
	}

	for _, format := range []string{"csv", "json"} {
		t.Run(format, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?report_id="+report.ID+"&format="+format, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("export status = %d, body = %s", rec.Code, rec.Body.String())
			}
			firstBody := append([]byte(nil), rec.Body.Bytes()...)

			if format == "json" {
				var exported strExportJSON
				if err := json.NewDecoder(bytes.NewReader(firstBody)).Decode(&exported); err != nil {
					t.Fatalf("decode JSON export: %v", err)
				}
				if exported.ReportID != report.ID || exported.Alert.Status != string(domain.AlertStatusOpen) || exported.Customer.ExternalID != "STR001" {
					t.Fatalf("JSON export = %+v, want durable alert/customer snapshot", exported)
				}
				if len(exported.TransactionIDs) != 1 || len(exported.Transactions) != 1 || exported.Transactions[0].ID != report.TransactionIDs[0] {
					t.Fatalf("JSON transactions = %+v, want durable transaction link", exported)
				}
			} else {
				rows, err := csv.NewReader(bytes.NewReader(firstBody)).ReadAll()
				if err != nil {
					t.Fatalf("decode CSV export: %v", err)
				}
				if len(rows) != 2 {
					t.Fatalf("CSV rows = %d, want header plus one report", len(rows))
				}
				columns := make(map[string]string, len(rows[0]))
				for index, header := range rows[0] {
					columns[header] = rows[1][index]
				}
				if columns["report_id"] != report.ID || columns["alert_status"] != string(domain.AlertStatusOpen) || columns["external_id"] != "STR001" {
					t.Fatalf("CSV columns = %v, want durable alert/customer snapshot", columns)
				}
			}

			secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?report_id="+report.ID+"&format="+format, nil)
			secondRec := httptest.NewRecorder()
			s.Handler().ServeHTTP(secondRec, secondReq)
			if secondRec.Code != http.StatusOK {
				t.Fatalf("second %s export status = %d, body = %s", format, secondRec.Code, secondRec.Body.String())
			}
			if !bytes.Equal(normalizedSTRExport(t, format, firstBody), normalizedSTRExport(t, format, secondRec.Body.Bytes())) {
				t.Fatalf("repeated %s export changed durable snapshot", format)
			}
		})
	}
}

func TestSTRLegacyExportRejectsMissingTransaction(t *testing.T) {
	s := testServerFull()
	customer := createTestCustomer(t, s)
	now := time.Now().UTC()
	alert := &domain.Alert{
		ID: "legacy-export-missing-transaction", CustomerID: customer.ID, ScenarioID: "legacy-export",
		Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusInvestigating, Score: 80,
		Description: "legacy export completeness test", TransactionIDs: []string{"missing-legacy-transaction"},
		DetectedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.alerts.Create(context.Background(), alert); err != nil {
		t.Fatalf("create legacy export alert: %v", err)
	}

	for _, format := range []string{"csv", "json"} {
		t.Run(format, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?alert_id="+alert.ID+"&format="+format, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("legacy export status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
			}
			if !strings.Contains(strings.ToLower(rec.Body.String()), "transaction") {
				t.Fatalf("legacy export error = %q, want explicit transaction completeness error", rec.Body.String())
			}
		})
	}
}

func TestSTRReportExportRejectsIncompleteSnapshot(t *testing.T) {
	s := testServerFull()
	_, alert := createTestCustomerAndAlert(t, s)
	report := &domain.STRReport{
		ID:              "report-incomplete-snapshot",
		AlertID:         alert.ID,
		CustomerID:      alert.CustomerID,
		ReportType:      domain.ReportTypeSTR,
		Status:          domain.ReportStatusDraft,
		SuspiciousPoint: "missing pinned evidence",
		TransactionIDs:  append([]string(nil), alert.TransactionIDs...),
		TotalAmount:     500000,
		Currency:        "JPY",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
		CreatedBy:       "analyst01",
	}
	if err := s.reports.Create(context.Background(), report); err != nil {
		t.Fatalf("create incomplete report: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?report_id="+report.ID, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("export status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "snapshot") {
		t.Fatalf("export error = %q, want explicit snapshot error", rec.Body.String())
	}
}

func TestSTRReportExportRejectsMismatchedSnapshotLinks(t *testing.T) {
	s := testServerFull()
	_, alert := createTestCustomerAndAlert(t, s)
	report := &domain.STRReport{
		ID: "report-mismatched-snapshot", AlertID: alert.ID, CustomerID: alert.CustomerID,
		ReportType: domain.ReportTypeSTR, Status: domain.ReportStatusDraft,
		SuspiciousPoint: "mismatched pinned evidence", TransactionIDs: []string{"wrong-txn"},
		TransactionSnapshot: []domain.STRTransactionSnapshot{{ID: alert.TransactionIDs[0], Currency: "JPY"}},
		AlertSnapshot:       domain.STRAlertSnapshot{ID: alert.ID, CustomerID: alert.CustomerID, TransactionIDs: append([]string(nil), alert.TransactionIDs...)},
		CustomerSnapshot:    domain.STRCustomerSnapshot{ID: alert.CustomerID},
		TotalAmount:         500000, Currency: "JPY", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), CreatedBy: "analyst01",
	}
	if err := s.reports.Create(context.Background(), report); err != nil {
		t.Fatalf("create report: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/str/export?report_id="+report.ID, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("export status = %d, want %d, body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
