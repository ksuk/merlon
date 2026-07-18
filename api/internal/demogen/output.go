package demogen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ksuk/merlon/api/internal/domain"
)

// accountsFile is the on-disk shape of accounts.json: the two related
// domain types (Account / AccountCustomer) written together since T2's
// loader needs both to reconstruct account membership.
type accountsFile struct {
	Accounts         []domain.Account         `json:"accounts"`
	AccountCustomers []domain.AccountCustomer `json:"account_customers"`
}

// WriteFiles emits customers.json, accounts.json, and score_history.json
// into dir (creating it if needed). Every value is marshaled straight from
// its domain struct (JSON field names come from the domain package's own
// tags, per the T1-W1 instructions), and encoding/json always emits
// map[string]any keys (Customer.Attributes) in sorted order, so two runs
// with identical input produce byte-identical files without any bespoke
// serialization logic.
func (r *Result) WriteFiles(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", dir, err)
	}
	if err := writeJSONFile(filepath.Join(dir, "customers.json"), r.Customers); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(dir, "accounts.json"), accountsFile{Accounts: r.Accounts, AccountCustomers: r.AccountCustomers}); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(dir, "score_history.json"), r.ScoreHistory); err != nil {
		return err
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
