package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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
	if count != 35 {
		t.Fatalf("schema_migrations count = %d, want 35", count)
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
