package demogen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ksuk/merlon/api/internal/domain"
	"gopkg.in/yaml.v3"
)

// accountsFile is the on-disk shape of accounts.json: the two related
// domain types (Account / AccountCustomer) written together since T2's
// loader needs both to reconstruct account membership.
type accountsFile struct {
	Accounts         []domain.Account         `json:"accounts"`
	AccountCustomers []domain.AccountCustomer `json:"account_customers"`
}

// WriteFiles emits every T1-W1/W2 output into dir (creating it if needed):
//
//   - customers.json / accounts.json / score_history.json / transactions.json /
//     alerts.json / cases.json / case_notes.json / screening_results.json /
//     audit_logs.json / rule_definitions.json — generated, gitignored
//     (deploy/seed/demo/*.json)
//   - STORY_IDS.md and screening_lists/*.yaml — small, committed artifacts
//     golden-tested against the repository's checked-in copies
//     (generator_test.go)
//
// Every JSON value is marshaled straight from its domain struct (field
// names come from the domain package's own tags), and encoding/json always
// emits map[string]any keys (e.g. Customer.Attributes) in sorted order, so
// two runs with identical input produce byte-identical files without any
// bespoke serialization logic.
func (r *Result) WriteFiles(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", dir, err)
	}
	jsonFiles := []struct {
		name string
		v    any
	}{
		{"customers.json", r.Customers},
		{"accounts.json", accountsFile{Accounts: r.Accounts, AccountCustomers: r.AccountCustomers}},
		{"score_history.json", r.ScoreHistory},
		{"transactions.json", r.Transactions},
		{"alerts.json", r.Alerts},
		{"cases.json", r.Cases},
		{"case_notes.json", r.CaseNotes},
		{"screening_results.json", r.ScreeningResults},
		{"audit_logs.json", r.AuditLogs},
		{"rule_definitions.json", r.RuleDefinitions},
	}
	for _, f := range jsonFiles {
		if err := writeJSONFile(filepath.Join(dir, f.name), f.v); err != nil {
			return err
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "STORY_IDS.md"), []byte(r.StoryIDsMarkdown), 0o644); err != nil {
		return fmt.Errorf("write STORY_IDS.md: %w", err)
	}

	listsDir := filepath.Join(dir, "screening_lists")
	if err := os.MkdirAll(listsDir, 0o755); err != nil {
		return fmt.Errorf("create screening_lists dir: %w", err)
	}
	for _, l := range r.ScreeningLists {
		if err := writeYAMLFile(filepath.Join(listsDir, l.ListID+".yaml"), l); err != nil {
			return err
		}
	}

	return nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeYAMLFile(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
