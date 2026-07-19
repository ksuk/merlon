package demogen

import (
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// buildStoryCustomers materializes the 6 fixed A6 narrative customers with
// directly authored attributes (no rejection sampling — their tier is a
// target to approach, not a value to hit by search). Their IDs are fixed
// ("demo-story-01".."demo-story-06" in A6 narrative order) so the next wave
// (transactions/alerts/STORY_IDS.md) can key off them.
//
// Every attribute here is fully synthetic; none of these six names, or the
// fictional company names, correspond to a real person or organization
// (DD3). Scores/tiers are never hardcoded — Generate scores every one of
// these customers through the real native engine, same as the rest of the
// population (A4). occupation_band mirrors the attrs["occupation"] actually
// recorded (via occupationBand), the same derivation buildCandidate uses for
// the rest of the population, so a story customer's occupation and its band
// can never disagree.
func buildStoryCustomers(anchor time.Time) ([]domain.Customer, []string) {
	day := func(n int) time.Time { return anchor.AddDate(0, 0, -n) }
	dateAttr := func(t time.Time) string { return t.Format("2006-01-02") }

	customers := []domain.Customer{
		// Story 1: "送金取りまとめ屋" (remittance consolidator) — Lina Santos,
		// PH, food-service worker (cash-intensive occupation band), agent
		// cash channel, frequent declared remittance activity. Targets
		// medium (~2.9).
		{
			ID:           "demo-story-01",
			CustomerType: domain.CustomerTypeIndividual,
			CountryCode:  "PH",
			ProductTypes: []string{"agent_cash_remittance"},
			Status:       domain.CustomerStatusActive,
			CreatedAt:    day(730), // ~2y -> account_age_band=maturing
			UpdatedAt:    anchor,
			Attributes: map[string]any{
				"name":              "Lina Santos",
				"name_en":           "Lina Santos",
				"date_of_birth":     dateAttr(anchor.AddDate(-32, -4, -10)),
				"occupation":        "自営業(飲食業)",
				"occupation_band":   occupationBand("自営業(飲食業)"),
				"industry":          "飲食サービス業",
				"address_pref":      "東京都",
				"channel":           "agent",
				"purpose":           "家族への送金",
				"account_opened_at": dateAttr(day(730)),
				"last_activity_at":  dateAttr(day(2)),
				"account_age_band":  accountAgeBandFromOpened(anchor, day(730)),
				"expected_activity": "frequent_remittance",
			},
		},
		// Story 2: "売り口座ミュール" (mule account) — Sano Takuma, JP, 21,
		// unemployed (occupation_band=unemployed), account opened 2 months
		// ago (account_age_band=new), agent cash channel, heavily
		// cross-border declared activity. Targets high (~3.7).
		{
			ID:           "demo-story-02",
			CustomerType: domain.CustomerTypeIndividual,
			CountryCode:  "JP",
			ProductTypes: []string{"agent_cash_remittance"},
			Status:       domain.CustomerStatusActive,
			CreatedAt:    day(60), // account opened 2 months ago -> new
			UpdatedAt:    anchor,
			Attributes: map[string]any{
				"name":              "佐野 巧真",
				"name_kana":         "サノ タクマ",
				"name_en":           "Takuma Sano",
				"date_of_birth":     dateAttr(anchor.AddDate(-21, -7, -3)),
				"occupation":        "無職",
				"occupation_band":   occupationBand("無職"),
				"industry":          nil,
				"address_pref":      "大阪府",
				"channel":           "agent",
				"purpose":           "生活費送金",
				"account_opened_at": dateAttr(day(60)),
				"last_activity_at":  dateAttr(day(1)),
				"account_age_band":  accountAgeBandFromOpened(anchor, day(60)),
				"expected_activity": "cross_border_heavy",
			},
		},
		// Story 3: "ハイリスク国送金" — 株式会社アオイ貿易 (used-car export
		// trading), corporate_domestic, account opened ~7 months ago
		// (account_age_band=recent), heavily cross-border declared trade
		// settlement activity. Targets medium (~3.2).
		{
			ID:           "demo-story-03",
			CustomerType: domain.CustomerTypeCorporateDomestic,
			CountryCode:  "JP",
			ProductTypes: []string{"corporate_trade_settlement"},
			Status:       domain.CustomerStatusActive,
			CreatedAt:    day(200), // ~7mo -> recent
			UpdatedAt:    anchor,
			Attributes: map[string]any{
				"name":              "株式会社アオイ貿易",
				"name_en":           "Aoi Boeki Co., Ltd.",
				"industry":          "中古車輸出業",
				"address_pref":      "神奈川県",
				"channel":           "web",
				"purpose":           "貿易代金決済",
				"account_opened_at": dateAttr(day(200)),
				"last_activity_at":  dateAttr(day(1)),
				"account_age_band":  accountAgeBandFromOpened(anchor, day(200)),
				"expected_activity": "cross_border_heavy",
			},
		},
		// Story 4: "パススルー" — Meridian Cross Trading Pte. Ltd., SG,
		// established under 2 months ago (account_age_band=new), routed
		// through a correspondent/pass-through settlement structure (its
		// entire business model), heavily cross-border declared activity.
		// Targets high (~4.1).
		{
			ID:           "demo-story-04",
			CustomerType: domain.CustomerTypeCorporateForeign,
			CountryCode:  "SG",
			ProductTypes: []string{"correspondent_pass_through"},
			Status:       domain.CustomerStatusActive,
			CreatedAt:    day(60), // <1y ("established under a year ago") -> new
			UpdatedAt:    anchor,
			Attributes: map[string]any{
				"name":              "Meridian Cross Trading Pte. Ltd.",
				"name_en":           "Meridian Cross Trading Pte. Ltd.",
				"industry":          "貿易業",
				"address_pref":      "東京都",
				"channel":           "web",
				"purpose":           "貿易代金決済",
				"account_opened_at": dateAttr(day(60)),
				"last_activity_at":  dateAttr(day(1)),
				"account_age_band":  accountAgeBandFromOpened(anchor, day(60)),
				"expected_activity": "cross_border_heavy",
			},
		},
		// Story 5: "休眠口座再活性化" — Hirao Yasuko, JP, 74, pensioner
		// (occupation_band=retired), long-standing account, dormant for 420
		// days, ordinary declared activity. Targets low (~1.6). Status is
		// Frozen (not Dormant): T1-W2 seeds her reactivation transactions at
		// last_activity_at (420 days before anchor — her account had been
		// silent since ~650 days before anchor) and, per A6, the account is
		// frozen immediately after the alert fires, which is why anchor-date
		// status is frozen rather than still dormant (T1-W1's self-check for
		// dormant-status last_activity_at consistency therefore does not
		// apply to her; W1's dormant/frozen population quotas absorb this
		// since they read EffectiveStatus() dynamically off this struct).
		{
			ID:           "demo-story-05",
			CustomerType: domain.CustomerTypeIndividual,
			CountryCode:  "JP",
			ProductTypes: []string{"domestic_wallet"},
			Status:       domain.CustomerStatusFrozen,
			CreatedAt:    day(15 * 365),
			UpdatedAt:    anchor,
			Attributes: map[string]any{
				"name":              "平尾 靖子",
				"name_kana":         "ヒラオ ヤスコ",
				"name_en":           "Yasuko Hirao",
				"date_of_birth":     dateAttr(anchor.AddDate(-74, -2, -18)),
				"occupation":        "年金受給者",
				"occupation_band":   occupationBand("年金受給者"),
				"industry":          nil,
				"address_pref":      "福岡県",
				"channel":           "atm",
				"purpose":           "生活費送金",
				"account_opened_at": dateAttr(day(15 * 365)),
				"last_activity_at":  dateAttr(day(420)),
				"account_age_band":  accountAgeBandFromOpened(anchor, day(15*365)),
				"expected_activity": "normal",
			},
		},
		// Story 6: "複合（structuring + rapid_movement）" — Nguyen Van Phung,
		// VN, self-employed import/export (occupation_band=self_employed),
		// recently opened account, heavily cross-border declared activity;
		// this wave records his initial medium-tier state (his escalation to
		// high via alerts/re-score is next wave's transaction/alert data).
		{
			ID:           "demo-story-06",
			CustomerType: domain.CustomerTypeIndividual,
			CountryCode:  "VN",
			ProductTypes: []string{"international_remittance"},
			Status:       domain.CustomerStatusActive,
			CreatedAt:    day(200), // ~7mo -> recent
			UpdatedAt:    anchor,
			Attributes: map[string]any{
				"name":              "Nguyen Van Phung",
				"name_en":           "Nguyen Van Phung",
				"date_of_birth":     dateAttr(anchor.AddDate(-37, -1, -9)),
				"occupation":        "自営業(輸出入業)",
				"occupation_band":   occupationBand("自営業(輸出入業)"),
				"industry":          "貿易業",
				"address_pref":      "愛知県",
				"channel":           "web",
				"purpose":           "事業性資金決済",
				"account_opened_at": dateAttr(day(200)),
				"last_activity_at":  dateAttr(day(1)),
				"account_age_band":  accountAgeBandFromOpened(anchor, day(200)),
				"expected_activity": "cross_border_heavy",
			},
		},
	}

	ids := make([]string, len(customers))
	for i, c := range customers {
		ids[i] = c.ID
	}
	return customers, ids
}
