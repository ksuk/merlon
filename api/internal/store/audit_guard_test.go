package store

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyAuditLogPrivilegesRejectsTheMigrationOwner(t *testing.T) {
	pool := newTestPgPool(t)
	err := VerifyAuditLogPrivileges(context.Background(), pool)
	if err == nil {
		t.Fatal("startup guard accepted the migration/database owner")
	}
	if !strings.Contains(err.Error(), "owner") {
		t.Fatalf("startup guard error = %v, want explicit owner/privilege rejection", err)
	}
}
