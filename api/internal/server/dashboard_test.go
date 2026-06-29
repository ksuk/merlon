package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
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
