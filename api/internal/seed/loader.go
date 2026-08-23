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

const demoDataDirEnv = "MERLON_DEMO_DATA_DIR"

// demoDataDir returns the (trimmed) value of MERLON_DEMO_DATA_DIR, or "" if
// unset/blank.
func demoDataDir() string {
	return strings.TrimSpace(os.Getenv(demoDataDirEnv))
}

// requiredDemoFiles are the demogen output files T2 loads, in their
// dependency order (customers -> accounts -> score_history -> transactions
// -> alerts -> cases -> case_notes -> screening_results -> rule_definitions
// -> audit_logs; see .tasks/archive/PH7-demo-publication.md T2 and
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

type demoDataset struct {
	Customers        []domain.Customer
	Accounts         demoAccountsFile
	Scores           []domain.ScoreRecord
	Transactions     []domain.Transaction
	Alerts           []domain.Alert
	Cases            []domain.Case
	Notes            []demoCaseNote
	ScreeningResults []domain.ScreeningResultRecord
	Rules            []domain.RuleDefinition
	AuditEntries     []domain.AuditEntry
}

// readDemoDataset fully decodes the dataset before any repository write. This
// makes a corrupt later file harmless to in-memory callers and lets the
// PostgreSQL caller wrap the subsequent writes in one transaction.
func readDemoDataset(dir string) (*demoDataset, error) {
	path := func(name string) string { return filepath.Join(dir, name) }
	d := &demoDataset{}

	var err error
	if d.Customers, err = readJSONArray[domain.Customer](path("customers.json")); err != nil {
		return nil, fmt.Errorf("customers.json: %w", err)
	}
	if err := readJSONObject(path("accounts.json"), &d.Accounts); err != nil {
		return nil, fmt.Errorf("accounts.json: %w", err)
	}
	if d.Scores, err = readJSONArray[domain.ScoreRecord](path("score_history.json")); err != nil {
		return nil, fmt.Errorf("score_history.json: %w", err)
	}
	if d.Transactions, err = readJSONArray[domain.Transaction](path("transactions.json")); err != nil {
		return nil, fmt.Errorf("transactions.json: %w", err)
	}
	for i := range d.Transactions {
		if d.Transactions[i].ExternalID == "" {
			d.Transactions[i].ExternalID = d.Transactions[i].ID
		}
	}
	if d.Alerts, err = readJSONArray[domain.Alert](path("alerts.json")); err != nil {
		return nil, fmt.Errorf("alerts.json: %w", err)
	}
	if d.Cases, err = readJSONArray[domain.Case](path("cases.json")); err != nil {
		return nil, fmt.Errorf("cases.json: %w", err)
	}
	if d.Notes, err = readJSONArray[demoCaseNote](path("case_notes.json")); err != nil {
		return nil, fmt.Errorf("case_notes.json: %w", err)
	}
	if d.ScreeningResults, err = readJSONArray[domain.ScreeningResultRecord](path("screening_results.json")); err != nil {
		return nil, fmt.Errorf("screening_results.json: %w", err)
	}
	if d.Rules, err = readJSONArray[domain.RuleDefinition](path("rule_definitions.json")); err != nil {
		return nil, fmt.Errorf("rule_definitions.json: %w", err)
	}
	if d.AuditEntries, err = readJSONArray[domain.AuditEntry](path("audit_logs.json")); err != nil {
		return nil, fmt.Errorf("audit_logs.json: %w", err)
	}
	if err := validateDemoCaseAlertLinks(d); err != nil {
		return nil, fmt.Errorf("case/alert lifecycle validation: %w", err)
	}
	return d, nil
}

