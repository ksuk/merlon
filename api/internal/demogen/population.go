package demogen

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine/native"
)

// countBucket tracks remaining quota for a fixed, ordered set of keys. Keys
// are drawn with probability proportional to their remaining count
// (sampling without replacement in expectation), and a draw is only
// committed (decremented) once the caller has confirmed the candidate it
// produced was actually usable — see generatePopulation's rejection loop.
type countBucket struct {
	keys   []string
	remain map[string]int
}

func newCountBucket(keys []string, counts map[string]int) *countBucket {
	remain := make(map[string]int, len(keys))
	for _, k := range keys {
		remain[k] = counts[k]
	}
	return &countBucket{keys: keys, remain: remain}
}

func (b *countBucket) total() int {
	t := 0
	for _, k := range b.keys {
		t += b.remain[k]
	}
	return t
}

func (b *countBucket) peek(rng *rand.Rand) string {
	total := b.total()
	r := rng.Intn(total)
	cum := 0
	for _, k := range b.keys {
		cum += b.remain[k]
		if r < cum {
			return k
		}
	}
	return b.keys[len(b.keys)-1]
}

func (b *countBucket) commit(k string) { b.remain[k]-- }

// peekFrom is like peek but restricted to a subset of keys, weighted by
// their remaining counts within that subset. It reports false if every key
// in allowed is currently exhausted.
func (b *countBucket) peekFrom(rng *rand.Rand, allowed []string) (string, bool) {
	total := 0
	for _, k := range allowed {
		total += b.remain[k]
	}
	if total == 0 {
		return "", false
	}
	r := rng.Intn(total)
	cum := 0
	for _, k := range allowed {
		cum += b.remain[k]
		if r < cum {
			return k, true
		}
	}
	return allowed[len(allowed)-1], true
}

// A3 customer_type / status / country quotas for the 1000-customer
// population, before subtracting the 6 fixed story customers (D-a/A3).
func typeQuota() (keys []string, counts map[string]int) {
	keys = []string{
		string(domain.CustomerTypeIndividual),
		string(domain.CustomerTypeCorporateDomestic),
		string(domain.CustomerTypeCorporateForeign),
		"npo", "trust", "partnership",
	}
	counts = map[string]int{
		string(domain.CustomerTypeIndividual):        880,
		string(domain.CustomerTypeCorporateDomestic): 85,
		string(domain.CustomerTypeCorporateForeign):  25,
		"npo":         6,
		"trust":       2,
		"partnership": 2,
	}
	return
}

func statusQuota() (keys []string, counts map[string]int) {
	keys = []string{
		string(domain.CustomerStatusActive),
		string(domain.CustomerStatusDormant),
		string(domain.CustomerStatusFrozen),
		string(domain.CustomerStatusClosed),
	}
	counts = map[string]int{
		string(domain.CustomerStatusActive):  930,
		string(domain.CustomerStatusDormant): 60,
		string(domain.CustomerStatusFrozen):  5,
		string(domain.CustomerStatusClosed):  5,
	}
	return
}

func countryQuota() (keys []string, counts map[string]int) {
	keys = []string{"JP", "PH", "VN", "NP", "ID", "BR", "CN", "US", "GB", "SG", "KR", "AU", "TH", "AE", "MM"}
	counts = map[string]int{
		"JP": 680,
		"PH": 80, "VN": 70, "NP": 30, "ID": 25, "BR": 20, "CN": 15,
		"US": 11, "GB": 11, "SG": 11, "KR": 11, "AU": 11, "TH": 11, "AE": 11, "MM": 3,
	}
	return
}

func tierQuota() (keys []string, counts map[string]int) {
	keys = []string{"low", "medium", "high"}
	counts = map[string]int{"low": 750, "medium": 200, "high": 50}
	return
}

// nonJPKeys filters "JP" out of a country key list, for corporate_foreign
// candidates (see the type/country pairing comment on generatePopulation).
func nonJPKeys(keys []string) []string {
	out := make([]string, 0, len(keys)-1)
	for _, k := range keys {
		if k != "JP" {
			out = append(out, k)
		}
	}
	return out
}

// jpDomiciledTypes are customer_type values that, by definition, can only be
// registered/resident in Japan: a "corporate_domestic" company is a Japanese
// corporation, and npo/trust/partnership entities in this dataset are all
// Japanese-law entities too (T1-W1's population has no foreign equivalents
// of those three). Pairing any of these with a non-JP country_code would be
// self-contradictory — and, discovered while tuning the redesigned CDD
// factors, some such pairings (e.g. corporate_domestic + MM) are also CDD-
// unreachable at low tier, which stalled rejection sampling near the end of
// a run once few quota slots remained. Constraining the pairing fixes both
// problems at once.
var jpDomiciledTypes = map[string]bool{
	string(domain.CustomerTypeCorporateDomestic): true,
	"npo":         true,
	"trust":       true,
	"partnership": true,
}

