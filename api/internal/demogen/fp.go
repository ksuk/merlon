package demogen

import (
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// fpIncident is one background False Positive block (A7): plausible,
// innocent-explanation transactions that nonetheless cross a scenario's
// threshold. narrative is recorded only for STORY_IDS.md-adjacent reporting
// (case_notes/description text), not asserted against the customer's own
// generated attributes.
type fpIncident struct {
	CustomerID       string
	Category         string // A7 category label
	Narrative        string
	ExpectedScenario string
	Transactions     []domain.Transaction
}

// selectCustomers walks customers in order (already deterministic) and
// returns the first `count` whose predicate matches and whose ID is not in
// used, marking each selected ID in used as it goes so categories never
// double-book the same customer.
func selectCustomers(customers []domain.Customer, used map[string]bool, count int, predicate func(domain.Customer) bool) []domain.Customer {
	var out []domain.Customer
	for _, c := range customers {
		if len(out) >= count {
			break
		}
		if used[c.ID] || !predicate(c) {
			continue
		}
		used[c.ID] = true
		out = append(out, c)
	}
	return out
}

// selectCustomerIndices is selectCustomers but returns indices into
// customers, for callers (buildFPIncidents' dormant category) that need to
// mutate the selected customer in place rather than just read it.
func selectCustomerIndices(customers []domain.Customer, used map[string]bool, count int, predicate func(domain.Customer) bool) []int {
	var out []int
	for i, c := range customers {
		if len(out) >= count {
			break
		}
		if used[c.ID] || !predicate(c) {
			continue
		}
		used[c.ID] = true
		out = append(out, i)
	}
	return out
}

func isIndividual(c domain.Customer) bool {
	return c.CustomerType == domain.CustomerTypeIndividual
}
func isCorporate(c domain.Customer) bool {
	return c.CustomerType == domain.CustomerTypeCorporateDomestic || c.CustomerType == domain.CustomerTypeCorporateForeign
}
func isDormant(c domain.Customer) bool {
	return c.EffectiveStatus() == domain.CustomerStatusDormant
}

// accountOldEnough reports whether c's account_opened_at is at least
// minDays before anchor — used to filter dormant-FP candidates down to
// those whose account can actually support a 180+ day silent gap followed
// by a reactivation, both strictly after account_opened_at.
func accountOldEnough(c domain.Customer, anchor time.Time, minDays int) bool {
	opened := parseAttrDate(c.Attributes, "account_opened_at", anchor.AddDate(-10, 0, 0))
	return anchor.Sub(opened) >= time.Duration(minDays)*24*time.Hour
}

// clampAfterOpened pushes proposed forward to at least 5 days after c's own
// attributes.account_opened_at, so an FP incident's fixed day-offset
// schedule can never land before an individually-selected customer's
// account existed (T1-W1 fixed each customer's account_opened_at/
// last_activity_at independently of any T1-W2 incident scheduling).
func clampAfterOpened(c domain.Customer, anchor, proposed time.Time) time.Time {
	opened := parseAttrDate(c.Attributes, "account_opened_at", anchor.AddDate(-10, 0, 0))
	minDay := opened.AddDate(0, 0, 5)
	if proposed.Before(minDay) {
		return minDay
	}
	return proposed
}

func tierString(c domain.Customer) string {
	if c.RiskTier != nil {
		return string(*c.RiskTier)
	}
	return "medium"
}

// buildStructuringFPBlock: "給料日まとめチャージ誤検知" — several legitimate
// same-day deposits (salary + bonus + reimbursement style) that happen to
// cross the structuring threshold. Every leg stays below individual_below so
// the shape matches the scenario's own candidate filter.
func buildStructuringFPBlock(cfg scenarioConfig, c domain.Customer, day time.Time, ids *idSeq) []domain.Transaction {
	below := cfg.additionalFloat("individual_below", 500000)
	threshold := cfg.threshold(tierString(c), 1000000)
	target := threshold * 1.15
	perTxnCap := below * 0.85
	count := int(target/perTxnCap) + 1
	if count < 3 {
		count = 3
	}
	var txns []domain.Transaction
	remaining := target
	for i := 0; i < count; i++ {
		left := count - i
		amt := remaining / float64(left)
		if amt > perTxnCap {
			amt = perTxnCap
		}
		remaining -= amt
		txns = append(txns, txn(ids.next("demo-txn-%07d"), c.ID, amt, domain.DirectionInbound, "JP", "app", day.Add(time.Duration(i*3)*time.Hour)))
	}
	return txns
}

// buildHighFrequencyFPBlock: "飲食店小口売上" — many small legitimate POS/
// register sales deposits within an hour, each within max_amount_per_txn.
// count_threshold is itself a per-tier value (like the amount thresholds),
// resolved via cfg.threshold rather than the flat conditions.additional
// fallback: native.go's thresholdParameter() list includes "count_threshold",
// so evalHFSA resolves it from conditions.threshold.by_customer_type.*.
// by_risk_tier first (LOW:15/MEDIUM:10/HIGH:5), the same table amount-based
// scenarios use — reading additionalInt("count_threshold", ...) here would
// silently use the flat fallback for LOW/MEDIUM/HIGH tiers where the real
// per-tier value differs.
func buildHighFrequencyFPBlock(cfg scenarioConfig, c domain.Customer, day time.Time, ids *idSeq) []domain.Transaction {
	max := cfg.additionalFloat("max_amount_per_txn", 100000)
	count := int(cfg.threshold(tierString(c), 10)) + 2 // comfortably over the tier's count threshold
	windowHours := cfg.WindowHours
	if windowHours <= 0 {
		windowHours = 1
	}
	// Spread all `count` transactions within a span comfortably inside the
	// scenario's window (50 minutes per hour of window), so a higher count
	// (LOW tier needs threshold 15+2=17) never overflows the window the way
	// a fixed per-transaction spacing would.
	spanMinutes := float64(windowHours) * 50
	var txns []domain.Transaction
	for i := 0; i < count; i++ {
		amt := max * (0.2 + 0.05*float64(i%6))
		offset := time.Duration(spanMinutes * float64(i) / float64(count) * float64(time.Minute))
		txns = append(txns, txn(ids.next("demo-txn-%07d"), c.ID, amt, domain.DirectionInbound, "JP", "atm", day.Add(offset)))
	}
	return txns
}

// buildHighRiskCountryFPBlock: "MM宛正当貿易" — a single legitimate-looking
// outbound trade settlement to MM, just over the threshold.
func buildHighRiskCountryFPBlock(cfg scenarioConfig, c domain.Customer, day time.Time, ids *idSeq) []domain.Transaction {
	threshold := cfg.threshold(tierString(c), 1000000)
	amount := threshold * 1.2
	return []domain.Transaction{
		txn(ids.next("demo-txn-%07d"), c.ID, amount, domain.DirectionOutbound, "MM", "web", day.Add(10*time.Hour)),
	}
}

// buildRapidMovementFPBlock: "法人月末資金繰り" — an ordinary month-end cash
// sweep: inbound receivables followed by outbound payables within 48h, at a
// ratio just over the scenario's minimum.
func buildRapidMovementFPBlock(cfg scenarioConfig, c domain.Customer, day time.Time, ids *idSeq) []domain.Transaction {
	threshold := cfg.threshold(tierString(c), 5000000)
	ratioMin := cfg.additionalFloat("outbound_ratio_min", 0.8)
	in := threshold * 1.2
	out := in * (ratioMin + 0.05)
	return []domain.Transaction{
		txn(ids.next("demo-txn-%07d"), c.ID, in, domain.DirectionInbound, "JP", "web", day),
		txn(ids.next("demo-txn-%07d"), c.ID, out, domain.DirectionOutbound, "JP", "web", day.Add(20*time.Hour)),
	}
}

// buildDormantFPBlock: "帰国後の技能実習生再開" — a single reactivation
// transaction after a 180+ day gap, just over the reactivation_threshold.
// See story_txns.go's doc comment: evalDormant checks this one transaction's
// amount, not a 24h sum, so a single leg is both faithful to the actual
// engine behavior and to the "one lump-sum remittance home" narrative.
func buildDormantFPBlock(cfg scenarioConfig, c domain.Customer, gapDay, reactivationDay time.Time, ids *idSeq) []domain.Transaction {
	threshold := cfg.threshold(tierString(c), 1000000)
	amount := threshold * 1.1
	return []domain.Transaction{
		txn(ids.next("demo-txn-%07d"), c.ID, 15000, domain.DirectionOutbound, "JP", "atm", gapDay),
		txn(ids.next("demo-txn-%07d"), c.ID, amount, domain.DirectionInbound, c.CountryCode, "agent", reactivationDay),
	}
}

// buildFPIncidents selects background customers (excluding story customers
// and each other) and builds A7's ~86 false-positive incidents across its 5
// categories, using thresholds read from the real scenario YAMLs (cfgs) —
// never hardcoded — so each block is constructed to plausibly cross the
// threshold that self-check (a) then verifies against the live engine.
func buildFPIncidents(anchor time.Time, customers []domain.Customer, storyIDs []string, cfgs map[string]scenarioConfig, ids *idSeq) []fpIncident {
	used := map[string]bool{}
	for _, id := range storyIDs {
		used[id] = true
	}
	day := func(n int) time.Time { return anchor.AddDate(0, 0, -n) }

	var out []fpIncident

	structuringCustomers := selectCustomers(customers, used, 30, isIndividual)
	for i, c := range structuringCustomers {
		d := clampAfterOpened(c, anchor, day(150+i*3))
		out = append(out, fpIncident{
			CustomerID: c.ID, Category: "structuring", Narrative: "給料日まとめチャージ誤検知",
			ExpectedScenario: "tm_structuring_basic",
			Transactions:     buildStructuringFPBlock(cfgs["tm_structuring_basic"], c, d, ids),
		})
	}

	hfsaCustomers := selectCustomers(customers, used, 14, func(c domain.Customer) bool { return isIndividual(c) || isCorporate(c) })
	for i, c := range hfsaCustomers {
		d := clampAfterOpened(c, anchor, day(150+i*4))
		out = append(out, fpIncident{
			CustomerID: c.ID, Category: "high_frequency_small_amount", Narrative: "飲食店小口売上",
			ExpectedScenario: "tm_high_frequency_small_amount",
			Transactions:     buildHighFrequencyFPBlock(cfgs["tm_high_frequency_small_amount"], c, d, ids),
		})
	}

	highRiskCustomers := selectCustomers(customers, used, 18, isCorporate)
	for i, c := range highRiskCustomers {
		d := clampAfterOpened(c, anchor, day(150+i*5))
		out = append(out, fpIncident{
			CustomerID: c.ID, Category: "high_risk_country_transfer", Narrative: "MM宛正当貿易",
			ExpectedScenario: "tm_high_risk_country_transfer",
			Transactions:     buildHighRiskCountryFPBlock(cfgs["tm_high_risk_country_transfer"], c, d, ids),
		})
	}

	rapidCustomers := selectCustomers(customers, used, 12, isCorporate)
	for i, c := range rapidCustomers {
		d := clampAfterOpened(c, anchor, day(150+i*6))
		out = append(out, fpIncident{
			CustomerID: c.ID, Category: "rapid_movement", Narrative: "法人月末資金繰り",
			ExpectedScenario: "tm_rapid_movement",
			Transactions:     buildRapidMovementFPBlock(cfgs["tm_rapid_movement"], c, d, ids),
		})
	}

	// The dormant-reactivation FP category needs its 12 selected customers
	// to still be status=dormant with a valid pre-existing >180-day-silent
	// gap (isDormant), but adding a genuine reactivation transaction only
	// ~90 days before anchor means they are no longer accurately described
	// as dormant afterward. selectCustomerIndices lets this loop mutate the
	// chosen customers in place (Status -> Active, attributes.
	// last_activity_at -> the reactivation date), the same pattern story 5
	// uses (story.go), so the T1-W1 dormant-consistency self-check — which
	// only constrains customers whose *final* status is still dormant —
	// stays satisfied.
	// Candidates must be old enough (>=500 days) to support a >=180-day
	// silent gap starting comfortably after account_opened_at, followed by
	// a reactivation that is itself comfortably before anchor — a customer
	// whose account is too new cannot support this narrative at all, so
	// they are filtered out here rather than patched up after the fact.
	dormantIdx := selectCustomerIndices(customers, used, 12, func(c domain.Customer) bool {
		return isIndividual(c) && isDormant(c) && accountOldEnough(c, anchor, 500)
	})
	for i, idx := range dormantIdx {
		c := customers[idx]
		gap := day(300 + i*2)
		reactivation := day(90 + i)
		out = append(out, fpIncident{
			CustomerID: c.ID, Category: "dormant_account_reactivation", Narrative: "帰国後の技能実習生再開",
			ExpectedScenario: "tm_dormant_account_reactivation",
			Transactions:     buildDormantFPBlock(cfgs["tm_dormant_account_reactivation"], c, gap, reactivation, ids),
		})
		customers[idx].Status = domain.CustomerStatusActive
		attrs := make(map[string]any, len(c.Attributes))
		for k, v := range c.Attributes {
			attrs[k] = v
		}
		attrs["last_activity_at"] = reactivation.Format("2006-01-02")
		customers[idx].Attributes = attrs
	}

	return out
}
