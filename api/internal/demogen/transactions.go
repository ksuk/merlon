package demogen

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// idSeq is a simple sequential ID generator. Callers create one per
// generation run and thread it through in customer/transaction iteration
// order, so IDs stay a pure function of that (already deterministic) order.
type idSeq struct{ n int }

func (s *idSeq) next(format string) string {
	s.n++
	return fmt.Sprintf(format, s.n)
}

// lognormal draws from a LogNormal(mu, sigma) distribution via the
// Box-Muller transform, using rng exclusively (no math/rand global state, no
// time-based seeding) so the sequence is reproducible.
func lognormal(rng *rand.Rand, mu, sigma float64) float64 {
	u1 := rng.Float64()
	if u1 <= 0 {
		u1 = 1e-12
	}
	u2 := rng.Float64()
	z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	return math.Exp(mu + sigma*z)
}

func roundToThousand(amount float64) float64 {
	r := math.Round(amount/1000) * 1000
	if r < 1000 {
		r = 1000
	}
	return r
}

// pickAmount returns one plausible transaction amount (A5's LogNormal
// distributions): corporate settlement, individual international
// remittance (truncated at the funds-transfer-business ¥1,000,000 per-
// transaction cap — A1's "第二種相当の1回100万円上限"), or individual
// wallet/charge activity depending on direction. 70% of individual amounts
// round to the nearest ¥1,000 (A5).
func pickAmount(rng *rand.Rand, isCorporate bool, productChannel string, direction domain.TransactionDirection) float64 {
	var amount float64
	switch {
	case isCorporate:
		amount = lognormal(rng, math.Log(450000), 1.1)
	case productChannel == "international_remittance" && rng.Intn(100) < 80:
		amount = lognormal(rng, math.Log(35000), 0.65)
		if amount > 1000000 {
			amount = 1000000
		}
	case direction == domain.DirectionInbound:
		amount = lognormal(rng, math.Log(30000), 0.7) // wallet charge/top-up
	default:
		amount = lognormal(rng, math.Log(9000), 0.9) // everyday wallet spend
	}
	if amount < 100 {
		amount = 100
	}
	if !isCorporate && rng.Intn(100) < 70 {
		amount = roundToThousand(amount)
	}
	return amount
}

// dayWeight approximates A5's time-pattern boosts: individuals skew toward
// Sundays, month-end (25th-28th), and December; corporates skew toward
// weekdays and 五十日 (5/10/15/20/25/month-end) settlement days.
func dayWeight(day time.Time, isCorporate bool) float64 {
	if isCorporate {
		w := 1.0
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			w *= 0.15
		}
		switch day.Day() {
		case 5, 10, 15, 20, 25, 28, 29, 30, 31:
			w *= 1.6
		}
		return w
	}
	w := 1.0
	if day.Weekday() == time.Sunday {
		w *= 1.6
	}
	if d := day.Day(); d >= 25 && d <= 28 {
		w *= 1.6
	}
	if day.Month() == time.December {
		w *= 1.4
	}
	return w
}

// pickDay chooses a day within [windowStart, windowEnd] (inclusive),
// rejection-sampled against dayWeight so the A5 boosts above are honored
// without needing a full weighted-enumeration over every day in the window.
func pickDay(rng *rand.Rand, windowStart, windowEnd time.Time, isCorporate bool) time.Time {
	span := int(windowEnd.Sub(windowStart).Hours() / 24)
	if span < 0 {
		span = 0
	}
	const maxWeight = 3.6 // 1.6 (Sunday) * 1.6 (month-end) * 1.4 (December), individual's ceiling
	for attempt := 0; attempt < 8; attempt++ {
		day := windowStart.AddDate(0, 0, rng.Intn(span+1))
		if rng.Float64() < dayWeight(day, isCorporate)/maxWeight {
			return day
		}
	}
	return windowStart.AddDate(0, 0, rng.Intn(span+1))
}

func pickExecutedAt(rng *rand.Rand, day time.Time, isCorporate bool) time.Time {
	var hour int
	if isCorporate {
		hour = 9 + rng.Intn(10) // weekday business hours, 9-18
	} else if rng.Intn(2) == 0 {
		hour = 11 + rng.Intn(4) // midday peak
	} else {
		hour = 19 + rng.Intn(4) // evening peak
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, rng.Intn(60), rng.Intn(60), 0, time.UTC)
}

func pickTxnChannel(rng *rand.Rand) string {
	return weightedPick(rng, []string{"app", "web", "agent", "atm"}, []int{70, 15, 10, 5})
}

// pickCounterpartyCountry correlates the counterparty with the customer's
// own nationality (A5: "counterparty_countryは国籍と相関(PH籍→80%PH宛)").
func pickCounterpartyCountry(rng *rand.Rand, customerCountry string) string {
	if customerCountry != "JP" {
		if rng.Intn(100) < 80 {
			return customerCountry
		}
		return "JP"
	}
	if rng.Intn(100) < 85 {
		return "JP"
	}
	pool := make([]string, 0, len(corridorCountries)+len(otherGroupCountries))
	pool = append(pool, corridorCountries...)
	pool = append(pool, otherGroupCountries...)
	return pool[rng.Intn(len(pool))]
}

