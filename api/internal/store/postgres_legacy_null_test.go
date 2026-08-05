package store

import (
	"context"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

// Legacy rows created before the nullable reader contract was made explicit
// must remain readable. In particular, pgx cannot scan SQL NULL directly into
// a Go string or float64; the repository owns the null-to-domain conversion.
func TestPostgresReadersAcceptLegacyNullableTransactionAndAlertColumns(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	compactCustomerID := domain.CanonicalUUID(customerID)
	txnID := newTestUUID()
	alertID := newTestUUID()

	if _, err := pool.Exec(ctx,
		`INSERT INTO transactions (id, customer_id, external_id, amount, currency, direction, counterparty_id, counterparty_country, channel, executed_at, created_at)
		 VALUES ($1, $2, $3, 100.00, 'JPY', 'outbound', NULL, NULL, NULL, NOW(), NOW())`,
		txnID, customerID, "legacy-null-"+txnID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO alerts (id, customer_id, scenario_id, severity, status, score, description, transaction_ids, detected_at, created_at, updated_at)
		 VALUES ($1, $2, 'legacy-null', 'high', 'open', NULL, NULL, $3, NOW(), NOW(), NOW())`,
		alertID, customerID, []string{txnID}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM alerts WHERE id = $1`, alertID)
		_, _ = pool.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, txnID)
	})

	txn, err := NewPgTransactionRepo(pool).Get(ctx, txnID)
	if err != nil {
		t.Fatalf("transaction GET: %v", err)
	}
	if txn.CounterpartyID != "" || txn.CounterpartyCountry != "" || txn.Channel != "" {
		t.Fatalf("legacy NULL transaction fields = (%q, %q, %q), want empty domain values", txn.CounterpartyID, txn.CounterpartyCountry, txn.Channel)
	}
	if txn.ID != txnID || txn.CustomerID != compactCustomerID {
		t.Fatalf("transaction UUIDs = (%q, %q), want compact (%q, %q)", txn.ID, txn.CustomerID, txnID, compactCustomerID)
	}

	alert, err := NewPgAlertRepo(pool).Get(ctx, alertID)
	if err != nil {
		t.Fatalf("alert GET: %v", err)
	}
	if alert.Score != 0 || alert.Description != "" {
		t.Fatalf("legacy NULL alert fields = (%v, %q), want (0, empty)", alert.Score, alert.Description)
	}
	if alert.ID != alertID || alert.CustomerID != compactCustomerID || len(alert.TransactionIDs) != 1 || alert.TransactionIDs[0] != txnID {
		t.Fatalf("alert UUID contract = %+v, want compact IDs", alert)
	}
}

func TestMemoryAndPostgresNullableDomainDefaultsAreEquivalent(t *testing.T) {
	// Keep the contract explicit even when the integration database is not
	// available: memory has always represented legacy NULL values as the zero
	// domain values exposed above.
	memoryTransaction := domain.Transaction{ID: "txn", CustomerID: "customer"}
	memoryAlert := domain.Alert{ID: "alert", CustomerID: "customer"}
	if memoryTransaction.CounterpartyID != "" || memoryTransaction.CounterpartyCountry != "" || memoryTransaction.Channel != "" || memoryAlert.Score != 0 || memoryAlert.Description != "" {
		t.Fatal("memory nullable defaults changed")
	}
}

func TestPostgresAuditReaderAcceptsLegacyNullableColumns(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	resourceType := "legacy-null-audit-" + newTestUUID()
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at)
		 VALUES (NULL, 'legacy_null', $1, NULL, NULL, NULL, NULL, NOW()) RETURNING id`,
		resourceType,
	).Scan(&id); err != nil {
		t.Fatalf("insert legacy audit row: %v", err)
	}
	t.Cleanup(func() {
		// 043 deliberately permits only the retention lifecycle to remove an
		// audit row; mark this fixture first so cleanup follows that contract.
		_, _ = pool.Exec(ctx, `UPDATE audit_logs SET purge_marked_at = NOW() WHERE id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE id = $1`, id)
	})

	entries, err := NewPgAuditRepo(pool).List(ctx, domain.AuditListFilter{ResourceType: resourceType, Limit: 1})
	if err != nil {
		t.Fatalf("audit GET/list for legacy NULL row: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("legacy audit result length = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.UserID != "" || entry.ResourceID != "" || entry.Details != nil || entry.IPAddress != "" || entry.UserAgent != "" {
		t.Fatalf("legacy audit nullable fields = %+v, want domain zero values", entry)
	}
}