// subtractStoryConsumption removes the story customers' fixed attributes
// from a quota's counts so the rejection-sampling pool for the remaining
// (generated) customers sums to exactly Options.Customers-len(story).
func subtractStoryConsumption(counts map[string]int, story []domain.Customer, extract func(domain.Customer) string) error {
	for _, c := range story {
		k := extract(c)
		if counts[k] <= 0 {
			return fmt.Errorf("story customer consumes more %q quota than available", k)
		}
		counts[k]--
	}
	return nil
}

func subtractStoryTier(counts map[string]int, story []domain.Customer) error {
	for _, c := range story {
		if c.RiskTier == nil {
			return fmt.Errorf("story customer %s has no tier set", c.ID)
		}
		k := string(*c.RiskTier)
		if counts[k] <= 0 {
			return fmt.Errorf("story customer consumes more tier %q quota than available", k)
		}
		counts[k]--
	}
	return nil
}

// generatePopulation fills `needed` additional customers via tier-targeted
// rejection sampling (D-a/A4/T1-W1 instructions): propose a candidate's
// attributes, score it through the real native engine, and only keep it if
// its actual tier matches the tier bucket that still needs filling. Rejected
// candidates are discarded without consuming any quota, so quotas land on
// target by construction rather than by tolerance.
//
// eng is the single native.Engine instance constructed by Generate; it is
// reused for every probe score in the rejection loop (the content root is
// parsed exactly once per generation run).
func generatePopulation(eng *native.Engine, rng *rand.Rand, anchor time.Time, needed int, story []domain.Customer, blocked *realNameGuard) ([]domain.Customer, error) {
	typeKeys, typeCounts := typeQuota()
	statusKeys, statusCounts := statusQuota()
	countryKeys, countryCounts := countryQuota()
	tierKeys, tierCounts := tierQuota()

	if err := subtractStoryConsumption(typeCounts, story, func(c domain.Customer) string { return string(c.CustomerType) }); err != nil {
		return nil, err
	}
	if err := subtractStoryConsumption(statusCounts, story, func(c domain.Customer) string { return string(c.EffectiveStatus()) }); err != nil {
		return nil, err
	}
	if err := subtractStoryConsumption(countryCounts, story, func(c domain.Customer) string { return c.CountryCode }); err != nil {
		return nil, err
	}
	if err := subtractStoryTier(tierCounts, story); err != nil {
		return nil, err
	}

	typeBucket := newCountBucket(typeKeys, typeCounts)
	statusBucket := newCountBucket(statusKeys, statusCounts)
	countryBucket := newCountBucket(countryKeys, countryCounts)
	tierBucket := newCountBucket(tierKeys, tierCounts)
	nonJP := nonJPKeys(countryKeys)

	ctx := context.Background()
	out := make([]domain.Customer, 0, needed)
	seq := 0
	attempts := 0
	for len(out) < needed {
		attempts++
		if attempts > maxRejectionAttempts {
			return nil, fmt.Errorf("rejection sampling did not converge after %d attempts (accepted %d/%d); tier/quota design may be unreachable", attempts, len(out), needed)
		}

		ctype := typeBucket.peek(rng)
		status := statusBucket.peek(rng)
		targetTier := tierBucket.peek(rng)

		// country_code must agree with customer_type (see jpDomiciledTypes):
		// corporate_domestic/npo/trust/partnership are always JP,
		// corporate_foreign is always non-JP, individual draws from the
		// full pool. If the required subset is currently exhausted, this
		// attempt is infeasible; retry with fresh draws.
		var country string
		switch {
		case jpDomiciledTypes[ctype]:
			c, ok := countryBucket.peekFrom(rng, []string{"JP"})
			if !ok {
				continue
			}
			country = c
		case ctype == string(domain.CustomerTypeCorporateForeign):
			c, ok := countryBucket.peekFrom(rng, nonJP)
			if !ok {
				continue
			}
			country = c
		default:
			country = countryBucket.peek(rng)
		}

		candidate := buildCandidate(rng, anchor, seq, ctype, status, country, targetTier)
		if blocked.collides(candidate.Attributes) {
			continue
		}

		scored, err := eng.ScoreCustomer(ctx, &candidate, FundsTransferPresetID)
		if err != nil {
			return nil, fmt.Errorf("probe-score candidate: %w", err)
		}
		if string(scored.Tier) != targetTier {
			continue
		}

		typeBucket.commit(ctype)
		statusBucket.commit(status)
		countryBucket.commit(country)
		tierBucket.commit(targetTier)

		score := scored.Score
		tier := scored.Tier
		candidate.RiskScore = &score
		candidate.RiskTier = &tier

		seq++
		out = append(out, candidate)
	}
	return out, nil
}
