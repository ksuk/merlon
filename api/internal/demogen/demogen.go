// Package demogen is Merlon's deterministic synthetic-demo-data generator
// (PH7 T1). It builds a population of customers whose CDD scores are
// computed by importing api/internal/engine/native directly — the same
// evaluation code path the API uses at runtime — so that re-scoring any
// generated customer during a demo reproduces the exact score/tier recorded
// at generation time (Auditability First, ADR-0004).
//
// T1-W1 built customers, accounts, and score history. T1-W2 (this file's
// Generate) extends the same deterministic pipeline with transactions,
// alerts, cases, screening, audit logs, and rule definitions — all scored
// and evaluated through the same single native.Engine instance, and every
// alert produced by actually calling the engine's realtime/batch evaluation
// on the seeded transactions (never a hand-computed "should fire" guess).
// See .tasks/archive/PH7-demo-publication.md Appendix A5-A9.
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
	// CountryRiskPath is read only for rule_definitions.json (T1-W2's
	// registered COUNTRY_RISK rule); the engine itself is loaded without a
	// country risk table (funds_transfer.yaml's geography factor uses its
	// own values map, not country_risk_table — see native.go's
	// resolveFactor), so this path does not affect scoring.
	CountryRiskPath string
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
	if o.CountryRiskPath == "" {
		o.CountryRiskPath = "../content/_sample/country_risk_sample.yaml"
	}
	return o
}

// DefaultAnchor returns the fixed anchor timestamp (2026-07-01T00:00:00Z)
// every date in the generated dataset is computed relative to. time.Now is
// never used anywhere in this package.
func DefaultAnchor() time.Time {
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
}

// Result is the full T1-W1+W2 output: everything the seed loader (T2) will
// read from deploy/seed/demo/*.json, plus the two committed artifacts
// (STORY_IDS.md, screening_lists/*.yaml).
type Result struct {
	Customers        []domain.Customer
	Accounts         []domain.Account
	AccountCustomers []domain.AccountCustomer
	ScoreHistory     []domain.ScoreRecord

	// StoryCustomerIDs lists the 6 fixed IDs in A6 narrative order, for the
	// next wave (transactions/alerts) and STORY_IDS.md to key off.
	StoryCustomerIDs []string

	// T1-W2 additions.
	Transactions     []domain.Transaction
	Alerts           []domain.Alert
	Cases            []domain.Case
	CaseNotes        []caseNoteRecord
	ScreeningResults []domain.ScreeningResultRecord
	AuditLogs        []domain.AuditEntry
	RuleDefinitions  []domain.RuleDefinition

	// ScreeningLists and StoryIDsMarkdown are the two small, committed
	// (non-gitignored) artifacts: deploy/seed/demo/screening_lists/*.yaml
	// and deploy/seed/demo/STORY_IDS.md. Both are golden-tested (generator_
	// test.go) against the copies actually committed to the repo.
	ScreeningLists   []screeningListSeed
	StoryIDsMarkdown string
}

