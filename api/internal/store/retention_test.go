package store

import (
	"context"
	"testing"
)

// TestRetentionPolicySeedDefaults verifies migrations/017_retention.sql seeds
// the five statutory data categories with the retention days transcribed
// from audit.md §6 (transaction/customer/alert-case/CDD score history: 7
// years = 2555 days; audit log: 10 years = 3650 days).
func TestRetentionPolicySeedDefaults(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	want := map[string]struct {
		retentionDays int
		hasMin        bool
	}{
		"customer_data":     {2555, true},
		"transaction_data":  {2555, true},
		"alert_case_data":   {2555, true},
		"cdd_score_history": {2555, true},
		"audit_log":         {3650, false},
	}

	for category, w := range want {
		var retentionDays int
		var minRetentionDays *int
		err := pool.QueryRow(ctx,
			`SELECT retention_days, min_retention_days FROM retention_policies WHERE data_category = $1`,
			category,
		).Scan(&retentionDays, &minRetentionDays)
		if err != nil {
			t.Fatalf("query %s: %v", category, err)
		}
		if retentionDays != w.retentionDays {
			t.Errorf("%s retention_days = %d, want %d", category, retentionDays, w.retentionDays)
		}
		hasMin := minRetentionDays != nil
		if hasMin != w.hasMin {
			t.Errorf("%s min_retention_days present = %v, want %v", category, hasMin, w.hasMin)
		}
	}
}

// TestRetentionPolicyShortenRejected verifies the retention_no_shorten CHECK
// constraint rejects reducing retention_days below min_retention_days for a
// statutory category (audit.md RET-002: 延長のみ可, 設計原則5).
func TestRetentionPolicyShortenRejected(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`UPDATE retention_policies SET retention_days = 100 WHERE data_category = 'customer_data'`,
	)
	if err == nil {
		t.Fatal("expected CHECK constraint violation when shortening customer_data retention, got nil error")
	}
}

// TestRetentionPolicyAuditLogExtendSucceeds verifies audit_log (no statutory
// minimum) may have its retention changed freely, including downward, since
// min_retention_days is NULL for that row.
func TestRetentionPolicyAuditLogExtendSucceeds(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	tag, err := pool.Exec(ctx,
		`UPDATE retention_policies SET retention_days = 4000 WHERE data_category = 'audit_log'`,
	)
	if err != nil {
		t.Fatalf("UPDATE audit_log retention: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("RowsAffected = %d, want 1", tag.RowsAffected())
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE retention_policies SET retention_days = 3650 WHERE data_category = 'audit_log'`)
	})
}

// TestRuleDefinitionEffectivenessReviewColumnExists verifies migrations/017
// added rule_definitions.last_effectiveness_review_at, defaulting to NULL
// (audit.md §8: レビュー未実施の追跡).
func TestRuleDefinitionEffectivenessReviewColumnExists(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'rule_definitions' AND column_name = 'last_effectiveness_review_at'
		)`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query information_schema: %v", err)
	}
	if !exists {
		t.Fatal("rule_definitions.last_effectiveness_review_at column does not exist")
	}
}

// TestCustomerAnonymizedAtColumnExists verifies migrations/017 added
// customers.anonymized_at (data-model.md §3.7).
func TestCustomerAnonymizedAtColumnExists(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'customers' AND column_name = 'anonymized_at'
		)`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query information_schema: %v", err)
	}
	if !exists {
		t.Fatal("customers.anonymized_at column does not exist")
	}
}
