package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// stubDBPinger is a test double for the DBPinger dependency /healthz/ready
// uses to check PostgreSQL connectivity (Task 3, the operational design §4.4 "ヘルス
// チェックの粒度").
type stubDBPinger struct {
	err error
}

func (p *stubDBPinger) Ping(_ context.Context) error {
	return p.err
}

func testServer() *Server {
	return testServerWithEngines(
		&engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		&engine.MockMonitoringEngine{},
		&engine.MockScreeningEngine{},
	)
}

func testServerFull() *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		Audit:        store.NewMemoryAuditRepo(),
		Cases:        store.NewMemoryCaseRepo(),
	})
}

func testServerWithEngine(scoring engine.ScoringEngine, monitoring engine.MonitoringEngine) *Server {
	return testServerWithEngines(scoring, monitoring, &engine.MockScreeningEngine{})
}

func testServerWithEngines(scoring engine.ScoringEngine, monitoring engine.MonitoringEngine, screening engine.ScreeningEngine) *Server {
	return New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      scoring,
		Monitoring:   monitoring,
		Screening:    screening,
		Backtest:     &engine.MockBacktestEngine{},
	})
}

func TestHandleHealth(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	if body := rec.Body.String(); !strings.Contains(body, "ok") {
		t.Errorf("body = %q, want it to contain %q", body, "ok")
	}
}

func TestHandleHealth_EngineUnreachable(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		EngineHealth: &engine.MockHealthChecker{Err: errors.New("engine unreachable")},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleHealth_EngineHealthy(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Scoring:      &engine.MockScoringEngine{},
		Monitoring:   &engine.MockMonitoringEngine{},
		Screening:    &engine.MockScreeningEngine{},
		Backtest:     &engine.MockBacktestEngine{},
		EngineHealth: &engine.MockHealthChecker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestHealthzLive_AlwaysHealthyRegardlessOfSetup is acceptance criterion 1:
// /healthz/live must report healthy even before initial setup completes.
func TestHealthzLive_AlwaysHealthyRegardlessOfSetup(t *testing.T) {
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     store.NewMemoryUserRepo(), // empty: setup not completed
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz/live", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHealthzReady_UnhealthyBeforeSetup(t *testing.T) {
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     store.NewMemoryUserRepo(),
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d, body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestHealthzReady_HealthyAfterSetup(t *testing.T) {
	users := store.NewMemoryUserRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     users,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(`{"email":"admin@example.com","password":"correct-horse-battery-staple"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestHealthzReadyUnhealthyOnDBDown is acceptance criterion 2: DB down makes
// /healthz/ready unhealthy while /healthz/live stays healthy.
func TestHealthzReadyUnhealthyOnDBDown(t *testing.T) {
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		DB:        &stubDBPinger{err: errors.New("connection refused")},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d, body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "postgres") {
		t.Errorf("body = %s, want it to mention the postgres check", rec.Body.String())
	}

	liveReq := httptest.NewRequest(http.MethodGet, "/healthz/live", nil)
	liveRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(liveRec, liveReq)

	if liveRec.Code != http.StatusOK {
		t.Errorf("/healthz/live status code = %d, want %d (DB down must not affect liveness)", liveRec.Code, http.StatusOK)
	}
}

// TestHealthzReadyHealthyWhenAllDepsUp is acceptance criterion 3: DB and
// engine both reachable makes /healthz/ready healthy.
func TestHealthzReadyHealthyWhenAllDepsUp(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		DB:           &stubDBPinger{},
		EngineHealth: &engine.MockHealthChecker{},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"postgres":"ok"`) {
		t.Errorf("body = %s, want postgres check reported ok", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"engine":"ok"`) {
		t.Errorf("body = %s, want engine check reported ok", rec.Body.String())
	}
}

// TestHealthProbesDoNotLeakDependencyErrors guards the consequence of these
// probes being unauthenticated: auth.go exempts /healthz, /healthz/live and
// /healthz/ready, and the OpenAPI document declares them with an empty
// security requirement, so whatever these bodies contain is readable by anyone
// who can reach the port. A pgx connection error is formatted "failed to
// connect to `host=... user=... database=...`" and engine errors carry
// configuration file paths, so neither may be echoed back.
func TestHealthProbesDoNotLeakDependencyErrors(t *testing.T) {
	const dbError = "failed to connect to `host=db.internal user=merlon database=merlon`: dial tcp 10.0.0.7:5432: connect: connection refused"
	const engineError = "load config /etc/merlon/content/cdd_weights.json: no such file or directory"

	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Users:        store.NewMemoryUserRepo(),
		DB:           &stubDBPinger{err: errors.New(dbError)},
		EngineHealth: &engine.MockHealthChecker{Err: errors.New(engineError)},
	})

	for _, path := range []string{"/healthz", "/healthz/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		body := rec.Body.String()
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s status code = %d, want %d, body: %s", path, rec.Code, http.StatusServiceUnavailable, body)
		}
		for _, leaked := range []string{dbError, engineError, "host=", "10.0.0.7", "/etc/merlon"} {
			if strings.Contains(body, leaked) {
				t.Errorf("%s body leaks %q: %s", path, leaked, body)
			}
		}
	}

	// Redaction must not cost operators the one thing the probe is for:
	// knowing which dependency is down.
	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	for _, want := range []string{`"postgres":"error"`, `"engine":"error"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body = %s, want it to contain %s", rec.Body.String(), want)
		}
	}
}

// TestHealthzReadyUnhealthyOnEngineDown ensures the engine check still gates
// readiness independently of the DB check (regression guard for the
// checks-map refactor).
func TestHealthzReadyUnhealthyOnEngineDown(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		DB:           &stubDBPinger{},
		EngineHealth: &engine.MockHealthChecker{Err: errors.New("engine unreachable")},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d, body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}
