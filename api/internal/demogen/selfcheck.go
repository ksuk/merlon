package demogen

import (
	"fmt"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// realNameGuard rejects any generated name that exactly collides (after
// whitespace/case normalization) with a hardcoded list of well-known real
// people or companies. It is deliberately not exhaustive (T1-W1
// instructions: "実在有名人物/企業の照合リストとの完全一致を拒否する程度でよい")
// — the name pools in names.go are themselves built from generic
// components, so a collision would require an unlucky combination on top of
// an already-narrow blocklist match.
type realNameGuard struct {
	blocked map[string]bool
}

func newRealNameGuard() *realNameGuard {
	names := []string{
		// Well-known real individuals (politicians, business leaders).
		"田中角栄", "安倍晋三", "岸田文雄", "孫正義", "豊田章男", "柳井正", "本田宗一郎", "盛田昭夫",
		"Elon Musk", "Warren Buffett", "Jeff Bezos", "Bill Gates", "Satya Nadella", "Tim Cook",
		"Mark Zuckerberg", "Masayoshi Son", "Shinzo Abe", "Akio Toyoda", "Tadashi Yanai",
		// Well-known real companies.
		"トヨタ自動車", "ソニーグループ", "ソフトバンク", "任天堂", "本田技研工業", "日本電信電話",
		"Toyota Motor Corporation", "Sony Group Corporation", "SoftBank Group Corp.", "Nintendo Co., Ltd.",
		"Apple Inc.", "Microsoft Corporation", "Amazon.com Inc.", "Alphabet Inc.", "Meta Platforms Inc.",
	}
	blocked := make(map[string]bool, len(names))
	for _, n := range names {
		blocked[normalizeName(n)] = true
	}
	return &realNameGuard{blocked: blocked}
}

func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

// collides reports whether any of the name-bearing attribute keys exactly
// match a blocklisted real name.
func (g *realNameGuard) collides(attrs map[string]any) bool {
	for _, key := range []string{"name", "name_en", "name_kana"} {
		v, ok := attrs[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		if g.blocked[normalizeName(s)] {
			return true
		}
	}
	return false
}

// tierFromScore independently re-derives the risk tier from a score using
// funds_transfer.yaml's thresholds (LOW<2.0, MEDIUM [2.0,3.5), HIGH>=3.5),
// so SelfCheck's (d) check does not simply trust whatever tier the pipeline
// already wrote down.
func tierFromScore(score float64) string {
	switch {
	case score < 2.0:
		return "low"
	case score < 3.5:
		return "medium"
	default:
		return "high"
	}
}

// SelfCheck runs every T1-W1 acceptance check against a generated
// population and returns a single aggregated error listing every violation
// (never just the first), so a failing generation run reports everything
// wrong in one pass.
func SelfCheck(customers []domain.Customer, anchor time.Time) error {
	var errs []string
	total := len(customers)
	if total == 0 {
		return fmt.Errorf("self-check: no customers generated")
	}

	// (d) tier distribution ±3pt, and tier must equal the value derived from
	// score via the funds_transfer.yaml thresholds for every customer.
	tierCounts := map[string]int{}
	for _, c := range customers {
		if c.RiskScore == nil || c.RiskTier == nil {
			errs = append(errs, fmt.Sprintf("customer %s missing risk_score/risk_tier", c.ID))
			continue
		}
		tierCounts[string(*c.RiskTier)]++
		if expected := tierFromScore(*c.RiskScore); expected != string(*c.RiskTier) {
			errs = append(errs, fmt.Sprintf("customer %s tier=%s does not match score=%.4f (thresholds derive %s)", c.ID, *c.RiskTier, *c.RiskScore, expected))
		}
	}
	checkPct(&errs, "tier=low", tierCounts["low"], total, 75)
	checkPct(&errs, "tier=medium", tierCounts["medium"], total, 20)
	checkPct(&errs, "tier=high", tierCounts["high"], total, 5)

	// customer_type distribution (A3): individual/corporate_domestic/
	// corporate_foreign are percentage targets; npo/trust/partnership are
	// small fixed counts, checked exactly.
	typeCounts := map[string]int{}
	for _, c := range customers {
		typeCounts[string(c.CustomerType)]++
	}
	checkPct(&errs, "customer_type=individual", typeCounts[string(domain.CustomerTypeIndividual)], total, 88)
	checkPct(&errs, "customer_type=corporate_domestic", typeCounts[string(domain.CustomerTypeCorporateDomestic)], total, 8.5)
	checkPct(&errs, "customer_type=corporate_foreign", typeCounts[string(domain.CustomerTypeCorporateForeign)], total, 2.5)
	checkExact(&errs, "customer_type=npo", typeCounts["npo"], 6)
	checkExact(&errs, "customer_type=trust", typeCounts["trust"], 2)
	checkExact(&errs, "customer_type=partnership", typeCounts["partnership"], 2)

	// status distribution (A3).
	statusCounts := map[string]int{}
	for _, c := range customers {
		statusCounts[string(c.EffectiveStatus())]++
	}
	checkPct(&errs, "status=active", statusCounts[string(domain.CustomerStatusActive)], total, 93)
	checkPct(&errs, "status=dormant", statusCounts[string(domain.CustomerStatusDormant)], total, 6)
	checkPct(&errs, "status=frozen", statusCounts[string(domain.CustomerStatusFrozen)], total, 0.5)
	checkPct(&errs, "status=closed", statusCounts[string(domain.CustomerStatusClosed)], total, 0.5)

	// Nationality distribution (A3): JP / コリドー24% / 他8%.
	corridorSet := make(map[string]bool, len(corridorCountries))
	for _, c := range corridorCountries {
		corridorSet[c] = true
	}
	var jp, corridor, other int
	for _, c := range customers {
		switch {
		case c.CountryCode == "JP":
			jp++
		case corridorSet[c.CountryCode]:
			corridor++
		default:
			other++
		}
	}
	checkPct(&errs, "country=JP", jp, total, 68)
	checkPct(&errs, "country=corridor(PH/VN/NP/ID/BR/CN)", corridor, total, 24)
	checkPct(&errs, "country=other", other, total, 8)

	// (e) real-name collision check over the final population.
	guard := newRealNameGuard()
	for _, c := range customers {
		if guard.collides(c.Attributes) {
			errs = append(errs, fmt.Sprintf("customer %s collides with the real-name blocklist", c.ID))
		}
	}

	// Dormant customers must have a last_activity_at strictly more than 180
	// days before anchor (T1-W1 instructions: dormancy-consistent flag/date).
	for _, c := range customers {
		if c.EffectiveStatus() != domain.CustomerStatusDormant {
			continue
		}
		raw, ok := c.Attributes["last_activity_at"].(string)
		if !ok || raw == "" {
			errs = append(errs, fmt.Sprintf("dormant customer %s is missing attributes.last_activity_at", c.ID))
			continue
		}
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("dormant customer %s attributes.last_activity_at %q is unparseable: %v", c.ID, raw, err))
			continue
		}
		if anchor.Sub(t) < 180*24*time.Hour {
			errs = append(errs, fmt.Sprintf("dormant customer %s attributes.last_activity_at=%s is not more than 180 days before anchor=%s", c.ID, raw, anchor.Format("2006-01-02")))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("self-check failed (%d issue(s)):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
	}
	return nil
}

func checkPct(errs *[]string, label string, count, total int, targetPct float64) {
	actual := float64(count) / float64(total) * 100
	if actual < targetPct-3 || actual > targetPct+3 {
		*errs = append(*errs, fmt.Sprintf("%s: %.2f%% (target %.1f%% ±3pt), count=%d/%d", label, actual, targetPct, count, total))
	}
}

func checkExact(errs *[]string, label string, count, target int) {
	if count != target {
		*errs = append(*errs, fmt.Sprintf("%s: count=%d (expected exactly %d)", label, count, target))
	}
}

// checkTolerance appends an error if count is outside want's ±tolerancePct
// band (A2's "推奨値±10%以内" acceptance criterion).
func checkTolerance(errs *[]string, label string, count, want int, tolerancePct float64) {
	lo := float64(want) * (1 - tolerancePct/100)
	hi := float64(want) * (1 + tolerancePct/100)
	if float64(count) < lo || float64(count) > hi {
		*errs = append(*errs, fmt.Sprintf("%s: count=%d (want ~%d ±%.0f%%)", label, count, want, tolerancePct))
	}
}

// SelfCheckW2 runs T1-W2's acceptance checks: self-check (a) (engine-fire
// verification) is already enforced as a hard error inside Generate itself
// (a design mistake there should fail generation, not just be reported by a
// separate check), so this covers (b) alert rate, (c) alert-key uniqueness
// (a redundant safety net over alertBuildContext's own dedup), (f)
// screening-list synthetic names, and A2's ±10% count tolerances.
func SelfCheckW2(r *Result) error {
	var errs []string

	if len(r.Transactions) > 0 {
		rate := float64(len(r.Alerts)) / float64(len(r.Transactions))
		if rate >= 0.01 {
			errs = append(errs, fmt.Sprintf("alert rate %.4f%% (%d alerts / %d transactions) is not below 1%%", rate*100, len(r.Alerts), len(r.Transactions)))
		}
	}

	seen := map[string]bool{}
	for _, a := range r.Alerts {
		window := ""
		if a.AggregationWindowStart != nil {
			window = a.AggregationWindowStart.Format(time.RFC3339)
		}
		key := a.CustomerID + "|" + a.ScenarioID + "|" + window
		if seen[key] {
			errs = append(errs, fmt.Sprintf("duplicate (customer_id, scenario_id, aggregation_window_start): %s", key))
		}
		seen[key] = true
	}

	guard := newRealNameGuard()
	for _, l := range r.ScreeningLists {
		for _, e := range l.Entries {
			for _, name := range e.Names {
				if guard.collides(map[string]any{"name": name}) {
					errs = append(errs, fmt.Sprintf("screening list %s entry %s name %q collides with the real-name blocklist", l.ListID, e.EntryID, name))
				}
			}
		}
	}

	checkTolerance(&errs, "transactions", len(r.Transactions), 48000, 10)
	checkTolerance(&errs, "alerts", len(r.Alerts), 95, 10)
	checkExact(&errs, "cases", len(r.Cases), 24)
	if len(r.AuditLogs) < 200 {
		errs = append(errs, fmt.Sprintf("audit_logs: count=%d (want >= 200, A9 \"200件強\")", len(r.AuditLogs)))
	}

	if len(errs) > 0 {
		return fmt.Errorf("self-check (W2) failed (%d issue(s)):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
	}
	return nil
}
