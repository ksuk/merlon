package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyAuditLogPrivileges is a production startup guard. The serving role
// must not own append-only audit tables and must not be able to UPDATE or
// DELETE them.
func VerifyAuditLogPrivileges(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range []string{"audit_logs", "rule_activation_events"} {
		var role, owner string
		var canUpdate, canDelete bool
		err := pool.QueryRow(ctx, `
		SELECT current_user,
		       COALESCE((SELECT tableowner FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = $1), ''),
		       has_table_privilege(current_user, 'public.' || $1, 'UPDATE'),
		       has_table_privilege(current_user, 'public.' || $1, 'DELETE')`, table).
			Scan(&role, &owner, &canUpdate, &canDelete)
		if err != nil {
			return fmt.Errorf("%s privilege preflight: %w", table, err)
		}
		if owner == "" {
			return fmt.Errorf("%s privilege preflight: table does not exist", table)
		}
		if owner == role || canUpdate || canDelete {
			return fmt.Errorf("unsafe %s privileges for app role %q (owner=%q update=%t delete=%t); use a separate migration owner", table, role, owner, canUpdate, canDelete)
		}
	}
	return nil
}
