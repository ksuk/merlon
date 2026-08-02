package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/screening"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestDashboardEmpty(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var stats domain.DashboardStats
	json.NewDecoder(rec.Body).Decode(&stats)

	if stats.TotalCustomers != 0 {
		t.Errorf("total_customers = %d, want 0", stats.TotalCustomers)
	}
	if stats.TotalAlerts != 0 {
		t.Errorf("total_alerts = %d, want 0", stats.TotalAlerts)
	}
}

func TestDashboardWithData(t *testing.T) {
	s := testServerFull()

	body := `{"external_id":"DASH001","customer_type":"individual","country_code":"JP","product_types":["spot_trading"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	body = `{"external_id":"DASH002","customer_type":"corporate_domestic","country_code":"US","product_types":["margin_trading"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var stats domain.DashboardStats
	json.NewDecoder(rec.Body).Decode(&stats)

	if stats.TotalCustomers != 2 {
		t.Errorf("total_customers = %d, want 2", stats.TotalCustomers)
	}
	if stats.CustomersByRiskTier["unscored"] != 2 {
		t.Errorf("unscored = %d, want 2", stats.CustomersByRiskTier["unscored"])
	}
}

func TestDashboardAggregatesBeyondTenThousandAndUsesTerminalCaseDefinition(t *testing.T) {
	s := testServerFull()
	customers := s.customers.(*store.MemoryCustomerRepo)
	ctx := context.Background()
	for i := 0; i < 10001; i++ {
		id := fmt.Sprintf("dashboard-customer-%05d", i)
		if err := customers.Create(ctx, &domain.Customer{
			ID: id, ExternalID: id, CustomerType: domain.CustomerTypeIndividual,
			CountryCode: "JP", CreatedAt: time.Unix(int64(i), 0).UTC(), UpdatedAt: time.Unix(int64(i), 0).UTC(),
		}); err != nil {
			t.Fatalf("create customer %d: %v", i, err)
		}
	}

	cases := s.cases.(*store.MemoryCaseRepo)
	for i, status := range []domain.CaseStatus{domain.CaseStatusInvestigating, domain.CaseStatusClosed, domain.CaseStatusStrFiled} {
		if err := cases.Create(ctx, &domain.Case{
			ID: fmt.Sprintf("dashboard-case-%d", i), CustomerID: "dashboard-customer-00000",
			Status: status, Priority: domain.CasePriorityMedium, Summary: "dashboard test",
		}); err != nil {
			t.Fatalf("create case %d: %v", i, err)
		}
	}
	alerts := s.alerts.(*store.MemoryAlertRepo)
	for i, status := range []domain.AlertStatus{domain.AlertStatusOpen, domain.AlertStatusInvestigating, domain.AlertStatusClosedFalsePositive} {
		if err := alerts.Create(ctx, &domain.Alert{
			ID: fmt.Sprintf("dashboard-alert-%d", i), CustomerID: "dashboard-customer-00000",
			Status: status, Severity: []domain.AlertSeverity{domain.AlertSeverityCritical, domain.AlertSeverityHigh, domain.AlertSeverityLow}[i],
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("create alert %d: %v", i, err)
		}
	}
	transactions := s.transactions.(*store.MemoryTransactionRepo)
	for i, executedAt := range []time.Time{time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(-25 * time.Hour)} {
		if err := transactions.Create(ctx, &domain.Transaction{
			ID: fmt.Sprintf("dashboard-transaction-%d", i), CustomerID: "dashboard-customer-00000",
			ExternalID: fmt.Sprintf("dashboard-external-%d", i), Currency: "JPY", Direction: domain.DirectionInbound,
			Amount: 100, ExecutedAt: executedAt, CreatedAt: executedAt,
		}); err != nil {
			t.Fatalf("create transaction %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var stats domain.DashboardStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.TotalCustomers != 10001 {
		t.Errorf("total_customers = %d, want 10001", stats.TotalCustomers)
	}
	if stats.TotalCases != 1 || stats.CasesByStatus[string(domain.CaseStatusInvestigating)] != 1 {
		t.Errorf("case totals = %d/%v, want one investigating case only", stats.TotalCases, stats.CasesByStatus)
	}
	if stats.TotalAlerts != 2 || stats.AlertsBySeverity[string(domain.AlertSeverityCritical)] != 1 {
		t.Errorf("alert totals = %d/%v, want two unresolved alerts with one critical", stats.TotalAlerts, stats.AlertsBySeverity)
	}
	if stats.RecentTransactions != 1 {
		t.Errorf("recent_transactions = %d, want one transaction in the last 24 hours", stats.RecentTransactions)
	}
	if stats.RecentTransactionsWindowHours != 24 {
		t.Errorf("recent_transactions_window_hours = %d, want 24", stats.RecentTransactionsWindowHours)
	}
}

func TestDashboard_ReportsScreeningListFreshness(t *testing.T) {
	listStore := screening.NewMemoryListStore()
	failureTracker := screening.NewMemoryFailureTracker()
	ctx := context.Background()

	if err := listStore.SaveList(ctx, &screening.RawListData{ListID: "ofac_sdn", ListType: "sanctions"}); err != nil {
		t.Fatalf("SaveList: %v", err)
	}
	if err := failureTracker.RecordSuccess(ctx, "ofac_sdn"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}

	s := New(":0", Deps{
		Customers:               store.NewMemoryCustomerRepo(),
		Alerts:                  store.NewMemoryAlertRepo(),
		Cases:                   store.NewMemoryCaseRepo(),
		Screening:               &engine.MockScreeningEngine{},
		ScreeningListStore:      listStore,
		ScreeningFailureTracker: failureTracker,
		ScreeningListIDs:        []string{"ofac_sdn", "pep_provider"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var stats domain.DashboardStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(stats.ScreeningListFreshness) != 1 {
		t.Fatalf("ScreeningListFreshness = %+v, want exactly 1 entry (pep_provider never imported is omitted)", stats.ScreeningListFreshness)
	}
	got := stats.ScreeningListFreshness[0]
	if got.ListID != "ofac_sdn" || got.ListType != "sanctions" || got.StaleDays != 0 {
		t.Errorf("entry = %+v, want fresh ofac_sdn", got)
	}
}
