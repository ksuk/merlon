// Package demogen is Merlon's deterministic synthetic-demo-data generator
// (PH7 T1). It builds a population of customers whose CDD scores are
// computed by importing api/internal/engine/native directly — the same
// evaluation code path the API uses at runtime — so that re-scoring any
// generated customer during a demo reproduces the exact score/tier recorded
// at generation time (Auditability First, ADR-0004).
//
// This first wave (T1-W1) produces customers, accounts, and score history
// only. Transactions, alerts, cases, and screening results are a later wave;
// see .release-tasks/PH7-demo-publication.md Appendix A.
package demogen

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
)

// DefaultSeed and DefaultAnchor make generation deterministic without any
// flags: the same binary invoked twice with no arguments produces
// byte-identical output.
const (
	DefaultSeed      int64  = 20260701
	DefaultAnchorStr string = "2026-07-01"

	// DefaultCustomers is the A2 population size.
	DefaultCustomers = 1000

	// FundsTransferPresetID is the preset_id of the CDD weight file this
	// generator scores customers against (D-a).
	FundsTransferPresetID = "funds_transfer"

	// maxRejectionAttempts bounds the tier-targeted rejection-sampling loop
	// (see population.go) so a design mistake fails fast with a clear error
	// instead of hanging.
	maxRejectionAttempts = 200_000
)

// Options configures a single generation run. Every field has a sane
// default; the CLI (api/cmd/merlon-demogen) exposes Seed and Anchor as
// flags.
type Options struct {
	Seed      int64
	Anchor    time.Time
	Customers int

	// CDDWeightsPath points at the risk_factors preset used to score
	// customers. Defaults to content/_sample/cdd_weights/funds_transfer.yaml
	// resolved relative to the repository root.
	CDDWeightsPath string
	// TMScenariosPath and ScreeningListsPath are required by native.New but
	// unused by ScoreCustomer; defaults point at the repo's sample content so
	// the engine loads standalone in tests and the CLI alike.
	TMScenariosPath    string
	ScreeningListsPath string
}

func (o Options) withDefaults() Options {
	if o.Seed == 0 {
		o.Seed = DefaultSeed
	}
	if o.Anchor.IsZero() {
		o.Anchor = DefaultAnchor()
	}
	if o.Customers <= 0 {
		o.Customers = DefaultCustomers
	}
	// These defaults assume a working directory of api/ (i.e. the "cd api &&
	// go run ./cmd/merlon-demogen" invocation the Makefile `demogen` target
	// uses); callers with a different working directory (tests included)
	// should set the three path fields explicitly.
	if o.CDDWeightsPath == "" {
		o.CDDWeightsPath = "../content/_sample/cdd_weights/funds_transfer.yaml"
	}
	if o.TMScenariosPath == "" {
		o.TMScenariosPath = "../content/_sample/tm_scenarios"
	}
	if o.ScreeningListsPath == "" {
		o.ScreeningListsPath = "../deploy/seed/demo/screening_lists"
	}
	return o
}

// DefaultAnchor returns the fixed anchor timestamp (2026-07-01T00:00:00Z)
// every date in the generated dataset is computed relative to. time.Now is
// never used anywhere in this package.
func DefaultAnchor() time.Time {
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
}

// Result is the full T1-W1 output: everything the seed loader (T2) will read
// from deploy/seed/demo/*.json.
type Result struct {
	Customers        []domain.Customer
	Accounts         []domain.Account
	AccountCustomers []domain.AccountCustomer
	ScoreHistory     []domain.ScoreRecord

	// StoryCustomerIDs lists the 6 fixed IDs in A6 narrative order, for the
	// next wave (transactions/alerts) and STORY_IDS.md to key off.
	StoryCustomerIDs []string
}

