package demogen

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// jpPrefectures lists Japan's 47 prefectures; every generated customer
// (regardless of nationality) is assigned a JP address_pref, matching the
// funds_transfer persona: a Japan-based remittance/wallet operator whose
// foreign-national customers reside in Japan (A1).
var jpPrefectures = []string{
	"北海道", "青森県", "岩手県", "宮城県", "秋田県", "山形県", "福島県", "茨城県", "栃木県", "群馬県",
	"埼玉県", "千葉県", "東京都", "神奈川県", "新潟県", "富山県", "石川県", "福井県", "山梨県", "長野県",
	"岐阜県", "静岡県", "愛知県", "三重県", "滋賀県", "京都府", "大阪府", "兵庫県", "奈良県", "和歌山県",
	"鳥取県", "島根県", "岡山県", "広島県", "山口県", "徳島県", "香川県", "愛媛県", "高知県", "福岡県",
	"佐賀県", "長崎県", "熊本県", "大分県", "宮崎県", "鹿児島県", "沖縄県",
}

var occupationsByEra = map[string][]string{
	"young":  {"学生", "技能実習生", "会社員", "フリーランス", "アルバイト", "無職"},
	"middle": {"会社員", "自営業(飲食業)", "自営業(小売業)", "公務員", "IT技術者", "製造業従事者", "建設業従事者", "看護師", "教員", "自営業(輸出入業)"},
	"senior": {"年金受給者", "無職", "自営業(小売業)", "嘱託社員"},
}

// occupationBands maps each occupation string used above to the
// occupation_band value funds_transfer.yaml's occupation_band risk factor
// resolves against (犯収法上のKYC属性: 職業リスク). This is a derivation, not
// an independent random draw: attrs["occupation_band"] always reflects the
// occupation actually recorded on the customer, so the two attributes can
// never disagree.
//   - stable: regular salaried/verifiable income (employees, civil servants,
//     licensed professionals)
//   - retired: pension income
//   - self_employed: self-employed/trainee/student — thinner, harder-to-verify
//     income history than salaried employment, but not cash-intensive
//   - cash_intensive: cash-heavy small business (food service, retail) —
//     classically elevated in AML guidance due to commingling risk
//   - unemployed: no declared income source at all
var occupationBands = map[string]string{
	"会社員": "stable", "公務員": "stable", "IT技術者": "stable", "製造業従事者": "stable",
	"建設業従事者": "stable", "看護師": "stable", "教員": "stable", "嘱託社員": "stable",
	"年金受給者":     "retired",
	"自営業(輸出入業)": "self_employed", "フリーランス": "self_employed", "学生": "self_employed",
	"技能実習生": "self_employed", "アルバイト": "self_employed",
	"自営業(飲食業)": "cash_intensive", "自営業(小売業)": "cash_intensive",
	"無職": "unemployed",
}

// occupationBand derives the funds_transfer.yaml occupation_band value for a
// given occupation string.
func occupationBand(occupation string) string {
	if band, ok := occupationBands[occupation]; ok {
		return band
	}
	return "stable"
}

var corporateIndustries = []string{
	"貿易業", "製造業", "小売業", "物流・運輸業", "IT・情報通信業", "飲食サービス業", "建設業", "卸売業", "不動産業",
}

// noIndustryOccupations are occupations with no employer industry to record
// (unemployed, retired, student); industryForOccupation returns nil for
// these so the attribute is present as JSON null rather than an invented
// value, matching how the 6 story customers with these occupations are
// authored (story.go).
var noIndustryOccupations = map[string]bool{
	"無職": true, "年金受給者": true, "学生": true,
}

func industryForOccupation(rng *rand.Rand, occupation string) any {
	if noIndustryOccupations[occupation] {
		return nil
	}
	return corporateIndustries[rng.Intn(len(corporateIndustries))]
}

var individualPurposes = []string{
	"生活費送金", "家族への送金", "給与受取", "貯蓄", "公共料金支払い", "投資運用",
}
var corporatePurposes = []string{
	"貿易代金決済", "事業性資金決済", "仕入決済", "給与支払", "運転資金決済",
}

// pickDeliveryChannel is the customer's preferred delivery channel (A5
// weights: app70 / web15 / agent10 / atm5). It is a display attribute
// distinct from the CDD product_channel risk factor (stored in
// Customer.ProductTypes).
func pickDeliveryChannel(rng *rand.Rand) string {
	return weightedPick(rng, []string{"app", "web", "agent", "atm"}, []int{70, 15, 10, 5})
}

