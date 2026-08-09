package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryCaseInvestigationReplaceRelationshipPreservesHistory(t *testing.T) {
	repo := NewMemoryCaseInvestigationRepo()
	now := time.Now().UTC()
	original := &domain.CaseRelationship{
		ID: "relationship-original", CaseID: "case-1", RelatedCaseID: "case-2",
		RelationshipType: "same_customer", Rationale: "initial review", CreatedBy: "analyst-1", CreatedAt: now, Source: "manual",
	}
	if err := repo.AddRelationship(context.Background(), original); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}
	replacement := &domain.CaseRelationship{
		ID: "relationship-correction", CaseID: original.CaseID, RelatedCaseID: original.RelatedCaseID,
		RelationshipType: "shared_controller", Rationale: "corporate registry review", CreatedBy: "analyst-2", CreatedAt: now.Add(time.Second), Source: "manual",
	}
	if err := repo.ReplaceRelationship(context.Background(), original.ID, replacement, "analyst-2", "corrected relationship type"); err != nil {
		t.Fatalf("ReplaceRelationship: %v", err)
	}

	relationships, err := repo.ListRelationships(context.Background(), original.CaseID, true)
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(relationships) != 2 {
		t.Fatalf("relationships = %+v, want original plus replacement history", relationships)
	}
	if relationships[0].Active || relationships[0].RemovalReason != "corrected relationship type" {
		t.Fatalf("original relationship = %+v, want inactive with correction reason", relationships[0])
	}
	if !relationships[1].Active || relationships[1].ID != replacement.ID || relationships[1].Rationale != replacement.Rationale {
		t.Fatalf("replacement relationship = %+v, want active corrected link", relationships[1])
	}
}

func TestMemoryCaseInvestigationRemoveRelationshipPreservesHistoryAndUnlocks(t *testing.T) {
	repo := NewMemoryCaseInvestigationRepo()
	relationship := &domain.CaseRelationship{
		ID: "relationship-remove", CaseID: "case-1", RelatedCaseID: "case-2",
		RelationshipType: "related", Rationale: "same customer review", CreatedBy: "analyst-1",
		CreatedAt: time.Now().UTC(), Source: "manual",
	}
	if err := repo.AddRelationship(context.Background(), relationship); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}
	if err := repo.RemoveRelationship(context.Background(), relationship.ID, "analyst-2", "link no longer relevant"); err != nil {
		t.Fatalf("RemoveRelationship: %v", err)
	}

	history, err := repo.ListRelationships(context.Background(), relationship.CaseID, true)
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(history) != 1 || history[0].Active || history[0].RemovalReason != "link no longer relevant" {
		t.Fatalf("relationship history = %+v, want one inactive record with removal evidence", history)
	}
	if _, err := repo.ListRelationships(context.Background(), relationship.CaseID, false); err != nil {
		t.Fatalf("ListRelationships active-only after removal: %v", err)
	}
}

