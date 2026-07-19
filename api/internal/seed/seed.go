// Package seed loads development/demo data into a freshly started API
// process (MERLON_SEED=true).
//
// Two datasets are supported:
//
//   - The demogen dataset (deploy/seed/demo/*.json, produced by `make
//     demogen` / api/cmd/merlon-demogen): ~1,000 synthetic customers plus
//     transactions, alerts, cases, screening hits, rule definitions, and
//     audit log entries (PH7 "recorded demo"). Loaded when
//     MERLON_DEMO_DATA_DIR points at a directory containing the full
//     dataset (see loader.go).
//   - The built-in hardcoded sample (5 customers), used whenever the demogen
//     dataset isn't available, preserving the pre-PH7 behavior.
//
// Both paths write through the same Repos (store) interfaces — the loader
// makes no assumption about whether it's talking to the in-memory store or
// PostgreSQL (Configuration as the Product / D-b in
// .release-tasks/PH7-demo-publication.md).
package seed

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// Repos bundles the repository interfaces the seeder writes to. Fields left
// nil are simply skipped by the hardcoded fallback (as before); the demogen
// dataset loader requires Accounts, ScreeningResults, and Rules in addition
// to the original five, since the demogen dataset covers those entities too.
type Repos struct {
	Customers        domain.CustomerRepository
	Transactions     domain.TransactionRepository
	Alerts           domain.AlertRepository
	Cases            domain.CaseRepository
	Audit            domain.AuditRepository
	Accounts         domain.AccountRepository
	ScreeningResults domain.ScreeningResultRepository
	Rules            domain.RuleRepository
}

// demoDataDirEnv is checked directly (rather than threaded through
// api/internal/config) to keep this wave's change footprint limited to the
// seed package: config.go / main.go are otherwise untouched by PH7 T2 aside
// from wiring the extra repos below.
const demoDataDirEnv = "MERLON_DEMO_DATA_DIR"

// Run seeds repos, choosing between the demogen JSON dataset and the
// built-in hardcoded sample. It is a no-op (skips seeding entirely) if repos
// already contains data, so restarting a compose stack without wiping its
// volume does not attempt to re-insert the same rows (the wave-T2
// instructions' "二重投入防止").
//
// Errors are returned for the caller to log, matching the project's
// existing seed error-handling posture: a seeding problem is surfaced, not
// fatal to the API process starting up.
func Run(ctx context.Context, repos Repos) error {
	seeded, err := alreadySeeded(ctx, repos)
	if err != nil {
		return fmt.Errorf("seed: checking for existing data: %w", err)
	}
	if seeded {
		log.Printf("seed: existing customer data found, skipping seed (compose restart without volume reset)")
		return nil
	}

	if dir := demoDataDir(); dir != "" {
		ok, err := hasDemoDataset(dir)
		if err != nil {
			return fmt.Errorf("seed: inspecting %s=%s: %w", demoDataDirEnv, dir, err)
		}
		if ok {
			if err := loadDemoDataset(ctx, repos, dir); err != nil {
				// Fail-Alert: a partially-generated/corrupt dataset must not
				// fall back to the hardcoded sample (which would silently
				// mask the problem behind 5 plausible-looking customers).
				return fmt.Errorf("seed: loading demo dataset from %s failed, seed aborted (no fallback): %w", dir, err)
			}
			return nil
		}
		log.Printf("seed: %s=%s does not contain a full demogen dataset (missing/unreadable required file); falling back to the built-in sample", demoDataDirEnv, dir)
	}

	runHardcoded(ctx, repos)
	return nil
}

// alreadySeeded reports whether repos.Customers already has at least one
// row. It's the lightweight duplicate-insert guard requested for T2: cheap
// (LIMIT 1), and customers is always the first table populated by either
// seeding path, so an empty customers table implies the rest are empty too
// (short of a hand-edited store, which is not a supported starting state).
func alreadySeeded(ctx context.Context, repos Repos) (bool, error) {
	if repos.Customers == nil {
		return false, nil
	}
	existing, err := repos.Customers.List(ctx, 1, 0)
	if err != nil {
		return false, err
	}
	return len(existing) > 0, nil
}

