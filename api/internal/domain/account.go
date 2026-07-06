package domain

import (
	"context"
	"time"
)

type AccountType string

const (
	AccountTypeIndividual AccountType = "individual"
	AccountTypeJoint      AccountType = "joint"
)

type AccountRole string

const (
	AccountRolePrimary  AccountRole = "primary"
	AccountRoleCoHolder AccountRole = "co_holder"
)

// Account is a joint/shared account (data-model.md §1.1.3), distinct from
// the pre-existing single-customer transaction model: Transaction.CustomerID
// still identifies the acting party, while AccountID (optional) links a
// transaction to a shared account with multiple holders.
type Account struct {
	ID          string      `json:"id"`
	ExternalID  string      `json:"external_id"`
	AccountType AccountType `json:"account_type"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type AccountCustomer struct {
	AccountID  string      `json:"account_id"`
	CustomerID string      `json:"customer_id"`
	Role       AccountRole `json:"role"`
}

type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Get(ctx context.Context, id string) (*Account, error)
	AddCustomer(ctx context.Context, accountID, customerID string, role AccountRole) error
	ListCustomers(ctx context.Context, accountID string) ([]AccountCustomer, error)
	// RepresentativeRiskScore returns the highest risk_score among all
	// customers linked to accountID (data-model.md §1.1.3 "保守的評価": a
	// joint account is represented by its riskiest holder, not an average),
	// or nil if none of the linked customers has been scored yet.
	RepresentativeRiskScore(ctx context.Context, accountID string) (*float64, error)
}
