// Package outcome contains the single deterministic matcher shared by
// backtests and known-matter coverage analysis. It deliberately knows only
// about alert-shaped detections and immutable historical state; consumers are
// responsible for loading those records as of their requested snapshot.
package outcome

import (
	"math"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const MatcherVersion = "outcome-matcher-v1"

type Label string

const (
	LabelTP          Label = "TP"
	LabelFP          Label = "FP"
	LabelUnlabeled   Label = "unlabeled"
	LabelUnevaluable Label = "unevaluable"
	// Descriptive aliases keep API consumers readable while the wire labels
	// remain the compact TP/FP contract.
	LabelTruePositive  = LabelTP
	LabelFalsePositive = LabelFP
)

type OutcomeLabel = Label

func (l Label) Valid() bool {
	switch l {
	case LabelTP, LabelFP, LabelUnlabeled, LabelUnevaluable:
		return true
	default:
		return false
	}
}

type Mode string

const (
	ModeBacktest Mode = "backtest"
	ModeCoverage Mode = "coverage"
)

const (
	MetricTransactions = "transaction_jaccard"
	MetricInterval     = "interval_overlap"
)

// Detection is the common alert-level representation. A transaction set is
// preferred for matching; windows are the deterministic fallback when a
// producer has no complete transaction list.
type Detection struct {
	ID             string
	CustomerID     string
	ScenarioID     string
	TransactionIDs []string
	WindowFrom     *time.Time
	WindowTo       *time.Time
	DetectedAt     time.Time
	ScoreTier      domain.RiskTier
	ScoreTierKnown bool
}

type HistoricalState struct {
	AlertStatus    domain.AlertStatus
	Decision       *domain.AlertDecisionEvent
	CaseStatus     domain.CaseStatus
	STRFiled       bool
	ScoreTier      domain.RiskTier
	ScoreTierKnown bool
}

type Reference struct {
	Detection
	State HistoricalState
	// Provenance is consumer-supplied source metadata (for example alert
	// provenance or the case/STR source row). It is copied into every result
	// rather than inferred from the current mutable alert.
	Provenance map[string]string
}

type Options struct {
	Mode             Mode
	SnapshotAt       time.Time
	MinOverlap       float64
	ResolveScoreTier func(customerID string, at time.Time) (domain.RiskTier, bool)
}

type Match struct {
	CandidateID string  `json:"candidate_id"`
	ReferenceID string  `json:"reference_id"`
	CustomerID  string  `json:"customer_id"`
	ScenarioID  string  `json:"scenario_id,omitempty"`
	Score       float64 `json:"score"`
	Metric      string  `json:"metric"`
	TimeDeltaMS int64   `json:"time_delta_ms"`
}

type Provenance struct {
	MatcherVersion string            `json:"matcher_version"`
	Mode           Mode              `json:"mode"`
	SnapshotAt     time.Time         `json:"snapshot_at"`
	Assumptions    []string          `json:"assumptions"`
	Source         map[string]string `json:"source,omitempty"`
}

type Evaluation struct {
	CandidateID string          `json:"candidate_id"`
	ReferenceID string          `json:"reference_id,omitempty"`
	CustomerID  string          `json:"customer_id"`
	ScenarioID  string          `json:"scenario_id,omitempty"`
	Label       Label           `json:"label"`
	Match       *Match          `json:"match,omitempty"`
	ScoreTier   domain.RiskTier `json:"score_tier,omitempty"`
	Denominator bool            `json:"denominator"`
	Provenance  Provenance      `json:"provenance"`
}

type Result struct {
	MatcherVersion      string       `json:"matcher_version"`
	Mode                Mode         `json:"mode"`
	SnapshotAt          time.Time    `json:"snapshot_at"`
	Assumptions         []string     `json:"assumptions"`
	Matches             []Match      `json:"matches"`
	Evaluations         []Evaluation `json:"evaluations"`
	UnmatchedCandidates []string     `json:"unmatched_candidates"`
	UnmatchedReferences []string     `json:"unmatched_references"`
	Denominator         int          `json:"denominator"`
}

type MatchResult = Result

// Matcher is a reusable, configured matcher. Keeping it stateless makes the
// same implementation safe for worker retries and API reads.
type Matcher struct{ options Options }

func NewMatcher(options Options) *Matcher {
	if options.Mode == "" {
		options.Mode = ModeBacktest
	}
	if options.MinOverlap <= 0 || options.MinOverlap > 1 {
		options.MinOverlap = 0.50
	}
	return &Matcher{options: options}
}

func (m *Matcher) Match(candidates []Detection, references []Reference) Result {
	if m == nil {
		return MatchAlerts(candidates, references, Options{})
	}
	return match(candidates, references, m.options)
}

func MatchAlerts(candidates []Detection, references []Reference, options Options) Result {
	return NewMatcher(options).Match(candidates, references)
}

func match(candidates []Detection, references []Reference, options Options) Result {
	if options.Mode == "" {
		options.Mode = ModeBacktest
	}
	if options.MinOverlap <= 0 || options.MinOverlap > 1 {
		options.MinOverlap = 0.50
	}
	snapshot := options.SnapshotAt.UTC()
	filteredCandidates := filterCandidates(candidates, snapshot)
	filteredReferences := filterReferences(references, snapshot)
	sort.Slice(filteredCandidates, func(i, j int) bool { return filteredCandidates[i].ID < filteredCandidates[j].ID })
	sort.Slice(filteredReferences, func(i, j int) bool { return filteredReferences[i].ID < filteredReferences[j].ID })

	assumptions := []string{
		"alert is the primary matching granularity",
		"transaction-set Jaccard is preferred; interval overlap is the fallback",
		"candidate threshold is inclusive at 0.50",
		"one-to-one assignment sorts overlap descending, time delta ascending, then IDs",
		"unlabeled and unevaluable observations are excluded from rates",
	}
	if options.Mode == ModeBacktest {
		assumptions = append(assumptions, "backtest requires customer and scenario equality")
	} else {
		assumptions = append(assumptions, "coverage permits scenario-union matching within a customer")
	}
	result := Result{MatcherVersion: MatcherVersion, Mode: options.Mode, SnapshotAt: snapshot, Assumptions: assumptions,
		Matches: []Match{}, Evaluations: []Evaluation{}, UnmatchedCandidates: []string{}, UnmatchedReferences: []string{}}
	candidatesByID := make(map[string]Detection, len(filteredCandidates))
	referencesByID := make(map[string]Reference, len(filteredReferences))
	for _, candidate := range filteredCandidates {
		candidatesByID[candidate.ID] = candidate
	}
	for _, reference := range filteredReferences {
		referencesByID[reference.ID] = reference
	}
	proposals := make([]proposal, 0)
	for _, candidate := range filteredCandidates {
		for _, reference := range filteredReferences {
			if !compatible(candidate, reference.Detection, options.Mode) {
				continue
			}
			metric, score, ok := overlap(candidate, reference.Detection)
			if ok && score >= options.MinOverlap {
				proposals = append(proposals, proposal{candidate: candidate, reference: reference, metric: metric, score: score})
			}
		}
	}
	sort.Slice(proposals, func(i, j int) bool {
		if proposals[i].score != proposals[j].score {
			return proposals[i].score > proposals[j].score
		}
		leftDelta, rightDelta := timeDelta(proposals[i].candidate.DetectedAt, proposals[i].reference.DetectedAt), timeDelta(proposals[j].candidate.DetectedAt, proposals[j].reference.DetectedAt)
		if leftDelta != rightDelta {
			return leftDelta < rightDelta
		}
		if proposals[i].candidate.ID != proposals[j].candidate.ID {
			return proposals[i].candidate.ID < proposals[j].candidate.ID
		}
		return proposals[i].reference.ID < proposals[j].reference.ID
	})
	usedCandidates, usedReferences := map[string]bool{}, map[string]bool{}
	matched := map[string]Match{}
	for _, candidate := range proposals {
		if usedCandidates[candidate.candidate.ID] || usedReferences[candidate.reference.ID] {
			continue
		}
		usedCandidates[candidate.candidate.ID], usedReferences[candidate.reference.ID] = true, true
		item := Match{CandidateID: candidate.candidate.ID, ReferenceID: candidate.reference.ID, CustomerID: candidate.candidate.CustomerID, ScenarioID: candidate.candidate.ScenarioID, Score: candidate.score, Metric: candidate.metric, TimeDeltaMS: timeDelta(candidate.candidate.DetectedAt, candidate.reference.DetectedAt)}
		matched[candidate.candidate.ID] = item
		result.Matches = append(result.Matches, item)
	}
	sort.Slice(result.Matches, func(i, j int) bool { return result.Matches[i].CandidateID < result.Matches[j].CandidateID })
	for _, candidate := range filteredCandidates {
		item, isMatched := matched[candidate.ID]
		state := HistoricalState{ScoreTier: candidate.ScoreTier, ScoreTierKnown: candidate.ScoreTierKnown}
		if !state.ScoreTierKnown && options.ResolveScoreTier != nil {
			state.ScoreTier, state.ScoreTierKnown = options.ResolveScoreTier(candidate.CustomerID, candidate.DetectedAt)
		}
		label := LabelUnevaluable
		var provenanceSource map[string]string
		if isMatched {
			ref := referencesByID[item.ReferenceID]
			if !state.ScoreTierKnown && ref.State.ScoreTierKnown {
				// The candidate's own event-time score is authoritative. Do not
				// fall back to a current or reference score when it is missing.
				state.ScoreTierKnown = false
			}
			if state.ScoreTierKnown {
				label = LabelFromState(ref.State)
			}
			provenanceSource = cloneStringMap(ref.Provenance)
		} else if state.ScoreTierKnown {
			label = LabelUnlabeled
		}
		denominator := label == LabelTP || label == LabelFP
		if denominator {
			result.Denominator++
		}
		evaluation := Evaluation{CandidateID: candidate.ID, CustomerID: candidate.CustomerID, ScenarioID: candidate.ScenarioID, Label: label, Denominator: denominator, Provenance: Provenance{MatcherVersion: MatcherVersion, Mode: options.Mode, SnapshotAt: snapshot, Assumptions: append([]string(nil), assumptions...), Source: provenanceSource}, ScoreTier: state.ScoreTier}
		if isMatched {
			evaluation.ReferenceID, evaluation.Match = item.ReferenceID, &item
		}
		result.Evaluations = append(result.Evaluations, evaluation)
		if !isMatched {
			result.UnmatchedCandidates = append(result.UnmatchedCandidates, candidate.ID)
		}
	}
	for _, reference := range filteredReferences {
		if !usedReferences[reference.ID] {
			result.UnmatchedReferences = append(result.UnmatchedReferences, reference.ID)
		}
	}
	return result
}

type proposal struct {
	candidate Detection
	reference Reference
	metric    string
	score     float64
}

func compatible(candidate, reference Detection, mode Mode) bool {
	if candidate.CustomerID == "" || candidate.CustomerID != reference.CustomerID {
		return false
	}
	return mode != ModeBacktest || candidate.ScenarioID == reference.ScenarioID
}

func overlap(candidate, reference Detection) (string, float64, bool) {
	bestMetric, bestScore, found := "", 0.0, false
	if len(candidate.TransactionIDs) > 0 && len(reference.TransactionIDs) > 0 {
		if score := jaccard(candidate.TransactionIDs, reference.TransactionIDs); score >= 0 {
			bestMetric, bestScore, found = MetricTransactions, score, true
		}
	}
	if score, ok := intervalOverlap(candidate.WindowFrom, candidate.WindowTo, reference.WindowFrom, reference.WindowTo); ok {
		if !found || score > bestScore {
			bestMetric, bestScore, found = MetricInterval, score, true
		}
	}
	return bestMetric, bestScore, found
}

func jaccard(left, right []string) float64 {
	a, b := uniqueIDs(left), uniqueIDs(right)
	if len(a) == 0 || len(b) == 0 {
		return -1
	}
	intersection := 0
	for id := range a {
		if _, ok := b[id]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return -1
	}
	return float64(intersection) / float64(union)
}

func intervalOverlap(leftFrom, leftTo, rightFrom, rightTo *time.Time) (float64, bool) {
	if leftFrom == nil || leftTo == nil || rightFrom == nil || rightTo == nil || leftTo.Before(*leftFrom) || rightTo.Before(*rightFrom) {
		return 0, false
	}
	leftDuration, rightDuration := leftTo.Sub(*leftFrom), rightTo.Sub(*rightFrom)
	if leftDuration == 0 && rightDuration == 0 {
		if leftFrom.Equal(*rightFrom) {
			return 1, true
		}
		return 0, true
	}
	from, to := *leftFrom, *leftTo
	if rightFrom.After(from) {
		from = *rightFrom
	}
	if rightTo.Before(to) {
		to = *rightTo
	}
	if !to.After(from) {
		return 0, true
	}
	shorter := leftDuration
	if rightDuration < shorter {
		shorter = rightDuration
	}
	if shorter <= 0 {
		return 0, true
	}
	return float64(to.Sub(from)) / float64(shorter), true
}

func LabelFromState(state HistoricalState) Label {
	if !state.ScoreTierKnown || state.ScoreTier == "" {
		return LabelUnevaluable
	}
	status := state.AlertStatus
	if state.Decision != nil {
		status = state.Decision.ToStatus
	}
	switch status {
	case domain.AlertStatusClosedTruePositive:
		return LabelTP
	case domain.AlertStatusClosedFalsePositive:
		return LabelFP
	}
	if state.STRFiled || state.CaseStatus == domain.CaseStatusEscalated || state.CaseStatus == domain.CaseStatusStrFiled {
		return LabelTP
	}
	return LabelUnlabeled
}

// TierAt reconstructs the score tier that was effective at an event time. It
// intentionally does not fall back to a customer's current tier.
func TierAt(records []domain.ScoreRecord, at time.Time) (domain.RiskTier, bool) {
	var selected *domain.ScoreRecord
	for i := range records {
		record := &records[i]
		if record.ScoredAt.IsZero() || record.ScoredAt.After(at) || record.Tier == "" {
			continue
		}
		if selected == nil || record.ScoredAt.After(selected.ScoredAt) || (record.ScoredAt.Equal(selected.ScoredAt) && record.ID > selected.ID) {
			selected = record
		}
	}
	if selected == nil {
		return "", false
	}
	return selected.Tier, true
}

func filterCandidates(input []Detection, snapshot time.Time) []Detection {
	result := make([]Detection, 0, len(input))
	for _, item := range input {
		if !afterSnapshot(item.DetectedAt, snapshot) {
			item.TransactionIDs = uniqueSorted(item.TransactionIDs)
			result = append(result, item)
		}
	}
	return result
}

func filterReferences(input []Reference, snapshot time.Time) []Reference {
	result := make([]Reference, 0, len(input))
	for _, item := range input {
		if !afterSnapshot(item.DetectedAt, snapshot) {
			item.TransactionIDs = uniqueSorted(item.TransactionIDs)
			result = append(result, item)
		}
	}
	return result
}

func afterSnapshot(value, snapshot time.Time) bool {
	return !snapshot.IsZero() && !value.IsZero() && value.After(snapshot)
}

func uniqueIDs(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func uniqueSorted(values []string) []string {
	set := uniqueIDs(values)
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func timeDelta(left, right time.Time) int64 {
	if left.IsZero() || right.IsZero() {
		return math.MaxInt64
	}
	delta := left.Sub(right)
	if delta < 0 {
		delta = -delta
	}
	return delta.Milliseconds()
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
