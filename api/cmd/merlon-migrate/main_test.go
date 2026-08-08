package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func writeMigration(t *testing.T, dir, name, sql string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMigrationsSortsAndValidatesNames(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "002_second.sql", "SELECT 2;")
	writeMigration(t, dir, "001_first.sql", "SELECT 1;")

	migrations, err := loadMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 || migrations[0].version != "001" || migrations[1].version != "002" {
		t.Fatalf("migrations = %+v, want versions 001, 002", migrations)
	}
	if migrations[0].sha256 == "" || migrations[1].sha256 == "" {
		t.Fatal("migration checksum is empty")
	}

	writeMigration(t, dir, "bad.sql", "SELECT 3;")
	if _, err := loadMigrations(dir); err == nil {
		t.Fatal("invalid migration filename was accepted")
	}
}

func TestLoadMigrationsRejectsDuplicateVersion(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "001_first.sql", "SELECT 1;")
	writeMigration(t, dir, "001_duplicate.sql", "SELECT 2;")
	if _, err := loadMigrations(dir); err == nil || !strings.Contains(err.Error(), "duplicate migration version") {
		t.Fatalf("error = %v, want duplicate migration version", err)
	}
}

func TestMigrationRunnerAppliesAllMigrationsAndIsIdempotent(t *testing.T) {
	dsn := newMigrationTestDatabase(t)
	ctx := context.Background()
	opts := options{databaseURL: dsn, migrationsDir: "../../../migrations"}
	if err := runWithOptions(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if err := runWithOptions(ctx, opts); err != nil {
		t.Fatalf("second run: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 36 {
		t.Fatalf("schema_migrations count = %d, want 36", count)
	}
}

var appRoleDMLTables = []string{
	"account_customers",
	"accounts",
	"alerts",
	"api_keys",
	"backtest_job_customer_snapshots",
	"backtest_job_customers",
	"backtest_jobs",
	"batch_runs",
	"case_notes",
	"cases",
	"customer_score_history",
	"customers",
	"pending_evaluations",
	"refresh_tokens",
	"retention_policies",
	"rule_definitions",
	"screening_list_failures",
	"screening_list_snapshots",
	"screening_results",
	"seed_state",
	"transactions",
	"users",
	"webhook_deliveries",
	"webhook_dlq",
	"webhooks",
	"whitelist_entries",
	"whitelist_reviews",
}

var appRoleAppendOnlyTables = []string{"audit_logs", "rule_activation_events"}

func TestApplicationRoleGrantClassificationCoversMigrationTables(t *testing.T) {
	migrations, err := loadMigrations("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	tablePattern := regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(?:"?public"?)\s*\.\s*)?"?([a-z_][a-z0-9_]*)"?`)
	var migrationTables []string
	for _, migration := range migrations {
		sql, err := os.ReadFile(migration.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range tablePattern.FindAllStringSubmatch(string(sql), -1) {
			migrationTables = append(migrationTables, strings.ToLower(match[1]))
		}
	}
	classified := append(append([]string{}, appRoleDMLTables...), appRoleAppendOnlyTables...)
	const expectedApplicationTableCount = 29
	if len(migrationTables) != expectedApplicationTableCount {
		t.Fatalf("extracted %d migration tables, want %d: %v", len(migrationTables), expectedApplicationTableCount, migrationTables)
	}
	if len(classified) != expectedApplicationTableCount {
		t.Fatalf("classified %d application tables, want %d: %v", len(classified), expectedApplicationTableCount, classified)
	}
	sort.Strings(migrationTables)
	sort.Strings(classified)
	if strings.Join(migrationTables, ",") != strings.Join(classified, ",") {
		t.Fatalf("migration tables = %v, application-role grant classification = %v", migrationTables, classified)
	}
	grantsSQL, err := os.ReadFile("../../../docs/operations/audit-hardening.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(grantsSQL), `\set MERLON_APP_ROLE merlon_app`) {
		t.Error("direct psql execution has no safe MERLON_APP_ROLE default")
	}
	if got := strings.Count(string(grantsSQL), "'MAINTAIN'"); got != 3 {
		t.Errorf("application-role grant procedure classifies MAINTAIN %d times, want 3 (ordinary, append-only, ledger)", got)
	}
	extractArray := func(name string) []string {
		t.Helper()
		arrayPattern := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(name) + ` text\[\] := ARRAY\[(.*?)\];`)
		match := arrayPattern.FindStringSubmatch(string(grantsSQL))
		if match == nil {
			t.Fatalf("application-role grant procedure has no %s classification", name)
		}
		itemPattern := regexp.MustCompile(`'([a-z_][a-z0-9_]*)'`)
		var items []string
		for _, item := range itemPattern.FindAllStringSubmatch(match[1], -1) {
			items = append(items, item[1])
		}
		sort.Strings(items)
		return items
	}
	wantDML := append([]string{}, appRoleDMLTables...)
	wantAppendOnly := append([]string{}, appRoleAppendOnlyTables...)
	sort.Strings(wantDML)
	sort.Strings(wantAppendOnly)
	if got := extractArray("dml_tables"); strings.Join(got, ",") != strings.Join(wantDML, ",") {
		t.Errorf("dml_tables = %v, want %v", got, wantDML)
	}
	if got := extractArray("append_only_tables"); strings.Join(got, ",") != strings.Join(wantAppendOnly, ",") {
		t.Errorf("append_only_tables = %v, want %v", got, wantAppendOnly)
	}
	for _, object := range []string{"public.schema_migrations", "public.audit_logs_id_seq"} {
		if !strings.Contains(string(grantsSQL), object) {
			t.Errorf("application-role grant procedure does not classify %s", object)
		}
	}
}

func TestApplicationRoleGrantsAreIdempotentAndLeastPrivilege(t *testing.T) {
	dsn := newMigrationTestDatabase(t)
	ctx := context.Background()
	if err := runWithOptions(ctx, options{databaseURL: dsn, migrationsDir: "../../../migrations"}); err != nil {
		t.Fatal(err)
	}

	owner, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	role := "merlon_app_test_" + hex.EncodeToString(random)
	password := "test_" + hex.EncodeToString(random)
	if _, err := owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	var databaseName string
	if err := owner.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx,
		"REVOKE CONNECT ON DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" FROM PUBLIC; "+
			"REVOKE CONNECT ON DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" FROM "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close(context.Background())
		_, _ = cleanup.Exec(context.Background(), "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = cleanup.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	})
	if _, err := owner.Exec(ctx,
		"GRANT ALL PRIVILEGES ON SCHEMA public TO PUBLIC, "+pgx.Identifier{role}.Sanitize()+"; "+
			"GRANT ALL PRIVILEGES ON TABLE public.customers, public.audit_logs, public.rule_activation_events, public.schema_migrations TO PUBLIC, "+pgx.Identifier{role}.Sanitize()+"; "+
			"GRANT ALL PRIVILEGES ON SEQUENCE public.audit_logs_id_seq TO PUBLIC, "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}

	grantsSQL, err := os.ReadFile("../../../docs/operations/audit-hardening.sql")
	if err != nil {
		t.Fatal(err)
	}
	var serverSQL []string
	for _, line := range strings.Split(string(grantsSQL), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), `\`) {
			serverSQL = append(serverSQL, line)
		}
	}
	sql := strings.ReplaceAll(strings.Join(serverSQL, "\n"), ":'MERLON_APP_ROLE'", "'"+role+"'")
	for run := 1; run <= 2; run++ {
		if _, err := owner.Exec(ctx, sql); err != nil {
			t.Fatalf("application-role grants run %d: %v", run, err)
		}
	}

	for _, table := range appRoleDMLTables {
		for _, test := range []struct {
			privilege string
			want      bool
		}{
			{privilege: "SELECT", want: true},
			{privilege: "INSERT", want: true},
			{privilege: "UPDATE", want: true},
			{privilege: "DELETE", want: true},
			{privilege: "TRUNCATE", want: false},
			{privilege: "REFERENCES", want: false},
			{privilege: "TRIGGER", want: false},
			{privilege: "MAINTAIN", want: false},
		} {
			var allowed bool
			if err := owner.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, role, "public."+table, test.privilege).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != test.want {
				t.Errorf("%s %s on %s = %t, want %t", role, test.privilege, table, allowed, test.want)
			}
		}
	}
	for _, table := range appRoleAppendOnlyTables {
		for _, test := range []struct {
			privilege string
			want      bool
		}{
			{privilege: "SELECT", want: true},
			{privilege: "INSERT", want: true},
			{privilege: "UPDATE", want: false},
			{privilege: "DELETE", want: false},
			{privilege: "TRUNCATE", want: false},
			{privilege: "REFERENCES", want: false},
			{privilege: "TRIGGER", want: false},
			{privilege: "MAINTAIN", want: false},
		} {
			var allowed bool
			if err := owner.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, role, "public."+table, test.privilege).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != test.want {
				t.Errorf("%s %s on %s = %t, want %t", role, test.privilege, table, allowed, test.want)
			}
		}
	}

	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER", "MAINTAIN"} {
		var allowed bool
		if err := owner.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.schema_migrations', $2)`, role, privilege).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Errorf("%s retains %s on schema_migrations", role, privilege)
		}
	}

	var sequenceUsable, sequenceReadable, sequenceUpdatable, schemaUsable, schemaCreatable, databaseConnectable bool
	if err := owner.QueryRow(ctx, `SELECT
		has_sequence_privilege($1, 'public.audit_logs_id_seq', 'USAGE'),
		has_sequence_privilege($1, 'public.audit_logs_id_seq', 'SELECT'),
		has_sequence_privilege($1, 'public.audit_logs_id_seq', 'UPDATE'),
		has_schema_privilege($1, 'public', 'USAGE'),
		has_schema_privilege($1, 'public', 'CREATE'),
		has_database_privilege($1, current_database(), 'CONNECT')`, role).
		Scan(&sequenceUsable, &sequenceReadable, &sequenceUpdatable, &schemaUsable, &schemaCreatable, &databaseConnectable); err != nil {
		t.Fatal(err)
	}
	if !sequenceUsable || sequenceReadable || sequenceUpdatable || !schemaUsable || schemaCreatable || !databaseConnectable {
		t.Fatalf("sequence_usage=%t sequence_select=%t sequence_update=%t schema_usage=%t schema_create=%t database_connect=%t", sequenceUsable, sequenceReadable, sequenceUpdatable, schemaUsable, schemaCreatable, databaseConnectable)
	}

	appConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	appConfig.User = role
	appConfig.Password = password
	app, err := pgx.ConnectConfig(ctx, appConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(ctx)

	var auditID int64
	if err := app.QueryRow(ctx, `INSERT INTO audit_logs(action, resource_type) VALUES ('restore-test', 'restore-test') RETURNING id`).Scan(&auditID); err != nil {
		t.Fatalf("append audit log using restored sequence grant: %v", err)
	}
	if _, err := app.Exec(ctx, `UPDATE audit_logs SET action = 'tampered' WHERE id = $1`, auditID); err == nil {
		t.Fatal("application role updated append-only audit_logs")
	}
	if _, err := app.Exec(ctx, `CREATE TABLE must_not_create (id integer)`); err == nil {
		t.Fatal("application role created a table in public schema")
	}

	extraRole := role + "_extra"
	if _, err := owner.Exec(ctx,
		"CREATE ROLE "+pgx.Identifier{extraRole}.Sanitize()+"; "+
			"GRANT MAINTAIN ON TABLE public.customers TO "+pgx.Identifier{extraRole}.Sanitize()+"; "+
			"GRANT "+pgx.Identifier{extraRole}.Sanitize()+" TO "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close(context.Background())
		_, _ = cleanup.Exec(context.Background(), "REVOKE "+pgx.Identifier{extraRole}.Sanitize()+" FROM "+pgx.Identifier{role}.Sanitize())
		_, _ = cleanup.Exec(context.Background(), "REVOKE CREATE ON DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" FROM "+pgx.Identifier{extraRole}.Sanitize())
		_, _ = cleanup.Exec(context.Background(), "DROP OWNED BY "+pgx.Identifier{extraRole}.Sanitize())
		_, _ = cleanup.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{extraRole}.Sanitize())
	})
	if _, err := owner.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "inherits forbidden MAINTAIN") {
		t.Fatalf("membership-derived MAINTAIN was not rejected: %v", err)
	}
	if _, err := owner.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("rollback after expected MAINTAIN rejection: %v", err)
	}
	if _, err := owner.Exec(ctx,
		"REVOKE MAINTAIN ON TABLE public.customers FROM "+pgx.Identifier{extraRole}.Sanitize()+"; "+
			"GRANT UPDATE ON SEQUENCE public.audit_logs_id_seq TO "+pgx.Identifier{extraRole}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "inherits forbidden UPDATE on audit sequence") {
		t.Fatalf("membership-derived audit sequence UPDATE was not rejected: %v", err)
	}
	if _, err := owner.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("rollback after expected audit sequence rejection: %v", err)
	}
	if _, err := owner.Exec(ctx,
		"REVOKE UPDATE ON SEQUENCE public.audit_logs_id_seq FROM "+pgx.Identifier{extraRole}.Sanitize()+"; "+
			"GRANT SELECT ON SEQUENCE public.audit_logs_id_seq TO "+pgx.Identifier{extraRole}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "inherits forbidden SELECT on audit sequence") {
		t.Fatalf("membership-derived audit sequence SELECT was not rejected: %v", err)
	}
	if _, err := owner.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("rollback after expected audit sequence SELECT rejection: %v", err)
	}
	if _, err := owner.Exec(ctx,
		"REVOKE SELECT ON SEQUENCE public.audit_logs_id_seq FROM "+pgx.Identifier{extraRole}.Sanitize()+"; "+
			"GRANT CREATE ON DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" TO "+pgx.Identifier{extraRole}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "has forbidden CREATE on database") {
		t.Fatalf("membership-derived database CREATE was not rejected: %v", err)
	}
	if _, err := owner.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("rollback after expected database CREATE rejection: %v", err)
	}
}

