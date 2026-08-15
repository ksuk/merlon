package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/crypto"
	"github.com/ksuk/merlon/api/internal/ingestion"
	"github.com/ksuk/merlon/api/internal/store"
)

type options struct {
	sourceDir   string
	dryRun      bool
	apply       bool
	onDuplicate ingestion.DuplicateMode
	actor       string
	reportJSON  string
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		slog.Error("bulk import failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out, errOut io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.sourceDir == "" {
		return errors.New("--source-dir is required")
	}
	if opts.dryRun && opts.apply {
		return errors.New("--dry-run and --apply are mutually exclusive")
	}
	if !opts.dryRun && !opts.apply {
		opts.dryRun = true
	}
	service := &ingestion.Importer{}
	var pool *pgxpool.Pool
	if opts.apply {
		databaseURL := getenv("MERLON_DATABASE_URL")
		if databaseURL == "" {
			return errors.New("MERLON_DATABASE_URL is required with --apply")
		}
		pool, err = pgxpool.New(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		var encryptor *crypto.Encryptor
		if getenv("MERLON_ENCRYPTION_KEY_RING") != "" {
			keyRing, keyErr := crypto.NewKeyRingFromEnv("MERLON_ENCRYPTION_KEY_RING")
			if keyErr != nil {
				return fmt.Errorf("parse MERLON_ENCRYPTION_KEY_RING: %w", keyErr)
			}
			encryptor = crypto.NewEncryptor(keyRing)
		}
		customers := store.NewPgCustomerRepo(pool, encryptor)
		service.Deps = ingestion.Dependencies{Customers: customers, Accounts: store.NewPgAccountRepo(pool), Transactions: store.NewPgTransactionRepo(pool)}
	}
	report, err := service.Run(ctx, ingestion.Options{SourceDir: opts.sourceDir, DryRun: opts.dryRun, Apply: opts.apply, OnDuplicate: opts.onDuplicate, Actor: opts.actor, ReportJSON: opts.reportJSON})
	if err == nil && opts.apply && report != nil {
		if persistErr := persistReport(ctx, pool, report); persistErr != nil {
			return persistErr
		}
	}
	if report != nil {
		if opts.reportJSON != "" {
			file, writeErr := os.OpenFile(opts.reportJSON, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if writeErr != nil {
				return writeErr
			}
			writeErr = ingestion.WriteReport(file, report)
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
		} else if writeErr := ingestion.WriteReport(out, report); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func persistReport(ctx context.Context, pool *pgxpool.Pool, report *ingestion.Report) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	files, _ := json.Marshal(report.SourceFiles)
	counts, _ := json.Marshal(report.Counts)
	if _, err := tx.Exec(ctx, `INSERT INTO import_runs(id,source_dir,actor,mode,status,source_files,result_counts,started_at,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, report.ID, report.SourceDir, report.Actor, "apply", "completed", files, counts, report.StartedAt, report.CompletedAt); err != nil {
		return err
	}
	for _, outcome := range report.Outcomes {
		if _, err := tx.Exec(ctx, `INSERT INTO import_record_outcomes(run_id,entity_type,external_id,source_file,line_number,payload_sha256,status,reason_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, report.ID, outcome.EntityType, outcome.ExternalID, outcome.SourceFile, outcome.Line, outcome.PayloadSHA256, outcome.Status, outcome.ReasonCode); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func parseOptions(args []string) (options, error) {
	var out options
	fs := flag.NewFlagSet("merlon-import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out.sourceDir, "source-dir", "", "directory containing fixed CSV source files")
	fs.BoolVar(&out.dryRun, "dry-run", false, "validate without changing the database (default when --apply is absent)")
	fs.BoolVar(&out.apply, "apply", false, "persist accepted source records")
	fs.Var((*duplicateModeValue)(&out.onDuplicate), "on-duplicate", "skip or error when an external_id already exists")
	fs.StringVar(&out.actor, "actor", "", "operator/service actor recorded on the import run")
	fs.StringVar(&out.reportJSON, "report-json", "", "write the machine-readable report to this path")
	if err := fs.Parse(args); err != nil {
		return out, err
	}
	if fs.NArg() != 0 {
		return out, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if out.onDuplicate == "" {
		out.onDuplicate = ingestion.DuplicateSkip
	}
	if out.onDuplicate != ingestion.DuplicateSkip && out.onDuplicate != ingestion.DuplicateError {
		return out, fmt.Errorf("--on-duplicate must be skip or error")
	}
	return out, nil
}

type duplicateModeValue ingestion.DuplicateMode

func (v *duplicateModeValue) String() string         { return string(*v) }
func (v *duplicateModeValue) Set(value string) error { *v = duplicateModeValue(value); return nil }
