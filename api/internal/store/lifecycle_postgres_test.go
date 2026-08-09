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

func TestPostgresCaseAlertLifecycleCreatesAndAppendsLinksAtomically(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var customerID, otherCustomerID string
	for externalID, target := range map[string]*string{
		"lifecycle-link-" + newTestUUID():       &customerID,
		"lifecycle-link-other-" + newTestUUID(): &otherCustomerID,
	} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
			 VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`, externalID,
		).Scan(target); err != nil {
			t.Fatalf("create customer: %v", err)
		}
	}

	now := time.Now().UTC()
	alertIDs := []string{newTestUUID(), newTestUUID(), newTestUUID()}
	for i, alertID := range alertIDs {
		owner := customerID
		if i == 2 {
			owner = otherCustomerID
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
			 VALUES ($1, $2, 'lifecycle_link_test', 'high', 'open', 1, '', '{}', $3, $3, $3)`, alertID, owner, now); err != nil {
			t.Fatalf("create alert: %v", err)
		}
	}

	caseID := "lifecycle-link-" + newTestUUID()
	invalidCaseID := "lifecycle-link-invalid-" + newTestUUID()
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM cases WHERE id = ANY($1)`, []string{caseID, invalidCaseID})
		pool.Exec(context.Background(), `DELETE FROM alerts WHERE id = ANY($1::uuid[])`, alertIDs)
		pool.Exec(context.Background(), `DELETE FROM customers WHERE id = ANY($1::uuid[])`, []string{customerID, otherCustomerID})
	})

	lifecycle := NewPgCaseAlertLifecycleRepo(pool)
	missingID := newTestUUID()
	err := lifecycle.CreateCaseWithAlerts(ctx, &domain.Case{
		ID: invalidCaseID, CustomerID: customerID, AlertIDs: []string{alertIDs[0], missingID},
		Status: domain.CaseStatusNew, Priority: domain.CasePriorityHigh, Summary: "must roll back",
		CreatedAt: now, UpdatedAt: now,
	})
	var notFound *domain.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("invalid create error = %v, want ErrNotFound", err)
	}
	var invalidCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM cases WHERE id = $1`, invalidCaseID).Scan(&invalidCount); err != nil {
		t.Fatal(err)
	}
	if invalidCount != 0 {
		t.Fatal("invalid linked create persisted an orphan case")
	}

	created := &domain.Case{
		ID: caseID, CustomerID: customerID, AlertIDs: []string{alertIDs[0]},
		Status: domain.CaseStatusNew, Priority: domain.CasePriorityHigh, Summary: "atomic links",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := lifecycle.CreateCaseWithAlerts(ctx, created); err != nil {
		t.Fatalf("valid create: %v", err)
	}
	stored, err := NewPgCaseRepo(pool).Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = lifecycle.AppendAlerts(ctx, caseID, stored.UpdatedAt, []string{alertIDs[1]})
	if err != nil {
		t.Fatalf("valid append: %v", err)
	}
	if len(stored.AlertIDs) != 2 || stored.AlertIDs[1] != alertIDs[1] {
		t.Fatalf("appended alert_ids = %v", stored.AlertIDs)
	}
	persistedAfterAppend, err := NewPgCaseRepo(pool).Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if !persistedAfterAppend.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatalf("returned updated_at %s differs from PostgreSQL %s", stored.UpdatedAt, persistedAfterAppend.UpdatedAt)
	}

	_, err = lifecycle.AppendAlerts(ctx, caseID, stored.UpdatedAt, []string{alertIDs[2]})
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("cross-customer append error = %v, want ErrConflict", err)
	}
	if conflict.Reason != "alert belongs to a different customer" {
		t.Fatalf("cross-customer append conflict = %q", conflict.Reason)
	}
	afterFailure, err := NewPgCaseRepo(pool).Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterFailure.AlertIDs) != 2 {
		t.Fatalf("failed append mutated alert_ids = %v", afterFailure.AlertIDs)
	}
}