// productChannelForTargetTier picks a product_channel value (the funds_
// transfer.yaml risk factor stored in Customer.ProductTypes) biased toward
// whichever tier is being targeted this attempt. It is one of six factors
// (weight 0.15) rather than the dominant lever it once was — see
// demogen.go's package doc for the full rationale — so hitting "high" now
// requires this AND several other levers (occupation_band,
// account_age_band, expected_activity) to align; generatePopulation's
// engine-verified rejection sampling corrects any wrong guesses here.
func productChannelForTargetTier(rng *rand.Rand, ctype, targetTier string) string {
	if ctype != string(domain.CustomerTypeIndividual) {
		// correspondent_pass_through models a corporate customer whose funds
		// are routed onward through a correspondent/pass-through structure
		// (elevated per FATF correspondent-banking guidance); corporate_
		// trade_settlement is the ordinary corporate channel.
		if targetTier == "high" {
			return weightedPick(rng, []string{"correspondent_pass_through", "corporate_trade_settlement"}, []int{70, 30})
		}
		return weightedPick(rng, []string{"corporate_trade_settlement", "correspondent_pass_through"}, []int{90, 10})
	}
	switch targetTier {
	case "high":
		return weightedPick(rng, []string{"agent_cash_remittance", "international_remittance"}, []int{85, 15})
	case "medium":
		return weightedPick(rng, []string{"international_remittance", "agent_cash_remittance", "domestic_wallet"}, []int{40, 30, 30})
	default:
		return weightedPick(rng, []string{"domestic_wallet", "international_remittance"}, []int{75, 25})
	}
}

// occupationBandTargetForTier picks the occupation_band this attempt aims
// for (individuals only — funds_transfer.yaml scopes occupation_band to
// applies_to: [individual]).
func occupationBandTargetForTier(rng *rand.Rand, targetTier string) string {
	switch targetTier {
	case "high":
		return weightedPick(rng, []string{"unemployed", "cash_intensive"}, []int{60, 40})
	case "medium":
		return weightedPick(rng, []string{"self_employed", "cash_intensive", "stable"}, []int{40, 30, 30})
	default:
		return weightedPick(rng, []string{"stable", "retired"}, []int{80, 20})
	}
}

// occupationForBand picks an occupation string from the era-appropriate
// pool whose derived occupationBand matches targetBand, falling back to any
// occupation in the era if none match (every era pool has at least one
// "stable" occupation, so this only matters for bands an era's pool lacks).
func occupationForBand(rng *rand.Rand, era, targetBand string) string {
	pool := occupationsByEra[era]
	var matches []string
	for _, occ := range pool {
		if occupationBand(occ) == targetBand {
			matches = append(matches, occ)
		}
	}
	if len(matches) == 0 {
		matches = pool
	}
	return matches[rng.Intn(len(matches))]
}

// expectedActivityForTargetTier picks the declared/expected activity level
// (funds_transfer.yaml's expected_activity factor — what the customer told
// the business at onboarding or last periodic review, never a transaction-
// monitoring detection outcome).
func expectedActivityForTargetTier(rng *rand.Rand, targetTier string) string {
	switch targetTier {
	case "high":
		return weightedPick(rng, []string{"cross_border_heavy", "high_value"}, []int{70, 30})
	case "medium":
		return weightedPick(rng, []string{"frequent_remittance", "high_value"}, []int{50, 50})
	default:
		return weightedPick(rng, []string{"normal", "frequent_remittance"}, []int{85, 15})
	}
}

// accountAgeBandTargetForTier picks the account_age_band this attempt aims
// for. It is only a target: the actual attrs["account_age_band"] value
// written onto the customer is always re-derived from the real
// account_opened_at date (see accountAgeBandFromOpened), so the two never
// disagree.
func accountAgeBandTargetForTier(rng *rand.Rand, targetTier string) string {
	switch targetTier {
	case "high":
		return weightedPick(rng, []string{"new", "recent"}, []int{85, 15})
	case "medium":
		return weightedPick(rng, []string{"recent", "maturing"}, []int{55, 45})
	default:
		return weightedPick(rng, []string{"established", "maturing"}, []int{65, 35})
	}
}

