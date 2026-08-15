package ingestion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ksuk/merlon/api/internal/store"
)

func writeImportFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const importCustomers = "external_id,customer_type,country_code,status,product_types_json,attributes_json\nC-1,individual,JP,active,[],{}\n"
const importAccounts = "external_id,account_type\nA-1,joint\n"
const importLinks = "account_external_id,customer_external_id,role\nA-1,C-1,primary\n"
const importTransactions = "external_id,customer_external_id,account_external_id,amount,currency,direction,transaction_type,executed_at,counterparty_id,counterparty_country,channel,metadata_json\nT-1,C-1,A-1,1000,JPY,inbound,transfer,2026-01-01T00:00:00Z,CP-1,JP,web,{}\n"

func TestImporterDryRunDoesNotWriteRepositories(t *testing.T) {
	dir := writeImportFixture(t, map[string]string{customersFile: importCustomers, accountsFile: importAccounts, accountCustomersFile: importLinks, transactionsFile: importTransactions})
	customers := store.NewMemoryCustomerRepo()
	accounts := store.NewMemoryAccountRepo(customers)
	transactions := store.NewMemoryTransactionRepo()
	report, err := (&Importer{Deps: Dependencies{Customers: customers, Accounts: accounts, Transactions: transactions}}).Run(context.Background(), Options{SourceDir: dir, DryRun: true, OnDuplicate: DuplicateSkip})
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Applied || report.Counts["accepted"] != 4 {
		t.Fatalf("report=%+v", report)
	}
	if got, _ := customers.List(context.Background(), 100, 0); len(got) != 0 {
		t.Fatalf("dry-run customers=%d", len(got))
	}
	if got, _ := transactions.ListByCustomer(context.Background(), "x", 100, 0); len(got) != 0 {
		t.Fatalf("dry-run transactions=%d", len(got))
	}
}

func TestImporterApplyUsesDependencyOrderAndIsIdempotent(t *testing.T) {
	dir := writeImportFixture(t, map[string]string{customersFile: importCustomers, accountsFile: importAccounts, accountCustomersFile: importLinks, transactionsFile: importTransactions})
	customers := store.NewMemoryCustomerRepo()
	accounts := store.NewMemoryAccountRepo(customers)
	transactions := store.NewMemoryTransactionRepo()
	service := &Importer{Deps: Dependencies{Customers: customers, Accounts: accounts, Transactions: transactions}}
	first, err := service.Run(context.Background(), Options{SourceDir: dir, Apply: true, OnDuplicate: DuplicateSkip})
	if err != nil {
		t.Fatal(err)
	}
	if first.Counts["accepted"] != 4 || !first.Applied {
		t.Fatalf("first report=%+v", first)
	}
	second, err := service.Run(context.Background(), Options{SourceDir: dir, Apply: true, OnDuplicate: DuplicateSkip})
	if err != nil {
		t.Fatal(err)
	}
	if second.Counts["skipped"] != 3 {
		t.Fatalf("second report=%+v", second)
	}
	if got, _ := customers.List(context.Background(), 100, 0); len(got) != 1 {
		t.Fatalf("customers=%d", len(got))
	}
	customerRows, _ := customers.List(context.Background(), 100, 0)
	if txRows, _ := transactions.ListByCustomer(context.Background(), customerRows[0].ID, 100, 0); len(txRows) != 1 {
		t.Fatalf("transactions=%d", len(txRows))
	}
}

func TestImporterRejectsDerivedArtifactsAndUnknownColumns(t *testing.T) {
	dir := writeImportFixture(t, map[string]string{"alerts.csv": "id\nalert\n"})
	if _, err := ParseSourceDir(dir); err == nil {
		t.Fatal("unsupported derived/source file accepted")
	}
	dir = writeImportFixture(t, map[string]string{customersFile: "external_id,customer_type,country_code,status,product_types_json,attributes_json,alert_ids\n"})
	if _, err := ParseSourceDir(dir); err == nil {
		t.Fatal("unknown column accepted")
	}
}

func TestImporterRejectsConflictingDuplicateRows(t *testing.T) {
	dir := writeImportFixture(t, map[string]string{customersFile: "external_id,customer_type,country_code,status,product_types_json,attributes_json\nC-1,individual,JP,active,[],{}\nC-1,individual,US,active,[],{}\n"})
	if _, err := ParseSourceDir(dir); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}
