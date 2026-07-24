package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestRateLimiting(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		RateLimit:    3,
	})

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request 4: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	if rec.Header().Get("X-RateLimit-Limit") != "3" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", rec.Header().Get("X-RateLimit-Limit"), "3")
	}
}

func TestRateLimitHealthzExempt(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		RateLimit:    1,
	})

	// Use the one allowed request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	// Healthz should still work
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimitIgnoresSpoofedForwardedForByDefault(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		RateLimit:    1,
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		if i == 0 && rec.Code != http.StatusOK {
			t.Fatalf("first request status = %d, want %d", rec.Code, http.StatusOK)
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second request status = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}
	}
}

func TestRateLimitSeparatesClientsBehindTrustedProxy(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		RateLimit:    1,
		TrustedProxyCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/24"),
		},
	})

	request := func(client string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
		req.RemoteAddr = "10.0.0.2:443"
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}

	if got := request("198.51.100.10"); got != http.StatusOK {
		t.Fatalf("first client status = %d, want %d", got, http.StatusOK)
	}
	if got := request("198.51.100.11"); got != http.StatusOK {
		t.Fatalf("second client status = %d, want %d", got, http.StatusOK)
	}
	if got := request("198.51.100.10"); got != http.StatusTooManyRequests {
		t.Fatalf("repeated first client status = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestRateLimiterSweepsExpiredClientBuckets(t *testing.T) {
	limiter := newRateLimiter(2, time.Minute)
	start := time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC)

	if allowed, _ := limiter.allowAt("expired-client", start); !allowed {
		t.Fatal("first request should be allowed")
	}
	if allowed, _ := limiter.allowAt("active-client", start.Add(2*time.Minute)); !allowed {
		t.Fatal("request after sweep should be allowed")
	}

	if _, ok := limiter.requests["expired-client"]; ok {
		t.Fatal("expired client bucket was not removed")
	}
	if _, ok := limiter.requests["active-client"]; !ok {
		t.Fatal("active client bucket was removed")
	}
}

func TestRateLimiterConcurrentAccess(t *testing.T) {
	limiter := newRateLimiter(100, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.allow("client")
		}()
	}
	wg.Wait()

	if got := len(limiter.requests["client"]); got != 100 {
		t.Errorf("recorded requests = %d, want 100", got)
	}
}