func TestPostgresInvestigationAndDecisionHistoryAreAppendOnly(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	var customerID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`,
		"investigation-history-"+newTestUUID()).Scan(&customerID); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	now := time.Now().UTC()
	caseID := "investigation-history-" + newTestUUID()
	if _, err := tx.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
		 VALUES ($1, $2, '{}', 'open', 'high', '', 'append-only investigation test', $3, $3)`, caseID, customerID, now); err != nil {
		t.Fatalf("create case: %v", err)
	}
	alertID := newTestUUID()
	if _, err := tx.Exec(ctx,
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
		 VALUES ($1, $2, 'append_only_test', 'high', 'open', 1, '', '{}', $3, $3, $3)`, alertID, customerID, now); err != nil {
		t.Fatalf("create alert: %v", err)
	}

	event := &domain.CaseEvent{
		ID: "case-event-" + newTestUUID(), CaseID: caseID, EventType: "test_event", Actor: "analyst-1",
		Reason: "original reason", Before: map[string]any{"status": "open"}, After: map[string]any{"status": "investigating"}, CreatedAt: now,
	}
	investigation := NewPgCaseInvestigationRepo(tx)
	if err := investigation.AppendEvent(ctx, event); err != nil {
		t.Fatalf("append case event: %v", err)
	}
	assertAppendOnly := func(savepoint, statement string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
			t.Fatalf("create savepoint %s: %v", savepoint, err)
		}
		if _, err := tx.Exec(ctx, statement, args...); err == nil {
			t.Fatalf("%s unexpectedly succeeded", statement)
		}
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
			t.Fatalf("rollback savepoint %s: %v", savepoint, err)
		}
	}
	assertAppendOnly("case_event_update", `UPDATE case_events SET reason='rewritten' WHERE id=$1`, event.ID)
	assertAppendOnly("case_event_delete", `DELETE FROM case_events WHERE id=$1`, event.ID)

	events, err := investigation.ListEvents(ctx, caseID)
	if err != nil {
		t.Fatalf("list case events: %v", err)
	}
	if len(events) != 1 || events[0].Reason != event.Reason || events[0].After["status"] != "investigating" {
		t.Fatalf("events = %+v, want original append-only event", events)
	}
	evidence := &domain.CaseEvidence{
		ID: "case-evidence-" + newTestUUID(), CaseID: caseID, Description: "original evidence", Source: "bank-api",
		EvidenceType: "statement", CollectedAt: now, CollectedBy: "analyst-1", Version: 1, CreatedAt: now,
	}
	if err := investigation.AddEvidence(ctx, evidence); err != nil {
		t.Fatalf("append evidence: %v", err)
	}
	assertAppendOnly("case_evidence_update", `UPDATE case_evidence SET description='rewritten' WHERE id=$1`, evidence.ID)
	assertAppendOnly("case_evidence_delete", `DELETE FROM case_evidence WHERE id=$1`, evidence.ID)
	evidenceRows, err := investigation.ListEvidence(ctx, caseID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidenceRows) != 1 || evidenceRows[0].Description != evidence.Description {
		t.Fatalf("evidence = %+v, want original append-only row", evidenceRows)
	}
	otherCaseID := "investigation-history-related-" + newTestUUID()
	if _, err := tx.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
		 VALUES ($1, $2, '{}', 'open', 'high', '', 'append-only related case', $3, $3)`, otherCaseID, customerID, now); err != nil {
		t.Fatalf("create related case: %v", err)
	}

	// These workflow columns were nullable from their first migration. A
	// legacy open checklist/work item/relationship must therefore round-trip
	// with nil completion/due/removal timestamps instead of failing a GET.
	checklist := &domain.CaseChecklistItem{
		CaseID: caseID, Key: "legacy-null-check", Label: "Legacy null check", CreatedAt: now, UpdatedAt: now,
	}
	if err := investigation.UpsertChecklist(ctx, checklist); err != nil {
		t.Fatalf("append nullable checklist: %v", err)
	}
	checklistRows, err := investigation.ListChecklist(ctx, caseID)
	if err != nil {
		t.Fatalf("list nullable checklist: %v", err)
	}
	if len(checklistRows) != 1 || checklistRows[0].CompletedAt != nil {
		t.Fatalf("nullable checklist rows = %+v, want nil completed_at", checklistRows)
	}

	workItem := &domain.CaseWorkItem{
		ID: "work-item-" + newTestUUID(), CaseID: caseID, Title: "Legacy null work item", Status: "open", CreatedAt: now, UpdatedAt: now,
	}
	if err := investigation.CreateWorkItem(ctx, workItem); err != nil {
		t.Fatalf("append nullable work item: %v", err)
	}
	workRows, err := investigation.ListWorkItems(ctx, caseID)
	if err != nil {
		t.Fatalf("list nullable work items: %v", err)
	}
	if len(workRows) != 1 || workRows[0].DueAt != nil || workRows[0].CompletedAt != nil {
		t.Fatalf("nullable work rows = %+v, want nil due/completed timestamps", workRows)
	}

	relationship := &domain.CaseRelationship{
		ID: "relationship-" + newTestUUID(), CaseID: caseID, RelatedCaseID: otherCaseID,
		RelationshipType: "related", Rationale: "legacy null relationship", CreatedBy: "analyst-1", CreatedAt: now, Source: "manual",
	}
	if err := investigation.AddRelationship(ctx, relationship); err != nil {
		t.Fatalf("append nullable relationship: %v", err)
	}
	relationshipRows, err := investigation.ListRelationships(ctx, caseID, true)
	if err != nil {
		t.Fatalf("list nullable relationships: %v", err)
	}
	if len(relationshipRows) != 1 || relationshipRows[0].RemovedAt != nil {
		t.Fatalf("nullable relationship rows = %+v, want nil removed_at", relationshipRows)
	}

	decision := &domain.AlertDecisionEvent{
		ID: "decision-event-" + newTestUUID(), AlertID: alertID, FromStatus: domain.AlertStatusOpen,
		ToStatus: domain.AlertStatusInvestigating, Outcome: "investigating", Rationale: "decision reason", Actor: "analyst-1", CreatedAt: now,
	}
	decisions := NewPgAlertDecisionRepo(tx)
	if err := decisions.CreateDecision(ctx, decision); err != nil {
		t.Fatalf("append alert decision: %v", err)
	}
	assertAppendOnly("decision_update", `UPDATE alert_decision_events SET rationale='rewritten' WHERE id=$1`, decision.ID)
	assertAppendOnly("decision_delete", `DELETE FROM alert_decision_events WHERE id=$1`, decision.ID)
	storedDecisions, err := decisions.ListDecisions(ctx, alertID)
	if err != nil {
		t.Fatalf("list alert decisions: %v", err)
	}
	if len(storedDecisions) != 1 || storedDecisions[0].Rationale != decision.Rationale {
		t.Fatalf("decisions = %+v, want original append-only decision", storedDecisions)
	}

	var auditID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id, created_at)
		 VALUES ('investigation-test', 'original', 'case', $1, $2) RETURNING id`, caseID, now).
		Scan(&auditID); err != nil {
		t.Fatalf("append audit log: %v", err)
	}
	assertAppendOnly("audit_log_update", `UPDATE audit_logs SET action='rewritten' WHERE id=$1`, auditID)
	assertAppendOnly("audit_log_delete", `DELETE FROM audit_logs WHERE id=$1`, auditID)

	ruleID := newTestUUID()
	if _, err := tx.Exec(ctx,
		`INSERT INTO rule_definitions (id, type, name, definition, version, is_active, created_by, created_at, updated_at)
		 VALUES ($1, 'TM_SCENARIO', $2, '{}', 1, true, 'investigation-test', $3, $3)`, ruleID, "append-only-"+newTestUUID(), now); err != nil {
		t.Fatalf("create rule definition: %v", err)
	}
	activationID := newTestUUID()
	if _, err := tx.Exec(ctx,
		`INSERT INTO rule_activation_events
		 (id, rule_definition_id, rule_name, rule_version, requested_active, rule_created_by, approved_by, created_at)
		 VALUES ($1, $2, 'append-only-rule', 1, true, 'maker', 'checker', $3)`, activationID, ruleID, now); err != nil {
		t.Fatalf("append rule activation event: %v", err)
	}
	assertAppendOnly("rule_activation_update", `UPDATE rule_activation_events SET changed=false WHERE id=$1`, activationID)
	assertAppendOnly("rule_activation_delete", `DELETE FROM rule_activation_events WHERE id=$1`, activationID)

	// Relationship and STR correction history are separate immutable streams;
	// their current rows are projections and must not be rewriteable either.
	relationshipEvent := &domain.CaseRelationshipEvent{
		ID: "relationship-event-" + newTestUUID(), RelationshipID: "relationship-" + newTestUUID(),
		CaseID: caseID, RelatedCaseID: otherCaseID, EventType: "added", Actor: "analyst-1",
		Reason: "relationship reason", Before: map[string]any{}, After: map[string]any{"active": true}, CorrelationID: "corr-relationship", CreatedAt: now,
	}
	if err := investigation.AppendRelationshipEvent(ctx, relationshipEvent); err != nil {
		t.Fatalf("append relationship event: %v", err)
	}
	assertAppendOnly("relationship_event_update", `UPDATE case_relationship_events SET reason='rewritten' WHERE id=$1`, relationshipEvent.ID)
	assertAppendOnly("relationship_event_delete", `DELETE FROM case_relationship_events WHERE id=$1`, relationshipEvent.ID)
	relationshipHistory, err := investigation.ListRelationshipEvents(ctx, relationshipEvent.RelationshipID)
	if err != nil {
		t.Fatalf("list relationship events: %v", err)
	}
	if len(relationshipHistory) != 1 || relationshipHistory[0].Reason != relationshipEvent.Reason {
		t.Fatalf("relationship history = %+v, want original append-only event", relationshipHistory)
	}

	report := &domain.STRReport{
		ID: "report-history-" + newTestUUID(), AlertID: alertID, CustomerID: customerID, CaseID: caseID,
		ReportType: domain.ReportTypeSTR, Status: domain.ReportStatusDraft, SuspiciousPoint: "report history reason",
		TotalAmount: 0, Currency: "JPY", CreatedAt: now, UpdatedAt: now, CreatedBy: "analyst-1",
	}
	strReports := NewPgSTRReportRepo(tx)
	if err := strReports.Create(ctx, report); err != nil {
		t.Fatalf("create STR report: %v", err)
	}
	reportEvent := &domain.STRReportEvent{
		ID: "str-report-event-" + newTestUUID(), ReportID: report.ID, EventType: "created", Actor: "analyst-1",
		Reason: "report reason", Before: map[string]any{}, After: map[string]any{"status": "draft"}, CorrelationID: "corr-report", CreatedAt: now,
	}
	if err := strReports.AppendReportEvent(ctx, reportEvent); err != nil {
		t.Fatalf("append STR report event: %v", err)
	}
	assertAppendOnly("str_report_event_update", `UPDATE str_report_events SET reason='rewritten' WHERE id=$1`, reportEvent.ID)
	assertAppendOnly("str_report_event_delete", `DELETE FROM str_report_events WHERE id=$1`, reportEvent.ID)
	reportHistory, err := strReports.ListReportEvents(ctx, report.ID)
	if err != nil {
		t.Fatalf("list STR report events: %v", err)
	}
	if len(reportHistory) != 1 || reportHistory[0].Reason != reportEvent.Reason {
		t.Fatalf("STR report history = %+v, want original append-only event", reportHistory)
	}
}
