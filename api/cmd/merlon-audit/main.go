// Command merlon-audit provides a coreプラン向けの監査ログ完全性検証CLI
// (the audit design §7). It ships as a single Go binary in the same image as
// merlon-api (api/cmd/merlon-api), so no separate deployment is required.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
	"github.com/ksuk/merlon/api/internal/store"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run implements the verify subcommand and returns the process exit code
// the audit design §7 specifies: 0 no anomaly, 1 anomaly detected, 2 execution
// error (bad flags, connection failure, query error). Exposed separately
// from main so tests can drive it without forking a subprocess.
func run(args []string, stdout io.Writer) int {
	if len(args) == 0 || args[0] != "verify" {
		fmt.Fprintln(stdout, "usage: merlon-audit verify [--database-url URL] [--since RFC3339] [--until RFC3339] [--format text|json] [--drop-threshold FLOAT] [--read-only]")
		return 2
	}

	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stdout)
	dbURL := fs.String("database-url", os.Getenv("MERLON_DATABASE_URL"), "PostgreSQL connection string (defaults to MERLON_DATABASE_URL)")
	sinceStr := fs.String("since", "", "only consider rows at or after this RFC3339 timestamp")
	untilStr := fs.String("until", "", "only consider rows at or before this RFC3339 timestamp")
	format := fs.String("format", "text", "output format: text or json")
	dropThreshold := fs.Float64("drop-threshold", defaultDropThreshold, "fraction below the 7-day moving average that counts as a drop")
	readOnly := fs.Bool("read-only", false, "the connection cannot INSERT; skip recording this run to the audit log")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	opts := VerifyOptions{Format: *format, DropThreshold: *dropThreshold, ReadOnly: *readOnly}
	if *sinceStr != "" {
		t, err := time.Parse(time.RFC3339, *sinceStr)
		if err != nil {
			fmt.Fprintf(stdout, "invalid --since: %v\n", err)
			return 2
		}
		opts.Since = &t
	}
	if *untilStr != "" {
		t, err := time.Parse(time.RFC3339, *untilStr)
		if err != nil {
			fmt.Fprintf(stdout, "invalid --until: %v\n", err)
			return 2
		}
		opts.Until = &t
	}

	if *dbURL == "" {
		fmt.Fprintln(stdout, "error: --database-url or MERLON_DATABASE_URL is required")
		return 2
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fmt.Fprintf(stdout, "database connection error: %v\n", err)
		return 2
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(stdout, "database connection error: %v\n", err)
		return 2
	}

	result, err := Verify(ctx, pool, opts)
	if err != nil {
		fmt.Fprintf(stdout, "verify error: %v\n", err)
		return 2
	}

	// the audit design §7 監査記録: verify の実行自体も監査ログに記録する。read-only
	// 接続時は書き込みできないためスキップする (§7 表の該当欄)。
	if opts.ReadOnly {
		fmt.Fprintln(stdout, "read-only connection: skipping audit log write for this run")
	} else {
		auditRepo := store.NewPgAuditRepo(pool)
		if err := auditRepo.Create(ctx, &domain.AuditEntry{
			Action:       "audit_verify",
			ResourceType: "audit_log",
			Details:      map[string]string{"has_anomalies": fmt.Sprintf("%v", result.HasAnomalies())},
			CreatedAt:    time.Now(),
		}); err != nil {
			fmt.Fprintf(stdout, "audit log write error: %v\n", err)
			return 2
		}
	}

	if result.HasAnomalies() {
		metrics.AuditIntegrityCheckFailedTotal.Inc()
	}

	switch opts.Format {
	case "json":
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stdout, "output error: %v\n", err)
			return 2
		}
	default:
		printText(stdout, result)
	}

	if result.HasAnomalies() {
		return 1
	}
	return 0
}

func printText(w io.Writer, r VerifyResult) {
	if !r.HasAnomalies() {
		fmt.Fprintln(w, "OK: no audit_logs integrity anomalies detected")
		return
	}
	for _, gap := range r.IDGaps {
		fmt.Fprintf(w, "ID_GAP: %d -> %d\n", gap.PreviousID, gap.NextID)
	}
	for _, tr := range r.TimeRegressions {
		fmt.Fprintf(w, "TIME_REGRESSION: id=%d created_at=%s is before previous id=%d created_at=%s\n",
			tr.ID, tr.CreatedAt.Format(time.RFC3339), tr.PreviousID, tr.PreviousCreatedAt.Format(time.RFC3339))
	}
	for _, drop := range r.CountDrops {
		fmt.Fprintf(w, "COUNT_DROP: day=%s count=%d moving_average_7d=%.1f\n",
			drop.Day.Format("2006-01-02"), drop.Count, drop.MovingAverage)
	}
}
