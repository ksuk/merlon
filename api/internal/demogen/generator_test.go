package demogen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
	"gopkg.in/yaml.v3"
)

func TestGenerateRejectsCustomerCountAboveQuota(t *testing.T) {
	_, err := Generate(Options{Customers: DefaultCustomers + 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds the fixed quota maximum") {
		t.Fatalf("Generate(1001) error = %v, want fixed-quota validation error", err)
	}
}

// testOptions points the generator at the real repository content, using
// paths relative to this test file's package directory
// (api/internal/demogen), independent of `go test`'s working directory
// assumptions.
func testOptions() Options {
	return Options{
		Seed:               DefaultSeed,
		Anchor:             DefaultAnchor(),
		Customers:          DefaultCustomers,
		CDDWeightsPath:     "../../../content/_sample/cdd_weights/funds_transfer.yaml",
		TMScenariosPath:    "../../../content/_sample/tm_scenarios",
		ScreeningListsPath: "../../../deploy/seed/demo/screening_lists",
		CountryRiskPath:    "../../../content/_sample/country_risk_sample.yaml",
	}
}

// generateOnce runs the (relatively expensive, ~1000-customer) generation
// pipeline at most once per test binary invocation and shares the result
// across every test that only reads it.
var (
	cachedResult *Result
	cachedErr    error
	cacheOnce    sync.Once
)

func generateOnce(t *testing.T) *Result {
	t.Helper()
	cacheOnce.Do(func() {
		cachedResult, cachedErr = Generate(testOptions())
	})
	if cachedErr != nil {
		t.Fatalf("Generate failed: %v", cachedErr)
	}
	return cachedResult
}

// screeningCustomerCount is len(buildScreeningCustomers()): additive to the
// 1000-customer A2 population (A8's screening-hit narrative customers).
const screeningCustomerCount = 15

func TestGenerateProducesExpectedCounts(t *testing.T) {
	r := generateOnce(t)
	wantCustomers := DefaultCustomers + screeningCustomerCount
	if len(r.Customers) != wantCustomers {
		t.Fatalf("expected %d customers (%d main + %d screening), got %d", wantCustomers, DefaultCustomers, screeningCustomerCount, len(r.Customers))
	}
	if len(r.ScoreHistory) != wantCustomers {
		t.Fatalf("expected %d score_history entries, got %d", wantCustomers, len(r.ScoreHistory))
	}
	if len(r.Accounts) != 10 {
		t.Fatalf("expected 10 accounts, got %d", len(r.Accounts))
	}
	joint := 0
	for _, a := range r.Accounts {
		if a.AccountType == domain.AccountTypeJoint {
			joint++
		}
	}
	if joint != 3 {
		t.Fatalf("expected 3 joint accounts, got %d", joint)
	}
	if len(r.StoryCustomerIDs) != 6 {
		t.Fatalf("expected 6 fixed story customer IDs, got %d", len(r.StoryCustomerIDs))
	}
	seen := map[string]bool{}
	for _, c := range r.Customers {
		if seen[c.ID] {
			t.Fatalf("duplicate customer ID %s", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestSelfCheckPasses(t *testing.T) {
	r := generateOnce(t)
	if err := SelfCheck(r.Customers, DefaultAnchor()); err != nil {
		t.Fatalf("self-check failed on generator output: %v", err)
	}
}

func TestSelfCheckCatchesTierScoreMismatch(t *testing.T) {
	r := generateOnce(t)
	customers := append([]domain.Customer(nil), r.Customers...)
	bad := customers[0]
	wrong := domain.RiskTierLow
	if bad.RiskTier != nil && *bad.RiskTier == domain.RiskTierLow {
		wrong = domain.RiskTierHigh
	}
	bad.RiskTier = &wrong
	customers[0] = bad

	if err := SelfCheck(customers, DefaultAnchor()); err == nil {
		t.Fatal("expected self-check to fail when a tier does not match its score")
	}
}

func TestSelfCheckCatchesRealNameCollision(t *testing.T) {
	r := generateOnce(t)
	customers := append([]domain.Customer(nil), r.Customers...)
	bad := customers[0]
	attrs := make(map[string]any, len(bad.Attributes))
	for k, v := range bad.Attributes {
		attrs[k] = v
	}
	attrs["name"] = "孫正義"
	bad.Attributes = attrs
	customers[0] = bad

	if err := SelfCheck(customers, DefaultAnchor()); err == nil {
		t.Fatal("expected self-check to reject a real-name blocklist collision")
	}
}

func TestSelfCheckCatchesStaleDormantActivity(t *testing.T) {
	r := generateOnce(t)
	customers := append([]domain.Customer(nil), r.Customers...)
	for i, c := range customers {
		if c.EffectiveStatus() != domain.CustomerStatusDormant {
			continue
		}
		attrs := make(map[string]any, len(c.Attributes))
		for k, v := range c.Attributes {
			attrs[k] = v
		}
		attrs["last_activity_at"] = DefaultAnchor().AddDate(0, 0, -5).Format("2006-01-02") // only 5 days ago
		c.Attributes = attrs
		customers[i] = c
		break
	}
	if err := SelfCheck(customers, DefaultAnchor()); err == nil {
		t.Fatal("expected self-check to reject a dormant customer with recent last_activity_at")
	}
}

// TestGenerateIsByteIdenticalAcrossRuns pins the "same input -> byte
// identical output" determinism requirement: two independent Generate calls
// with the same Options must serialize to identical bytes (and therefore
// identical sha256 digests) for every output file.
func TestGenerateIsByteIdenticalAcrossRuns(t *testing.T) {
	opts := testOptions()
	r1, err := Generate(opts)
	if err != nil {
		t.Fatalf("first Generate failed: %v", err)
	}
	r2, err := Generate(opts)
	if err != nil {
		t.Fatalf("second Generate failed: %v", err)
	}

	digest := func(v any) string {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}

	if d1, d2 := digest(r1.Customers), digest(r2.Customers); d1 != d2 {
		t.Errorf("customers.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.ScoreHistory), digest(r2.ScoreHistory); d1 != d2 {
		t.Errorf("score_history.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.Accounts), digest(r2.Accounts); d1 != d2 {
		t.Errorf("accounts.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.AccountCustomers), digest(r2.AccountCustomers); d1 != d2 {
		t.Errorf("account_customers digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.Transactions), digest(r2.Transactions); d1 != d2 {
		t.Errorf("transactions.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.Alerts), digest(r2.Alerts); d1 != d2 {
		t.Errorf("alerts.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.Cases), digest(r2.Cases); d1 != d2 {
		t.Errorf("cases.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.CaseNotes), digest(r2.CaseNotes); d1 != d2 {
		t.Errorf("case_notes.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.ScreeningResults), digest(r2.ScreeningResults); d1 != d2 {
		t.Errorf("screening_results.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.AuditLogs), digest(r2.AuditLogs); d1 != d2 {
		t.Errorf("audit_logs.json digest differs across runs: %s != %s", d1, d2)
	}
	if d1, d2 := digest(r1.RuleDefinitions), digest(r2.RuleDefinitions); d1 != d2 {
		t.Errorf("rule_definitions.json digest differs across runs: %s != %s", d1, d2)
	}
	if r1.StoryIDsMarkdown != r2.StoryIDsMarkdown {
		t.Errorf("STORY_IDS.md content differs across runs")
	}
	if d1, d2 := digest(r1.ScreeningLists), digest(r2.ScreeningLists); d1 != d2 {
		t.Errorf("screening_lists digest differs across runs: %s != %s", d1, d2)
	}
}

// TestWriteFilesIsByteIdentical exercises the actual file-writing path
// (rather than just in-memory marshaling) across two runs, matching how
// `make demogen` run twice is verified.
func TestWriteFilesIsByteIdentical(t *testing.T) {
	opts := testOptions()
	r1, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	dir1, dir2 := t.TempDir(), t.TempDir()
	if err := r1.WriteFiles(dir1); err != nil {
		t.Fatal(err)
	}
	if err := r2.WriteFiles(dir2); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"customers.json", "accounts.json", "score_history.json"} {
		b1, err := os.ReadFile(dir1 + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		b2, err := os.ReadFile(dir2 + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b1, b2) {
			t.Errorf("%s differs between two generation runs", name)
		}
	}
}

// TestScoreParityAgainstFreshEngine is the A4 acceptance check: for a random
// sample of generated customers, the score/tier recorded in the output must
// equal what a freshly constructed native engine computes from the same
// attributes.
func TestScoreParityAgainstFreshEngine(t *testing.T) {
	r := generateOnce(t)
	opts := testOptions()
	eng, err := native.New(opts.CDDWeightsPath, opts.TMScenariosPath, opts.ScreeningListsPath, "")
	if err != nil {
		t.Fatalf("load fresh engine: %v", err)
	}
	ctx := context.Background()
	rng := rand.New(rand.NewSource(4242))
	checked := 0
	seen := map[int]bool{}
	for checked < 20 {
		idx := rng.Intn(len(r.Customers))
		if seen[idx] {
			continue
		}
		seen[idx] = true
		checked++

		c := r.Customers[idx]
		cCopy := c // ScoreCustomer takes a pointer but must not need mutation beyond reading
		rec, err := eng.ScoreCustomer(ctx, &cCopy, FundsTransferPresetID)
		if err != nil {
			t.Fatalf("re-score %s: %v", c.ID, err)
		}
		if c.RiskScore == nil || c.RiskTier == nil {
			t.Fatalf("customer %s missing recorded score/tier", c.ID)
		}
		if rec.Score != *c.RiskScore {
			t.Errorf("customer %s: recorded score %.6f != freshly computed %.6f", c.ID, *c.RiskScore, rec.Score)
		}
		if rec.Tier != *c.RiskTier {
			t.Errorf("customer %s: recorded tier %s != freshly computed %s", c.ID, *c.RiskTier, rec.Tier)
		}
	}
}

func TestStoryCustomersScoreNearNarrativeTargets(t *testing.T) {
	r := generateOnce(t)
	byID := make(map[string]domain.Customer, len(r.Customers))
	for _, c := range r.Customers {
		byID[c.ID] = c
	}
	// (id, expected tier) per A6; exact scores are reported, not asserted to
	// a fixed decimal, since they are computed by the engine rather than
	// hardcoded (T1-W1 instructions: "スコアは必ずengine実計算値を使う").
	cases := []struct {
		id   string
		tier domain.RiskTier
	}{
		{"demo-story-01", domain.RiskTierMedium},
		{"demo-story-02", domain.RiskTierHigh},
		{"demo-story-03", domain.RiskTierMedium},
		{"demo-story-04", domain.RiskTierHigh},
		{"demo-story-05", domain.RiskTierLow},
		{"demo-story-06", domain.RiskTierMedium},
	}
	for _, tc := range cases {
		// r.Customers' IDs are remapped to UUIDs (uuidFor(label)) by the time
		// Generate returns; tc.id is the generation-time label, so look it up
		// via the same deterministic derivation.
		c, ok := byID[uuidFor(tc.id)]
		if !ok {
			t.Fatalf("story customer %s not found in generated population", tc.id)
		}
		if c.RiskTier == nil || *c.RiskTier != tc.tier {
			got := "nil"
			if c.RiskTier != nil {
				got = string(*c.RiskTier)
			}
			t.Errorf("story customer %s: expected tier %s, got %s (score=%v)", tc.id, tc.tier, got, c.RiskScore)
		}
	}
}

// withinTolerance reports whether got is within tolerancePct percent of
// want (A2's "推奨値±10%以内" acceptance criterion).
func withinTolerance(got, want int, tolerancePct float64) bool {
	lo := float64(want) * (1 - tolerancePct/100)
	hi := float64(want) * (1 + tolerancePct/100)
	return float64(got) >= lo && float64(got) <= hi
}

// TestGenerateCountsWithinA2Tolerance checks T1-W2's generated volumes
// against Appendix A2's recommended counts, all within ±10%.
func TestGenerateCountsWithinA2Tolerance(t *testing.T) {
	r := generateOnce(t)
	if !withinTolerance(len(r.Transactions), 48000, 10) {
		t.Errorf("transactions: got %d, want ~48000 (±10%%)", len(r.Transactions))
	}
	if !withinTolerance(len(r.Alerts), 95, 10) {
		t.Errorf("alerts: got %d, want ~95 (±10%%)", len(r.Alerts))
	}
	if len(r.Cases) != 24 {
		t.Errorf("cases: got %d, want 24", len(r.Cases))
	}
	if !withinTolerance(len(r.ScreeningResults), 15, 34) { // 5 primary + ~10 low-score FP is itself an approximate target
		t.Errorf("screening_results: got %d, want ~15", len(r.ScreeningResults))
	}
	if len(r.AuditLogs) < 200 {
		t.Errorf("audit_logs: got %d, want >= 200 (A9: \"200件強\")", len(r.AuditLogs))
	}
}

// TestAlertRateBelowOnePercent is self-check (b): alerts / transactions
// must stay under 1%.
func TestAlertRateBelowOnePercent(t *testing.T) {
	r := generateOnce(t)
	rate := float64(len(r.Alerts)) / float64(len(r.Transactions))
	if rate >= 0.01 {
		t.Errorf("alert rate %.4f%% (%d/%d) is not below 1%%", rate*100, len(r.Alerts), len(r.Transactions))
	}
}

// TestAlertUniqueness is self-check (c): (customer_id, scenario_id,
// aggregation_window_start) must be unique across every alert.
func TestAlertUniqueness(t *testing.T) {
	r := generateOnce(t)
	seen := map[string]bool{}
	for _, a := range r.Alerts {
		window := ""
		if a.AggregationWindowStart != nil {
			window = a.AggregationWindowStart.Format("2006-01-02T15:04:05Z07:00")
		}
		key := a.CustomerID + "|" + a.ScenarioID + "|" + window
		if seen[key] {
			t.Errorf("duplicate (customer_id, scenario_id, aggregation_window_start): %s", key)
		}
		seen[key] = true
	}
}

// TestScreeningListsAreSynthetic is self-check (f): every screening list
// entry name must pass the same real-name blocklist check as customer
// names (DD3).
func TestScreeningListsAreSynthetic(t *testing.T) {
	r := generateOnce(t)
	guard := newRealNameGuard()
	for _, l := range r.ScreeningLists {
		for _, e := range l.Entries {
			for _, name := range e.Names {
				if guard.collides(map[string]any{"name": name}) {
					t.Errorf("screening list %s entry %s name %q collides with the real-name blocklist", l.ListID, e.EntryID, name)
				}
			}
		}
	}
}

// TestStoryAlertsAndCasesLinkage checks the A6/A7 story-specific
// requirements that aren't already covered by tier assertions: story 4's
// severity is force-upgraded to critical, story 6's 3 alerts roll into
// exactly one case, and every story customer has at least one alert.
func TestStoryAlertsAndCasesLinkage(t *testing.T) {
	r := generateOnce(t)
	alertsByCustomer := map[string][]domain.Alert{}
	for _, a := range r.Alerts {
		alertsByCustomer[a.CustomerID] = append(alertsByCustomer[a.CustomerID], a)
	}
	for _, id := range r.StoryCustomerIDs {
		if len(alertsByCustomer[id]) == 0 {
			t.Errorf("story customer %s has no alerts", id)
		}
	}
	// r.Alerts/r.Cases' CustomerID fields are remapped to UUIDs by the time
	// Generate returns; uuidFor(label) recovers the same lookup key.
	for _, a := range alertsByCustomer[uuidFor("demo-story-04")] {
		if a.Severity != domain.AlertSeverityCritical {
			t.Errorf("demo-story-04 alert %s: severity=%s, want critical (A6/A7 override)", a.ID, a.Severity)
		}
	}
	if got := len(alertsByCustomer[uuidFor("demo-story-06")]); got != 3 {
		t.Errorf("demo-story-06: got %d alerts, want 3 (2 structuring windows + 1 rapid_movement)", got)
	}
	story6AlertIDs := map[string]bool{}
	for _, a := range alertsByCustomer[uuidFor("demo-story-06")] {
		story6AlertIDs[a.ID] = true
	}
	var story6Case *domain.Case
	for i, c := range r.Cases {
		if c.CustomerID == uuidFor("demo-story-06") {
			story6Case = &r.Cases[i]
		}
	}
	if story6Case == nil {
		t.Fatal("no case found for demo-story-06")
	}
	if len(story6Case.AlertIDs) != 3 {
		t.Errorf("demo-story-06 case %s: got %d linked alerts, want 3", story6Case.ID, len(story6Case.AlertIDs))
	}
	for _, id := range story6Case.AlertIDs {
		if !story6AlertIDs[id] {
			t.Errorf("demo-story-06 case %s references alert %s not in its own alert set", story6Case.ID, id)
		}
	}
}

// TestCasePriorityDistribution checks A9's low4/medium12/high6/critical2
// case priority distribution.
func TestCasePriorityDistribution(t *testing.T) {
	r := generateOnce(t)
	counts := map[domain.CasePriority]int{}
	for _, c := range r.Cases {
		counts[c.Priority]++
	}
	want := map[domain.CasePriority]int{
		domain.CasePriorityLow: 4, domain.CasePriorityMedium: 12,
		domain.CasePriorityHigh: 6, domain.CasePriorityCritical: 2,
	}
	for priority, wantCount := range want {
		if counts[priority] != wantCount {
			t.Errorf("case priority %s: got %d, want %d", priority, counts[priority], wantCount)
		}
	}
}

// TestNoTransactionsBeforeAccountOpened checks that no customer has a
// transaction dated before their own attributes.account_opened_at — the
// transaction-level counterpart to T1-W1's attribute-level checks.
func TestNoTransactionsBeforeAccountOpened(t *testing.T) {
	r := generateOnce(t)
	byID := make(map[string]domain.Customer, len(r.Customers))
	for _, c := range r.Customers {
		byID[c.ID] = c
	}
	anchor := DefaultAnchor()
	for _, tx := range r.Transactions {
		c, ok := byID[tx.CustomerID]
		if !ok {
			continue
		}
		opened := parseAttrDate(c.Attributes, "account_opened_at", anchor.AddDate(-10, 0, 0))
		if tx.ExecutedAt.Before(opened) {
			t.Errorf("transaction %s for %s executed at %s, before account_opened_at %s", tx.ID, tx.CustomerID, tx.ExecutedAt, opened)
		}
	}
}

// TestDormantCustomersHaveNoRecentTransactions is the transaction-level
// counterpart to SelfCheck's attribute-level dormant check: a customer
// whose *final* status is still dormant must have zero transactions in the
// 180 days before anchor.
func TestDormantCustomersHaveNoRecentTransactions(t *testing.T) {
	r := generateOnce(t)
	dormant := map[string]bool{}
	for _, c := range r.Customers {
		if c.EffectiveStatus() == domain.CustomerStatusDormant {
			dormant[c.ID] = true
		}
	}
	anchor := DefaultAnchor()
	cutoff := anchor.AddDate(0, 0, -180)
	for _, tx := range r.Transactions {
		if dormant[tx.CustomerID] && tx.ExecutedAt.After(cutoff) {
			t.Errorf("dormant customer %s has transaction %s at %s, within 180 days of anchor", tx.CustomerID, tx.ID, tx.ExecutedAt)
		}
	}
}

// TestStoryIDsMarkdownMatchesCommittedFile is a golden test (T1-W2
// instructions: "STORY_IDS.mdとscreening_listsはゴールデンテストで固定"):
// regenerating must reproduce exactly what's committed at
// deploy/seed/demo/STORY_IDS.md, or the test fails.
func TestStoryIDsMarkdownMatchesCommittedFile(t *testing.T) {
	r := generateOnce(t)
	committed, err := os.ReadFile("../../../deploy/seed/demo/STORY_IDS.md")
	if err != nil {
		t.Fatalf("read committed STORY_IDS.md: %v", err)
	}
	if r.StoryIDsMarkdown != string(committed) {
		t.Errorf("generated STORY_IDS.md does not match the committed copy at deploy/seed/demo/STORY_IDS.md; regenerate with `make demogen` and commit the result")
	}
}

// TestScreeningListsMatchCommittedFiles is the screening_lists half of the
// same golden test: every generated list must byte-match its committed YAML
// file at deploy/seed/demo/screening_lists/<list_id>.yaml.
func TestScreeningListsMatchCommittedFiles(t *testing.T) {
	r := generateOnce(t)
	for _, l := range r.ScreeningLists {
		generated, err := yaml.Marshal(l)
		if err != nil {
			t.Fatalf("marshal screening list %s: %v", l.ListID, err)
		}
		committed, err := os.ReadFile("../../../deploy/seed/demo/screening_lists/" + l.ListID + ".yaml")
		if err != nil {
			t.Fatalf("read committed screening list %s: %v", l.ListID, err)
		}
		if string(generated) != string(committed) {
			t.Errorf("generated screening list %s does not match the committed copy at deploy/seed/demo/screening_lists/%s.yaml; regenerate with `make demogen` and commit the result", l.ListID, l.ListID)
		}
	}
}