// monthlyFrequency picks a customer's activity tier and per-month
// transaction rate (A5: heavy10%/middle40%/light50%, tuned within each
// band's stated range to land the population's total transaction count
// near A2's ~48,000 target across the whole 1000-customer population).
func monthlyFrequency(rng *rand.Rand) (tier string, perMonth int) {
	tier = weightedPick(rng, []string{"heavy", "middle", "light"}, []int{10, 40, 50})
	switch tier {
	case "heavy":
		perMonth = 14 + rng.Intn(15) // 14-28
	case "middle":
		perMonth = 5 + rng.Intn(5) // 5-9
	default:
		perMonth = rng.Intn(4) // 0-3
	}
	return
}

// parseAttrDate reads a "YYYY-MM-DD" attribute, falling back to fallback on
// a missing/unparseable value.
func parseAttrDate(attrs map[string]any, key string, fallback time.Time) time.Time {
	raw, ok := attrs[key].(string)
	if !ok || raw == "" {
		return fallback
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return fallback
	}
	return t
}

// generateBackgroundTransactions builds one customer's ordinary (non-
// story, non-FP-injected) transaction history. account_opened_at and
// last_activity_at (both fixed by T1-W1) bound the window: no transaction
// occurs before the account existed or after its last known activity — for
// a dormant customer this is exactly the "zero transactions in the most
// recent 180+ days" invariant self-check already enforces on the attribute,
// now also enforced on the transaction timeline itself.
func generateBackgroundTransactions(rng *rand.Rand, anchor time.Time, c domain.Customer, ids *idSeq) []domain.Transaction {
	opened := parseAttrDate(c.Attributes, "account_opened_at", anchor.AddDate(-1, 0, 0))
	lastActivity := parseAttrDate(c.Attributes, "last_activity_at", anchor)

	windowStart := anchor.AddDate(-1, 0, 0)
	if opened.After(windowStart) {
		windowStart = opened
	}
	windowEnd := lastActivity
	if windowEnd.Before(windowStart) {
		windowEnd = windowStart
	}

	isCorporate := c.CustomerType != domain.CustomerTypeIndividual
	productChannel := ""
	if len(c.ProductTypes) > 0 {
		productChannel = c.ProductTypes[0]
	}

	_, perMonth := monthlyFrequency(rng)
	windowDays := int(windowEnd.Sub(windowStart).Hours() / 24)
	if windowDays < 0 {
		windowDays = 0
	}
	total := perMonth * windowDays / 30

	txns := make([]domain.Transaction, 0, total+1)
	for i := 0; i < total; i++ {
		day := pickDay(rng, windowStart, windowEnd, isCorporate)
		executedAt := pickExecutedAt(rng, day, isCorporate)
		direction := domain.DirectionOutbound
		if rng.Intn(100) < 45 {
			direction = domain.DirectionInbound
		}
		amount := pickAmount(rng, isCorporate, productChannel, direction)
		txns = append(txns, domain.Transaction{
			ID:                  ids.next("demo-txn-%07d"),
			CustomerID:          c.ID,
			Amount:              amount,
			Currency:            "JPY",
			Direction:           direction,
			CounterpartyCountry: pickCounterpartyCountry(rng, c.CountryCode),
			Channel:             pickTxnChannel(rng),
			ExecutedAt:          executedAt,
			CreatedAt:           executedAt,
		})
	}

	if len(txns) == 0 {
		// A recorded last_activity_at implies at least one real transaction
		// happened; without this, a light/new-account customer could have
		// last_activity_at with nothing behind it.
		txns = append(txns, domain.Transaction{
			ID:                  ids.next("demo-txn-%07d"),
			CustomerID:          c.ID,
			Amount:              pickAmount(rng, isCorporate, productChannel, domain.DirectionInbound),
			Currency:            "JPY",
			Direction:           domain.DirectionInbound,
			CounterpartyCountry: pickCounterpartyCountry(rng, c.CountryCode),
			Channel:             pickTxnChannel(rng),
			ExecutedAt:          windowEnd,
			CreatedAt:           windowEnd,
		})
		return txns
	}

	// Snap the chronologically latest transaction onto windowEnd's date (its
	// own decided time-of-day is kept) so the recorded last_activity_at
	// corresponds to a real transaction rather than merely bounding one.
	latest := 0
	for i, t := range txns {
		if t.ExecutedAt.After(txns[latest].ExecutedAt) {
			latest = i
		}
	}
	snapped := time.Date(windowEnd.Year(), windowEnd.Month(), windowEnd.Day(), txns[latest].ExecutedAt.Hour(), txns[latest].ExecutedAt.Minute(), txns[latest].ExecutedAt.Second(), 0, time.UTC)
	txns[latest].ExecutedAt = snapped
	txns[latest].CreatedAt = snapped

	return txns
}
