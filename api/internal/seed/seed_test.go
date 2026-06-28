package seed

import (
	"context"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/store"
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

	logs, err := audit.List(context.Background(), "", "", 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(logs) < 5 {
		t.Errorf("expected at least 5 audit entries, got %d", len(logs))
	}
}
