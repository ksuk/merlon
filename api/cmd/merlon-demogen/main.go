// Command merlon-demogen generates Merlon's deterministic synthetic demo
// dataset (PH7 T1: customers, accounts, CDD score history, transactions,
// alerts, cases, screening, audit logs, and rule definitions), scored and
// evaluated through the real native engine so a live re-score/re-evaluation
// during a demo reproduces the same values (Auditability First). See
// .release-tasks/PH7-demo-publication.md Appendix A.
//
// Usage:
//
//	cd api && go run ./cmd/merlon-demogen -out ../deploy/seed/demo
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ksuk/merlon/api/internal/demogen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "merlon-demogen:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "../deploy/seed/demo", "output directory for customers.json/accounts.json/score_history.json")
	seed := flag.Int64("seed", demogen.DefaultSeed, "deterministic PRNG seed")
	anchorStr := flag.String("anchor", demogen.DefaultAnchorStr, "anchor date (YYYY-MM-DD) every generated date is relative to")
	customers := flag.Int("customers", demogen.DefaultCustomers, "number of customers to generate (maximum 1000)")
	flag.Parse()

	anchor, err := time.ParseInLocation("2006-01-02", *anchorStr, time.UTC)
	if err != nil {
		return fmt.Errorf("invalid -anchor %q: %w", *anchorStr, err)
	}

	result, err := demogen.Generate(demogen.Options{
		Seed:      *seed,
		Anchor:    anchor,
		Customers: *customers,
	})
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	if err := demogen.SelfCheck(result.Customers, anchor); err != nil {
		return err
	}
	if err := demogen.SelfCheckW2(result); err != nil {
		return err
	}

	if err := result.WriteFiles(*out); err != nil {
		return err
	}

	fmt.Printf(
		"generated %d customers (%d accounts, %d score_history entries, %d transactions, %d alerts, %d cases, %d screening_results, %d audit_logs, %d rule_definitions) into %s (seed=%d anchor=%s)\n",
		len(result.Customers), len(result.Accounts), len(result.ScoreHistory), len(result.Transactions), len(result.Alerts),
		len(result.Cases), len(result.ScreeningResults), len(result.AuditLogs), len(result.RuleDefinitions),
		*out, *seed, anchor.Format("2006-01-02"),
	)
	fmt.Printf("story customers: %v\n", result.StoryCustomerIDs)
	return nil
}
