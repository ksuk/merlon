package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

// demoDatasetFixture is a small hand-written stand-in for a demogen-produced
// deploy/seed/demo/ directory: one customer/account/score/transaction/
// alert/case/case_note/screening_result/rule_definition/audit_log record
// each, using the exact same file names, shapes, and dependency order as the
// real dataset (see loadDemoDataset's doc comment).
var demoDatasetFixture = map[string]string{
	"customers.json": `[
		{"id":"demo-cust-01","external_id":"MNP-I000001","customer_type":"individual","country_code":"JP",
		 "product_types":["agent_cash_remittance"],"status":"active","attributes":{"name":"Demo One"},
		 "risk_score":2.5,"risk_tier":"medium","last_scored_at":"2026-06-30T00:00:00Z",
		 "created_at":"2024-07-01T00:00:00Z","updated_at":"2026-07-01T00:00:00Z"},
		{"id":"demo-cust-02","external_id":"MNP-I000002","customer_type":"individual","country_code":"JP",
		 "product_types":["agent_cash_remittance"],"status":"active","attributes":{"name":"Demo Two"},
		 "created_at":"2024-07-01T00:00:00Z","updated_at":"2026-07-01T00:00:00Z"}
	]`,
	"accounts.json": `{
		"accounts":[{"id":"demo-acct-01","external_id":"MNP-ACC-000001","account_type":"individual",
			"created_at":"2025-07-01T00:00:00Z","updated_at":"2026-07-01T00:00:00Z"}],
		"account_customers":[{"account_id":"demo-acct-01","customer_id":"demo-cust-01","role":"primary"}]
	}`,
	"score_history.json": `[
		{"id":"demo-score-01","customer_id":"demo-cust-01","score":2.5,"tier":"medium",
		 "factors":[{"name":"geography","axis":"geography","score":0.4,"description":"geography=2"}],
		 "rule_set_id":"funds_transfer","rule_set_version":1,"scored_at":"2026-06-30T00:00:00Z"}
	]`,
	"transactions.json": `[
		{"id":"demo-txn-01","customer_id":"demo-cust-01","external_id":"","amount":48000,"currency":"JPY",
		 "direction":"outbound","channel":"agent","executed_at":"2026-06-28T00:00:00Z","created_at":"2026-06-28T00:00:00Z"},
		{"id":"demo-txn-02","customer_id":"demo-cust-01","external_id":"","amount":45000,"currency":"JPY",
		 "direction":"outbound","channel":"agent","executed_at":"2026-06-28T06:00:00Z","created_at":"2026-06-28T06:00:00Z"}
	]`,
	"alerts.json": `[
		{"id":"demo-alert-01","customer_id":"demo-cust-01","scenario_id":"tm_structuring_basic","severity":"medium",
		 "status":"open","score":2.9,"description":"test structuring alert","transaction_ids":["demo-txn-01","demo-txn-02"],
		 "detected_at":"2026-06-28T06:00:00Z","created_at":"2026-06-28T06:00:00Z","updated_at":"2026-06-28T06:00:00Z"},
		{"id":"demo-alert-02","customer_id":"demo-cust-02","scenario_id":"tm_reviewed_activity","severity":"low",
		 "status":"closed_false_positive","score":1.2,"description":"reviewed synthetic alert","transaction_ids":[],
		 "detected_at":"2026-06-20T06:00:00Z","resolved_at":"2026-06-21T07:00:00Z","resolved_by":"demo-reviewer",
		 "created_at":"2026-06-20T06:00:00Z","updated_at":"2026-06-21T07:00:00Z"}
	]`,
	"cases.json": `[
		{"id":"demo-case-01","customer_id":"demo-cust-01","alert_ids":["demo-alert-01"],"status":"open",
		 "priority":"medium","assigned_to":"m.sato","summary":"test case",
		 "created_at":"2026-06-28T06:00:00Z","updated_at":"2026-06-28T06:00:00Z"},
		{"id":"demo-case-02","customer_id":"demo-cust-02","alert_ids":[],"status":"closed",
		 "priority":"low","summary":"reviewed synthetic case","created_at":"2026-06-20T06:00:00Z",
		 "updated_at":"2026-06-21T07:00:00Z","closed_at":"2026-06-21T07:00:00Z"}
	]`,
	"case_notes.json": `[
		{"case_id":"demo-case-01","id":"demo-note-01","author":"m.sato","content":"initial review",
		 "created_at":"2026-06-29T00:00:00Z"}
	]`,
	"screening_results.json": `[
		{"id":"demo-screening-result-01","customer_id":"demo-cust-02","list_id":"demo_sanctions","list_type":"sanctions",
		 "entry_id":"DEMO-SANCTIONS-001","matched_name":"Demo Subject Alpha","similarity":1,"status":"REVIEWING",
		 "screened_at":"2026-05-02T00:00:00Z","created_at":"2026-05-02T00:00:00Z"}
	]`,
	"rule_definitions.json": `[
		{"id":"demo-rule-01","type":"TM_SCENARIO","name":"structuring_basic","description":"test rule",
		 "definition":{"foo":"bar"},"version":1,"is_active":true,
		 "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}
	]`,
	"audit_logs.json": `[
		{"id":1,"user_id":"m.sato","action":"register","resource_type":"rule_definitions","resource_id":"demo-rule-01",
		 "details":{"synthetic":"true"},"ip_address":"192.0.2.10","user_agent":"merlon-demogen","created_at":"2026-01-01T00:00:00Z"}
	]`,
}

func writeDemoDatasetFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return dir
}

func copyDemoDatasetFixture() map[string]string {
	copy := make(map[string]string, len(demoDatasetFixture))
	for name, content := range demoDatasetFixture {
		copy[name] = content
	}
	return copy
}

func newFullMemoryRepos() (Repos, *store.MemoryCustomerRepo) {
	customers := store.NewMemoryCustomerRepo()
	return Repos{
		Customers:        customers,
		Transactions:     store.NewMemoryTransactionRepo(),
		Alerts:           store.NewMemoryAlertRepo(),
		Cases:            store.NewMemoryCaseRepo(),
		Audit:            store.NewMemoryAuditRepo(),
		Accounts:         store.NewMemoryAccountRepo(customers),
		ScreeningResults: store.NewMemoryScreeningResultRepo(),
		Rules:            store.NewMemoryRuleRepo(),
		State:            store.NewMemorySeedStateRepo(),
	}, customers
}

func TestRunLoadsDemoDatasetWhenEnvPointsAtCompleteDataset(t *testing.T) {
	dir := writeDemoDatasetFixture(t, demoDatasetFixture)
	t.Setenv(demoDataDirEnv, dir)

	repos, _ := newFullMemoryRepos()
	ctx := context.Background()

	result, err := Run(ctx, repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DatasetKind != domain.SeedDatasetDemo || !result.Applied || !result.DemoDataEnabled() {
		t.Fatalf("result = %+v, want newly applied demo dataset", result)
	}

	custs, err := repos.Customers.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(custs) != 2 {
		t.Fatalf("expected 2 customers from the dataset, got %d (hardcoded fallback not skipped?)", len(custs))
	}

	// The fixed demo ID must resolve directly (STORY_IDS.md-style direct
	// link), not just exist under some other generated ID.
	c, err := repos.Customers.Get(ctx, "demo-cust-01")
	if err != nil {
		t.Fatalf("get demo-cust-01: %v", err)
	}
	if c.ExternalID != "MNP-I000001" {
		t.Errorf("unexpected external_id: %s", c.ExternalID)
	}

	hist, err := repos.Customers.ListScoreHistory(ctx, "demo-cust-01", 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("expected 1 score record for demo-cust-01, got %d (err=%v)", len(hist), err)
	}

	txn, err := repos.Transactions.Get(ctx, "demo-txn-01")
	if err != nil {
		t.Fatalf("get demo-txn-01: %v", err)
	}
	if txn.ExternalID != txn.ID {
		t.Errorf("expected blank external_id to default to the transaction's own id, got %q", txn.ExternalID)
	}

	alert, err := repos.Alerts.Get(ctx, "demo-alert-01")
	if err != nil {
		t.Fatalf("get demo-alert-01: %v", err)
	}
	if len(alert.TransactionIDs) != 2 {
		t.Errorf("expected 2 linked transactions on demo-alert-01, got %d", len(alert.TransactionIDs))
	}
	terminalAlert, err := repos.Alerts.Get(ctx, "demo-alert-02")
	if err != nil {
		t.Fatalf("get demo-alert-02: %v", err)
	}
	if terminalAlert.ResolvedAt == nil || terminalAlert.ResolvedBy != "demo-reviewer" {
		t.Fatalf("terminal demo alert resolution metadata = (%v, %q), want timestamp and actor", terminalAlert.ResolvedAt, terminalAlert.ResolvedBy)
	}

	kase, err := repos.Cases.Get(ctx, "demo-case-01")
	if err != nil {
		t.Fatalf("get demo-case-01: %v", err)
	}
	if len(kase.Notes) != 1 || kase.Notes[0].ID != "demo-note-01" {
		t.Fatalf("expected demo-case-01 to carry the replayed case note, got %+v", kase.Notes)
	}
	terminalCase, err := repos.Cases.Get(ctx, "demo-case-02")
	if err != nil {
		t.Fatalf("get demo-case-02: %v", err)
	}
	if terminalCase.ClosedAt == nil {
		t.Fatal("terminal demo case is missing closed_at")
	}

	srs, err := repos.ScreeningResults.ListByCustomer(ctx, "demo-cust-02", 10, 0)
	if err != nil || len(srs) != 1 {
		t.Fatalf("expected 1 screening result for demo-cust-02, got %d (err=%v)", len(srs), err)
	}

	// RuleRepository.Get's "id" parameter is actually RuleDefinition.Name
	// (see store.PgRuleRepo.Get's doc comment) — the row's own ID is
	// regenerated per version and isn't the lookup key.
	rule, err := repos.Rules.Get(ctx, "structuring_basic")
	if err != nil {
		t.Fatalf("get rule by name structuring_basic: %v", err)
	}
	if !rule.IsActive {
		t.Errorf("expected demo-rule-01 to be active")
	}

	logs, err := repos.Audit.List(ctx, domain.AuditListFilter{Limit: 10})
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected 1 audit entry, got %d (err=%v)", len(logs), err)
	}

	acct, err := repos.Accounts.Get(ctx, "demo-acct-01")
	if err != nil {
		t.Fatalf("get demo-acct-01: %v", err)
	}
	linked, err := repos.Accounts.ListCustomers(ctx, acct.ID)
	if err != nil || len(linked) != 1 || linked[0].CustomerID != "demo-cust-01" {
		t.Fatalf("expected demo-acct-01 linked to demo-cust-01, got %+v (err=%v)", linked, err)
	}
}

