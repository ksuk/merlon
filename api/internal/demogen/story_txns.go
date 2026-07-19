package demogen

import (
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// storyIncident is one seeded transaction block for a story customer: a
// short list of transactions intended to make exactly one TM scenario fire
// (or, for story 1's backtest precedent, intended NOT to fire). label is a
// short human-readable tag used only in STORY_IDS.md.
type storyIncident struct {
	CustomerID       string
	Label            string
	ExpectedScenario string // "" for a deliberately non-firing precedent
	Transactions     []domain.Transaction
}

func txn(id, customerID string, amount float64, direction domain.TransactionDirection, country, channel string, at time.Time) domain.Transaction {
	return domain.Transaction{
		ID:                  id,
		CustomerID:          customerID,
		Amount:              amount,
		Currency:            "JPY",
		Direction:           direction,
		CounterpartyCountry: country,
		Channel:             channel,
		ExecutedAt:          at,
		CreatedAt:           at,
	}
}

// buildStoryIncidents constructs A6's six narrative transaction blocks.
// Amounts and timing are chosen to breach the tier-appropriate threshold
// read from the real TM scenario YAML (cfgs), and every firing incident is
// verified against the live native engine before being trusted (see
// alerts.go's self-check (a)) — this function only proposes the
// transactions; it never asserts they fire.
//
// One adaptation from A6's literal prose was required: dormant_account_
// reactivation's native.go implementation (evalDormant) checks a single
// transaction's amount against the threshold, not a 24h sum, despite the
// scenario YAML's declared "aggregation: sum over 24h" — the Go evaluator
// does not implement that aggregation for this scenario. A6's "180万+150万
// inbound, same-day 160万 outbound" would not fire under the actual engine
// (no single leg reaches the ¥3,000,000 LOW-tier threshold), so story 5's
// first reactivation transaction is a single ¥3,300,000 inbound instead of
// two smaller ones, preserving the narrative (large reactivation deposit,
// same-day outbound) while firing under the engine as actually implemented.
func buildStoryIncidents(anchor time.Time, cfgs map[string]scenarioConfig, ids *idSeq) []storyIncident {
	day := func(n int) time.Time { return anchor.AddDate(0, 0, -n) }
	at := func(base time.Time, hour, minute int) time.Time {
		return time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, time.UTC)
	}
	tx := func(customerID string, amount float64, direction domain.TransactionDirection, country, channel string, when time.Time) domain.Transaction {
		return txn(ids.next("demo-txn-%07d"), customerID, amount, direction, country, channel, when)
	}

	var out []storyIncident

	// Story 1: Lina Santos (demo-story-01) — structuring "remittance
	// consolidator". 3 inbound transactions within 24h, each below
	// individual_below, summing above the MEDIUM threshold (¥1,000,000):
	// ¥480,000+¥450,000+¥420,000=¥1,350,000. A non-firing precedent (3
	// transactions summing to ¥940,000, below threshold) is also recorded 4
	// months earlier as backtest material.
	story1Day := day(3)
	out = append(out, storyIncident{
		CustomerID: "demo-story-01", Label: "structuring (remittance consolidator)", ExpectedScenario: "tm_structuring_basic",
		Transactions: []domain.Transaction{
			tx("demo-story-01", 480000, domain.DirectionInbound, "JP", "agent", at(story1Day, 9, 0)),
			tx("demo-story-01", 450000, domain.DirectionInbound, "JP", "agent", at(story1Day, 15, 0)),
			tx("demo-story-01", 420000, domain.DirectionInbound, "JP", "agent", at(story1Day, 21, 0)),
		},
	})
	story1PrecedentDay := day(120)
	out = append(out, storyIncident{
		CustomerID: "demo-story-01", Label: "structuring precedent (non-firing, backtest material)", ExpectedScenario: "",
		Transactions: []domain.Transaction{
			tx("demo-story-01", 320000, domain.DirectionInbound, "JP", "agent", at(story1PrecedentDay, 9, 0)),
			tx("demo-story-01", 310000, domain.DirectionInbound, "JP", "agent", at(story1PrecedentDay, 15, 0)),
			tx("demo-story-01", 310000, domain.DirectionInbound, "JP", "agent", at(story1PrecedentDay, 21, 0)),
		},
	})

	// Story 2: Sano Takuma (demo-story-02) — high-frequency small amount
	// "mule account". 9 inbound transactions within 1h (>5 required at HIGH
	// tier), each <=¥100,000 (max_amount_per_txn) and summing to well under
	// the HIGH structuring threshold (¥500,000) so this block does not also
	// cross-trigger structuring (evalStructuring's candidate pool is not
	// restricted by direction, so a same-day cluster of small transactions
	// can trigger both scenarios unless the sum is kept below the
	// structuring threshold). Followed by 2 outbound "extraction"
	// transactions large enough (>=¥500,000 each) to fall outside
	// structuring's below-threshold candidate pool entirely (A6: "structuring
	// には届かない額に設計しシナリオ混線回避").
	story2Base := day(60)
	story2Hour := at(story2Base, 14, 0)
	amounts := []float64{38000, 40000, 42000, 45000, 48000, 50000, 52000, 55000, 58000} // sum=428,000 < 500,000
	var story2Txns []domain.Transaction
	for i, amt := range amounts {
		story2Txns = append(story2Txns, tx("demo-story-02", amt, domain.DirectionInbound, "JP", "agent", story2Hour.Add(time.Duration(i*6)*time.Minute)))
	}
	story2Txns = append(story2Txns,
		tx("demo-story-02", 550000, domain.DirectionOutbound, "JP", "agent", story2Hour.Add(70*time.Minute)),
		tx("demo-story-02", 600000, domain.DirectionOutbound, "JP", "agent", story2Hour.Add(80*time.Minute)),
	)
	out = append(out, storyIncident{
		CustomerID: "demo-story-02", Label: "high-frequency small amount (mule account)", ExpectedScenario: "tm_high_frequency_small_amount",
		Transactions: story2Txns,
	})

	// Story 3: 株式会社アオイ貿易 (demo-story-03) — high-risk country
	// transfer. Single outbound ¥2,800,000 to MM (>¥2,000,000 MEDIUM
	// threshold). Legitimate TH/AE settlements provide innocuous background
	// (TH/AE are not in the scenario's high_risk_countries list, so they
	// never trigger regardless of amount).
	story3Day := day(10)
	out = append(out, storyIncident{
		CustomerID: "demo-story-03", Label: "high-risk country transfer (used-car export)", ExpectedScenario: "tm_high_risk_country_transfer",
		Transactions: []domain.Transaction{
			tx("demo-story-03", 2800000, domain.DirectionOutbound, "MM", "web", at(story3Day, 10, 30)),
			tx("demo-story-03", 1200000, domain.DirectionOutbound, "TH", "web", at(day(45), 11, 0)),
			tx("demo-story-03", 900000, domain.DirectionOutbound, "AE", "web", at(day(90), 11, 0)),
		},
	})

	// Story 4: Meridian Cross Trading Pte. Ltd. (demo-story-04) — rapid
	// movement "pass-through". Inbound ¥3,200,000 (HK), then within 6h
	// outbound ¥1,400,000 (SG) + ¥1,300,000 (MY). 48h total ¥5,900,000,
	// outbound ratio (2,700,000/3,200,000) ≈ 0.84 >= 0.80. Severity is
	// force-upgraded to critical in alerts.go (A6/A7: "④のみcritical格上げ").
	story4Base := day(15)
	story4T0 := at(story4Base, 9, 0)
	out = append(out, storyIncident{
		CustomerID: "demo-story-04", Label: "rapid movement (pass-through)", ExpectedScenario: "tm_rapid_movement",
		Transactions: []domain.Transaction{
			tx("demo-story-04", 3200000, domain.DirectionInbound, "HK", "web", story4T0),
			tx("demo-story-04", 1400000, domain.DirectionOutbound, "SG", "web", story4T0.Add(2*time.Hour)),
			tx("demo-story-04", 1300000, domain.DirectionOutbound, "MY", "web", story4T0.Add(5*time.Hour)),
		},
	})

	// Story 5: 平尾靖子 (demo-story-05) — dormant account reactivation.
	// evalDormant computes the gap from the *previous* transaction, so a
	// pre-dormancy transaction (230 days before the reactivation, comfortably
	// over dormant_days=180) is included to establish that gap; a single
	// ¥3,300,000 inbound (>= the LOW-tier ¥3,000,000 reactivation_threshold)
	// then reactivates the account, with a same-day ¥1,600,000 outbound
	// preserving A6's "quickly moved back out" narrative.
	story5PreDormancyDay := day(650)
	story5Day := day(420) // matches her attributes.last_activity_at set in T1-W1
	out = append(out, storyIncident{
		CustomerID: "demo-story-05", Label: "dormant account reactivation", ExpectedScenario: "tm_dormant_account_reactivation",
		Transactions: []domain.Transaction{
			tx("demo-story-05", 25000, domain.DirectionOutbound, "JP", "atm", at(story5PreDormancyDay, 11, 0)),
			tx("demo-story-05", 3300000, domain.DirectionInbound, "JP", "atm", at(story5Day, 10, 0)),
			tx("demo-story-05", 1600000, domain.DirectionOutbound, "JP", "atm", at(story5Day, 16, 0)),
		},
	})

	// Story 6: Nguyen Van Phung (demo-story-06) — composite (structuring x2
	// in different aggregation windows, far enough apart not to merge into
	// one sliding window, + rapid_movement x1). "医療medium→high遷移" is
	// realized as a second score_history entry with EDD-updated attributes
	// (see demogen.go's story6 re-score step), not a change to this
	// transaction data.
	s6w1 := day(90)
	s6w2 := day(40)
	s6rapid := day(10)
	out = append(out, storyIncident{
		CustomerID: "demo-story-06", Label: "structuring window 1", ExpectedScenario: "tm_structuring_basic",
		Transactions: []domain.Transaction{
			tx("demo-story-06", 380000, domain.DirectionInbound, "VN", "web", at(s6w1, 9, 0)),
			tx("demo-story-06", 400000, domain.DirectionInbound, "VN", "web", at(s6w1, 14, 0)),
			tx("demo-story-06", 420000, domain.DirectionInbound, "VN", "web", at(s6w1, 20, 0)),
		},
	})
	out = append(out, storyIncident{
		CustomerID: "demo-story-06", Label: "structuring window 2", ExpectedScenario: "tm_structuring_basic",
		Transactions: []domain.Transaction{
			tx("demo-story-06", 350000, domain.DirectionInbound, "VN", "web", at(s6w2, 9, 0)),
			tx("demo-story-06", 390000, domain.DirectionInbound, "VN", "web", at(s6w2, 14, 0)),
			tx("demo-story-06", 410000, domain.DirectionInbound, "VN", "web", at(s6w2, 20, 0)),
		},
	})
	out = append(out, storyIncident{
		CustomerID: "demo-story-06", Label: "rapid movement", ExpectedScenario: "tm_rapid_movement",
		Transactions: []domain.Transaction{
			tx("demo-story-06", 6000000, domain.DirectionInbound, "VN", "web", at(s6rapid, 9, 0)),
			tx("demo-story-06", 5500000, domain.DirectionOutbound, "VN", "web", at(s6rapid.AddDate(0, 0, 1), 9, 0)),
		},
	})

	return out
}