// runHardcoded is the pre-PH7 fixed 5-customer sample, unchanged, used
// whenever no demogen dataset is configured/available (backward
// compatibility).
func runHardcoded(ctx context.Context, repos Repos) {
	now := time.Now()

	customers := []*domain.Customer{
		{
			ID: "cust-001", ExternalID: "EXT-TANAKA-001",
			CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
			ProductTypes: []string{"deposit", "remittance"},
			Attributes:   map[string]any{"name": "田中太郎", "branch": "東京本店"},
			CreatedAt:    now.Add(-90 * 24 * time.Hour), UpdatedAt: now.Add(-2 * 24 * time.Hour),
		},
		{
			ID: "cust-002", ExternalID: "EXT-SUZUKI-002",
			CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
			ProductTypes: []string{"deposit", "foreign_exchange"},
			Attributes:   map[string]any{"name": "鈴木花子", "branch": "大阪支店"},
			CreatedAt:    now.Add(-60 * 24 * time.Hour), UpdatedAt: now.Add(-1 * 24 * time.Hour),
		},
		{
			ID: "cust-003", ExternalID: "EXT-GLOBALCORP-003",
			CustomerType: domain.CustomerTypeCorporateForeign, CountryCode: "HK",
			ProductTypes: []string{"trade_finance", "remittance"},
			Attributes:   map[string]any{"name": "Global Trade Corp", "branch": "東京本店", "industry": "trading"},
			CreatedAt:    now.Add(-120 * 24 * time.Hour), UpdatedAt: now.Add(-5 * 24 * time.Hour),
		},
		{
			ID: "cust-004", ExternalID: "EXT-YAMAMOTO-004",
			CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
			ProductTypes: []string{"deposit"},
			Attributes:   map[string]any{"name": "山本一郎", "branch": "名古屋支店"},
			CreatedAt:    now.Add(-30 * 24 * time.Hour), UpdatedAt: now.Add(-3 * 24 * time.Hour),
		},
		{
			ID: "cust-005", ExternalID: "EXT-NIPPONSHOJI-005",
			CustomerType: domain.CustomerTypeCorporateDomestic, CountryCode: "JP",
			ProductTypes: []string{"deposit", "loan"},
			Attributes:   map[string]any{"name": "日本商事株式会社", "branch": "東京本店", "industry": "manufacturing"},
			CreatedAt:    now.Add(-180 * 24 * time.Hour), UpdatedAt: now.Add(-10 * 24 * time.Hour),
		},
	}

	// Scores and tiers below are the native CDD engine's actual computed
	// values (1-5 scale) for these customers' attributes, wired against
	// content/_sample/cdd_weights/crypto_exchange.yaml (measured via
	// POST /customers/{id}/score against a shell-run API with
	// MERLON_CDD_WEIGHTS_PATH pointed at that preset; rounded to 2
	// decimals). Using the real computed value (rather than an arbitrary
	// value that merely satisfies the tier thresholds) keeps a demo
	// re-score from flipping the tier shown at seed time. If these
	// customers' attributes change, re-measure and update the values here.
	tier001 := domain.RiskTierMedium
	tier002 := domain.RiskTierMedium
	tier003 := domain.RiskTierHigh
	tier004 := domain.RiskTierMedium
	tier005 := domain.RiskTierMedium
	score001 := 2.65
	score002 := 2.65
	score003 := 4.6
	score004 := 2.65
	score005 := 3.2

	scored1 := now.Add(-2 * 24 * time.Hour)
	scored2 := now.Add(-1 * 24 * time.Hour)
	scored3 := now.Add(-5 * 24 * time.Hour)
	scored4 := now.Add(-3 * 24 * time.Hour)
	scored5 := now.Add(-10 * 24 * time.Hour)

	customers[0].RiskScore = &score001
	customers[0].RiskTier = &tier001
	customers[0].LastScoredAt = &scored1

	customers[1].RiskScore = &score002
	customers[1].RiskTier = &tier002
	customers[1].LastScoredAt = &scored2

	customers[2].RiskScore = &score003
	customers[2].RiskTier = &tier003
	customers[2].LastScoredAt = &scored3

	customers[3].RiskScore = &score004
	customers[3].RiskTier = &tier004
	customers[3].LastScoredAt = &scored4

	customers[4].RiskScore = &score005
	customers[4].RiskTier = &tier005
	customers[4].LastScoredAt = &scored5

	for _, c := range customers {
		if err := repos.Customers.Create(ctx, c); err != nil {
			log.Printf("seed: customer %s: %v", c.ID, err)
		}
	}

	transactions := []*domain.Transaction{
		{ID: "txn-001", CustomerID: "cust-001", ExternalID: "WIRE-20250610-001", Amount: 150000, Currency: "JPY", Direction: domain.DirectionOutbound, CounterpartyCountry: "JP", Channel: "online", ExecutedAt: now.Add(-7 * 24 * time.Hour), CreatedAt: now.Add(-7 * 24 * time.Hour)},
		{ID: "txn-002", CustomerID: "cust-001", ExternalID: "WIRE-20250612-002", Amount: 3500000, Currency: "JPY", Direction: domain.DirectionOutbound, CounterpartyCountry: "PH", Channel: "branch", ExecutedAt: now.Add(-5 * 24 * time.Hour), CreatedAt: now.Add(-5 * 24 * time.Hour)},
		{ID: "txn-003", CustomerID: "cust-002", ExternalID: "FX-20250613-001", Amount: 8200000, Currency: "JPY", Direction: domain.DirectionOutbound, CounterpartyCountry: "CN", Channel: "online", ExecutedAt: now.Add(-4 * 24 * time.Hour), CreatedAt: now.Add(-4 * 24 * time.Hour)},
		{ID: "txn-004", CustomerID: "cust-003", ExternalID: "TT-20250614-001", Amount: 45000000, Currency: "JPY", Direction: domain.DirectionInbound, CounterpartyCountry: "HK", Channel: "swift", ExecutedAt: now.Add(-3 * 24 * time.Hour), CreatedAt: now.Add(-3 * 24 * time.Hour)},
		{ID: "txn-005", CustomerID: "cust-003", ExternalID: "TT-20250614-002", Amount: 44800000, Currency: "JPY", Direction: domain.DirectionOutbound, CounterpartyCountry: "SG", Channel: "swift", ExecutedAt: now.Add(-3*24*time.Hour + 2*time.Hour), CreatedAt: now.Add(-3*24*time.Hour + 2*time.Hour)},
		{ID: "txn-006", CustomerID: "cust-004", ExternalID: "DEP-20250615-001", Amount: 990000, Currency: "JPY", Direction: domain.DirectionInbound, Channel: "atm", ExecutedAt: now.Add(-2 * 24 * time.Hour), CreatedAt: now.Add(-2 * 24 * time.Hour)},
		{ID: "txn-007", CustomerID: "cust-004", ExternalID: "DEP-20250615-002", Amount: 980000, Currency: "JPY", Direction: domain.DirectionInbound, Channel: "atm", ExecutedAt: now.Add(-2*24*time.Hour + 3*time.Hour), CreatedAt: now.Add(-2*24*time.Hour + 3*time.Hour)},
		{ID: "txn-008", CustomerID: "cust-004", ExternalID: "DEP-20250615-003", Amount: 970000, Currency: "JPY", Direction: domain.DirectionInbound, Channel: "atm", ExecutedAt: now.Add(-2*24*time.Hour + 6*time.Hour), CreatedAt: now.Add(-2*24*time.Hour + 6*time.Hour)},
		{ID: "txn-009", CustomerID: "cust-005", ExternalID: "PAY-20250610-001", Amount: 12000000, Currency: "JPY", Direction: domain.DirectionOutbound, CounterpartyCountry: "JP", Channel: "online", ExecutedAt: now.Add(-7 * 24 * time.Hour), CreatedAt: now.Add(-7 * 24 * time.Hour)},
		{ID: "txn-010", CustomerID: "cust-002", ExternalID: "FX-20250616-002", Amount: 500000, Currency: "JPY", Direction: domain.DirectionInbound, CounterpartyCountry: "US", Channel: "online", ExecutedAt: now.Add(-1 * 24 * time.Hour), CreatedAt: now.Add(-1 * 24 * time.Hour)},
	}

	for _, t := range transactions {
		if err := repos.Transactions.Create(ctx, t); err != nil {
			log.Printf("seed: transaction %s: %v", t.ID, err)
		}
	}

	alerts := []*domain.Alert{
		{
			ID: "alert-001", CustomerID: "cust-001", ScenarioID: "SC-LARGE-CROSS-BORDER",
			Severity: domain.AlertSeverityHigh, Status: domain.AlertStatusOpen, Score: 78.5,
			Description:    "短期間での高額海外送金（フィリピン宛）",
			TransactionIDs: []string{"txn-002"},
			DetectedAt:     now.Add(-5 * 24 * time.Hour), CreatedAt: now.Add(-5 * 24 * time.Hour), UpdatedAt: now.Add(-5 * 24 * time.Hour),
		},
		{
			ID: "alert-002", CustomerID: "cust-003", ScenarioID: "SC-PASS-THROUGH",
			Severity: domain.AlertSeverityCritical, Status: domain.AlertStatusEscalated, Score: 92.0,
			Description:    "入金後短時間での同額出金（パススルー疑い：HK→SG）",
			TransactionIDs: []string{"txn-004", "txn-005"},
			DetectedAt:     now.Add(-3 * 24 * time.Hour), CreatedAt: now.Add(-3 * 24 * time.Hour), UpdatedAt: now.Add(-2 * 24 * time.Hour),
		},
		{
			ID: "alert-003", CustomerID: "cust-004", ScenarioID: "SC-STRUCTURING",
			Severity: domain.AlertSeverityMedium, Status: domain.AlertStatusInvestigating, Score: 65.0,
			Description:    "100万円未満の連続ATM入金（ストラクチャリング疑い）",
			TransactionIDs: []string{"txn-006", "txn-007", "txn-008"},
			DetectedAt:     now.Add(-2 * 24 * time.Hour), CreatedAt: now.Add(-2 * 24 * time.Hour), UpdatedAt: now.Add(-1 * 24 * time.Hour),
		},
		{
			ID: "alert-004", CustomerID: "cust-002", ScenarioID: "SC-LARGE-FX",
			Severity: domain.AlertSeverityLow, Status: domain.AlertStatusOpen, Score: 42.0,
			Description:    "大口外国為替取引（中国元転換）",
			TransactionIDs: []string{"txn-003"},
			DetectedAt:     now.Add(-4 * 24 * time.Hour), CreatedAt: now.Add(-4 * 24 * time.Hour), UpdatedAt: now.Add(-4 * 24 * time.Hour),
		},
	}

	for _, a := range alerts {
		if err := repos.Alerts.Create(ctx, a); err != nil {
			log.Printf("seed: alert %s: %v", a.ID, err)
		}
	}

	cases := []*domain.Case{
		{
			ID: "case-001", CustomerID: "cust-003",
			AlertIDs: []string{"alert-002"}, Status: domain.CaseStatusInvestigating,
			Priority: domain.CasePriorityHigh, AssignedTo: "tanaka",
			Summary: "Global Trade Corp: パススルー取引の調査 - HKからSGへの4500万円のフロースルー",
			Notes: []domain.CaseNote{
				{ID: "note-001", Author: "tanaka", Content: "HK法人からの入金後2時間以内にSGへほぼ同額を送金。実態確認が必要。", CreatedAt: now.Add(-2 * 24 * time.Hour)},
				{ID: "note-002", Author: "yamada", Content: "顧客の過去取引パターンを確認。同様の入出金パターンが過去3ヶ月で5回発生。", CreatedAt: now.Add(-1 * 24 * time.Hour)},
			},
			CreatedAt: now.Add(-2 * 24 * time.Hour), UpdatedAt: now.Add(-1 * 24 * time.Hour),
		},
		{
			ID: "case-002", CustomerID: "cust-004",
			AlertIDs: []string{"alert-003"}, Status: domain.CaseStatusOpen,
			Priority: domain.CasePriorityMedium, AssignedTo: "suzuki",
			Summary: "山本一郎: ATMストラクチャリング疑い - 100万円閾値直下の連続入金",
			Notes: []domain.CaseNote{
				{ID: "note-003", Author: "suzuki", Content: "本人確認書類の再取得を依頼済み。", CreatedAt: now.Add(-1 * 24 * time.Hour)},
			},
			CreatedAt: now.Add(-1 * 24 * time.Hour), UpdatedAt: now.Add(-1 * 24 * time.Hour),
		},
	}

	for _, c := range cases {
		if err := repos.Cases.Create(ctx, c); err != nil {
			log.Printf("seed: case %s: %v", c.ID, err)
		}
	}

	if repos.Audit != nil {
		auditEntries := []*domain.AuditEntry{
			{UserID: "system", Action: "create", ResourceType: "customer", ResourceID: "cust-001", IPAddress: "127.0.0.1", CreatedAt: now.Add(-90 * 24 * time.Hour)},
			{UserID: "tanaka", Action: "update_status", ResourceType: "alert", ResourceID: "alert-002", IPAddress: "10.0.1.15", CreatedAt: now.Add(-2 * 24 * time.Hour)},
			{UserID: "tanaka", Action: "create", ResourceType: "case", ResourceID: "case-001", IPAddress: "10.0.1.15", CreatedAt: now.Add(-2 * 24 * time.Hour)},
			{UserID: "yamada", Action: "add_note", ResourceType: "case", ResourceID: "case-001", IPAddress: "10.0.1.20", CreatedAt: now.Add(-1 * 24 * time.Hour)},
			{UserID: "suzuki", Action: "create", ResourceType: "case", ResourceID: "case-002", IPAddress: "10.0.2.10", CreatedAt: now.Add(-1 * 24 * time.Hour)},
		}
		for i, e := range auditEntries {
			if err := repos.Audit.Create(ctx, e); err != nil {
				log.Printf("seed: audit entry %d: %v", i, err)
			}
		}
	}

	log.Printf("seed: loaded built-in sample: %d customers, %d transactions, %d alerts, %d cases",
		len(customers), len(transactions), len(alerts), len(cases))
}