func TestRunRejectsContradictoryDemoLifecycleBeforeWriting(t *testing.T) {
	fixture := copyDemoDatasetFixture()
	fixture["cases.json"] = `[
		{"id":"demo-case-01","customer_id":"demo-cust-01","alert_ids":["demo-alert-01"],"status":"closed",
		 "priority":"medium","summary":"contradictory case","created_at":"2026-06-28T06:00:00Z",
		 "updated_at":"2026-06-29T00:00:00Z","closed_at":"2026-06-29T00:00:00Z"}
	]`
	dir := writeDemoDatasetFixture(t, fixture)
	t.Setenv(demoDataDirEnv, dir)
	repos, customers := newFullMemoryRepos()

	if _, err := Run(context.Background(), repos); err == nil {
		t.Fatal("contradictory demo lifecycle was accepted")
	}
	got, err := customers.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("validation failure wrote %d customer rows", len(got))
	}
}

func TestRunRetainsDemoProvenanceOnRestart(t *testing.T) {
	dir := writeDemoDatasetFixture(t, demoDatasetFixture)
	t.Setenv(demoDataDirEnv, dir)
	repos, customers := newFullMemoryRepos()
	ctx := context.Background()

	if _, err := Run(ctx, repos); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	result, err := Run(ctx, repos)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if result.DatasetKind != domain.SeedDatasetDemo || result.Applied || !result.DemoDataEnabled() {
		t.Fatalf("restart result = %+v, want skipped demo dataset", result)
	}
	custs, err := customers.List(ctx, 100, 0)
	if err != nil || len(custs) != 2 {
		t.Fatalf("restart changed customer count to %d (err=%v)", len(custs), err)
	}
}

