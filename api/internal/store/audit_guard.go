package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyAuditLogPrivileges is a production startup guard. The serving role
// must not own audit_logs and must not be able to UPDATE or DELETE it.
func VerifyAuditLogPrivileges(ctx context.Context, pool *pgxpool.Pool) error {
	var role, owner string
	var canUpdate, canDelete bool
	err := pool.QueryRow(ctx, `
		SELECT current_user,
		       COALESCE((SELECT tableowner FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = 'audit_logs'), ''),
		       has_table_privilege(current_user, 'public.audit_logs', 'UPDATE'),
		       has_table_privilege(current_user, 'public.audit_logs', 'DELETE')`).
		Scan(&role, &owner, &canUpdate, &canDelete)
	if err != nil {
		return fmt.Errorf("audit_logs privilege preflight: %w", err)
	}
	if owner == "" {
		return fmt.Errorf("audit_logs privilege preflight: table does not exist")
	}
	if owner == role || canUpdate || canDelete {
		return fmt.Errorf("unsafe audit_logs privileges for app role %q (owner=%q update=%t delete=%t); use a separate migration owner", role, owner, canUpdate, canDelete)
	}
	return nil
}
