package retention

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func openRetentionTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres retention integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("pool.Ping: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPostgresPurgerMarksAndDeletesTransactionsAfterGrace(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	customerID := integrationID()
	transactionID := integrationID()
	old := time.Now().UTC().Add(-4000 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id, external_id, customer_type, country_code, product_types, attributes, created_at, updated_at) VALUES ($1, $2, 'individual', 'JP', '{}', '{}', $3, $3)`, customerID, "retention-customer-"+customerID, old); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transactions (id, customer_id, external_id, amount, currency, direction, executed_at, created_at) VALUES ($1, $2, $3, 1, 'JPY', 'inbound', $4, $4)`, transactionID, customerID, "retention-transaction-"+transactionID, old); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, transactionID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	purger := NewPostgresPurger(pool)
	now := time.Now().UTC()
	marked, deleted, err := purger.Transactions(ctx, now.Add(-2555*24*time.Hour), now)
	if err != nil {
		t.Fatalf("mark transaction: %v", err)
	}
	if marked != 1 || deleted != 0 {
		t.Fatalf("first purge = (%d marked, %d deleted), want (1, 0)", marked, deleted)
	}
	var markedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT purge_marked_at FROM transactions WHERE id = $1`, transactionID).Scan(&markedAt); err != nil {
		t.Fatalf("read purge mark: %v", err)
	}
	if markedAt.IsZero() {
		t.Fatal("purge_marked_at is zero")
	}

	marked, deleted, err = purger.Transactions(ctx, now.Add(-2555*24*time.Hour), now.Add(PhysicalDeletionGracePeriod))
	if err != nil {
		t.Fatalf("physical purge: %v", err)
	}
	if marked != 0 || deleted != 1 {
		t.Fatalf("second purge = (%d marked, %d deleted), want (0, 1)", marked, deleted)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE id = $1`, transactionID).Scan(&count); err != nil {
		t.Fatalf("count transaction: %v", err)
	}
	if count != 0 {
		t.Fatalf("transaction count = %d after grace purge, want 0", count)
	}
}

func TestPostgresPurgerDeletesAuditLogsAfterGrace(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-4000 * 24 * time.Hour)
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO audit_logs (action, resource_type, created_at) VALUES ('retention_test', 'retention_policy', $1) RETURNING id`, old).Scan(&id); err != nil {
		t.Fatalf("insert audit log: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM audit_logs WHERE id = $1`, id) })

	purger := NewPostgresPurger(pool)
	now := time.Now().UTC()
	if marked, deleted, err := purger.AuditLogs(ctx, now.Add(-3650*24*time.Hour), now); err != nil {
		t.Fatalf("mark audit log: %v", err)
	} else if marked != 1 || deleted != 0 {
		t.Fatalf("first purge = (%d marked, %d deleted), want (1, 0)", marked, deleted)
	}
	if marked, deleted, err := purger.AuditLogs(ctx, now.Add(-3650*24*time.Hour), now.Add(PhysicalDeletionGracePeriod)); err != nil {
		t.Fatalf("delete audit log: %v", err)
	} else if marked != 0 || deleted != 1 {
		t.Fatalf("second purge = (%d marked, %d deleted), want (0, 1)", marked, deleted)
	}
}

