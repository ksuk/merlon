package demogen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"sync"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
)

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

func TestGenerateProducesExpectedCounts(t *testing.T) {
	r := generateOnce(t)
	if len(r.Customers) != DefaultCustomers {
		t.Fatalf("expected %d customers, got %d", DefaultCustomers, len(r.Customers))
	}
	if len(r.ScoreHistory) != DefaultCustomers {
		t.Fatalf("expected %d score_history entries, got %d", DefaultCustomers, len(r.ScoreHistory))
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
		c, ok := byID[tc.id]
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
