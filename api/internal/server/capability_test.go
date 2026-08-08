package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// capabilityBody is the decoded GET /api/v1/system/capabilities response.
type capabilityBody struct {
	AuthMode    string                 `json:"auth_mode"`
	Role        string                 `json:"role"`
	Permissions []string               `json:"permissions"`
	Data        []CapabilityDescriptor `json:"data"`
}

func fetchCapabilities(t *testing.T, s *Server, cookies []*http.Cookie) capabilityBody {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /system/capabilities status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body capabilityBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode capabilities: %v; body: %s", err, rec.Body.String())
	}
	if len(body.Data) == 0 {
		t.Fatal("capabilities response carried no descriptors; the catalog must never be empty")
	}
	return body
}

func capabilityByID(t *testing.T, body capabilityBody, id string) CapabilityDescriptor {
	t.Helper()
	for _, d := range body.Data {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("capability %q missing from the catalog", id)
	return CapabilityDescriptor{}
}

// TestCapabilities_AuthDisabledStatesTheModeAndGrantsConfiguredWork asserts
// ADR-0024 DR-17: a deployment without authentication has no roles at all, so
// guessing one is worse than naming the fact. Every configured capability is
// available and auth_mode says why.
func TestCapabilities_AuthDisabledStatesTheModeAndGrantsConfiguredWork(t *testing.T) {
	s := testServerFull()

	body := fetchCapabilities(t, s, nil)

	if body.AuthMode != string(AuthModeDisabled) {
		t.Errorf("auth_mode = %q, want %q; a deployment that authenticates nobody must say so rather than look like a normal session", body.AuthMode, AuthModeDisabled)
	}
	if body.Role != "" {
		t.Errorf("role = %q, want empty; no role exists when authentication is disabled", body.Role)
	}

	// audit:read and cdd:score are permission-gated, and their dependencies are
	// wired in testServerFull. With no authentication there is no role to
	// refuse them with.
	for _, id := range []string{"audit.read", "cdd.score"} {
		got := capabilityByID(t, body, id)
		if got.Availability != CapabilityAvailable {
			t.Errorf("%s availability = %q, want %q with authentication disabled (reason %q)", id, got.Availability, CapabilityAvailable, got.ReasonCode)
		}
	}
}

// TestCapabilities_UnconfiguredDependencyIsNotConfiguredNotAvailable pins the
// boundary of "auth disabled grants everything": a permission we cannot refuse
// is not the same as a feature this deployment has. Reporting an absent
// dependency as available would be the class of untruth Wave 4 exists to remove.
func TestCapabilities_UnconfiguredDependencyIsNotConfiguredNotAvailable(t *testing.T) {
	s := testServerFull() // no API keys, users, webhooks, rules or config engine

	body := fetchCapabilities(t, s, nil)

	for _, id := range []string{"api_keys.manage", "users.manage", "webhooks.manage", "rules.write", "config.validate"} {
		got := capabilityByID(t, body, id)
		if got.Availability != CapabilityNotConfigured {
			t.Errorf("%s availability = %q, want %q; the dependency is absent from this deployment", id, got.Availability, CapabilityNotConfigured)
		}
		if got.ReasonCode == "" {
			t.Errorf("%s is unavailable without a reason_code; an operator cannot act on a bare absence", id)
		}
	}
}

// TestCapabilities_ForbiddenWithoutThePermission asserts a role that lacks a
// permission is told so explicitly, rather than the control simply vanishing.
func TestCapabilities_ForbiddenWithoutThePermission(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "viewer@example.com", testUserPassword, domain.RoleViewer)

	rec := doLogin(t, s, "viewer@example.com", testUserPassword)
	body := fetchCapabilities(t, s, rec.Result().Cookies())

	if body.AuthMode != string(AuthModeSession) {
		t.Errorf("auth_mode = %q, want %q", body.AuthMode, AuthModeSession)
	}
	if body.Role != string(domain.RoleViewer) {
		t.Errorf("role = %q, want %q", body.Role, domain.RoleViewer)
	}
	if len(body.Permissions) != 0 {
		t.Errorf("permissions = %v, want none for viewer", body.Permissions)
	}

	got := capabilityByID(t, body, "audit.read")
	if got.Availability != CapabilityForbidden {
		t.Errorf("audit.read availability = %q, want %q for a viewer", got.Availability, CapabilityForbidden)
	}
	if got.RequiredPermission != string(auth.PermAuditRead) {
		t.Errorf("audit.read required_permission = %q, want %q", got.RequiredPermission, auth.PermAuditRead)
	}
}

// TestCapabilities_AdminHoldsEveryPermissionedCapability checks the derivation
// from auth.RolePermissions rather than a second, hand-maintained matrix.
func TestCapabilities_AdminHoldsEveryPermissionedCapability(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)

	rec := doLogin(t, s, "admin@example.com", testUserPassword)
	body := fetchCapabilities(t, s, rec.Result().Cookies())

	if len(body.Permissions) != len(auth.RolePermissions[domain.RoleAdmin]) {
		t.Errorf("permissions count = %d, want %d (the admin grants in auth.RolePermissions)", len(body.Permissions), len(auth.RolePermissions[domain.RoleAdmin]))
	}

	for _, d := range body.Data {
		if d.Availability == CapabilityForbidden {
			t.Errorf("%s is forbidden for admin, but admin holds every permission in auth.RolePermissions", d.ID)
		}
	}
}

