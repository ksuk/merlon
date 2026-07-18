package demogen

import (
	"fmt"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// buildAccounts creates the A2-scale 10 accounts (3 joint), linking them to
// the first customers in the final population in generation order — a pure
// function of the already-deterministic customer slice, so it needs no RNG
// of its own.
func buildAccounts(customers []domain.Customer, anchor time.Time) ([]domain.Account, []domain.AccountCustomer) {
	const totalAccounts = 10
	const jointAccounts = 3

	accounts := make([]domain.Account, 0, totalAccounts)
	links := make([]domain.AccountCustomer, 0, totalAccounts+jointAccounts)

	for i := 0; i < totalAccounts && i < len(customers); i++ {
		isJoint := i >= totalAccounts-jointAccounts
		accType := domain.AccountTypeIndividual
		if isJoint {
			accType = domain.AccountTypeJoint
		}
		acc := domain.Account{
			ID:          fmt.Sprintf("demo-account-%03d", i+1),
			ExternalID:  fmt.Sprintf("MNP-ACC-%06d", i+1),
			AccountType: accType,
			CreatedAt:   anchor.AddDate(0, 0, -(365 + i*17)),
			UpdatedAt:   anchor,
		}
		accounts = append(accounts, acc)
		links = append(links, domain.AccountCustomer{AccountID: acc.ID, CustomerID: customers[i].ID, Role: domain.AccountRolePrimary})

		if isJoint {
			coHolderIdx := totalAccounts + (i - (totalAccounts - jointAccounts))
			if coHolderIdx < len(customers) {
				links = append(links, domain.AccountCustomer{AccountID: acc.ID, CustomerID: customers[coHolderIdx].ID, Role: domain.AccountRoleCoHolder})
			}
		}
	}
	return accounts, links
}
