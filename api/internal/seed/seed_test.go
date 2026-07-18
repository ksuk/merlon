package seed

import (
	"context"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestSeedPopulatesAllStores(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	transactions := store.NewMemoryTransactionRepo()
	alerts := store.NewMemoryAlertRepo()
	cases := store.NewMemoryCaseRepo()
	audit := store.NewMemoryAuditRepo()

	Run(context.Background(), Repos{
		Customers:    customers,
		Transactions: transactions,
		Alerts:       alerts,
		Cases:        cases,
		Audit:        audit,
	})

	custs, err := customers.List(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if len(custs) != 5 {
		t.Errorf("expected 5 customers, got %d", len(custs))
	}

	c, err := customers.Get(context.Background(), "cust-003")
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	if c.CountryCode != "HK" {
		t.Errorf("expected HK, got %s", c.CountryCode)
	}
	if c.RiskTier == nil || string(*c.RiskTier) != "high" {
		t.Errorf("expected high risk tier for cust-003")
	}

	txns, err := transactions.ListByCustomer(context.Background(), "cust-004", 100, 0)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(txns) != 3 {
		t.Errorf("expected 3 transactions for cust-004, got %d", len(txns))
	}

	openAlerts, err := alerts.ListOpen(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if len(openAlerts) < 2 {
		t.Errorf("expected at least 2 open alerts, got %d", len(openAlerts))
	}

	openCases, err := cases.ListOpen(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("list cases: %v", err)
	}
	if len(openCases) < 1 {
		t.Errorf("expected at least 1 case, got %d", len(openCases))
	}

	logs, err := audit.List(context.Background(), domain.AuditListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(logs) < 5 {
		t.Errorf("expected at least 5 audit entries, got %d", len(logs))
	}
}

// TestSeedScoresUseNativeEngineScale pins the seeded customer risk scores and
// tiers to the native engine's 1-5 CDD scale (see
// content/_sample/cdd_weights/crypto_exchange.yaml tier_thresholds: low<2.0,
// medium 2.0-3.5, high>=3.5). The expected values are the engine's actual
// computed scores for these customers (measured via POST /customers/{id}/score
// against that preset), not arbitrary values that merely satisfy the
// thresholds, so a demo re-score never contradicts the displayed seed data.
func TestSeedScoresUseNativeEngineScale(t *testing.T) {
	const (
		lowMax    = 2.0
		mediumMax = 3.5
	)
	tierFor := func(score float64) domain.RiskTier {
		switch {
		case score < lowMax:
			return domain.RiskTierLow
		case score < mediumMax:
			return domain.RiskTierMedium
		default:
			return domain.RiskTierHigh
		}
	}

	customers := store.NewMemoryCustomerRepo()
	Run(context.Background(), Repos{
		Customers:    customers,
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		Audit:        store.NewMemoryAuditRepo(),
	})

	cases := []struct {
		id        string
		wantScore float64
		wantTier  domain.RiskTier
	}{
		{"cust-001", 2.65, domain.RiskTierMedium},
		{"cust-002", 2.65, domain.RiskTierMedium},
		{"cust-003", 4.6, domain.RiskTierHigh},
		{"cust-004", 2.65, domain.RiskTierMedium},
		{"cust-005", 3.2, domain.RiskTierMedium},
	}

	for _, tc := range cases {
		c, err := customers.Get(context.Background(), tc.id)
		if err != nil {
			t.Fatalf("get customer %s: %v", tc.id, err)
		}
		if c.RiskScore == nil {
			t.Fatalf("expected risk score for %s", tc.id)
		}
		if *c.RiskScore != tc.wantScore {
			t.Errorf("%s: expected score %v, got %v", tc.id, tc.wantScore, *c.RiskScore)
		}
		if *c.RiskScore < 0 || *c.RiskScore > 5 {
			t.Errorf("%s: score %v out of native engine's 1-5 scale", tc.id, *c.RiskScore)
		}
		if c.RiskTier == nil {
			t.Fatalf("expected risk tier for %s", tc.id)
		}
		if *c.RiskTier != tc.wantTier {
			t.Errorf("%s: expected tier %v, got %v", tc.id, tc.wantTier, *c.RiskTier)
		}
		if derived := tierFor(*c.RiskScore); derived != *c.RiskTier {
			t.Errorf("%s: tier %v is inconsistent with score %v (threshold-derived tier: %v)",
				tc.id, *c.RiskTier, *c.RiskScore, derived)
		}
	}
}