func TestRunFallsBackToHardcodedSampleWhenEnvUnset(t *testing.T) {
	repos, _ := newFullMemoryRepos()
	ctx := context.Background()

	result, err := Run(ctx, repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DatasetKind != domain.SeedDatasetHardcoded || !result.Applied || result.DemoDataEnabled() {
		t.Fatalf("result = %+v, want newly applied hardcoded dataset", result)
	}

	custs, err := repos.Customers.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(custs) != 5 {
		t.Fatalf("expected the 5-customer hardcoded sample, got %d", len(custs))
	}
}

func TestRunFallsBackWhenDatasetDirHasMissingRequiredFile(t *testing.T) {
	incomplete := make(map[string]string, len(demoDatasetFixture))
	for name, content := range demoDatasetFixture {
		incomplete[name] = content
	}
	delete(incomplete, "alerts.json") // simulate an incomplete demogen run

	dir := writeDemoDatasetFixture(t, incomplete)
	t.Setenv(demoDataDirEnv, dir)

	repos, _ := newFullMemoryRepos()
	ctx := context.Background()

	result, err := Run(ctx, repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DatasetKind != domain.SeedDatasetHardcoded || !result.Applied || result.DemoDataEnabled() {
		t.Fatalf("result = %+v, want hardcoded fallback", result)
	}

	custs, err := repos.Customers.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(custs) != 5 {
		t.Fatalf("expected fallback to the 5-customer hardcoded sample when a required file is missing, got %d", len(custs))
	}
}

func TestRunFailsExplicitlyOnCorruptDatasetWithoutFallback(t *testing.T) {
	corrupt := make(map[string]string, len(demoDatasetFixture))
	for name, content := range demoDatasetFixture {
		corrupt[name] = content
	}
	// customers.json is well-formed but alerts.json (later in the dependency
	// order) is not. The loader must report an error without writing the
	// already-decoded customers or falling back to the hardcoded sample.
	corrupt["alerts.json"] = `{not valid json`

	dir := writeDemoDatasetFixture(t, corrupt)
	t.Setenv(demoDataDirEnv, dir)

	repos, _ := newFullMemoryRepos()
	ctx := context.Background()

	if _, err := Run(ctx, repos); err == nil {
		t.Fatal("expected Run to return an error for a corrupt dataset")
	}

	custs, err := repos.Customers.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(custs) != 0 {
		t.Fatalf("expected no customers after pre-write validation failure, got %d", len(custs))
	}
	if _, err := repos.Customers.Get(ctx, "cust-001"); err == nil {
		t.Fatal("hardcoded fallback customer cust-001 must not appear after a failed dataset load")
	}
}

func TestRunSkipsSeedingWhenDataAlreadyExists(t *testing.T) {
	dir := writeDemoDatasetFixture(t, demoDatasetFixture)
	t.Setenv(demoDataDirEnv, dir)

	repos, customers := newFullMemoryRepos()
	ctx := context.Background()

	pre := &domain.Customer{ID: "pre-existing", ExternalID: "PRE-1", CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP"}
	if err := customers.Create(ctx, pre); err != nil {
		t.Fatalf("seed pre-existing customer: %v", err)
	}

	result, err := Run(ctx, repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DatasetKind != "" || result.Applied || result.DemoDataEnabled() {
		t.Fatalf("result = %+v, want unmarked existing database", result)
	}

	custs, err := repos.Customers.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(custs) != 1 {
		t.Fatalf("expected Run to skip seeding entirely when data already exists, got %d customers", len(custs))
	}
}
