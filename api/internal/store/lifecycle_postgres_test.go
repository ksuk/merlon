package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresCaseAlertLifecycleIsAtomic(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var customerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`,
		"lifecycle-"+newTestUUID(),
	).Scan(&customerID); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	alertID := newTestUUID()
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx,
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
		 VALUES ($1, $2, 'lifecycle_test', 'high', 'open', 1, '', '{}', $3, $3, $3)`, alertID, customerID, now); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	caseID := "lifecycle-" + newTestUUID()
	if _, err := pool.Exec(ctx,
		`INSERT INTO cases (id, customer_id, alert_ids, status, priority, assigned_to, summary, created_at, updated_at)
		 VALUES ($1, $2, $3, 'new', 'medium', '', 'lifecycle test', $4, $4)`, caseID, customerID, []string{alertID}, now); err != nil {
		t.Fatalf("create case: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM cases WHERE id = $1`, caseID)
		pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = $1`, alertID)
		pool.Exec(context.Background(), `DELETE FROM customers WHERE id = $1`, customerID)
	})

	cases := NewPgCaseRepo(pool)
	lifecycle := NewPgCaseAlertLifecycleRepo(pool)
	caseRecord, err := cases.Get(ctx, caseID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	alertRecord, err := NewPgAlertRepo(pool).Get(ctx, alertID)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	transition := domain.AlertStatusTransition{
		ID: alertID, From: domain.AlertStatusOpen, To: domain.AlertStatusInvestigating,
		ExpectedUpdatedAt: alertRecord.UpdatedAt,
	}
	caseRecord.Status = domain.CaseStatusInvestigating
	if err := lifecycle.UpdateCaseAndAlerts(ctx, caseRecord, caseRecord.UpdatedAt, []domain.AlertStatusTransition{transition}); err != nil {
		t.Fatalf("atomic active transition: %v", err)
	}

	var alertStatus string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM alerts WHERE id = $1`, alertID).Scan(&alertStatus); err != nil {
		t.Fatal(err)
	}
	if alertStatus != string(domain.AlertStatusInvestigating) {
		t.Fatalf("alert status = %q, want investigating", alertStatus)
	}

	// A failed linked-alert validation must not leave the case advanced.
	caseRecord.Status = domain.CaseStatusEscalated
	caseBeforeFailure, err := cases.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	alertBeforeFailure, err := NewPgAlertRepo(pool).Get(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}
	err = lifecycle.UpdateCaseAndAlerts(ctx, caseRecord, caseBeforeFailure.UpdatedAt, []domain.AlertStatusTransition{
		{ID: alertID, From: domain.AlertStatusInvestigating, To: domain.AlertStatusOpen, ExpectedUpdatedAt: alertBeforeFailure.UpdatedAt},
	})
	var invalid *domain.ErrInvalidStateTransition
	if !errors.As(err, &invalid) {
		t.Fatalf("invalid atomic update error = %v, want ErrInvalidStateTransition", err)
	}
	caseAfterFailure, err := cases.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if caseAfterFailure.Status != domain.CaseStatusInvestigating {
		t.Fatalf("case status after failed transaction = %q, want investigating", caseAfterFailure.Status)
	}
}
