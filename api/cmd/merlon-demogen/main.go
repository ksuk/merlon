// Command merlon-demogen generates Merlon's deterministic synthetic demo
// dataset (PH7 T1-W1): customers, accounts, and CDD score history, scored
// through the real native engine against the funds_transfer CDD preset so a
// live re-score during a demo reproduces the same value (Auditability
// First). See .release-tasks/PH7-demo-publication.md Appendix A.
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
	customers := flag.Int("customers", demogen.DefaultCustomers, "number of customers to generate")
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

	if err := result.WriteFiles(*out); err != nil {
		return err
	}

	fmt.Printf(
		"generated %d customers (%d accounts, %d score_history entries) into %s (seed=%d anchor=%s)\n",
		len(result.Customers), len(result.Accounts), len(result.ScoreHistory), *out, *seed, anchor.Format("2006-01-02"),
	)
	fmt.Printf("story customers: %v\n", result.StoryCustomerIDs)
	return nil
}
