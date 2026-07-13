package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type migration struct {
	version string
	name    string
	path    string
	sha256  string
}

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	url := os.Getenv("MERLON_MIGRATION_DATABASE_URL")
	if url == "" {
		url = os.Getenv("MERLON_DATABASE_URL")
		if os.Getenv("MERLON_ENV") == "production" {
			return errors.New("MERLON_MIGRATION_DATABASE_URL is required in production")
		}
		if url == "" {
			return errors.New("MERLON_MIGRATION_DATABASE_URL or MERLON_DATABASE_URL is required")
		}
		slog.Warn("using MERLON_DATABASE_URL as migration role; production must use a separate role")
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY, name text NOT NULL, checksum text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := pool.Exec(ctx, `SELECT pg_advisory_lock(hashtext('merlon.schema_migrations'))`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer pool.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('merlon.schema_migrations'))`)

	migrations, err := loadMigrations("migrations")
	if err != nil {
		return err
	}
	if err := applyBaselineIfRequested(ctx, pool, migrations); err != nil {
		return err
	}
	for _, m := range migrations {
		var checksum string
		err := pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version = $1`, m.version).Scan(&checksum)
		if err == nil {
			if checksum != m.sha256 {
				return fmt.Errorf("migration %s checksum mismatch: ledger=%s file=%s", m.version, checksum, m.sha256)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		sql, err := os.ReadFile(m.path)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version, name, checksum) VALUES ($1, $2, $3)`, m.version, m.name, m.sha256)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", m.name, err)
		}
		slog.Info("migration applied", "version", m.version, "name", m.name)
	}
	return nil
}

func loadMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration filename must start with numeric version: %s", entry.Name())
		}
		h := sha256.Sum256(data)
		out = append(out, migration{version: parts[0], name: entry.Name(), path: path, sha256: hex.EncodeToString(h[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func applyBaselineIfRequested(ctx context.Context, pool *pgxpool.Pool, migrations []migration) error {
	baseline := os.Getenv("MERLON_MIGRATION_BASELINE")
	if baseline == "" {
		return nil
	}
	found := false
	for _, m := range migrations {
		if m.name > baseline {
			break
		}
		if m.name == baseline {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("MERLON_MIGRATION_BASELINE %q does not match a migration filename", baseline)
	}
	for _, m := range migrations {
		if m.name > baseline {
			break
		}
		_, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES ($1, $2, $3, $4) ON CONFLICT (version) DO NOTHING`, m.version, "baseline:"+m.name, m.sha256, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("record baseline %s: %w", m.name, err)
		}
	}
	return nil
}