func validateDemoCaseAlertLinks(d *demoDataset) error {
	alerts := make(map[string]domain.Alert, len(d.Alerts))
	for _, alert := range d.Alerts {
		alerts[alert.ID] = alert
	}
	for _, c := range d.Cases {
		if !domain.IsCaseUnresolved(c.Status) && !domain.IsCaseTerminal(c.Status) {
			return fmt.Errorf("case %s has unsupported status %q", c.ID, c.Status)
		}
		seen := make(map[string]struct{}, len(c.AlertIDs))
		for _, alertID := range c.AlertIDs {
			if _, duplicate := seen[alertID]; duplicate {
				return fmt.Errorf("case %s contains duplicate alert %s", c.ID, alertID)
			}
			seen[alertID] = struct{}{}
			alert, ok := alerts[alertID]
			if !ok {
				return fmt.Errorf("case %s references missing alert %s", c.ID, alertID)
			}
			if alert.CustomerID != c.CustomerID {
				return fmt.Errorf("case %s and alert %s belong to different customers", c.ID, alertID)
			}
			if !domain.CompatibleCaseAlertState(c.Status, alert.Status) {
				return fmt.Errorf("case %s status %q is incompatible with alert %s status %q", c.ID, c.Status, alertID, alert.Status)
			}
		}
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
	d, err := readDemoDataset(dir)
	if err != nil {
		return err
	}
	if repos.Customers == nil || repos.Transactions == nil || repos.Alerts == nil || repos.Cases == nil || repos.Audit == nil || repos.Accounts == nil || repos.ScreeningResults == nil || repos.Rules == nil {
		return fmt.Errorf("complete demo dataset requires all seed repositories")
	}

	for i := range d.Customers {
		c := &d.Customers[i]
		if err := repos.Customers.Create(ctx, c); err != nil {
			return fmt.Errorf("create customer %s (customers.json[%d]): %w", c.ID, i, err)
		}
	}
	for i := range d.Accounts.Accounts {
		a := &d.Accounts.Accounts[i]
		if err := repos.Accounts.Create(ctx, a); err != nil {
			return fmt.Errorf("create account %s (accounts.json.accounts[%d]): %w", a.ID, i, err)
		}
	}
	for i, ac := range d.Accounts.AccountCustomers {
		if err := repos.Accounts.AddCustomer(ctx, ac.AccountID, ac.CustomerID, ac.Role); err != nil {
			return fmt.Errorf("link account_customer %s/%s (accounts.json.account_customers[%d]): %w", ac.AccountID, ac.CustomerID, i, err)
		}
	}
	for i := range d.Scores {
		s := &d.Scores[i]
		if err := repos.Customers.SaveScoreRecord(ctx, s); err != nil {
			return fmt.Errorf("save score record %s (score_history.json[%d]): %w", s.ID, i, err)
		}
	}
	for i := range d.Transactions {
		t := &d.Transactions[i]
		if err := repos.Transactions.Create(ctx, t); err != nil {
			return fmt.Errorf("create transaction %s (transactions.json[%d]): %w", t.ID, i, err)
		}
	}
	for i := range d.Alerts {
		a := &d.Alerts[i]
		if err := repos.Alerts.Create(ctx, a); err != nil {
			return fmt.Errorf("create alert %s (alerts.json[%d]): %w", a.ID, i, err)
		}
	}
	for i := range d.Cases {
		c := &d.Cases[i]
		if err := repos.Cases.Create(ctx, c); err != nil {
			return fmt.Errorf("create case %s (cases.json[%d]): %w", c.ID, i, err)
		}
	}
	for i, n := range d.Notes {
		note := domain.CaseNote{ID: n.ID, Author: n.Author, Content: n.Content, CreatedAt: n.CreatedAt}
		if err := repos.Cases.AddNote(ctx, n.CaseID, &note); err != nil {
			return fmt.Errorf("add case note %s to case %s (case_notes.json[%d]): %w", n.ID, n.CaseID, i, err)
		}
	}
	for i := range d.ScreeningResults {
		sr := &d.ScreeningResults[i]
		if err := repos.ScreeningResults.Create(ctx, sr); err != nil {
			return fmt.Errorf("create screening result %s (screening_results.json[%d]): %w", sr.ID, i, err)
		}
	}
	for i := range d.Rules {
		rd := &d.Rules[i]
		if err := repos.Rules.Create(ctx, rd); err != nil {
			return fmt.Errorf("create rule definition %s (rule_definitions.json[%d]): %w", rd.ID, i, err)
		}
	}
	for i := range d.AuditEntries {
		e := &d.AuditEntries[i]
		if err := repos.Audit.Create(ctx, e); err != nil {
			return fmt.Errorf("create audit entry id=%d (audit_logs.json[%d]): %w", e.ID, i, err)
		}
	}

	log.Printf("seed: loaded demo dataset from %s: %d customers, %d accounts (%d links), %d score records, %d transactions, %d alerts, %d cases (%d notes), %d screening results, %d rule definitions, %d audit entries",
		dir, len(d.Customers), len(d.Accounts.Accounts), len(d.Accounts.AccountCustomers), len(d.Scores),
		len(d.Transactions), len(d.Alerts), len(d.Cases), len(d.Notes), len(d.ScreeningResults), len(d.Rules), len(d.AuditEntries))
	return nil
}