// Generate runs the full deterministic pipeline: load the native engine
// against the funds_transfer CDD preset, materialize the 6 fixed story
// customers, then fill the remaining population via tier-targeted rejection
// sampling, scoring every customer through the engine exactly once.
func Generate(opts Options) (*Result, error) {
	o := opts.withDefaults()
	if o.Customers > DefaultCustomers {
		return nil, fmt.Errorf("customers option %d exceeds the fixed quota maximum of %d", o.Customers, DefaultCustomers)
	}

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

	// --- T1-W2: A8 screening-narrative customers, additive to the 1000-
	// customer population and scored through the same engine, but excluded
	// from T1-W1's population-distribution quotas (they are not part of the
	// "1,000 realistic customers" target — A8 is a separate narrative axis).
	screeningSeeds := buildScreeningCustomers()
	screeningCustomers := buildScreeningCustomerRecords(o.Anchor, screeningSeeds)
	for i := range screeningCustomers {
		if blocked.collides(screeningCustomers[i].Attributes) {
			return nil, fmt.Errorf("screening customer %s collides with real-name blocklist", screeningCustomers[i].ID)
		}
	}
	mainPopulationCount := len(all)
	for i := range screeningCustomers {
		rec, err := eng.ScoreCustomer(ctx, &screeningCustomers[i], FundsTransferPresetID)
		if err != nil {
			return nil, fmt.Errorf("score screening customer %s: %w", screeningCustomers[i].ID, err)
		}
		idx := mainPopulationCount + i
		scoredAt := deterministicScoredAt(o.Anchor, idx)
		rec.ID = fmt.Sprintf("demo-score-%06d", idx+1)
		rec.ScoredAt = scoredAt
		score, tier := rec.Score, rec.Tier
		screeningCustomers[i].RiskScore = &score
		screeningCustomers[i].RiskTier = &tier
		screeningCustomers[i].LastScoredAt = &scoredAt
		scoreHistory = append(scoreHistory, *rec)
	}
	all = append(all, screeningCustomers...)
	// Idempotent for the unchanged prefix (assignExternalIDs is a pure
	// function of slice order): re-running it just extends the sequence for
	// the newly-appended screening customers.
	assignExternalIDs(all)

	// --- T1-W2: transactions, alerts, cases, screening results, audit
	// logs, rule definitions (A5-A9).
	cfgs, err := loadScenarioConfigs(o.TMScenariosPath)
	if err != nil {
		return nil, fmt.Errorf("load scenario configs: %w", err)
	}
	txIDs := &idSeq{}
	mainAndStory := all[:mainPopulationCount]

	tierByCustomer := make(map[string]domain.RiskTier, len(all))
	for _, c := range all {
		if c.RiskTier != nil {
			tierByCustomer[c.ID] = *c.RiskTier
		} else {
			tierByCustomer[c.ID] = domain.RiskTierMedium
		}
	}

	// Ordinary background transaction history (A5) for every main-
	// population + story customer. Screening customers get none (A8 is a
	// screening narrative, not a transaction narrative).
	backgroundByCustomer := make(map[string][]domain.Transaction, len(mainAndStory))
	var allTxns []domain.Transaction
	for _, c := range mainAndStory {
		txns := generateBackgroundTransactions(rng, o.Anchor, c, txIDs)
		backgroundByCustomer[c.ID] = txns
		allTxns = append(allTxns, txns...)
	}

	storyIncidents := buildStoryIncidents(o.Anchor, cfgs, txIDs)
	for _, inc := range storyIncidents {
		allTxns = append(allTxns, inc.Transactions...)
	}

	// buildFPIncidents mutates a handful of dormant background customers in
	// place (Status -> Active, attributes.last_activity_at -> reactivation
	// date) via mainAndStory, which shares all's backing array.
	fpIncidents := buildFPIncidents(o.Anchor, mainAndStory, storyIDs, cfgs, txIDs)
	for _, inc := range fpIncidents {
		allTxns = append(allTxns, inc.Transactions...)
	}

	alertCtx := newAlertBuildContext(o.Anchor, cfgs, allTxns)

	// Background "organic noise" alerts: rare (a handful out of ~1000
	// customers empirically), but real customer/transaction combinations can
	// still cross a threshold by chance. Treated the same as FP alerts for
	// status assignment since they carry no story of their own.
	for _, c := range mainAndStory {
		raw, err := evaluateAlerts(ctx, eng, c.ID, tierByCustomer[c.ID], backgroundByCustomer[c.ID])
		if err != nil {
			return nil, err
		}
		s, e := alertCtx.add(c.ID, raw)
		for i := s; i < e; i++ {
			alertCtx.alerts[i].Status = fpStatusWeights(rng)
		}
	}

	// A6 story TP incidents (8 of A7's ~9), evaluated one incident at a time
	// in isolation: evalStructuring/evalRapid/evalHFSA/evalHighRisk all
	// return on the first breach found within the transactions they are
	// given, so story 6's 3 separate incidents (2 structuring windows +
	// 1 rapid_movement, at different points in time) would only ever
	// surface as 1 alert if evaluated together against the customer's full
	// history — see evaluateAlerts' doc comment.
	storyAlertStatus := map[string]domain.AlertStatus{
		"demo-story-01": domain.AlertStatusClosedTruePositive,
		"demo-story-02": domain.AlertStatusClosedTruePositive,
		"demo-story-03": domain.AlertStatusClosedTruePositive,
		"demo-story-04": domain.AlertStatusInvestigating, // linked to the investigating case; A7 open-family story
		"demo-story-05": domain.AlertStatusClosedTruePositive,
		"demo-story-06": domain.AlertStatusInvestigating, // A7: "直近ストーリーはopen系に"
	}
	// storyAlertIdx/fpAlertIdx record *indices* into alertCtx.alerts rather
	// than copying alert values now: finalizeAlerts (below) assigns each
	// alert's ID after every incident has been processed, so a value copy
	// taken here would carry an empty ID forever. Indices are resolved into
	// real domain.Alert values only after finalizeAlerts runs.
	storyAlertIdx := map[string][]int{}
	for _, inc := range storyIncidents {
		raw, err := evaluateAlerts(ctx, eng, inc.CustomerID, tierByCustomer[inc.CustomerID], inc.Transactions)
		if err != nil {
			return nil, err
		}
		fired := scenarioFired(raw, inc.ExpectedScenario)
		if inc.ExpectedScenario != "" && !fired {
			return nil, fmt.Errorf("self-check (a) failed: story incident %s [%s] expected %s to fire but it did not", inc.CustomerID, inc.Label, inc.ExpectedScenario)
		}
		if inc.ExpectedScenario == "" && fired {
			return nil, fmt.Errorf("self-check (a) failed: story incident %s [%s] expected no alert but one fired", inc.CustomerID, inc.Label)
		}
		if inc.ExpectedScenario == "" {
			continue // story 1's non-firing backtest precedent
		}
		s, e := alertCtx.add(inc.CustomerID, raw)
		for i := s; i < e; i++ {
			alertCtx.alerts[i].Status = storyAlertStatus[inc.CustomerID]
			if inc.CustomerID == "demo-story-04" {
				alertCtx.alerts[i].Severity = domain.AlertSeverityCritical // A6/A7: "④のみcritical格上げ"
			}
			storyAlertIdx[inc.CustomerID] = append(storyAlertIdx[inc.CustomerID], i)
		}
	}

	// A7 background FP incidents (~86), also evaluated in isolation.
	fpAlertIdx := map[string][]int{}
	var fpOrder []string
	for _, inc := range fpIncidents {
		raw, err := evaluateAlerts(ctx, eng, inc.CustomerID, tierByCustomer[inc.CustomerID], inc.Transactions)
		if err != nil {
			return nil, err
		}
		if !scenarioFired(raw, inc.ExpectedScenario) {
			return nil, fmt.Errorf("self-check (a) failed: FP incident %s [%s/%s] expected %s to fire but it did not", inc.CustomerID, inc.Category, inc.Narrative, inc.ExpectedScenario)
		}
		s, e := alertCtx.add(inc.CustomerID, raw)
		for i := s; i < e; i++ {
			alertCtx.alerts[i].Status = fpStatusWeights(rng)
		}
		if e > s {
			if len(fpAlertIdx[inc.CustomerID]) == 0 {
				fpOrder = append(fpOrder, inc.CustomerID)
			}
			for i := s; i < e; i++ {
				fpAlertIdx[inc.CustomerID] = append(fpAlertIdx[inc.CustomerID], i)
			}
		}
	}

	alerts := finalizeAlerts(alertCtx)

	storyAlertsByCustomer := make(map[string][]domain.Alert, len(storyAlertIdx))
	for custID, idxs := range storyAlertIdx {
		for _, i := range idxs {
			storyAlertsByCustomer[custID] = append(storyAlertsByCustomer[custID], alerts[i])
		}
	}
	fpAlertsByCustomer := make(map[string][]domain.Alert, len(fpAlertIdx))
	for custID, idxs := range fpAlertIdx {
		for _, i := range idxs {
			fpAlertsByCustomer[custID] = append(fpAlertsByCustomer[custID], alerts[i])
		}
	}

	caseSeeds := append(storyCaseSeeds(storyAlertsByCustomer), backgroundCaseSeeds(fpAlertsByCustomer, fpOrder)...)
	cases, caseNotes := finalizeCases(o.Anchor, caseSeeds)

	screeningResultIDs := &idSeq{}
	screeningResults := buildScreeningResults(o.Anchor, screeningResultIDs)
	screeningLists := buildScreeningLists()

	ruleDefinitions, err := buildRuleDefinitions(o.Anchor, o.TMScenariosPath, o.CDDWeightsPath, o.CountryRiskPath)
	if err != nil {
		return nil, err
	}

	auditLogs := buildAuditLogs(o.Anchor, ruleDefinitions, sampleAuditCustomers(all), cases, caseNotes, screeningResults)

	// STORY_IDS.md is authored from the plain human-readable labels (all,
	// allTxns, cases, etc. still carry "demo-story-01"-style IDs at this
	// point) — assembleStoryIDsInput computes each row's UUID itself (via
	// uuidFor) for display, so the markdown can show "label / UUID" without
	// needing the entities themselves remapped yet.
	storyIDsMarkdown := buildStoryIDsMarkdown(assembleStoryIDsInput(o.Seed, o.Anchor.Format("2006-01-02"), all, storyIDs, allTxns, storyAlertsByCustomer, cases, screeningResults, screeningLists))

	result := &Result{
		Customers:        all,
		Accounts:         accounts,
		AccountCustomers: accountCustomers,
		ScoreHistory:     scoreHistory,
		StoryCustomerIDs: storyIDs,
		Transactions:     allTxns,
		Alerts:           alerts,
		Cases:            cases,
		CaseNotes:        caseNotes,
		ScreeningResults: screeningResults,
		AuditLogs:        auditLogs,
		RuleDefinitions:  ruleDefinitions,
		ScreeningLists:   screeningLists,
		StoryIDsMarkdown: storyIDsMarkdown,
	}

	// Every entity above still carries its human-readable generation-time
	// label as its ID; this is the one place that rewrites them to the
	// deterministic UUIDs PostgreSQL's UUID-typed columns require (see
	// remap.go's doc comment). Everything upstream of this call (self-
	// checks, story wiring, STORY_IDS.md) intentionally still sees labels.
	remapIDsToUUIDs(result)

	return result, nil
}

// scenarioFired reports whether any alert in raw has the given scenario ID.
func scenarioFired(raw []domain.Alert, scenarioID string) bool {
	for _, a := range raw {
		if a.ScenarioID == scenarioID {
			return true
		}
	}
	return false
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
