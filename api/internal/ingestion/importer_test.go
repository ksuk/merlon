package ingestion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	first, err := service.Run(context.Background(), Options{SourceDir: dir, Apply: true, Actor: "migration-operator", OnDuplicate: DuplicateSkip})
	if err != nil {
		t.Fatal(err)
	}
	if first.Counts["accepted"] != 4 || !first.Applied {
		t.Fatalf("first report=%+v", first)
	}
	second, err := service.Run(context.Background(), Options{SourceDir: dir, Apply: true, Actor: "migration-operator", OnDuplicate: DuplicateSkip})
	if err != nil {
		t.Fatal(err)
	}
	if second.Counts["skipped"] != 4 {
		t.Fatalf("second report=%+v", second)
	}
	if got, _ := customers.List(context.Background(), 100, 0); len(got) != 1 {
		t.Fatalf("customers=%d", len(got))
	}
	customerRows, _ := customers.List(context.Background(), 100, 0)
	if txRows, _ := transactions.ListByCustomer(context.Background(), customerRows[0].ID, 100, 0); len(txRows) != 1 {
		t.Fatalf("transactions=%d", len(txRows))
	}
	account, _ := accounts.GetByExternalID(context.Background(), "A-1")
	if links, _ := accounts.ListCustomers(context.Background(), account.ID); len(links) != 1 {
		t.Fatalf("account links=%d", len(links))
	}
}

type recordingRunRecorder struct {
	started  bool
	finished bool
	runErr   error
}

func (r *recordingRunRecorder) Start(_ context.Context, _ *Report) error {
	r.started = true
	return nil
}

func (r *recordingRunRecorder) Finish(_ context.Context, _ *Report, runErr error) error {
	r.finished = true
	r.runErr = runErr
	return nil
}

func TestImporterRecordsFailedApplyLifecycle(t *testing.T) {
	dir := writeImportFixture(t, map[string]string{customersFile: importCustomers})
	recorder := &recordingRunRecorder{}
	report, err := (&Importer{Recorder: recorder}).Run(context.Background(), Options{
		SourceDir: dir, Apply: true, Actor: "migration-operator", OnDuplicate: DuplicateSkip,
	})
	if err == nil {
		t.Fatal("expected missing repository failure")
	}
	if report == nil || !recorder.started || !recorder.finished || recorder.runErr == nil {
		t.Fatalf("report=%#v recorder=%#v", report, recorder)
	}
}

func TestImporterRequiresActorForApply(t *testing.T) {
	dir := writeImportFixture(t, map[string]string{customersFile: importCustomers})
	if _, err := (&Importer{}).Run(context.Background(), Options{SourceDir: dir, Apply: true}); err == nil || !strings.Contains(err.Error(), "--actor") {
		t.Fatalf("err = %v, want actor validation", err)
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

func TestParseSourceDirRejectsWrongColumnCountsWithoutPanicking(t *testing.T) {
	tests := []struct {
		name string
		file string
		body string
	}{
		{name: "accounts short", file: accountsFile, body: "external_id,account_type\nA-1\n"},
		{name: "links short", file: accountCustomersFile, body: "account_external_id,customer_external_id,role\nA-1,C-1\n"},
		{name: "transactions short", file: transactionsFile, body: "external_id,customer_external_id,account_external_id,amount,currency,direction,transaction_type,executed_at,counterparty_id,counterparty_country,channel,metadata_json\nT-1,C-1\n"},
		{name: "accounts extra", file: accountsFile, body: "external_id,account_type\nA-1,joint,unexpected\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeImportFixture(t, map[string]string{tt.file: tt.body})
			_, err := ParseSourceDir(dir)
			if err == nil || !strings.Contains(err.Error(), "wrong column count") {
				t.Fatalf("err = %v, want wrong column count", err)
			}
		})
	}
}