// TestCapabilityCatalogIsDerivedFromRolePermissions guards ADR-0024's decision
// not to create a second permission vocabulary: every permission a capability
// names must be one the authorization model actually grants somewhere.
func TestCapabilityCatalogIsDerivedFromRolePermissions(t *testing.T) {
	granted := map[auth.Permission]bool{}
	for _, perms := range auth.RolePermissions {
		for _, p := range perms {
			granted[p] = true
		}
	}

	seen := map[string]bool{}
	for _, c := range capabilityCatalog {
		if seen[c.id] {
			t.Errorf("capability %q is declared twice", c.id)
		}
		seen[c.id] = true

		if c.permission != "" && !granted[c.permission] {
			t.Errorf("capability %q requires permission %q, which no role in auth.RolePermissions grants", c.id, c.permission)
		}
		if len(c.surfaces) == 0 {
			t.Errorf("capability %q declares no surface; an operator cannot tell whether it is a screen or an endpoint", c.id)
		}
		if c.docsURL == "" {
			t.Errorf("capability %q has no docs_url; an API-only capability without documentation is an unexplained omission", c.id)
		}
	}
}

// TestAuthMe_ReportsAuthModeRolesAndPermissions asserts the additive extension
// of the existing /auth/me contract (CAP-01).
func TestAuthMe_ReportsAuthModeRolesAndPermissions(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "analyst@example.com", testUserPassword, domain.RoleAnalyst)

	login := doLogin(t, s, "analyst@example.com", testUserPassword)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/me status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		ID          string   `json:"id"`
		Email       string   `json:"email"`
		Role        string   `json:"role"`
		AuthMode    string   `json:"auth_mode"`
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /auth/me: %v", err)
	}

	// The pre-existing fields must survive unchanged (12-month contract stability).
	if body.Email != "analyst@example.com" || body.Role != string(domain.RoleAnalyst) || body.ID == "" {
		t.Errorf("existing /auth/me fields changed: %+v", body)
	}
	if body.AuthMode != string(AuthModeSession) {
		t.Errorf("auth_mode = %q, want %q", body.AuthMode, AuthModeSession)
	}
	if len(body.Roles) != 1 || body.Roles[0] != string(domain.RoleAnalyst) {
		t.Errorf("roles = %v, want [analyst]", body.Roles)
	}
	if len(body.Permissions) != len(auth.RolePermissions[domain.RoleAnalyst]) {
		t.Errorf("permissions = %v, want the %d analyst grants", body.Permissions, len(auth.RolePermissions[domain.RoleAnalyst]))
	}
}

// TestCapabilities_ReportedAvailabilityMatchesRouteEnforcement is the point of
// the whole contract: a capability reported as available must actually be
// reachable. Before Wave 4 the rule-write and export routes used the strict
// permission middleware, so an auth-disabled deployment answered 403 for work
// the capability contract now calls available.
func TestCapabilities_ReportedAvailabilityMatchesRouteEnforcement(t *testing.T) {
	// No API key store: authentication is disabled, exactly as a single-tenant
	// evaluation deployment runs.
	s := New(":0", Deps{
		Customers:          store.NewMemoryCustomerRepo(),
		Transactions:       store.NewMemoryTransactionRepo(),
		Alerts:             store.NewMemoryAlertRepo(),
		Cases:              store.NewMemoryCaseRepo(),
		Audit:              store.NewMemoryAuditRepo(),
		Rules:              store.NewMemoryRuleRepo(),
		PendingEvaluations: store.NewMemoryPendingEvaluationRepo(),
		Config:             &engine.MockConfigEngine{},
	})

	body := fetchCapabilities(t, s, nil)
	if body.AuthMode != string(AuthModeDisabled) {
		t.Fatalf("auth_mode = %q, want %q", body.AuthMode, AuthModeDisabled)
	}
	if got := capabilityByID(t, body, "rules.write"); got.Availability != CapabilityAvailable {
		t.Fatalf("rules.write availability = %q, want %q", got.Availability, CapabilityAvailable)
	}

	// The permission gate must not be what stops this request. A 403 here means
	// the capability contract advertised authority the route refuses.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Errorf("POST /rules returned 403 in an auth-disabled deployment, but capabilities reported rules.write as available")
	}

	for _, path := range []string{"/api/v1/audit/export", "/api/v1/pending-evaluations/export"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Errorf("GET %s returned 403 in an auth-disabled deployment; the paginated list of the same records is open, so the export gate stopped nothing while looking like a control", path)
		}
	}
}

// TestCapabilities_AuthenticatedDeploymentStillEnforcesTheExportGate is the
// other half of the change above: relaxing the roleless case must not relax the
// authenticated one.
func TestCapabilities_AuthenticatedDeploymentStillEnforcesTheExportGate(t *testing.T) {
	s := testServerWithAuth()
	viewerKey := createAPIKey(t, s, "capability-viewer", domain.RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export", nil)
	req.Header.Set("Authorization", "Bearer "+viewerKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /audit/export status = %d, want 403 for a viewer in an authenticated deployment", rec.Code)
	}
}

// TestCapabilities_APIKeyOnlyDeploymentIsDistinguishable covers the third
// authentication state main.go can produce: API keys configured but no token
// issuer, so no user can log in. Reporting it as "session" would tell an
// operator to look for a login that does not exist.
func TestCapabilities_APIKeyOnlyDeploymentIsDistinguishable(t *testing.T) {
	s := testServerWithAuth()
	adminKey := createAPIKey(t, s, "capability-admin", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body capabilityBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.AuthMode != string(AuthModeAPIKeyOnly) {
		t.Errorf("auth_mode = %q, want %q", body.AuthMode, AuthModeAPIKeyOnly)
	}
}