// Generate runs the full deterministic pipeline: load the native engine
// against the funds_transfer CDD preset, materialize the 6 fixed story
// customers, then fill the remaining population via tier-targeted rejection
// sampling, scoring every customer through the engine exactly once.
func Generate(opts Options) (*Result, error) {
	o := opts.withDefaults()

	eng, err := native.New(o.CDDWeightsPath, o.TMScenariosPath, o.ScreeningListsPath, "")
	if err != nil {
		return nil, fmt.Errorf("load native engine: %w", err)
	}

	rng := rand.New(rand.NewSource(o.Seed))
	ctx := context.Background()

	blocked := newRealNameGuard()

	storyCustomers, storyIDs := buildStoryCustomers(o.Anchor)
	if len(storyCustomers) != len(storyIDs) {
		return nil, fmt.Errorf("story customer/id count mismatch")
	}
	for i := range storyCustomers {
		if blocked.collides(storyCustomers[i].Attributes) {
			return nil, fmt.Errorf("story customer %s collides with real-name blocklist", storyIDs[i])
		}
	}

	// Story customers must carry their engine-computed tier before
	// generatePopulation runs: it reads RiskTier off each story customer to
	// size the remaining tier quota for the rejection-sampling loop
	// (subtractStoryTier in population.go). The final unified scoring pass
	// below re-scores every customer (story included) to build ScoreHistory;
	// re-scoring identical attributes through the same engine is
	// deterministic, so this early pass and the later one always agree.
	for i := range storyCustomers {
		rec, err := eng.ScoreCustomer(ctx, &storyCustomers[i], FundsTransferPresetID)
		if err != nil {
			return nil, fmt.Errorf("pre-score story customer %s: %w", storyCustomers[i].ID, err)
		}
		score, tier := rec.Score, rec.Tier
		storyCustomers[i].RiskScore = &score
		storyCustomers[i].RiskTier = &tier
	}

	remainingNeeded := o.Customers - len(storyCustomers)
	if remainingNeeded < 0 {
		return nil, fmt.Errorf("customers option %d is smaller than the %d fixed story customers", o.Customers, len(storyCustomers))
	}

	generated, err := generatePopulation(eng, rng, o.Anchor, remainingNeeded, storyCustomers, blocked)
	if err != nil {
		return nil, err
	}

	all := make([]domain.Customer, 0, o.Customers)
	all = append(all, storyCustomers...)
	all = append(all, generated...)

	for i := len(storyCustomers); i < len(all); i++ {
		all[i].ID = fmt.Sprintf("demo-cust-%06d", i-len(storyCustomers)+1)
	}

	assignExternalIDs(all)

	scoreHistory := make([]domain.ScoreRecord, 0, len(all))
	for i := range all {
		rec, err := eng.ScoreCustomer(ctx, &all[i], FundsTransferPresetID)
		if err != nil {
			return nil, fmt.Errorf("score customer %s: %w", all[i].ID, err)
		}
		// The engine stamps ScoredAt with time.Now(); replace it with an
		// anchor-derived deterministic past date so re-running the generator
		// produces byte-identical output (no wall-clock dependency).
		scoredAt := deterministicScoredAt(o.Anchor, i)
		rec.ID = fmt.Sprintf("demo-score-%06d", i+1)
		rec.ScoredAt = scoredAt

		score := rec.Score
		tier := rec.Tier
		all[i].RiskScore = &score
		all[i].RiskTier = &tier
		all[i].LastScoredAt = &scoredAt

		scoreHistory = append(scoreHistory, *rec)
	}

	accounts, accountCustomers := buildAccounts(all, o.Anchor)

	return &Result{
		Customers:        all,
		Accounts:         accounts,
		AccountCustomers: accountCustomers,
		ScoreHistory:     scoreHistory,
		StoryCustomerIDs: storyIDs,
	}, nil
}

// deterministicScoredAt spreads score timestamps over the 30 days before the
// anchor, purely for display variety; it carries no other meaning.
func deterministicScoredAt(anchor time.Time, index int) time.Time {
	return anchor.Add(-time.Duration(1+index%30) * 24 * time.Hour)
}

// assignExternalIDs stamps MNP-{I|C}{6-digit} external IDs in slice order
// (A3), so the assignment is a pure function of generation order and stays
// byte-identical across runs. "I" covers individual customers; every other
// customer_type (corporate_domestic/corporate_foreign/npo/trust/partnership)
// gets "C".
func assignExternalIDs(customers []domain.Customer) {
	var individualSeq, corporateSeq int
	for i := range customers {
		if customers[i].CustomerType == domain.CustomerTypeIndividual {
			individualSeq++
			customers[i].ExternalID = fmt.Sprintf("MNP-I%06d", individualSeq)
		} else {
			corporateSeq++
			customers[i].ExternalID = fmt.Sprintf("MNP-C%06d", corporateSeq)
		}
	}
}

// weightedPick performs a weighted random choice over parallel keys/weights
// slices using rng. Keys must be supplied in a fixed order (never derived
// from map iteration) so the same seed always draws the same sequence.
func weightedPick(rng *rand.Rand, keys []string, weights []int) string {
	total := 0
	for _, w := range weights {
		total += w
	}
	r := rng.Intn(total)
	cum := 0
	for i, w := range weights {
		cum += w
		if r < cum {
			return keys[i]
		}
	}
	return keys[len(keys)-1]
}

// sortedStringKeys returns a stably sorted copy of keys, used wherever a map
// would otherwise be iterated for JSON or log output.
func sortedStringKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
