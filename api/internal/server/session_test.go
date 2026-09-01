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

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

const testUserPassword = "correct-horse-battery-staple"

func testServerWithSessions(t *testing.T) (*Server, domain.UserRepository, domain.AuditRepository) {
	t.Helper()

	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}

	users := store.NewMemoryUserRepo()
	auditRepo := store.NewMemoryAuditRepo()

	s := New(":0", Deps{
		Customers:      store.NewMemoryCustomerRepo(),
		Transactions:   store.NewMemoryTransactionRepo(),
		Alerts:         store.NewMemoryAlertRepo(),
		Scoring:        &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium},
		Monitoring:     &engine.MockMonitoringEngine{},
		Screening:      &engine.MockScreeningEngine{},
		Backtest:       &engine.MockBacktestEngine{},
		Audit:          auditRepo,
		Cases:          store.NewMemoryCaseRepo(),
		APIKeys:        store.NewMemoryAPIKeyRepo(),
		BootstrapToken: testBootstrapToken,
		TokenIssuer:    issuer,
		Denylist:       auth.NewInMemoryDenylist(),
		Users:          users,
		RefreshTokens:  store.NewMemoryRefreshTokenRepo(),
	})
	return s, users, auditRepo
}

func createTestUser(t *testing.T, users domain.UserRepository, email, password string, role domain.Role) *domain.User {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	u := &domain.User{
		ID:           generateID(),
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	return u
}

func doLogin(t *testing.T, s *Server, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	return rec
}

func attachCookies(req *http.Request, cookies []*http.Cookie) {
	for _, c := range cookies {
		req.AddCookie(c)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLogin_Success(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	rec := doLogin(t, s, "alice@example.com", testUserPassword)

	cookies := rec.Result().Cookies()
	accessCookie := findCookie(cookies, accessTokenCookie)
	if accessCookie == nil {
		t.Fatal("access_token cookie not set")
	}
	if !accessCookie.HttpOnly {
		t.Error("access_token cookie is not HttpOnly")
	}
	if !accessCookie.Secure {
		t.Error("access_token cookie is not Secure")
	}
	if accessCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("access_token cookie SameSite = %v, want Strict", accessCookie.SameSite)
	}

	if findCookie(cookies, refreshTokenCookie) == nil {
		t.Fatal("refresh_token cookie not set")
	}
	if findCookie(cookies, csrfCookieName) == nil {
		t.Fatal("csrf_token cookie not set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	body := `{"email":"alice@example.com","password":"totally-wrong-password"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if strings.Contains(rec.Body.String(), testUserPassword) || strings.Contains(rec.Body.String(), "totally-wrong-password") {
		t.Fatal("response body leaks a plaintext password")
	}
	assertErrorCode(t, rec, "unauthorized")
}

func TestLogin_RecordsAuditEvent(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	doLogin(t, s, "alice@example.com", testUserPassword)

	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasAction(entries, "login_success") {
		t.Fatal("no login_success audit entry recorded")
	}

	body := `{"email":"alice@example.com","password":"wrong-password"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected failed login, got status %d", rec.Code)
	}

	entries, err = auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasAction(entries, "login_failed") {
		t.Fatal("no login_failed audit entry recorded")
	}
}

func hasAction(entries []domain.AuditEntry, action string) bool {
	for _, e := range entries {
		if e.Action == action {
			return true
		}
	}
	return false
}

func TestLogout_RevokesSession(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	cookies := loginRec.Result().Cookies()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, cookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-logout access status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, cookies)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, cookies)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout access status = %d, want %d, body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

type revokeFailingDenylist struct {
	delegate auth.Denylist
}

func (d revokeFailingDenylist) RevokeToken(context.Context, string, time.Duration) error {
	return fmt.Errorf("denylist unavailable")
}

func (d revokeFailingDenylist) RevokeSession(context.Context, string, time.Duration) error {
	return fmt.Errorf("denylist unavailable")
}

func (d revokeFailingDenylist) IsTokenRevoked(ctx context.Context, tokenID string) (bool, error) {
	return d.delegate.IsTokenRevoked(ctx, tokenID)
}

func (d revokeFailingDenylist) IsSessionRevoked(ctx context.Context, sessionID string) (bool, error) {
	return d.delegate.IsSessionRevoked(ctx, sessionID)
}

func TestLogout_DoesNotReportSuccessWhenRevocationCannotBeConfirmed(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	s.denylist = revokeFailingDenylist{delegate: s.denylist}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, loginRec.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	assertErrorCode(t, rec, "internal_error")
	for _, name := range []string{accessTokenCookie, refreshTokenCookie, csrfCookieName} {
		cookie := findCookie(rec.Result().Cookies(), name)
		if cookie == nil || cookie.MaxAge >= 0 {
			t.Errorf("%s cookie was not cleared after failed logout", name)
		}
	}

	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	if !hasAction(entries, "logout_failed") {
		t.Fatal("no logout_failed audit entry recorded")
	}
	if hasAction(entries, "logout") {
		t.Fatal("failed logout was recorded as successful logout")
	}
}

func TestLogout_SucceedsWhenAccessCookieIsInvalidButRefreshFamilyIsRevoked(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	refreshCookie := findCookie(loginRec.Result().Cookies(), refreshTokenCookie)
	if refreshCookie == nil {
		t.Fatal("refresh_token cookie missing after login")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: accessTokenCookie, Value: "invalid-access-token"})
	req.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(refreshCookie)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want %d, body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestLogout_AllowsImmediateRelogin(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	firstLogin := doLogin(t, s, "alice@example.com", testUserPassword)
	firstCookies := firstLogin.Result().Cookies()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, firstCookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	secondLogin := doLogin(t, s, "alice@example.com", testUserPassword)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, secondLogin.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("access after immediate re-login = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestLogout_DoesNotRevokeAConcurrentSession(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	firstLogin := doLogin(t, s, "alice@example.com", testUserPassword)
	secondLogin := doLogin(t, s, "alice@example.com", testUserPassword)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, firstLogin.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, firstLogin.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out session status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, secondLogin.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("concurrent session status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestLogin_EvictsTheOldestAccessAndRefreshSessionAtTheConcurrentLimit(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	logins := make([]*httptest.ResponseRecorder, 0, auth.MaxConcurrentSessions+1)
	for range auth.MaxConcurrentSessions + 1 {
		logins = append(logins, doLogin(t, s, "alice@example.com", testUserPassword))
	}

	oldestCookies := logins[0].Result().Cookies()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, oldestCookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("oldest access session status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	oldestRefresh := findCookie(oldestCookies, refreshTokenCookie)
	if oldestRefresh == nil {
		t.Fatal("oldest session refresh_token cookie missing")
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(oldestRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("oldest refresh session status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	newestCookies := logins[len(logins)-1].Result().Cookies()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, newestCookies)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("newest session status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRevokeUserSessions_RevokesExistingSessionsButNotFutureLogin(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	admin := createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAnalyst)
	firstLogin := doLogin(t, s, "alice@example.com", testUserPassword)
	secondLogin := doLogin(t, s, "alice@example.com", testUserPassword)
	adminLogin := doLogin(t, s, "admin@example.com", testUserPassword)
	adminCookies := adminLogin.Result().Cookies()
	csrfCookie := findCookie(adminCookies, csrfCookieName)
	if csrfCookie == nil {
		t.Fatal("csrf_token cookie missing after admin login")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+user.ID+"/revoke-sessions", nil)
	attachCookies(req, adminCookies)
	req.Header.Set(csrfHeaderName, csrfCookie.Value)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke sessions status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	for index, cookies := range [][]*http.Cookie{firstLogin.Result().Cookies(), secondLogin.Result().Cookies()} {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
		attachCookies(req, cookies)
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("revoked session %d status = %d, want %d", index+1, rec.Code, http.StatusUnauthorized)
		}

		refreshCookie := findCookie(cookies, refreshTokenCookie)
		if refreshCookie == nil {
			t.Fatalf("revoked session %d has no refresh token", index+1)
		}
		req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		req.AddCookie(refreshCookie)
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("revoked refresh session %d status = %d, want %d", index+1, rec.Code, http.StatusUnauthorized)
		}
	}

	futureLogin := doLogin(t, s, "alice@example.com", testUserPassword)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, futureLogin.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("future session status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	if !hasAction(entries, "user_wide_session_revocation") {
		t.Fatal("no user_wide_session_revocation audit entry recorded")
	}
	for _, entry := range entries {
		if entry.Action == "user_wide_session_revocation" {
			if entry.UserID != admin.ID || entry.ResourceID != user.ID {
				t.Fatalf("revocation audit actor/resource = %q/%q, want %q/%q", entry.UserID, entry.ResourceID, admin.ID, user.ID)
			}
		}
	}
}

func TestRevokeUserSessions_RequiresAdminAndKnownUser(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	createTestUser(t, users, "viewer@example.com", testUserPassword, domain.RoleViewer)

	viewerLogin := doLogin(t, s, "viewer@example.com", testUserPassword)
	viewerCookies := viewerLogin.Result().Cookies()
	viewerCSRF := findCookie(viewerCookies, csrfCookieName)
	if viewerCSRF == nil {
		t.Fatal("viewer csrf_token cookie missing")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/missing/revoke-sessions", nil)
	attachCookies(req, viewerCookies)
	req.Header.Set(csrfHeaderName, viewerCSRF.Value)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer revoke status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	adminLogin := doLogin(t, s, "admin@example.com", testUserPassword)
	adminCookies := adminLogin.Result().Cookies()
	adminCSRF := findCookie(adminCookies, csrfCookieName)
	if adminCSRF == nil {
		t.Fatal("admin csrf_token cookie missing")
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/missing/revoke-sessions", nil)
	attachCookies(req, adminCookies)
	req.Header.Set(csrfHeaderName, adminCSRF.Value)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing-user revoke status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSessionLifecycle_RecordsDistinctAuditEvents(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	refreshCookie := findCookie(loginRec.Result().Cookies(), refreshTokenCookie)
	if refreshCookie == nil {
		t.Fatal("refresh_token cookie missing after login")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(refreshCookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, rec.Result().Cookies())
	logoutRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(logoutRec, req)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", logoutRec.Code, http.StatusOK, logoutRec.Body.String())
	}

	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	for _, action := range []string{"login_success", "refresh", "session_revocation", "logout"} {
		if !hasAction(entries, action) {
			t.Errorf("no %s audit entry recorded", action)
		}
	}
}

func TestRefresh_RotatesToken(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	oldRefresh := findCookie(loginRec.Result().Cookies(), refreshTokenCookie)
	if oldRefresh == nil {
		t.Fatal("refresh_token cookie missing after login")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(oldRefresh)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(oldRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("reusing a rotated refresh token succeeded")
	}
	assertErrorCode(t, rec, "unauthorized")
}

func TestRefresh_ReuseDetection_RevokesAllSessions(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	oldRefresh := findCookie(loginRec.Result().Cookies(), refreshTokenCookie)
	if oldRefresh == nil {
		t.Fatal("refresh_token cookie missing after login")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(oldRefresh)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh status = %d, want %d", rec.Code, http.StatusOK)
	}
	rotatedRefresh := findCookie(rec.Result().Cookies(), refreshTokenCookie)
	rotatedAccess := findCookie(rec.Result().Cookies(), accessTokenCookie)
	if rotatedRefresh == nil || rotatedAccess == nil {
		t.Fatal("refresh did not return rotated access and refresh cookies")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.AddCookie(rotatedAccess)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotated access status before reuse = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(oldRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rotatedRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("refresh succeeded using a token from a family that should have been fully revoked")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.AddCookie(rotatedAccess)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("access token from reused family status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	if !hasAction(entries, "session_revocation") {
		t.Fatal("refresh-token reuse did not record session_revocation")
	}
}

func TestMe_ReturnsCurrentUser(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	cookies := loginRec.Result().Cookies()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	attachCookies(req, cookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp meResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Email != "alice@example.com" {
		t.Errorf("email = %s, want alice@example.com", resp.Email)
	}
	if resp.Role != domain.RoleAdmin {
		t.Errorf("role = %s, want %s", resp.Role, domain.RoleAdmin)
	}
}

func TestListUsers_ReturnsAllUsers(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	createTestUser(t, users, "bob@example.com", testUserPassword, domain.RoleAnalyst)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	cookies := loginRec.Result().Cookies()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	attachCookies(req, cookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []domain.User
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(got))
	}
}

// TestAuthFlow_LoginAccessRefreshRotationReuseDetection is the acceptance
// criterion 2 E2E test: login -> access -> refresh -> rotation -> reuse
// detection revokes the whole session family.
func TestAuthFlow_LoginAccessRefreshRotationReuseDetection(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	cookies := loginRec.Result().Cookies()
	initialRefresh := findCookie(cookies, refreshTokenCookie)
	if initialRefresh == nil {
		t.Fatal("refresh_token cookie missing after login")
	}

	// 1. Access with the issued access token.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, cookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("access status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// 2. Refresh rotates both tokens.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(initialRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rotatedRefresh := findCookie(rec.Result().Cookies(), refreshTokenCookie)
	rotatedAccess := findCookie(rec.Result().Cookies(), accessTokenCookie)
	if rotatedRefresh == nil || rotatedAccess == nil {
		t.Fatal("refresh did not reissue both cookies")
	}

	// 3. The new access token works.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.AddCookie(rotatedAccess)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-rotation access status = %d, want %d", rec.Code, http.StatusOK)
	}

	// 4. Reusing the pre-rotation refresh token is detected as reuse.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(initialRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// 5. The whole family (including the rotated token) is now revoked.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rotatedRefresh)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("rotated refresh token still works after reuse-triggered family revocation")
	}
}
