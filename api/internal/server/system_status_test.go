package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// stubEngineHealth reports whatever the test wants the engine to be doing.
type stubEngineHealth struct{ err error }

func (h *stubEngineHealth) CheckHealth(context.Context) error { return h.err }

func fetchStatus(t *testing.T, s *Server, query string) SystemStatus {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status"+query, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var body SystemStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body: %s", err, rec.Body.String())
	}
	return body
}

func componentByName(t *testing.T, status SystemStatus, name string) ComponentStatus {
	t.Helper()
	for _, c := range status.Components {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("component %q missing from status", name)
	return ComponentStatus{}
}

// TestSystemStatus_FailingDatabaseIsNotReady is the defect #83 describes:
// System reported the same thing whether the database answered or not.
func TestSystemStatus_FailingDatabaseIsNotReady(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		DB:           &stubDBPinger{err: errors.New("connection refused")},
	})

	status := fetchStatus(t, s, "")

	db := componentByName(t, status, "database")
	if db.OperationalState != OperationalUnavailable {
		t.Errorf("database operational_state = %q, want %q", db.OperationalState, OperationalUnavailable)
	}
	if !db.Configured {
		t.Error("database configured = false, but a pool was supplied")
	}
	if db.ReasonCode != reasonCheckFailed {
		t.Errorf("reason_code = %q, want %q", db.ReasonCode, reasonCheckFailed)
	}
}

// TestSystemStatus_RedactsDependencyDetail keeps the /healthz/ready rule: a
// pgx error formats the host, user and database name.
func TestSystemStatus_RedactsDependencyDetail(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		DB:           &stubDBPinger{err: errors.New("failed to connect to `host=db.internal user=merlon database=merlon`")},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	for _, secret := range []string{"db.internal", "user=merlon", "failed to connect"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("status body leaked %q; dependency errors belong in the log, not the response", secret)
		}
	}
}

// TestSystemStatus_UnconfiguredIsUnknownNotReady: a deployment without a
// database is not a broken deployment, and it is not a healthy database.
func TestSystemStatus_UnconfiguredIsUnknownNotReady(t *testing.T) {
	s := testServerFull()

	status := fetchStatus(t, s, "")

	db := componentByName(t, status, "database")
	if db.Configured {
		t.Error("database configured = true without a pool")
	}
	if db.OperationalState != OperationalUnknown {
		t.Errorf("database operational_state = %q, want %q", db.OperationalState, OperationalUnknown)
	}
	if db.ReasonCode != reasonNotConfigured {
		t.Errorf("reason_code = %q, want %q", db.ReasonCode, reasonNotConfigured)
	}
}

// TestSystemStatus_EngineWithoutAProbeIsUnknown: an engine that exposes no
// health check has not been observed. Calling it ready would be an assertion
// nothing supports.
func TestSystemStatus_EngineWithoutAProbeIsUnknown(t *testing.T) {
	s := testServerFull()

	engineStatus := componentByName(t, fetchStatus(t, s, ""), "engine")

	if engineStatus.OperationalState != OperationalUnknown {
		t.Errorf("engine operational_state = %q, want %q", engineStatus.OperationalState, OperationalUnknown)
	}
	if engineStatus.ReasonCode != reasonNoProbeAvailable {
		t.Errorf("reason_code = %q, want %q", engineStatus.ReasonCode, reasonNoProbeAvailable)
	}
	if !engineStatus.Configured {
		t.Error("engine configured = false, but scoring and monitoring engines are wired")
	}
}

func TestSystemStatus_HealthyEngineIsReady(t *testing.T) {
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		Scoring:      &engine.MockScoringEngine{Score: 1, Tier: domain.RiskTierLow},
		EngineHealth: &stubEngineHealth{},
	})

	engineStatus := componentByName(t, fetchStatus(t, s, ""), "engine")
	if engineStatus.OperationalState != OperationalReady {
		t.Errorf("engine operational_state = %q, want %q", engineStatus.OperationalState, OperationalReady)
	}
}

// TestSystemStatus_CarriesActiveConfigurationProvenance: the digests existed on
// the server since Wave 1 and no screen ever showed them.
func TestSystemStatus_CarriesActiveConfigurationProvenance(t *testing.T) {
	s := New(":0", Deps{
		Customers:      store.NewMemoryCustomerRepo(),
		Transactions:   store.NewMemoryTransactionRepo(),
		Alerts:         store.NewMemoryAlertRepo(),
		Cases:          store.NewMemoryCaseRepo(),
		ConfigDigests:  map[string]string{"tm_scenarios": "abc123"},
		TMBaseCurrency: "JPY",
	})

	status := fetchStatus(t, s, "")

	if status.ConfigDigests["tm_scenarios"] != "abc123" {
		t.Errorf("config_digests = %v, want the loaded digest", status.ConfigDigests)
	}
	if status.BaseCurrency != "JPY" {
		t.Errorf("base_currency = %q, want JPY", status.BaseCurrency)
	}
	if status.Version == "" {
		t.Error("version is empty; a record-producing system must identify its build")
	}
	if len(status.Policies) == 0 {
		t.Error("no policy provenance reported; the in-code defaults are still a fact about this deployment")
	}
	for _, p := range status.Policies {
		if p.Source != "default" && p.Source != "file" {
			t.Errorf("policy %q source = %q, want file or default", p.Name, p.Source)
		}
	}
	if status.AuthMode != AuthModeDisabled {
		t.Errorf("auth_mode = %q, want %q for a deployment with no API key store", status.AuthMode, AuthModeDisabled)
	}
}

// TestSystemStatus_CachedAnswerSaysSoAndRefreshForcesALiveCheck: an operator
// who cannot tell a cached green from a fresh one has learned nothing.
func TestSystemStatus_CachedAnswerSaysSoAndRefreshForcesALiveCheck(t *testing.T) {
	pinger := &stubDBPinger{}
	s := New(":0", Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
		Cases:        store.NewMemoryCaseRepo(),
		DB:           pinger,
	})

	first := fetchStatus(t, s, "")
	if first.Source != "live" {
		t.Errorf("first source = %q, want live", first.Source)
	}
	if !first.ExpiresAt.After(first.CheckedAt) {
		t.Error("expires_at is not after checked_at, so the answer carries no age")
	}

	second := fetchStatus(t, s, "")
	if second.Source != "cached" {
		t.Errorf("second source = %q, want cached", second.Source)
	}

	// A dependency that has since failed must be visible on demand rather than
	// after the cache happens to expire.
	pinger.err = errors.New("connection refused")
	refreshed := fetchStatus(t, s, "?refresh=true")
	if refreshed.Source != "live" {
		t.Errorf("refreshed source = %q, want live", refreshed.Source)
	}
	if componentByName(t, refreshed, "database").OperationalState != OperationalUnavailable {
		t.Error("refresh=true reused a cached result while the database was failing")
	}
}

// TestSystemInfo_KeepsItsExistingContract: /system/info is consumed by AuthGate
// and the demo badge. Extending status must not change it.
func TestSystemInfo_KeepsItsExistingContract(t *testing.T) {
	s := testServerFull()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body struct {
		Version   string          `json:"version"`
		Endpoints int             `json:"endpoints"`
		Features  map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version == "" || body.Endpoints != s.routeCount || body.Features == nil {
		t.Errorf("/system/info contract changed: %+v", body)
	}
}
