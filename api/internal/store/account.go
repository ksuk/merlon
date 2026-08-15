package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgAccountRepo implements domain.AccountRepository against accounts /
// account_customers (migrations/020_accounts.sql, WS-11 Task 4).
type PgAccountRepo struct {
	pool DBTX
}

func NewPgAccountRepo(pool DBTX) *PgAccountRepo {
	return &PgAccountRepo{pool: pool}
}

func (r *PgAccountRepo) Create(ctx context.Context, a *domain.Account) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO accounts (id, external_id, account_type)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at`,
		a.ID, a.ExternalID, string(a.AccountType),
	).Scan(&a.CreatedAt, &a.UpdatedAt)
}

func (r *PgAccountRepo) Get(ctx context.Context, id string) (*domain.Account, error) {
	var a domain.Account
	err := r.pool.QueryRow(ctx,
		`SELECT id, external_id, account_type, created_at, updated_at FROM accounts WHERE id = $1`, id,
	).Scan(&a.ID, &a.ExternalID, &a.AccountType, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "account", ID: id}
		}
		return nil, err
	}
	a.ID = compactUUID(a.ID)
	return &a, nil
}

func (r *PgAccountRepo) GetByExternalID(ctx context.Context, externalID string) (*domain.Account, error) {
	var a domain.Account
	err := r.pool.QueryRow(ctx, `SELECT id, external_id, account_type, created_at, updated_at FROM accounts WHERE external_id=$1`, externalID).
		Scan(&a.ID, &a.ExternalID, &a.AccountType, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ErrNotFound{Entity: "account", ID: externalID}
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PgAccountRepo) AddCustomer(ctx context.Context, accountID, customerID string, role domain.AccountRole) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO account_customers (account_id, customer_id, role) VALUES ($1, $2, $3)`,
		accountID, customerID, string(role),
	)
	return err
}

func (r *PgAccountRepo) ListCustomers(ctx context.Context, accountID string) ([]domain.AccountCustomer, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ac.account_id, ac.customer_id, ac.role
		 FROM account_customers ac
		 JOIN customers c ON c.id = ac.customer_id
		 WHERE ac.account_id = $1 AND c.purge_marked_at IS NULL`, accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.AccountCustomer
	for rows.Next() {
		var ac domain.AccountCustomer
		if err := rows.Scan(&ac.AccountID, &ac.CustomerID, &ac.Role); err != nil {
			return nil, err
		}
		ac.AccountID = compactUUID(ac.AccountID)
		ac.CustomerID = compactUUID(ac.CustomerID)
		result = append(result, ac)
	}
	return result, rows.Err()
}

// RepresentativeRiskScore takes MAX(risk_score) across every customer linked
// to accountID (the data model §1.1.3 "保守的評価"), returning nil if none of
// them has been scored yet.
func (r *PgAccountRepo) RepresentativeRiskScore(ctx context.Context, accountID string) (*float64, error) {
	var score *float64
	err := r.pool.QueryRow(ctx,
		`SELECT MAX(c.risk_score) FROM account_customers ac
		JOIN customers c ON c.id = ac.customer_id
		WHERE ac.account_id = $1 AND c.purge_marked_at IS NULL`,
		accountID,
	).Scan(&score)
	if err != nil {
		return nil, err
	}
	return score, nil
}
