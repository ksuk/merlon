package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ksuk/merlon/api/internal/domain"
)

// PgRuleRepo implements domain.RuleRepository against rule_definitions
// (migrations/001_init.sql, 008_rule_definitions_country_risk.sql). "id" in
// every method refers to RuleDefinition.Name, not the row's primary key: the
// primary key is regenerated on every version INSERT (CreateNewVersion never
// UPDATEs, per Auditability First), so name is the only value stable enough
// to keep resolving GET /api/v1/rules/{id}?version=N after a PUT creates a
// new row.
type PgRuleRepo struct {
	pool DBTX
}

// NewPostgresRuleRepo constructs the Postgres-backed domain.RuleRepository.
func NewPostgresRuleRepo(pool DBTX) *PgRuleRepo {
	return &PgRuleRepo{pool: pool}
}

const ruleColumns = "id, type, name, description, definition, version, is_active, created_by, created_at, updated_at"

func scanRule(row pgx.Row) (*domain.RuleDefinition, error) {
	var rd domain.RuleDefinition
	var description, createdBy *string
	var def []byte

	err := row.Scan(
		&rd.ID, &rd.Type, &rd.Name, &description, &def,
		&rd.Version, &rd.IsActive, &createdBy, &rd.CreatedAt, &rd.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if description != nil {
		rd.Description = *description
	}
	if createdBy != nil {
		rd.CreatedBy = *createdBy
	}
	rd.Definition = json.RawMessage(def)
	return &rd, nil
}

func (r *PgRuleRepo) Get(ctx context.Context, id string) (*domain.RuleDefinition, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM rule_definitions WHERE name = $1 ORDER BY version DESC LIMIT 1`,
		id,
	)
	rd, err := scanRule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "rule_definition", ID: id}
		}
		return nil, err
	}
	return rd, nil
}

func (r *PgRuleRepo) GetActive(ctx context.Context, id string) (*domain.RuleDefinition, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM rule_definitions WHERE name = $1 AND is_active = true ORDER BY version DESC LIMIT 1`,
		id,
	)
	rd, err := scanRule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "active_rule_definition", ID: id}
		}
		return nil, err
	}
	return rd, nil
}

func (r *PgRuleRepo) GetVersion(ctx context.Context, id string, version int) (*domain.RuleDefinition, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM rule_definitions WHERE name = $1 AND version = $2`,
		id, version,
	)
	rd, err := scanRule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "rule_definition", ID: fmt.Sprintf("%s@v%d", id, version)}
		}
		return nil, err
	}
	return rd, nil
}

func (r *PgRuleRepo) List(ctx context.Context, ruleType domain.RuleType, activeOnly bool, limit int, after *domain.Cursor) ([]domain.RuleDefinition, error) {
	query := `SELECT ` + ruleColumns + ` FROM rule_definitions WHERE 1=1`
	args := []any{}
	argN := 1

	if ruleType != "" {
		args = append(args, ruleType)
		query += fmt.Sprintf(" AND type = $%d", argN)
		argN++
	}
	if activeOnly {
		query += " AND is_active = true"
	}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", argN, argN+1)
		argN += 2
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", argN)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RuleDefinition
	for rows.Next() {
		rd, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rd)
	}
	return out, rows.Err()
}

func (r *PgRuleRepo) Create(ctx context.Context, rd *domain.RuleDefinition) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockRuleName(ctx, tx, rd.Name); err != nil {
		return err
	}
	rd.Version = 1
	_, err = tx.Exec(ctx,
		`INSERT INTO rule_definitions (id, type, name, description, definition, version, is_active, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rd.ID, rd.Type, rd.Name, nullableString(rd.Description), []byte(rd.Definition),
		rd.Version, rd.IsActive, nullableString(rd.CreatedBy), rd.CreatedAt, rd.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateNewVersion inserts a new row for rd.Name with an incremented version.
// A per-rule transaction advisory lock serializes the MAX(version) lookup and
// insert. It never UPDATEs an existing row (Auditability First — no overwrite
// of prior versions).
func (r *PgRuleRepo) CreateNewVersion(ctx context.Context, rd *domain.RuleDefinition) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := lockRuleName(ctx, tx, rd.Name); err != nil {
		return err
	}

	var maxVersion int
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM rule_definitions WHERE name = $1`, rd.Name).Scan(&maxVersion)
	if err != nil {
		return err
	}
	if maxVersion == 0 {
		return &domain.ErrNotFound{Entity: "rule_definition", ID: rd.Name}
	}
	rd.Version = maxVersion + 1

	if rd.IsActive {
		if _, err := tx.Exec(ctx, `UPDATE rule_definitions SET is_active = false WHERE name = $1`, rd.Name); err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO rule_definitions (id, type, name, description, definition, version, is_active, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rd.ID, rd.Type, rd.Name, nullableString(rd.Description), []byte(rd.Definition),
		rd.Version, rd.IsActive, nullableString(rd.CreatedBy), rd.CreatedAt, rd.UpdatedAt,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// SetActive changes rule state under a per-rule transaction lock. The target
// selection, maker-checker decision, state mutation, and approval record are
// committed atomically.
func (r *PgRuleRepo) SetActive(ctx context.Context, id string, active bool, actor string) (*domain.RuleStateChange, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := lockRuleName(ctx, tx, id); err != nil {
		return nil, err
	}

	latest, err := scanRule(tx.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM rule_definitions WHERE name = $1 ORDER BY version DESC LIMIT 1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "rule_definition", ID: id}
		}
		return nil, err
	}

	target := latest
	if !active {
		target, err = scanRule(tx.QueryRow(ctx,
			`SELECT `+ruleColumns+` FROM rule_definitions WHERE name = $1 AND is_active = true ORDER BY version DESC LIMIT 1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.RuleStateChange{
				Current: latest, TargetVersion: latest.Version, TargetCreatedBy: latest.CreatedBy,
			}, nil
		}
		if err != nil {
			return nil, err
		}
	}

	if actor == "" {
		return nil, separationOfDutiesError(target, "approver identity is missing")
	}
	if target.CreatedBy == "" {
		return nil, separationOfDutiesError(target, "rule creator identity is missing")
	}
	if target.CreatedBy == actor {
		return nil, separationOfDutiesError(target, "the rule author cannot change its active state")
	}

	changed := target.IsActive != active
	if changed {
		if _, err := tx.Exec(ctx, `UPDATE rule_definitions SET is_active = false WHERE name = $1`, id); err != nil {
			return nil, err
		}
		if active {
			if _, err := tx.Exec(ctx,
				`UPDATE rule_definitions SET is_active = true, updated_at = now() WHERE id = $1`, target.ID); err != nil {
				return nil, err
			}
		} else if _, err := tx.Exec(ctx,
			`UPDATE rule_definitions SET updated_at = now() WHERE id = $1`, target.ID); err != nil {
			return nil, err
		}

		if _, err := tx.Exec(ctx, `INSERT INTO rule_activation_events
			(rule_definition_id, rule_name, rule_version, requested_active, rule_created_by, approved_by, changed)
			VALUES ($1, $2, $3, $4, $5, $6, true)`,
			target.ID, target.Name, target.Version, active, target.CreatedBy, actor); err != nil {
			return nil, fmt.Errorf("record rule approval: %w", err)
		}
	}

	current, err := scanRule(tx.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM rule_definitions WHERE name = $1 ORDER BY version DESC LIMIT 1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.RuleStateChange{
		Current: current, TargetVersion: target.Version, TargetCreatedBy: target.CreatedBy, Changed: changed,
	}, nil
}

func lockRuleName(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('merlon.rule:' || $1, 0))`, name)
	return err
}
