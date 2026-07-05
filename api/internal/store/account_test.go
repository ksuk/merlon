package store

import (
	"context"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// TestMemoryAccountRepoRepresentativeScoreUsesHighestRisk verifies
// data-model.md §1.1.3 "保守的評価": a joint account's representative risk
// score is the highest score among its linked customers, not an average or
// the primary holder's score alone. Exercised against MemoryAccountRepo
// since it needs no live Postgres connection.
func TestMemoryAccountRepoRepresentativeScoreUsesHighestRisk(t *testing.T) {
	ctx := context.Background()
	customers := NewMemoryCustomerRepo()
	accounts := NewMemoryAccountRepo(customers)

	low, high := 20.0, 85.0
	primary := &domain.Customer{ID: "cust-primary", ExternalID: "EXT-P", RiskScore: &low}
	coHolder := &domain.Customer{ID: "cust-coholder", ExternalID: "EXT-C", RiskScore: &high}
	if err := customers.Create(ctx, primary); err != nil {
		t.Fatalf("create primary customer: %v", err)
	}
	if err := customers.Create(ctx, coHolder); err != nil {
		t.Fatalf("create co-holder customer: %v", err)
	}

	acc := &domain.Account{ID: "acc-1", ExternalID: "ACC-EXT-1", AccountType: domain.AccountTypeJoint}
	if err := accounts.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := accounts.AddCustomer(ctx, acc.ID, primary.ID, domain.AccountRolePrimary); err != nil {
		t.Fatalf("add primary: %v", err)
	}
	if err := accounts.AddCustomer(ctx, acc.ID, coHolder.ID, domain.AccountRoleCoHolder); err != nil {
		t.Fatalf("add co-holder: %v", err)
	}

	score, err := accounts.RepresentativeRiskScore(ctx, acc.ID)
	if err != nil {
		t.Fatalf("RepresentativeRiskScore: %v", err)
	}
	if score == nil {
		t.Fatal("expected non-nil representative score")
	}
	if *score != high {
		t.Errorf("representative score = %v, want %v (highest among holders)", *score, high)
	}
}

// TestMemoryAccountRepoRepresentativeScoreNilWhenUnscored verifies the
// "none scored yet" case doesn't panic and returns nil rather than a bogus
// zero value.
func TestMemoryAccountRepoRepresentativeScoreNilWhenUnscored(t *testing.T) {
	ctx := context.Background()
	customers := NewMemoryCustomerRepo()
	accounts := NewMemoryAccountRepo(customers)

	c := &domain.Customer{ID: "cust-unscored", ExternalID: "EXT-U"}
	if err := customers.Create(ctx, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	acc := &domain.Account{ID: "acc-2", ExternalID: "ACC-EXT-2", AccountType: domain.AccountTypeJoint}
	if err := accounts.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := accounts.AddCustomer(ctx, acc.ID, c.ID, domain.AccountRolePrimary); err != nil {
		t.Fatalf("add customer: %v", err)
	}

	score, err := accounts.RepresentativeRiskScore(ctx, acc.ID)
	if err != nil {
		t.Fatalf("RepresentativeRiskScore: %v", err)
	}
	if score != nil {
		t.Errorf("expected nil score, got %v", *score)
	}
}

// TestPgAccountCreateJointWithMultipleCustomers exercises PgAccountRepo
// against a live Postgres connection (skipped when MERLON_DATABASE_URL is
// unset, e.g. in a sandbox without Docker).
func TestPgAccountCreateJointWithMultipleCustomers(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()

	repo := NewPgAccountRepo(pool)
	customerRepo := NewPgCustomerRepo(pool)

	primary := &domain.Customer{
		ID: newTestUUID(), ExternalID: "acct-primary-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
		Status: domain.CustomerStatusActive, Attributes: map[string]any{},
	}
	coHolder := &domain.Customer{
		ID: newTestUUID(), ExternalID: "acct-coholder-" + newTestUUID(),
		CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
		Status: domain.CustomerStatusActive, Attributes: map[string]any{},
	}
	if err := customerRepo.Create(ctx, primary); err != nil {
		t.Fatalf("create primary customer: %v", err)
	}
	if err := customerRepo.Create(ctx, coHolder); err != nil {
		t.Fatalf("create co-holder customer: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM customers WHERE id = ANY($1)`, []string{primary.ID, coHolder.ID})
	})

	acc := &domain.Account{ID: newTestUUID(), ExternalID: "acct-ext-" + newTestUUID(), AccountType: domain.AccountTypeJoint}
	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM account_customers WHERE account_id = $1`, acc.ID)
		pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, acc.ID)
	})

	if err := repo.AddCustomer(ctx, acc.ID, primary.ID, domain.AccountRolePrimary); err != nil {
		t.Fatalf("add primary: %v", err)
	}
	if err := repo.AddCustomer(ctx, acc.ID, coHolder.ID, domain.AccountRoleCoHolder); err != nil {
		t.Fatalf("add co-holder: %v", err)
	}

	links, err := repo.ListCustomers(ctx, acc.ID)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(links))
	}
}
