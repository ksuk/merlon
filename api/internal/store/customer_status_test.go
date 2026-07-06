package store

import (
	"context"
	"testing"
)

// TestCustomerStatusDefaultsToActive verifies data-model.md §1.1: customers.status
// defaults to 'active' when not specified at INSERT time.
func TestCustomerStatusDefaultsToActive(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	var status string
	err := pool.QueryRow(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING status`,
		"status-default-"+newTestUUID(),
	).Scan(&status)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if status != "active" {
		t.Errorf("status = %q, want %q", status, "active")
	}
}

// TestCustomerStatusAcceptsAllFourValues verifies all four customer_status
// ENUM values (data-model.md §1.1.2) are accepted.
func TestCustomerStatusAcceptsAllFourValues(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	for _, want := range []string{"active", "dormant", "frozen", "closed"} {
		var status string
		err := pool.QueryRow(ctx,
			`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes, status)
			VALUES ($1, 'individual', 'JP', '{}', '{}', $2) RETURNING status`,
			"status-"+want+"-"+newTestUUID(), want,
		).Scan(&status)
		if err != nil {
			t.Fatalf("insert customer with status=%q: %v", want, err)
		}
		if status != want {
			t.Errorf("status = %q, want %q", status, want)
		}
	}
}

// TestCustomerStatusRejectsInvalidValue verifies an undefined ENUM value is
// rejected by the customer_status type constraint.
func TestCustomerStatusRejectsInvalidValue(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes, status)
		VALUES ($1, 'individual', 'JP', '{}', '{}', 'nonexistent_status')`,
		"status-invalid-"+newTestUUID(),
	)
	if err == nil {
		t.Fatal("expected error inserting invalid customer_status value, got nil")
	}
}
