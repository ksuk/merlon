package store

import (
	"context"
	"testing"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// TestCustomerTypeAcceptsTrustPartnershipNpoGovernmentForeignLegalArrangement
// verifies data-model.md §1.1.1: the five non-natural-person customer_type
// values added by migrations/019_customer_type_extension.sql are all
// accepted by the ENUM constraint.
func TestCustomerTypeAcceptsTrustPartnershipNpoGovernmentForeignLegalArrangement(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	for _, want := range []domain.CustomerType{
		domain.CustomerTypeTrust,
		domain.CustomerTypePartnership,
		domain.CustomerTypeNPO,
		domain.CustomerTypeGovernment,
		domain.CustomerTypeForeignLegalArrangement,
	} {
		var got string
		err := pool.QueryRow(ctx,
			`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
			VALUES ($1, $2, 'JP', '{}', '{}') RETURNING customer_type`,
			"customer-type-"+string(want)+"-"+newTestUUID(), string(want),
		).Scan(&got)
		if err != nil {
			t.Fatalf("insert customer with customer_type=%q: %v", want, err)
		}
		if got != string(want) {
			t.Errorf("customer_type = %q, want %q", got, want)
		}
	}
}

// TestCustomerTypeExistingValuesUnaffected verifies the pre-existing three
// customer_type values (individual/corporate_domestic/corporate_foreign)
// remain readable and insertable unchanged after the ENUM extension
// (additive-only migration, no data migration involved).
func TestCustomerTypeExistingValuesUnaffected(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	for _, want := range []domain.CustomerType{
		domain.CustomerTypeIndividual,
		domain.CustomerTypeCorporateDomestic,
		domain.CustomerTypeCorporateForeign,
	} {
		var got string
		err := pool.QueryRow(ctx,
			`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
			VALUES ($1, $2, 'JP', '{}', '{}') RETURNING customer_type`,
			"customer-type-existing-"+string(want)+"-"+newTestUUID(), string(want),
		).Scan(&got)
		if err != nil {
			t.Fatalf("insert customer with customer_type=%q: %v", want, err)
		}
		if got != string(want) {
			t.Errorf("customer_type = %q, want %q", got, want)
		}
	}
}

// TestCustomerAttributesTrustPartiesStructure verifies a round trip of
// attributes.trust_parties (data-model.md §1.1.1: JSONB array with
// settlor/trustee/beneficiary role entries) through PgCustomerRepo.
func TestCustomerAttributesTrustPartiesStructure(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgCustomerRepo(pool, nil)

	trustParties := []any{
		map[string]any{"role": "settlor", "name": "山田太郎"},
		map[string]any{"role": "trustee", "name": "信託銀行株式会社"},
		map[string]any{"role": "beneficiary", "name": "山田花子"},
	}

	now := time.Now()
	c := &domain.Customer{
		ID:           newTestUUID(),
		ExternalID:   "trust-parties-" + newTestUUID(),
		CustomerType: domain.CustomerTypeTrust,
		CountryCode:  "JP",
		Status:       domain.CustomerStatusActive,
		Attributes: map[string]any{
			"trust_parties": trustParties,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, c.ID)
	})

	got, err := repo.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}

	parties, ok := got.Attributes["trust_parties"].([]any)
	if !ok {
		t.Fatalf("trust_parties = %T, want []any", got.Attributes["trust_parties"])
	}
	if len(parties) != 3 {
		t.Fatalf("trust_parties length = %d, want 3", len(parties))
	}
	first, ok := parties[0].(map[string]any)
	if !ok {
		t.Fatalf("trust_parties[0] = %T, want map[string]any", parties[0])
	}
	if first["role"] != "settlor" {
		t.Errorf("trust_parties[0].role = %v, want %q", first["role"], "settlor")
	}
}