func TestMigrationRunnerDetectsChecksumMismatch(t *testing.T) {
	dsn := newMigrationTestDatabase(t)
	dir := t.TempDir()
	writeMigration(t, dir, "001_test.sql", "CREATE TABLE checksum_test (id integer);")
	opts := options{databaseURL: dsn, migrationsDir: dir}
	if err := runWithOptions(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	writeMigration(t, dir, "001_test.sql", "CREATE TABLE checksum_test (id bigint);")
	if err := runWithOptions(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}

func TestMigrationRunnerBaselineAndRollback(t *testing.T) {
	t.Run("baseline", func(t *testing.T) {
		dsn := newMigrationTestDatabase(t)
		dir := t.TempDir()
		writeMigration(t, dir, "001_existing.sql", "CREATE TABLE existing_table (id integer);")
		writeMigration(t, dir, "002_after.sql", "CREATE TABLE after_table (id integer);")

		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `CREATE TABLE existing_table (id integer)`); err != nil {
			t.Fatal(err)
		}
		conn.Close(ctx)

		if err := runWithOptions(ctx, options{databaseURL: dsn, migrationsDir: dir, baseline: "001_existing.sql"}); err != nil {
			t.Fatal(err)
		}
		conn, err = pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		var name string
		if err := conn.QueryRow(ctx, `SELECT name FROM schema_migrations WHERE version = '001'`).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != "baseline:001_existing.sql" {
			t.Fatalf("baseline ledger name = %q", name)
		}
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass('public.after_table') IS NOT NULL`).Scan(&exists); err != nil || !exists {
			t.Fatalf("after_table exists = %t, err = %v", exists, err)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		dsn := newMigrationTestDatabase(t)
		dir := t.TempDir()
		writeMigration(t, dir, "001_broken.sql", "CREATE TABLE must_rollback (id integer); SELECT invalid syntax;")
		if err := runWithOptions(context.Background(), options{databaseURL: dsn, migrationsDir: dir}); err == nil {
			t.Fatal("broken migration succeeded")
		}
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(context.Background())
		var exists bool
		if err := conn.QueryRow(context.Background(), `SELECT to_regclass('public.must_rollback') IS NOT NULL`).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("table from failed migration was not rolled back")
		}
	})
}

func newMigrationTestDatabase(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "merlon_migrate_test_" + hex.EncodeToString(random)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := pgx.ConnectConfig(context.Background(), cfg)
		if err != nil {
			return
		}
		defer cleanup.Close(context.Background())
		_, _ = cleanup.Exec(context.Background(), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		_, _ = cleanup.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize())
	})

	parsed, err := url.Parse(dsn)
	if err == nil && parsed.Scheme != "" {
		parsed.Path = "/" + name
		return parsed.String()
	}
	return fmt.Sprintf("%s dbname=%s", dsn, name)
}
