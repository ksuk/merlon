package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func newLifecycleTestReport(id, customerID, alertID string, createdAt time.Time) *domain.STRReport {
	return &domain.STRReport{
		ID:              id,
		AlertID:         alertID,
		CustomerID:      customerID,
		CaseID:          "case-1",
		ReportType:      domain.ReportTypeSTR,
		Status:          domain.ReportStatusDraft,
		SuspiciousPoint: "structuring pattern",
		TransactionIDs:  []string{"txn-1"},
		TransactionSnapshot: []domain.STRTransactionSnapshot{{
			ID: "txn-1", Amount: 100, Currency: "JPY",
			Metadata: map[string]any{"nested": map[string]any{"reviewed": false}},
		}},
		TotalAmount: 100,
		Currency:    "JPY",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
		CreatedBy:   "analyst-1",
	}
}

func TestMemorySTRReportRepoLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := NewMemorySTRReportRepo()
	createdAt := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
	report := newLifecycleTestReport("report-1", "customer-1", "alert-1", createdAt)

	if err := repo.Create(ctx, report); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, report.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CaseID != report.CaseID || len(got.TransactionSnapshot) != 1 {
		t.Fatalf("Get = %+v, want stable source links and transaction snapshot", got)
	}

	// Reads must not hand out mutable slices that can alter the stored filing.
	got.TransactionIDs[0] = "mutated"
	got.TransactionSnapshot[0].Amount = 999
	got.TransactionSnapshot[0].Metadata["nested"].(map[string]any)["reviewed"] = true
	unchanged, err := repo.Get(ctx, report.ID)
	if err != nil {
		t.Fatalf("Get after mutation: %v", err)
	}
	if unchanged.TransactionIDs[0] != "txn-1" || unchanged.TransactionSnapshot[0].Amount != 100 ||
		unchanged.TransactionSnapshot[0].Metadata["nested"].(map[string]any)["reviewed"] != false {
		t.Fatal("Get exposed mutable repository state")
	}

	listed, err := repo.List(ctx, domain.ReportListFilter{Status: domain.ReportStatusDraft, Limit: 10})
	if err != nil {
		t.Fatalf("List drafts: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != report.ID {
		t.Fatalf("List drafts = %+v, want report-1", listed)
	}

	report.SuspiciousPoint = "updated rationale"
	if err := repo.Update(ctx, report); err != nil {
		t.Fatalf("Update draft: %v", err)
	}
	updated, err := repo.Get(ctx, report.ID)
	if err != nil || updated.SuspiciousPoint != "updated rationale" {
		t.Fatalf("updated report = %+v, err=%v", updated, err)
	}

	submitted, err := repo.Submit(ctx, report.ID, " analyst-1 ", " filing-ref-1 ")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if submitted.Status != domain.ReportStatusSubmitted || submitted.SubmittedAt == nil ||
		submitted.SubmittedBy != "analyst-1" || submitted.SubmissionEvidence != "filing-ref-1" {
		t.Fatalf("submitted report = %+v", submitted)
	}

	// A retry with the same evidence is idempotent and returns the persisted row.
	retried, err := repo.Submit(ctx, report.ID, "analyst-1", "filing-ref-1")
	if err != nil {
		t.Fatalf("duplicate Submit: %v", err)
	}
	if !retried.SubmittedAt.Equal(*submitted.SubmittedAt) {
		t.Fatal("duplicate Submit changed submitted_at")
	}

	_, err = repo.Submit(ctx, report.ID, "analyst-1", "different-ref")
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("different duplicate Submit error = %v, want ErrConflict", err)
	}

	report.SuspiciousPoint = "silent overwrite"
	if err := repo.Update(ctx, report); !errors.As(err, &conflict) {
		t.Fatalf("Update submitted error = %v, want ErrConflict", err)
	}
}

func TestPostgresSTRReportRepoLifecycle(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgSTRReportRepo(pool)
	customerID := compactUUID(seedTestCustomer(t, pool))
	txnID := newTestUUID()
	alertID := newTestUUID()
	reportID := newTestUUID()
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := pool.Exec(ctx, `INSERT INTO transactions (id, customer_id, external_id, amount, currency, direction, executed_at, created_at)
		VALUES ($1, $2, $3, 100, 'JPY', 'inbound', $4, $4)`, txnID, customerID, "report-txn-"+txnID, now); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
		VALUES ($1, $2, 'report-test', 'high', 'investigating', 90, 'report test', $3, $4, $4, $4)`, alertID, customerID, []string{txnID}, now); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	report := newLifecycleTestReport(reportID, customerID, alertID, now)
	// The source-case foreign key is optional for this isolated repository test;
	// the memory contract above separately covers the stable case link.
	report.CaseID = ""
	report.TransactionIDs = []string{txnID}
	report.TransactionSnapshot[0].ID = txnID
	report.AlertSnapshot = domain.STRAlertSnapshot{
		ID: alertID, CustomerID: customerID, TransactionIDs: []string{txnID},
	}
	report.CustomerSnapshot = domain.STRCustomerSnapshot{ID: customerID, ExternalID: "report-customer", CountryCode: "JP"}
	if err := repo.Create(ctx, report); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM str_reports WHERE id = $1`, reportID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, alertID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM transactions WHERE id = $1`, txnID)
	})

	got, err := repo.Get(ctx, reportID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AlertID != alertID || got.CustomerID != customerID || got.CaseID != report.CaseID ||
		got.AlertSnapshot.ID != alertID || got.AlertSnapshot.CustomerID != customerID ||
		got.AlertSnapshot.TransactionIDs[0] != txnID || got.CustomerSnapshot.ID != customerID ||
		got.TransactionSnapshot[0].ID != txnID || len(got.TransactionSnapshot) != 1 {
		t.Fatalf("Get = %+v, want stable links and snapshot", got)
	}

	listed, err := repo.List(ctx, domain.ReportListFilter{CustomerID: customerID, Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != reportID {
		t.Fatalf("List = %+v, want %s", listed, reportID)
	}

	report.SuspiciousPoint = "updated rationale"
	if err := repo.Update(ctx, report); err != nil {
		t.Fatalf("Update draft: %v", err)
	}
	submitted, err := repo.Submit(ctx, reportID, "analyst-1", "filing-ref-1")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if submitted.Status != domain.ReportStatusSubmitted || submitted.SubmittedAt == nil {
		t.Fatalf("submitted report = %+v", submitted)
	}
	if _, err := repo.Submit(ctx, reportID, "analyst-1", "filing-ref-1"); err != nil {
		t.Fatalf("idempotent Submit: %v", err)
	}
	var conflict *domain.ErrConflict
	if _, err := repo.Submit(ctx, reportID, "analyst-1", "different-ref"); !errors.As(err, &conflict) {
		t.Fatalf("different duplicate Submit error = %v, want ErrConflict", err)
	}
}
