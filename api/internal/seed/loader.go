package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// demoDataDir returns the (trimmed) value of MERLON_DEMO_DATA_DIR, or "" if
// unset/blank.
func demoDataDir() string {
	return strings.TrimSpace(os.Getenv(demoDataDirEnv))
}

// requiredDemoFiles are the demogen output files T2 loads, in their
// dependency order (customers -> accounts -> score_history -> transactions
// -> alerts -> cases -> case_notes -> screening_results -> rule_definitions
// -> audit_logs; see .release-tasks/PH7-demo-publication.md T2 and
// api/internal/demogen). All must be present for hasDemoDataset to treat dir
// as a usable dataset; a missing file falls back to the hardcoded sample
// rather than attempting a partial load.
var requiredDemoFiles = []string{
	"customers.json",
	"accounts.json",
	"score_history.json",
	"transactions.json",
	"alerts.json",
	"cases.json",
	"case_notes.json",
	"screening_results.json",
	"rule_definitions.json",
	"audit_logs.json",
}

// hasDemoDataset reports whether dir looks like a complete demogen dataset:
// it exists, is a directory, and contains every file in requiredDemoFiles.
// It does not validate file contents — a present-but-corrupt file is a
// loadDemoDataset error (hard failure), not a "dataset absent" fallback
// signal (the wave-T2 instructions' distinction between "missing" and
// "broken").
func hasDemoDataset(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	for _, name := range requiredDemoFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

// demoAccountsFile mirrors accounts.json's shape: {"accounts": [...],
// "account_customers": [...]} rather than a bare array (the only demogen
// output file structured this way, since AccountCustomer is a join row with
// no id of its own).
type demoAccountsFile struct {
	Accounts         []domain.Account         `json:"accounts"`
	AccountCustomers []domain.AccountCustomer `json:"account_customers"`
}

// demoCaseNote mirrors case_notes.json's flat-array shape ({case_id, id,
// author, content, created_at} per row), replayed through
// CaseRepository.AddNote rather than unmarshaled directly into
// domain.CaseNote (which has no CaseID field — notes are addressed via
// Case.Notes/AddNote's caseID parameter instead).
type demoCaseNote struct {
	CaseID    string    `json:"case_id"`
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// readJSONArray reads and decodes path as a JSON array of T.
func readJSONArray[T any](path string) ([]T, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return out, nil
}

// readJSONObject reads and decodes path as a single JSON object into v.
func readJSONObject(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// loadDemoDataset loads the full demogen dataset from dir into repos, in FK
// dependency order. Every domain type here round-trips its JSON tags
// directly (the demogen output was written to match api/internal/domain's
// existing `json:"..."` tags), except case_notes.json (see demoCaseNote)
// and accounts.json (see demoAccountsFile).
//
// IDs are preserved exactly as given in the JSON (not re-generated) so the
// fixed IDs in deploy/seed/demo/STORY_IDS.md resolve directly via the HTTP
// API — including against PostgreSQL: customers, transactions, alerts,
// accounts, rule_definitions, and customer_score_history have UUID primary
// key columns (migrations/001, 002, 004, 011, 020), which reject arbitrary
// strings, so demogen itself now emits deterministic UUIDv5 IDs
// (api/internal/demogen/ids.go's uuidFor, derived from each entity's
// human-readable generation-time label) rather than the raw labels;
// cases/case_notes/screening_results have TEXT primary key columns and get
// UUIDs too, purely for a uniform ID scheme across the dataset. See
// deploy/seed/demo/STORY_IDS.md for the label/UUID mapping.
//
// A second, narrower incompatibility that *is* fixed here: every
// transactions.json row has external_id="" (T1-W2's synthetic transactions
// carry no external system reference), but transactions.external_id is
// UNIQUE NOT NULL (migrations/002_transactions_alerts.sql). Loading 46k
// blank external_ids verbatim would violate that constraint after the first
// row, so a blank external_id is defaulted to the transaction's own
// (unique) id before Create.
func loadDemoDataset(ctx context.Context, repos Repos, dir string) error {
	path := func(name string) string { return filepath.Join(dir, name) }

	customers, err := readJSONArray[domain.Customer](path("customers.json"))
	if err != nil {
		return fmt.Errorf("customers.json: %w", err)
	}
	for i := range customers {
		c := &customers[i]
		if err := repos.Customers.Create(ctx, c); err != nil {
			return fmt.Errorf("create customer %s (customers.json[%d]): %w", c.ID, i, err)
		}
	}

	var accountsFile demoAccountsFile
	if err := readJSONObject(path("accounts.json"), &accountsFile); err != nil {
		return fmt.Errorf("accounts.json: %w", err)
	}
	if repos.Accounts == nil {
		return fmt.Errorf("accounts.json present but no Accounts repository is configured")
	}
	for i := range accountsFile.Accounts {
		a := &accountsFile.Accounts[i]
		if err := repos.Accounts.Create(ctx, a); err != nil {
			return fmt.Errorf("create account %s (accounts.json.accounts[%d]): %w", a.ID, i, err)
		}
	}
	for i, ac := range accountsFile.AccountCustomers {
		if err := repos.Accounts.AddCustomer(ctx, ac.AccountID, ac.CustomerID, ac.Role); err != nil {
			return fmt.Errorf("link account_customer %s/%s (accounts.json.account_customers[%d]): %w", ac.AccountID, ac.CustomerID, i, err)
		}
	}

	scores, err := readJSONArray[domain.ScoreRecord](path("score_history.json"))
	if err != nil {
		return fmt.Errorf("score_history.json: %w", err)
	}
	for i := range scores {
		s := &scores[i]
		if err := repos.Customers.SaveScoreRecord(ctx, s); err != nil {
			return fmt.Errorf("save score record %s (score_history.json[%d]): %w", s.ID, i, err)
		}
	}

	transactions, err := readJSONArray[domain.Transaction](path("transactions.json"))
	if err != nil {
		return fmt.Errorf("transactions.json: %w", err)
	}
	for i := range transactions {
		t := &transactions[i]
		if t.ExternalID == "" {
			// See the func doc: transactions_external_id_unique (UNIQUE NOT
			// NULL) rejects the blank external_id every demogen transaction
			// carries. The transaction's own id is already guaranteed
			// unique, so it's a safe, deterministic stand-in.
			t.ExternalID = t.ID
		}
		if err := repos.Transactions.Create(ctx, t); err != nil {
			return fmt.Errorf("create transaction %s (transactions.json[%d]): %w", t.ID, i, err)
		}
	}

	alerts, err := readJSONArray[domain.Alert](path("alerts.json"))
	if err != nil {
		return fmt.Errorf("alerts.json: %w", err)
	}
	for i := range alerts {
		a := &alerts[i]
		if err := repos.Alerts.Create(ctx, a); err != nil {
			return fmt.Errorf("create alert %s (alerts.json[%d]): %w", a.ID, i, err)
		}
	}

	cases, err := readJSONArray[domain.Case](path("cases.json"))
	if err != nil {
		return fmt.Errorf("cases.json: %w", err)
	}
	for i := range cases {
		c := &cases[i]
		if err := repos.Cases.Create(ctx, c); err != nil {
			return fmt.Errorf("create case %s (cases.json[%d]): %w", c.ID, i, err)
		}
	}

	notes, err := readJSONArray[demoCaseNote](path("case_notes.json"))
	if err != nil {
		return fmt.Errorf("case_notes.json: %w", err)
	}
	for i, n := range notes {
		note := domain.CaseNote{ID: n.ID, Author: n.Author, Content: n.Content, CreatedAt: n.CreatedAt}
		if err := repos.Cases.AddNote(ctx, n.CaseID, &note); err != nil {
			return fmt.Errorf("add case note %s to case %s (case_notes.json[%d]): %w", n.ID, n.CaseID, i, err)
		}
	}

	screeningResults, err := readJSONArray[domain.ScreeningResultRecord](path("screening_results.json"))
	if err != nil {
		return fmt.Errorf("screening_results.json: %w", err)
	}
	if repos.ScreeningResults == nil {
		return fmt.Errorf("screening_results.json present but no ScreeningResults repository is configured")
	}
	for i := range screeningResults {
		sr := &screeningResults[i]
		if err := repos.ScreeningResults.Create(ctx, sr); err != nil {
			return fmt.Errorf("create screening result %s (screening_results.json[%d]): %w", sr.ID, i, err)
		}
	}

	rules, err := readJSONArray[domain.RuleDefinition](path("rule_definitions.json"))
	if err != nil {
		return fmt.Errorf("rule_definitions.json: %w", err)
	}
	if repos.Rules == nil {
		return fmt.Errorf("rule_definitions.json present but no Rules repository is configured")
	}
	for i := range rules {
		rd := &rules[i]
		if err := repos.Rules.Create(ctx, rd); err != nil {
			return fmt.Errorf("create rule definition %s (rule_definitions.json[%d]): %w", rd.ID, i, err)
		}
	}

	auditEntries, err := readJSONArray[domain.AuditEntry](path("audit_logs.json"))
	if err != nil {
		return fmt.Errorf("audit_logs.json: %w", err)
	}
	if repos.Audit == nil {
		return fmt.Errorf("audit_logs.json present but no Audit repository is configured")
	}
	for i := range auditEntries {
		e := &auditEntries[i]
		if err := repos.Audit.Create(ctx, e); err != nil {
			return fmt.Errorf("create audit entry id=%d (audit_logs.json[%d]): %w", e.ID, i, err)
		}
	}

	log.Printf("seed: loaded demo dataset from %s: %d customers, %d accounts (%d links), %d score records, %d transactions, %d alerts, %d cases (%d notes), %d screening results, %d rule definitions, %d audit entries",
		dir, len(customers), len(accountsFile.Accounts), len(accountsFile.AccountCustomers), len(scores),
		len(transactions), len(alerts), len(cases), len(notes), len(screeningResults), len(rules), len(auditEntries))
	return nil
}