// openedDateForAgeBandTarget returns a plausible account_opened_at for the
// requested band (established >=3y / maturing 1-3y / recent 3-12mo / new
// <3mo before anchor).
func openedDateForAgeBandTarget(rng *rand.Rand, anchor time.Time, targetBand string) time.Time {
	var daysAgo int
	switch targetBand {
	case "new":
		daysAgo = 1 + rng.Intn(89) // 1-89 days
	case "recent":
		daysAgo = 90 + rng.Intn(275) // 90-364 days
	case "maturing":
		daysAgo = 365 + rng.Intn(730) // 365-1094 days
	default: // established
		daysAgo = 1095 + rng.Intn(2555) // 1095-3649 days (up to ~10y)
	}
	return anchor.AddDate(0, 0, -daysAgo)
}

// accountAgeBandFromOpened re-derives the account_age_band attribute from a
// real account_opened_at date, using the same day thresholds
// openedDateForAgeBandTarget draws from — the single source of truth for
// this attribute is always the date, never a target hint.
func accountAgeBandFromOpened(anchor, opened time.Time) string {
	days := int(anchor.Sub(opened).Hours() / 24)
	switch {
	case days >= 1095:
		return "established"
	case days >= 365:
		return "maturing"
	case days >= 90:
		return "recent"
	default:
		return "new"
	}
}

// eraAndAge picks a generation bucket (A3 has no explicit age-mix target, so
// weights here are simply plausible) and an age within that bucket's range.
func eraAndAge(rng *rand.Rand) (era string, age int) {
	era = weightedPick(rng, []string{"young", "middle", "senior"}, []int{20, 55, 25})
	switch era {
	case "young":
		age = 18 + rng.Intn(12) // 18-29
	case "senior":
		age = 60 + rng.Intn(26) // 60-85
	default:
		age = 30 + rng.Intn(30) // 30-59
	}
	return
}

func dateOfBirth(anchor time.Time, rng *rand.Rand, age int) time.Time {
	return anchor.AddDate(-age, -rng.Intn(12), -rng.Intn(28))
}

// japaneseIndividualName builds a Japanese kanji/kana/romaji name triple for
// the supplied generation era.
func japaneseIndividualName(rng *rand.Rand, era string) (name, kana, romaji string) {
	surname := japaneseSurnames[rng.Intn(len(japaneseSurnames))]
	given := pickJapaneseGivenName(rng, era)
	return surname.Kanji + " " + given.Kanji, surname.Kana + " " + given.Kana, given.Romaji + " " + surname.Romaji
}

// individualName dispatches to the Japanese or foreign name pool based on
// nationality. Foreign customers get no kana reading (kana is absent from
// the resulting attributes map), matching how the pre-existing demo seed
// already models non-Japanese customers (name_kana: null).
func individualName(rng *rand.Rand, country, era string) (name, kana, romaji string) {
	if country == "JP" {
		return japaneseIndividualName(rng, era)
	}
	first, last := pickForeignName(rng, country)
	full := first + " " + last
	return full, "", full
}

func corporateDomesticName(rng *rand.Rand) (name, romaji string) {
	stem := corporateDomesticStems[rng.Intn(len(corporateDomesticStems))]
	suffix := corporateDomesticSuffixes[rng.Intn(len(corporateDomesticSuffixes))]
	return "株式会社" + stem.Word + suffix.Word, stem.Romaji + " " + suffix.Romaji + " Co., Ltd."
}

func corporateForeignName(rng *rand.Rand, country string) string {
	w1 := corporateForeignWords1[rng.Intn(len(corporateForeignWords1))]
	w2 := corporateForeignWords2[rng.Intn(len(corporateForeignWords2))]
	return fmt.Sprintf("%s %s %s", w1, w2, corporateForeignLegalForm(country))
}

// accountOpenedAndLastActivity derives account_opened_at and last_activity_at
// consistent with the customer's status and (for non-dormant customers) the
// account_age_band this attempt is targeting.
//
// Dormant customers ignore the age-band target entirely: dormancy requires
// last_activity_at to be >180 days before anchor (T1-W1 self-check), which
// by construction means the account itself must be at least that old, so
// account_opened_at is derived backward from last_activity_at instead
// (almost always landing in "maturing" or "established" once re-derived —
// tier-shaping for dormant customers relies on occupation_band/
// expected_activity/product_channel, not account age).
//
// For non-dormant customers, last_activity_at is clamped to never precede
// account_opened_at (an account cannot have activity before it existed).
func accountOpenedAndLastActivity(rng *rand.Rand, anchor time.Time, status, ageBandTarget string) (opened, lastActivity time.Time) {
	if status == string(domain.CustomerStatusDormant) {
		lastActivity = anchor.AddDate(0, 0, -(181 + rng.Intn(420))) // 181-600 days before anchor
		opened = lastActivity.AddDate(0, 0, -(30 + rng.Intn(1970))) // strictly earlier than last activity
		return
	}
	opened = openedDateForAgeBandTarget(rng, anchor, ageBandTarget)
	daysSinceOpened := int(anchor.Sub(opened).Hours() / 24)
	maxLookback := 170
	if daysSinceOpened < maxLookback {
		maxLookback = daysSinceOpened
	}
	if maxLookback < 0 {
		maxLookback = 0
	}
	lastActivity = anchor.AddDate(0, 0, -rng.Intn(maxLookback+1))
	return
}

