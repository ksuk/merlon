package demogen

import (
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// nameSimilarity mirrors api/internal/engine/native's ScreenCustomer
// similarity metric (normalized Levenshtein distance) so demogen can
// construct plausible A8 screening_results (name, similarity) pairs. It is
// NOT used for any runtime screening decision — it exists purely as a demo-
// data authoring aid, since the engine's own ScreenCustomer only returns
// matches at or above its configured threshold (0.85 by default) and A8
// explicitly calls for some recorded hits below that (representing
// analyst/manual review entries, or a threshold that has since changed),
// which the live API has no way to produce on demand.
func nameSimilarity(a, b string) float64 {
	a, b = normalizeForSimilarity(a), normalizeForSimilarity(b)
	if a == b {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 || len(br) == 0 {
		if len(ar) == 0 && len(br) == 0 {
			return 1
		}
		return 0
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ar {
		cur := make([]int, len(br)+1)
		cur[0] = i + 1
		for j, cb := range br {
			cost := 0
			if ca != cb {
				cost = 1
			}
			cur[j+1] = minInt(minInt(prev[j+1]+1, cur[j]+1), prev[j]+cost)
		}
		prev = cur
	}
	max := len(ar)
	if len(br) > max {
		max = len(br)
	}
	return 1 - float64(prev[len(br)])/float64(max)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeForSimilarity(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x21 && r <= 0x7e && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) && r != '-' {
			continue
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// screeningEntrySeed / screeningListSeed mirror the on-disk screening list
// YAML schema (content/_sample/screening_lists/*.yaml): list_id/list_type/
// name/source/entries[entry_id/names/country/type].
type screeningEntrySeed struct {
	EntryID string   `yaml:"entry_id"`
	Names   []string `yaml:"names"`
	Country string   `yaml:"country"`
	Type    string   `yaml:"type"`
}
type screeningListSeed struct {
	SchemaVersion string               `yaml:"schema_version"`
	ListID        string               `yaml:"list_id"`
	ListType      string               `yaml:"list_type"`
	Name          string               `yaml:"name"`
	Source        string               `yaml:"source"`
	Entries       []screeningEntrySeed `yaml:"entries"`
}

// buildScreeningLists is the D-c committed (small, auditable) synthetic
// screening content (A8): a 3-entry sanctions list and a 1-entry PEP list,
// entirely DEMO-prefixed and synthetic (DD3) — this replaces the hand-
// written deploy/seed/demo/screening_lists/demo-synthetic.yaml.
func buildScreeningLists() []screeningListSeed {
	return []screeningListSeed{
		{
			SchemaVersion: "1.0", ListID: "demo_sanctions", ListType: "sanctions",
			Name: "Merlon Demo Synthetic Sanctions List", Source: "synthetic-only",
			Entries: []screeningEntrySeed{
				{EntryID: "DEMO-SANCTIONS-001", Names: []string{"Demo Subject Alpha"}, Country: "ZZ", Type: "individual"},
				{EntryID: "DEMO-SANCTIONS-002", Names: []string{"Demo Mining Development Corporation"}, Country: "ZZ", Type: "entity"},
				{EntryID: "DEMO-SANCTIONS-003", Names: []string{"Demo Asia Logistics Group"}, Country: "ZZ", Type: "entity"},
			},
		},
		{
			SchemaVersion: "1.0", ListID: "demo_pep", ListType: "pep",
			Name: "Merlon Demo Synthetic PEP List", Source: "synthetic-only",
			Entries: []screeningEntrySeed{
				{EntryID: "DEMO-PEP-001", Names: []string{"Demo Person Beta"}, Country: "ZZ", Type: "individual"},
			},
		},
	}
}

// screeningCustomerSeed is one A8 screening-narrative customer: added to
// the population on top of (not counted against) the 1000-customer quota,
// so their names can be tuned freely against buildScreeningLists' entries
// without perturbing T1-W1's distribution self-checks.
type screeningCustomerSeed struct {
	ID           string
	CustomerType domain.CustomerType
	Country      string
	Name         string
	DOBOffsetYrs int // for individuals: age in years, for a name+DOB screening mismatch story
}

// buildScreeningCustomers returns A8's 5 primary hits + ~10 low-score-FP
// customers. Every name here is fully synthetic (the "Demo " prefix is a
// deliberate, visible synthetic marker per DD3) and is additionally passed
// through realNameGuard by Generate.
func buildScreeningCustomers() []screeningCustomerSeed {
	return []screeningCustomerSeed{
		// A8 primary 5.
		{ID: "demo-screening-01", CustomerType: domain.CustomerTypeIndividual, Country: "US", Name: "Demo Subject Alpha", DOBOffsetYrs: 45},
		{ID: "demo-screening-02", CustomerType: domain.CustomerTypeIndividual, Country: "US", Name: "Demo Subject Alpha Jr.", DOBOffsetYrs: 46},
		{ID: "demo-screening-03", CustomerType: domain.CustomerTypeCorporateForeign, Country: "SG", Name: "Demo Mining Development Corp."},
		{ID: "demo-screening-04", CustomerType: domain.CustomerTypeCorporateForeign, Country: "HK", Name: "Demo Asia Logistics Partners"},
		{ID: "demo-screening-05", CustomerType: domain.CustomerTypeIndividual, Country: "JP", Name: "Demo Person Beta", DOBOffsetYrs: 38},
		// ~10 additional low-score (generic-collision) false positives.
		{ID: "demo-screening-06", CustomerType: domain.CustomerTypeIndividual, Country: "JP", Name: "Demo Person Bravo", DOBOffsetYrs: 29},
		{ID: "demo-screening-07", CustomerType: domain.CustomerTypeIndividual, Country: "US", Name: "Demo Subject Gamma", DOBOffsetYrs: 51},
		{ID: "demo-screening-08", CustomerType: domain.CustomerTypeCorporateForeign, Country: "SG", Name: "Demo Mining Ventures Ltd."},
		{ID: "demo-screening-09", CustomerType: domain.CustomerTypeCorporateForeign, Country: "HK", Name: "Demo Asia Trading Co."},
		{ID: "demo-screening-10", CustomerType: domain.CustomerTypeIndividual, Country: "GB", Name: "Demo Subject Delta", DOBOffsetYrs: 60},
		{ID: "demo-screening-11", CustomerType: domain.CustomerTypeIndividual, Country: "JP", Name: "Demo Person Charlie", DOBOffsetYrs: 33},
		{ID: "demo-screening-12", CustomerType: domain.CustomerTypeCorporateDomestic, Country: "JP", Name: "Demo Kogyo Kaihatsu Co., Ltd."},
		{ID: "demo-screening-13", CustomerType: domain.CustomerTypeIndividual, Country: "AU", Name: "Demo Subject Epsilon", DOBOffsetYrs: 41},
		{ID: "demo-screening-14", CustomerType: domain.CustomerTypeCorporateForeign, Country: "TH", Name: "Demo Asia Freight Services"},
		{ID: "demo-screening-15", CustomerType: domain.CustomerTypeIndividual, Country: "JP", Name: "Demo Person Delta", DOBOffsetYrs: 55},
	}
}

// buildScreeningCustomerRecords materializes the screeningCustomerSeed list
// into domain.Customer values (scored through the same engine as everyone
// else — see Generate — so their CDD score/tier is real, even though A8's
// narrative purpose for them is screening, not CDD).
func buildScreeningCustomerRecords(anchor time.Time, seeds []screeningCustomerSeed) []domain.Customer {
	out := make([]domain.Customer, 0, len(seeds))
	for _, s := range seeds {
		opened := anchor.AddDate(-2, 0, 0)
		productChannel := "corporate_trade_settlement"
		attrs := map[string]any{
			"name":              s.Name,
			"name_en":           s.Name,
			"address_pref":      "東京都",
			"channel":           "web",
			"account_opened_at": opened.Format("2006-01-02"),
			"last_activity_at":  anchor.AddDate(0, 0, -30).Format("2006-01-02"),
			"account_age_band":  accountAgeBandFromOpened(anchor, opened),
			"expected_activity": "normal",
		}
		if s.CustomerType == domain.CustomerTypeIndividual {
			productChannel = "domestic_wallet"
			attrs["date_of_birth"] = anchor.AddDate(-s.DOBOffsetYrs, -3, -10).Format("2006-01-02")
			attrs["occupation"] = "会社員"
			attrs["occupation_band"] = occupationBand("会社員")
			attrs["industry"] = "IT・情報通信業"
			attrs["purpose"] = "生活費送金"
		} else {
			attrs["industry"] = "貿易業"
			attrs["purpose"] = "貿易代金決済"
		}
		out = append(out, domain.Customer{
			ID:           s.ID,
			CustomerType: s.CustomerType,
			CountryCode:  s.Country,
			ProductTypes: []string{productChannel},
			Status:       domain.CustomerStatusActive,
			CreatedAt:    opened,
			UpdatedAt:    anchor,
			Attributes:   attrs,
		})
	}
	return out
}

// buildScreeningResults constructs A8's 5 primary hits + ~10 low-score FPs,
// computing similarity via nameSimilarity against buildScreeningLists'
// entries (never hardcoded numbers).
func buildScreeningResults(anchor time.Time, ids *idSeq) []domain.ScreeningResultRecord {
	entryName := map[string]string{
		"DEMO-SANCTIONS-001": "Demo Subject Alpha",
		"DEMO-SANCTIONS-002": "Demo Mining Development Corporation",
		"DEMO-SANCTIONS-003": "Demo Asia Logistics Group",
		"DEMO-PEP-001":       "Demo Person Beta",
	}
	type hit struct {
		CustomerID          string
		CustomerName        string
		ListID              string
		ListType            string
		EntryID             string
		Status              domain.ScreeningResultStatus
		FalsePositiveReason string
		ReviewedAfterHours  int // 0 = not yet reviewed
	}
	hits := []hit{
		{"demo-screening-01", "Demo Subject Alpha", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-001", domain.ScreeningResultStatusReviewing, "", 0},
		{"demo-screening-02", "Demo Subject Alpha Jr.", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: different individual, name variant only.", 24},
		{"demo-screening-03", "Demo Mining Development Corp.", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-002", domain.ScreeningResultStatusNew, "", 0},
		{"demo-screening-04", "Demo Asia Logistics Partners", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-003", domain.ScreeningResultStatusFalsePositive, "Reviewed: unrelated logistics company, coincidental name overlap.", 30},
		{"demo-screening-05", "Demo Person Beta", "demo_pep", "pep", "DEMO-PEP-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: date of birth does not match PEP record (same name, different person).", 20},
		// Low-score FPs.
		{"demo-screening-06", "Demo Person Bravo", "demo_pep", "pep", "DEMO-PEP-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: generic name overlap only.", 12},
		{"demo-screening-07", "Demo Subject Gamma", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: generic name overlap only.", 10},
		{"demo-screening-08", "Demo Mining Ventures Ltd.", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-002", domain.ScreeningResultStatusFalsePositive, "Reviewed: unrelated mining company.", 14},
		{"demo-screening-09", "Demo Asia Trading Co.", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-003", domain.ScreeningResultStatusFalsePositive, "Reviewed: unrelated trading company.", 16},
		{"demo-screening-10", "Demo Subject Delta", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: generic name overlap only.", 8},
		{"demo-screening-11", "Demo Person Charlie", "demo_pep", "pep", "DEMO-PEP-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: generic name overlap only.", 9},
		{"demo-screening-12", "Demo Kogyo Kaihatsu Co., Ltd.", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-002", domain.ScreeningResultStatusFalsePositive, "Reviewed: unrelated Japanese company, transliteration overlap only.", 18},
		{"demo-screening-13", "Demo Subject Epsilon", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: generic name overlap only.", 11},
		{"demo-screening-14", "Demo Asia Freight Services", "demo_sanctions", "sanctions", "DEMO-SANCTIONS-003", domain.ScreeningResultStatusFalsePositive, "Reviewed: unrelated freight company.", 13},
		{"demo-screening-15", "Demo Person Delta", "demo_pep", "pep", "DEMO-PEP-001", domain.ScreeningResultStatusFalsePositive, "Reviewed: generic name overlap only.", 15},
	}

	out := make([]domain.ScreeningResultRecord, 0, len(hits))
	for i, h := range hits {
		screenedAt := anchor.AddDate(0, 0, -(60 - i))
		r := domain.ScreeningResultRecord{
			ID:          ids.next("demo-screening-result-%03d"),
			CustomerID:  h.CustomerID,
			ListID:      h.ListID,
			ListType:    h.ListType,
			EntryID:     h.EntryID,
			MatchedName: entryName[h.EntryID],
			Similarity:  nameSimilarity(h.CustomerName, entryName[h.EntryID]),
			Status:      h.Status,
			CreatedAt:   screenedAt,
			ScreenedAt:  screenedAt,
		}
		if h.FalsePositiveReason != "" {
			r.FalsePositiveReason = h.FalsePositiveReason
		}
		if h.ReviewedAfterHours > 0 {
			reviewedAt := screenedAt.Add(time.Duration(h.ReviewedAfterHours) * time.Hour)
			r.ReviewedAt = &reviewedAt
			r.ReviewedBy = analysts[i%len(analysts)]
		}
		out = append(out, r)
	}
	return out
}
