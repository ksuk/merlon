package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/screening"
	"github.com/merlon-aml/merlon/api/internal/store"
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
