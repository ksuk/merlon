package retention

import (
	"context"
	"slices"
	"testing"
	"time"
)

// TestCustomerGuardCoversEveryForeignKey compares CustomerReferencingTables
// against the live catalogue. A migration that adds a foreign key onto
// customers without teaching CustomerData about it fails here rather than in
// production, where the omission aborts the whole purge transaction with
// foreign_key_violation instead of skipping the one customer involved.
func TestCustomerGuardCoversEveryForeignKey(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT child.relname
		FROM pg_constraint con
		JOIN pg_class child  ON child.oid = con.conrelid
		JOIN pg_class parent ON parent.oid = con.confrelid
		WHERE con.contype = 'f' AND parent.relname = 'customers'
		ORDER BY child.relname`)
	if err != nil {
		t.Fatalf("query foreign keys: %v", err)
	}
	defer rows.Close()

	var actual []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan: %v", err)
		}
		actual = append(actual, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(actual) == 0 {
		t.Fatal("found no foreign keys onto customers; this guard cannot compare anything")
	}

	for _, table := range actual {
		if !slices.Contains(CustomerReferencingTables, table) {
			t.Errorf("%s references customers(id) but CustomerData does not account for it; "+
				"add it to CustomerReferencingTables and to the DELETE FROM customers guard", table)
		}
	}
	for _, table := range CustomerReferencingTables {
		if !slices.Contains(actual, table) {
			t.Errorf("CustomerReferencingTables lists %s, which no longer references customers(id)", table)
		}
	}
}

// TestCustomerDataPurgesWave3EvidenceChildren is the regression test for the
// defect migration 045 introduced. Every customer created after 045 gets a
// customer_identity_history row, so the unguarded DELETE FROM customers raised
// foreign_key_violation (23503) and rolled back the entire purge -- retention
// stopped working for every customer, not just this one.
//
// The customer here is fully purgeable: no transactions, no alerts, no cases,
// no score history. Only the Wave 3 evidence children stand between it and
// deletion, so the test isolates exactly the regression.
func TestCustomerDataPurgesWave3EvidenceChildren(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()
	purger := NewPostgresPurger(pool)

	externalID := "purge-wave3-" + integrationID()
	var customerID string
	// created_at is set well before the cutoff so the customer is eligible on
	// the very first pass.
	if err := pool.QueryRow(ctx, `
		INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes, created_at)
		VALUES ($1, 'individual', 'JP', '{}', '{}', now() - interval '4000 days')
		RETURNING id`, externalID).Scan(&customerID); err != nil {
		t.Fatalf("insert customer: %v", err)
	}

	caseID := "case-" + integrationID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO cases (id, customer_id, status, priority, summary, closed_at, created_at, updated_at)
		VALUES ($1, $2, 'closed', 'low', 'wave3 purge fixture',
		        now() - interval '4000 days', now() - interval '4000 days', now() - interval '4000 days')`,
		caseID, customerID); err != nil {
		t.Fatalf("insert case: %v", err)
	}

	resultID := "screening-result-" + integrationID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO screening_results (id, customer_id, list_id, list_type, entry_id, matched_name, similarity, status, case_id, screened_at, created_at)
		VALUES ($1, $2, 'ofac_sdn', 'sanctions', 'entry-1', 'fixture name', 0.9, 'NEW', $3,
		        now() - interval '4000 days', now() - interval '4000 days')`,
		resultID, customerID, caseID); err != nil {
		t.Fatalf("insert screening result: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO screening_result_history (id, screening_result_id, from_status, to_status, actor, rationale, version)
		VALUES ($1, $2, 'NEW', 'NEW', 'tester', 'wave3 purge fixture', 1)`,
		integrationID(), resultID); err != nil {
		t.Fatalf("insert screening result history: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO screening_runs (id, customer_id, status, actor, created_at)
		VALUES ($1, $2, 'completed', 'tester', now() - interval '4000 days')`,
		integrationID(), customerID); err != nil {
		t.Fatalf("insert screening run: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO customer_identity_history (id, customer_id, actor, rationale)
		VALUES ($1, $2, 'tester', 'wave3 purge fixture')`,
		integrationID(), customerID); err != nil {
		t.Fatalf("insert identity history: %v", err)
	}

	t.Cleanup(func() {
		cleanup := context.Background()
		// History rows are append-only: they must be marked before they can be
		// deleted, even by test cleanup.
		pool.Exec(cleanup, `UPDATE screening_result_history SET purge_marked_at = now() WHERE screening_result_id = $1`, resultID)
		pool.Exec(cleanup, `DELETE FROM screening_result_history WHERE screening_result_id = $1`, resultID)
		pool.Exec(cleanup, `DELETE FROM screening_results WHERE customer_id = $1`, customerID)
		pool.Exec(cleanup, `DELETE FROM screening_runs WHERE customer_id = $1`, customerID)
		pool.Exec(cleanup, `UPDATE customer_identity_history SET purge_marked_at = now() WHERE customer_id = $1`, customerID)
		pool.Exec(cleanup, `DELETE FROM customer_identity_history WHERE customer_id = $1`, customerID)
		pool.Exec(cleanup, `DELETE FROM cases WHERE id = $1`, caseID)
		pool.Exec(cleanup, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	now := time.Now()
	cutoff := now.Add(-2555 * 24 * time.Hour)

	// Both targets run in the same scheduled pass, so the test drives them the
	// way the scheduler does. AlertCaseData additionally exercises the
	// screening_results.case_id relaxation: before migration 049 the surviving
	// screening result held the closed case hostage.
	if _, _, err := purger.AlertCaseData(ctx, cutoff, now); err != nil {
		t.Fatalf("first alert/case pass: %v", err)
	}
	// First pass marks the customer and its children. Before the fix this
	// already succeeded; the failure came on the second pass.
	if _, _, err := purger.CustomerData(ctx, cutoff, now); err != nil {
		t.Fatalf("first purge pass: %v", err)
	}

	// Second pass, past the grace period, is where the children must be
	// deleted before their parents.
	afterGrace := now.Add(PhysicalDeletionGracePeriod + time.Hour)
	if _, _, err := purger.AlertCaseData(ctx, cutoff, afterGrace); err != nil {
		t.Fatalf("alert/case purge after grace: %v", err)
	}
	if _, deleted, err := purger.CustomerData(ctx, cutoff, afterGrace); err != nil {
		t.Fatalf("purge after grace: %v", err)
	} else if deleted == 0 {
		t.Fatal("no customer was deleted; the Wave 3 children still block the parent")
	}

	for _, check := range []struct {
		table string
		query string
	}{
		{"customers", `SELECT count(*) FROM customers WHERE id = $1`},
		{"customer_identity_history", `SELECT count(*) FROM customer_identity_history WHERE customer_id = $1`},
		{"screening_runs", `SELECT count(*) FROM screening_runs WHERE customer_id = $1`},
		{"screening_results", `SELECT count(*) FROM screening_results WHERE customer_id = $1`},
	} {
		var count int
		if err := pool.QueryRow(ctx, check.query, customerID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.table, err)
		}
		if count != 0 {
			t.Errorf("%s still holds %d row(s) for the purged customer", check.table, count)
		}
	}
	var historyCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM screening_result_history WHERE screening_result_id = $1`, resultID).Scan(&historyCount); err != nil {
		t.Fatalf("count screening_result_history: %v", err)
	}
	if historyCount != 0 {
		t.Errorf("screening_result_history still holds %d row(s) for the purged result", historyCount)
	}
}

// The purge-aware trigger must grant exactly one exception and no more: mark a
// row for purge, then delete a marked row. Rewriting a business column, or
// deleting an unmarked row, stays impossible.
func TestPurgeAwareTriggerPermitsOnlyThePurgeLifecycle(t *testing.T) {
	pool := openRetentionTestPool(t)
	ctx := context.Background()

	externalID := "trigger-wave3-" + integrationID()
	var customerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`, externalID).Scan(&customerID); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	historyID := integrationID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO customer_identity_history (id, customer_id, actor, rationale)
		VALUES ($1, $2, 'tester', 'original rationale')`, historyID, customerID); err != nil {
		t.Fatalf("insert identity history: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		pool.Exec(cleanup, `UPDATE customer_identity_history SET purge_marked_at = now() WHERE id = $1`, historyID)
		pool.Exec(cleanup, `DELETE FROM customer_identity_history WHERE id = $1`, historyID)
		pool.Exec(cleanup, `DELETE FROM customers WHERE id = $1`, customerID)
	})

	// Rewriting evidence is still refused.
	if _, err := pool.Exec(ctx,
		`UPDATE customer_identity_history SET rationale = 'rewritten' WHERE id = $1`, historyID); err == nil {
		t.Fatal("rewriting a history row must be refused")
	}
	// Deleting unmarked evidence is still refused.
	if _, err := pool.Exec(ctx, `DELETE FROM customer_identity_history WHERE id = $1`, historyID); err == nil {
		t.Fatal("deleting an unmarked history row must be refused")
	}
	// Marking for purge is the one permitted update.
	if _, err := pool.Exec(ctx,
		`UPDATE customer_identity_history SET purge_marked_at = now() WHERE id = $1`, historyID); err != nil {
		t.Fatalf("marking for purge must be permitted: %v", err)
	}
	// Changing a business column while also touching the marker is refused,
	// so the exception cannot be used to smuggle in an edit.
	if _, err := pool.Exec(ctx,
		`UPDATE customer_identity_history SET purge_marked_at = now(), rationale = 'smuggled' WHERE id = $1`, historyID); err == nil {
		t.Fatal("changing a business column alongside purge_marked_at must be refused")
	}
	// A marked row may finally be deleted.
	if _, err := pool.Exec(ctx, `DELETE FROM customer_identity_history WHERE id = $1`, historyID); err != nil {
		t.Fatalf("deleting a marked row must be permitted: %v", err)
	}
}