func TestPostgresPurgerDeletesAlertCaseDataInForeignKeyOrder(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	customerID := integrationID()
	alertID := integrationID()
	caseID := integrationID()
	old := time.Now().UTC().Add(-4000 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id, external_id, customer_type, country_code, product_types, attributes, created_at, updated_at) VALUES ($1, $2, 'individual', 'JP', '{}', '{}', $3, $3)`, customerID, "retention-alert-customer-"+customerID, old); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alerts (id, customer_id, scenario_id, severity, status, detected_at, resolved_at, created_at, updated_at) VALUES ($1, $2, 'retention_test', 'low', 'closed_false_positive', $3, $3, $3, $3)`, alertID, customerID, old); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO cases (id, customer_id, summary, status, created_at, updated_at, closed_at) VALUES ($1, $2, 'retention test case', 'closed', $3, $3, $3)`, caseID, customerID, old); err != nil {
		t.Fatalf("insert case: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO case_notes (id, case_id, author, content, created_at) VALUES ($1, $2, 'retention-test', 'test note', $3)`, integrationID(), caseID, old); err != nil {
		t.Fatalf("insert case note: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM case_notes WHERE case_id = $1`, caseID)
		pool.Exec(ctx, `DELETE FROM cases WHERE id = $1`, caseID)
		pool.Exec(ctx, `DELETE FROM alerts WHERE id = $1`, alertID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	purger := NewPostgresPurger(pool)
	now := time.Now().UTC()
	if marked, deleted, err := purger.AlertCaseData(ctx, now.Add(-2555*24*time.Hour), now); err != nil {
		t.Fatalf("mark alert/case data: %v", err)
	} else if marked != 2 || deleted != 0 {
		t.Fatalf("first purge = (%d marked, %d deleted), want (2, 0)", marked, deleted)
	}
	if marked, deleted, err := purger.AlertCaseData(ctx, now.Add(-2555*24*time.Hour), now.Add(PhysicalDeletionGracePeriod)); err != nil {
		t.Fatalf("delete alert/case data: %v", err)
	} else if marked != 0 || deleted != 2 {
		t.Fatalf("second purge = (%d marked, %d deleted), want (0, 2)", marked, deleted)
	}
}

func TestPostgresPurgerKeepsOpenCasesRegardlessOfAge(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	customerID := integrationID()
	caseID := integrationID()
	old := time.Now().UTC().Add(-4000 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id, external_id, customer_type, country_code, product_types, attributes, created_at, updated_at) VALUES ($1, $2, 'individual', 'JP', '{}', '{}', $3, $3)`, customerID, "retention-open-case-customer-"+customerID, old); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO cases (id, customer_id, summary, status, created_at, updated_at) VALUES ($1, $2, 'active retention test case', 'investigating', $3, $3)`, caseID, customerID, old); err != nil {
		t.Fatalf("insert case: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM cases WHERE id = $1`, caseID)
		pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	purger := NewPostgresPurger(pool)
	now := time.Now().UTC()
	if _, _, err := purger.AlertCaseData(ctx, now.Add(-2555*24*time.Hour), now); err != nil {
		t.Fatalf("purge alert/case data: %v", err)
	}
	var markedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT purge_marked_at FROM cases WHERE id = $1`, caseID).Scan(&markedAt); err != nil {
		t.Fatalf("read case: %v", err)
	}
	if markedAt != nil {
		t.Fatalf("open case was marked for purge at %v", *markedAt)
	}
	if _, _, err := purger.CustomerData(ctx, now.Add(-2555*24*time.Hour), now); err != nil {
		t.Fatalf("purge customer data: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT purge_marked_at FROM customers WHERE id = $1`, customerID).Scan(&markedAt); err != nil {
		t.Fatalf("read customer: %v", err)
	}
	if markedAt != nil {
		t.Fatalf("customer under active investigation was marked for purge at %v", *markedAt)
	}
}

func TestPostgresPurgerDeletesCustomerAfterGrace(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	customerID := integrationID()
	old := time.Now().UTC().Add(-4000 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id, external_id, customer_type, country_code, product_types, attributes, created_at, updated_at) VALUES ($1, $2, 'individual', 'JP', '{}', '{}', $3, $3)`, customerID, "retention-customer-only-"+customerID, old); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, customerID) })

	purger := NewPostgresPurger(pool)
	now := time.Now().UTC()
	if marked, deleted, err := purger.CustomerData(ctx, now.Add(-2555*24*time.Hour), now); err != nil {
		t.Fatalf("mark customer: %v", err)
	} else if marked != 1 || deleted != 0 {
		t.Fatalf("first purge = (%d marked, %d deleted), want (1, 0)", marked, deleted)
	}
	if marked, deleted, err := purger.CustomerData(ctx, now.Add(-2555*24*time.Hour), now.Add(PhysicalDeletionGracePeriod)); err != nil {
		t.Fatalf("delete customer: %v", err)
	} else if marked != 0 || deleted != 1 {
		t.Fatalf("second purge = (%d marked, %d deleted), want (0, 1)", marked, deleted)
	}
}

func TestPostgresPurgerDoesNotDeleteUnrelatedOrphanAccount(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	accountID := integrationID()
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, external_id, account_type, created_at, updated_at) VALUES ($1, $2, 'individual', $3, $3)`, accountID, "retention-unrelated-account-"+accountID, now); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID) })

	purger := NewPostgresPurger(pool)
	if _, _, err := purger.CustomerData(ctx, now.Add(-2555*24*time.Hour), now); err != nil {
		t.Fatalf("purge customer data: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE id = $1`, accountID).Scan(&count); err != nil {
		t.Fatalf("count account: %v", err)
	}
	if count != 1 {
		t.Fatalf("unrelated orphan account count = %d, want 1", count)
	}
}
