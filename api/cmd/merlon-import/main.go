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
	if opts.apply && strings.TrimSpace(opts.actor) == "" {
		return errors.New("--actor is required with --apply")
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
		service.Recorder = &pgImportRunRecorder{pool: pool}
	}
	report, err := service.Run(ctx, ingestion.Options{SourceDir: opts.sourceDir, DryRun: opts.dryRun, Apply: opts.apply, OnDuplicate: opts.onDuplicate, Actor: opts.actor, ReportJSON: opts.reportJSON})
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

type pgImportRunRecorder struct{ pool *pgxpool.Pool }

func (r *pgImportRunRecorder) Start(ctx context.Context, report *ingestion.Report) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO import_runs(id,source_dir,actor,mode,status,source_files,result_counts,started_at)
		VALUES($1,$2,$3,'apply','running','{}','{}',$4)`, report.ID, report.SourceDir, report.Actor, report.StartedAt)
	return err
}

func (r *pgImportRunRecorder) Finish(ctx context.Context, report *ingestion.Report, runErr error) error {
	ctx = context.WithoutCancel(ctx)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	files, _ := json.Marshal(report.SourceFiles)
	counts, _ := json.Marshal(report.Counts)
	status := "completed"
	errorMessage := ""
	if runErr != nil {
		status = "failed"
		errorMessage = runErr.Error()
	}
	if _, err := tx.Exec(ctx, `UPDATE import_runs SET status=$2,source_files=$3,result_counts=$4,error_message=$5,completed_at=$6 WHERE id=$1`, report.ID, status, files, counts, errorMessage, report.CompletedAt); err != nil {
		return err
	}
	for _, outcome := range report.Outcomes {
		if _, err := tx.Exec(ctx, `INSERT INTO import_record_outcomes(run_id,entity_type,external_id,source_file,line_number,payload_sha256,status,reason_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (run_id,entity_type,external_id,source_file,line_number) DO NOTHING`, report.ID, outcome.EntityType, outcome.ExternalID, outcome.SourceFile, outcome.Line, outcome.PayloadSHA256, outcome.Status, outcome.ReasonCode); err != nil {
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