// buildCandidate assembles one candidate customer's full attribute set for
// the rejection-sampling loop. It never sets ID or ExternalID: those are
// assigned once the final accepted population (story + generated) is known,
// so numbering stays a pure function of final output order.
func buildCandidate(rng *rand.Rand, anchor time.Time, seq int, ctype, status, country, targetTier string) domain.Customer {
	productChannel := productChannelForTargetTier(rng, ctype, targetTier)
	ageBandTarget := accountAgeBandTargetForTier(rng, targetTier)
	deliveryChannel := pickDeliveryChannel(rng)
	addressPref := jpPrefectures[rng.Intn(len(jpPrefectures))]
	opened, lastActivity := accountOpenedAndLastActivity(rng, anchor, status, ageBandTarget)
	expectedActivity := expectedActivityForTargetTier(rng, targetTier)

	attrs := map[string]any{
		"account_age_band":  accountAgeBandFromOpened(anchor, opened),
		"expected_activity": expectedActivity,
		"channel":           deliveryChannel,
		"address_pref":      addressPref,
		"account_opened_at": opened.Format("2006-01-02"),
		"last_activity_at":  lastActivity.Format("2006-01-02"),
	}

	c := domain.Customer{
		CustomerType: domain.CustomerType(ctype),
		CountryCode:  country,
		ProductTypes: []string{productChannel},
		Status:       domain.CustomerStatus(status),
		CreatedAt:    opened,
		UpdatedAt:    anchor,
	}

	switch ctype {
	case string(domain.CustomerTypeIndividual):
		era, age := eraAndAge(rng)
		name, kana, romaji := individualName(rng, country, era)
		attrs["name"] = name
		if kana != "" {
			attrs["name_kana"] = kana
		}
		attrs["name_en"] = romaji
		attrs["date_of_birth"] = dateOfBirth(anchor, rng, age).Format("2006-01-02")
		occupationBandTarget := occupationBandTargetForTier(rng, targetTier)
		occupation := occupationForBand(rng, era, occupationBandTarget)
		attrs["occupation"] = occupation
		attrs["occupation_band"] = occupationBand(occupation)
		attrs["industry"] = industryForOccupation(rng, occupation)
		attrs["purpose"] = individualPurposes[rng.Intn(len(individualPurposes))]
	case string(domain.CustomerTypeCorporateDomestic):
		name, romaji := corporateDomesticName(rng)
		attrs["name"] = name
		attrs["name_en"] = romaji
		attrs["industry"] = corporateIndustries[rng.Intn(len(corporateIndustries))]
		attrs["purpose"] = corporatePurposes[rng.Intn(len(corporatePurposes))]
	case string(domain.CustomerTypeCorporateForeign):
		name := corporateForeignName(rng, country)
		attrs["name"] = name
		attrs["name_en"] = name
		attrs["industry"] = corporateIndustries[rng.Intn(len(corporateIndustries))]
		attrs["purpose"] = corporatePurposes[rng.Intn(len(corporatePurposes))]
	case "npo":
		attrs["name"] = npoNames[rng.Intn(len(npoNames))]
		attrs["industry"] = "非営利活動"
		attrs["purpose"] = "会費・寄付金の管理"
	case "trust":
		attrs["name"] = trustNames[rng.Intn(len(trustNames))]
		attrs["industry"] = "資産管理"
		attrs["purpose"] = "信託財産の管理"
	case "partnership":
		attrs["name"] = partnershipNames[rng.Intn(len(partnershipNames))]
		attrs["industry"] = "共同事業"
		attrs["purpose"] = corporatePurposes[rng.Intn(len(corporatePurposes))]
	}

	c.Attributes = attrs
	return c
}
