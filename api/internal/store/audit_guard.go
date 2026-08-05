package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AppendOnlyTables is the canonical list shared by the migration grants,
// database triggers, and the serving-role startup guard. Keeping the list in
// one Go value makes a newly introduced immutable stream fail closed until its
// ACL and trigger are reviewed alongside the migration.
var AppendOnlyTables = []string{
	"alert_decision_events",
	"audit_logs",
	"case_events",
	"case_evidence",
	"case_relationship_events",
	"rule_activation_events",
	"str_report_events",
}

// VerifyAuditLogPrivileges is a production startup guard. The serving role
// must not own append-only audit tables and must not be able to UPDATE or
// DELETE them.
func VerifyAuditLogPrivileges(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range AppendOnlyTables {
		var role, owner string
		var canUpdate, canDelete, hasTrigger bool
		err := pool.QueryRow(ctx, `
		SELECT current_user,
		       COALESCE((SELECT tableowner FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = $1), ''),
		       has_table_privilege(current_user, 'public.' || $1, 'UPDATE'),
		       has_table_privilege(current_user, 'public.' || $1, 'DELETE'),
		       EXISTS (SELECT 1 FROM pg_catalog.pg_trigger t
		                 JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
		                 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		                WHERE n.nspname = 'public' AND c.relname = $1
		                  AND NOT t.tgisinternal)`, table).
			Scan(&role, &owner, &canUpdate, &canDelete, &hasTrigger)
		if err != nil {
			return fmt.Errorf("%s privilege preflight: %w", table, err)
		}
		if owner == "" {
			return fmt.Errorf("%s privilege preflight: table does not exist", table)
		}
		if owner == role || canUpdate || canDelete || !hasTrigger {
			return fmt.Errorf("unsafe %s privileges or protection for app role %q (owner=%q update=%t delete=%t trigger=%t); use a separate migration owner", table, role, owner, canUpdate, canDelete, hasTrigger)
		}
	}
	return nil
}
