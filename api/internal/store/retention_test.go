package store

import (
	"context"
	"testing"
)

// TestRetentionPolicySeedDefaults verifies the configured defaults after all
// migrations. They are defaults, not immutable statutory lower bounds.
func TestRetentionPolicySeedDefaults(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	want := map[string]struct {
		retentionDays int
		hasMin        bool
	}{
		"customer_data":     {2555, false},
		"transaction_data":  {2555, false},
		"alert_case_data":   {2555, false},
		"cdd_score_history": {2555, false},
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

// TestRetentionPolicyShortenSucceeds verifies a deployment can select a
// shorter positive period when its retention policy requires one.
func TestRetentionPolicyShortenSucceeds(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`UPDATE retention_policies SET retention_days = 100 WHERE data_category = 'customer_data'`,
	)
	if err != nil {
		t.Fatalf("shorten customer_data retention: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE retention_policies SET retention_days = 2555 WHERE data_category = 'customer_data'`)
	})
}

func TestRetentionPolicyNonPositiveRejected(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	for _, days := range []int{0, -1} {
		if _, err := pool.Exec(ctx, `UPDATE retention_policies SET retention_days = $1 WHERE data_category = 'cdd_score_history'`, days); err == nil {
			t.Errorf("retention_days=%d: expected CHECK constraint violation", days)
		}
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
// (the audit design §8: レビュー未実施の追跡).
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
// customers.anonymized_at (the data model §3.7).
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
