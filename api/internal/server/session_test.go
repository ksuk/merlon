package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
	refreshTokens := store.NewMemoryRefreshTokenRepoWithAudit(auditRepo)
	s := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	return s, users, auditRepo
}

func newTestSessionServer(issuer *auth.TokenIssuer, users domain.UserRepository, refreshTokens domain.RefreshTokenRepository, denylist auth.Denylist, auditRepo domain.AuditRepository) *Server {
	return New(":0", Deps{
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
		Denylist:       denylist,
		Users:          users,
		RefreshTokens:  refreshTokens,
	})
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
	if !isSafeMethod(req.Method) {
		if csrf := findCookie(cookies, csrfCookieName); csrf != nil {
			req.Header.Set(csrfHeaderName, csrf.Value)
		}
	}
}

func TestRefreshAndLogoutRequireCSRF(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/auth/logout"} {
		t.Run(path, func(t *testing.T) {
			s, users, _ := testServerWithSessions(t)
			user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
			login := doLogin(t, s, user.Email, testUserPassword)
			cookies := login.Result().Cookies()

			req := httptest.NewRequest(http.MethodPost, path, nil)
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status without CSRF header = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}

			count, err := s.refreshTokens.CountActiveByUser(context.Background(), user.ID)
			if err != nil {
				t.Fatalf("CountActiveByUser: %v", err)
			}
			if count != 1 {
				t.Fatalf("active refresh tokens after rejected request = %d, want 1", count)
			}
		})
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

type familyCheckFailingRepo struct {
	domain.RefreshTokenRepository
}

type familyRevokeFailingRepo struct {
	domain.RefreshTokenRepository
}

func (r familyCheckFailingRepo) IsFamilyActive(context.Context, string) (bool, error) {
	return false, fmt.Errorf("refresh-token repository unavailable")
}

func (r familyRevokeFailingRepo) RevokeFamily(context.Context, string) error {
	return fmt.Errorf("refresh-token repository unavailable")
}

type failingSessionAuditRepo struct{}

func (failingSessionAuditRepo) Create(context.Context, *domain.AuditEntry) error {
	return fmt.Errorf("audit repository unavailable")
}

func (failingSessionAuditRepo) List(context.Context, domain.AuditListFilter) ([]domain.AuditEntry, error) {
	return nil, nil
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

func TestLogout_SucceedsWhenPersistentRevocationIsConfirmedButDenylistCacheFails(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	loginRec := doLogin(t, s, "alice@example.com", testUserPassword)
	s.denylist = revokeFailingDenylist{delegate: s.denylist}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, loginRec.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	for _, name := range []string{accessTokenCookie, refreshTokenCookie, csrfCookieName} {
		cookie := findCookie(rec.Result().Cookies(), name)
		if cookie == nil || cookie.MaxAge >= 0 {
			t.Errorf("%s cookie was not cleared after logout", name)
		}
	}

	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	if hasAction(entries, "logout_failed") {
		t.Fatal("cache-only failure was recorded as logout_failed")
	}
	if !hasAction(entries, "logout") {
		t.Fatal("successful persistent logout was not audited")
	}
}

func TestLogout_FailsWhenPersistentRevocationCannotBeConfirmed(t *testing.T) {
	s, users, auditRepo := testServerWithSessions(t)
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	login := doLogin(t, s, user.Email, testUserPassword)
	refresh := findCookie(login.Result().Cookies(), refreshTokenCookie)
	if refresh == nil {
		t.Fatal("refresh token missing")
	}
	stored, err := s.refreshTokens.GetByHash(context.Background(), auth.HashRefreshToken(refresh.Value))
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	s.refreshTokens = familyRevokeFailingRepo{RefreshTokenRepository: s.refreshTokens}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, login.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	active, err := s.refreshTokens.IsFamilyActive(context.Background(), stored.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if !active {
		t.Fatal("failing authoritative revocation unexpectedly ended the session")
	}
	entries, err := auditRepo.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	if !hasAction(entries, "logout_failed") || hasAction(entries, "logout") {
		t.Fatal("authoritative revocation failure audit actions are incorrect")
	}
}

func TestJWTAuthenticationFailsClosedWhenPersistentFamilyCheckFails(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	login := doLogin(t, s, "alice@example.com", testUserPassword)
	s.refreshTokens = familyCheckFailingRepo{RefreshTokenRepository: s.refreshTokens}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, login.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("family-check failure status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestLoginDoesNotIssueSessionWhenAuditCannotBeRecorded(t *testing.T) {
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	auditRepo := failingSessionAuditRepo{}
	refreshTokens := store.NewMemoryRefreshTokenRepoWithAudit(auditRepo)
	s := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	body := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, testUserPassword)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("login audit failure status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if findCookie(rec.Result().Cookies(), accessTokenCookie) != nil || findCookie(rec.Result().Cookies(), refreshTokenCookie) != nil {
		t.Fatal("login audit failure returned session cookies")
	}
	count, err := refreshTokens.CountActiveByUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("CountActiveByUser: %v", err)
	}
	if count != 0 {
		t.Fatalf("active sessions after login audit failure = %d, want 0", count)
	}
}

func TestLoginAuditFailureDoesNotEvictAnExistingSession(t *testing.T) {
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	auditRepo := store.NewMemoryAuditRepo()
	refreshTokens := store.NewMemoryRefreshTokenRepoWithAudit(auditRepo)
	s := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)

	logins := make([]*httptest.ResponseRecorder, 0, auth.MaxConcurrentSessions)
	for range auth.MaxConcurrentSessions {
		logins = append(logins, doLogin(t, s, user.Email, testUserPassword))
	}
	auditRepo.SetCreateFailure(errors.New("audit unavailable"))
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, testUserPassword)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("login status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	oldestRefresh := findCookie(logins[0].Result().Cookies(), refreshTokenCookie)
	if oldestRefresh == nil {
		t.Fatal("oldest refresh token missing")
	}
	stored, err := refreshTokens.GetByHash(context.Background(), auth.HashRefreshToken(oldestRefresh.Value))
	if err != nil {
		t.Fatalf("GetByHash oldest: %v", err)
	}
	active, err := refreshTokens.IsFamilyActive(context.Background(), stored.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive oldest: %v", err)
	}
	if !active {
		t.Fatal("oldest session was evicted by a login whose audit failed")
	}
}

func TestRevokeUserSessions_AuditFailureRollsBackRevocation(t *testing.T) {
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	auditRepo := store.NewMemoryAuditRepo()
	refreshTokens := store.NewMemoryRefreshTokenRepoWithAudit(auditRepo)
	s := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	user := createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	login := doLogin(t, s, user.Email, testUserPassword)
	refresh := findCookie(login.Result().Cookies(), refreshTokenCookie)
	if refresh == nil {
		t.Fatal("refresh token missing")
	}
	stored, err := refreshTokens.GetByHash(context.Background(), auth.HashRefreshToken(refresh.Value))
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	auditRepo.SetCreateFailure(errors.New("audit unavailable"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+user.ID+"/revoke-sessions", nil)
	attachCookies(req, login.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("revoke status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	active, err := refreshTokens.IsFamilyActive(context.Background(), stored.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if !active {
		t.Fatal("session was revoked despite audit rollback")
	}
}

func TestRefresh_AuditFailureRevokesRotatedFamilyAndReturnsNoSession(t *testing.T) {
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	auditRepo := store.NewMemoryAuditRepo()
	refreshTokens := store.NewMemoryRefreshTokenRepoWithAudit(auditRepo)
	s := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	login := doLogin(t, s, user.Email, testUserPassword)
	refresh := findCookie(login.Result().Cookies(), refreshTokenCookie)
	if refresh == nil {
		t.Fatal("refresh token missing")
	}
	stored, err := refreshTokens.GetByHash(context.Background(), auth.HashRefreshToken(refresh.Value))
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	auditRepo.SetCreateFailure(errors.New("audit unavailable"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	attachCookies(req, login.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("refresh status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if findCookie(rec.Result().Cookies(), accessTokenCookie) != nil || findCookie(rec.Result().Cookies(), refreshTokenCookie) != nil {
		t.Fatal("failed refresh returned session cookies")
	}
	active, err := refreshTokens.IsFamilyActive(context.Background(), stored.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if active {
		t.Fatal("refresh family remains active after audit failure")
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
	csrf := findCookie(loginRec.Result().Cookies(), csrfCookieName)
	if csrf == nil {
		t.Fatal("csrf_token cookie missing after login")
	}
	req.AddCookie(csrf)
	req.Header.Set(csrfHeaderName, csrf.Value)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	attachCookies(req, loginRec.Result().Cookies())
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

	secondRefresh := findCookie(secondLogin.Result().Cookies(), refreshTokenCookie)
	if secondRefresh == nil {
		t.Fatal("concurrent session refresh_token cookie missing")
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	attachCookies(req, secondLogin.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("concurrent session refresh status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestLogout_WithAccessCookieOnlyRevokesPersistedRefreshFamily(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	login := doLogin(t, s, "alice@example.com", testUserPassword)
	access := findCookie(login.Result().Cookies(), accessTokenCookie)
	refresh := findCookie(login.Result().Cookies(), refreshTokenCookie)
	if access == nil || refresh == nil {
		t.Fatal("login did not return access and refresh cookies")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(access)
	csrf := findCookie(login.Result().Cookies(), csrfCookieName)
	if csrf == nil {
		t.Fatal("login did not return a CSRF cookie")
	}
	req.AddCookie(csrf)
	req.Header.Set(csrfHeaderName, csrf.Value)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("access-only logout status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	attachCookies(req, login.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after access-only logout status = %d, want %d, body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestLogout_RevocationIsVisibleAcrossServerInstances(t *testing.T) {
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	auditRepo := store.NewMemoryAuditRepo()
	refreshTokens := store.NewMemoryRefreshTokenRepoWithAudit(auditRepo)
	first := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	second := newTestSessionServer(issuer, users, refreshTokens, auth.NewInMemoryDenylist(), auditRepo)
	createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	login := doLogin(t, first, "alice@example.com", testUserPassword)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, login.Result().Cookies())
	rec := httptest.NewRecorder()
	second.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second instance pre-logout status = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	attachCookies(req, login.Result().Cookies())
	rec = httptest.NewRecorder()
	first.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first instance logout status = %d, want %d", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, login.Result().Cookies())
	rec = httptest.NewRecorder()
	second.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("second instance post-logout status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTWithoutSessionIdentifierIsRejectedAfterUpgrade(t *testing.T) {
	const secret = "test-only-secret-not-for-production"
	issuer, err := auth.NewHS256Issuer(secret)
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	s := newTestSessionServer(issuer, users, store.NewMemoryRefreshTokenRepo(), auth.NewInMemoryDenylist(), store.NewMemoryAuditRepo())
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAdmin)
	now := time.Now()
	legacyClaims := auth.Claims{
		UserID: user.ID,
		Role:   string(user.Role),
		JTI:    "legacy-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(auth.AccessTokenTTL)),
		},
	}
	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, legacyClaims)
	fingerprint := sha256.Sum256([]byte(secret))
	legacy.Header["kid"] = hex.EncodeToString(fingerprint[:])[:16]
	raw, err := legacy.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign legacy access token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	req.AddCookie(&http.Cookie{Name: accessTokenCookie, Value: raw})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("legacy sid-less token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRoleChangeInvalidatesExistingSession(t *testing.T) {
	s, users, _ := testServerWithSessions(t)
	user := createTestUser(t, users, "alice@example.com", testUserPassword, domain.RoleAnalyst)
	login := doLogin(t, s, "alice@example.com", testUserPassword)
	refresh := findCookie(login.Result().Cookies(), refreshTokenCookie)
	if refresh == nil {
		t.Fatal("refresh_token cookie missing")
	}

	user.Role = domain.RoleViewer
	user.UpdatedAt = time.Now()
	if err := users.Update(context.Background(), user); err != nil {
		t.Fatalf("update role: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, login.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("existing session after role change status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	attachCookies(req, login.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after role change status = %d, want %d, body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	stored, err := s.refreshTokens.GetByHash(context.Background(), auth.HashRefreshToken(refresh.Value))
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	active, err := s.refreshTokens.IsFamilyActive(context.Background(), stored.TokenFamily)
	if err != nil {
		t.Fatalf("IsFamilyActive: %v", err)
	}
	if active {
		t.Fatal("role-changed refresh token family remains active")
	}

	fresh := doLogin(t, s, "alice@example.com", testUserPassword)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	attachCookies(req, fresh.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh session status = %d, want %d", rec.Code, http.StatusOK)
	}
	var profile meResponse
	if err := json.NewDecoder(rec.Body).Decode(&profile); err != nil {
		t.Fatalf("decode fresh profile: %v", err)
	}
	if profile.Role != domain.RoleViewer {
		t.Fatalf("fresh session role = %s, want %s", profile.Role, domain.RoleViewer)
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
	attachCookies(req, oldestCookies)
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
	var response struct {
		Status          string `json:"status"`
		RevokedSessions int    `json:"revoked_sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode revoke sessions response: %v", err)
	}
	if response.Status != "revoked" || response.RevokedSessions != 2 {
		t.Fatalf("revoke sessions response = %#v, want status revoked and count 2", response)
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
		attachCookies(req, cookies)
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
	attachCookies(req, loginRec.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	refreshCookies := append([]*http.Cookie{}, rec.Result().Cookies()...)
	refreshCookies = append(refreshCookies, findCookie(loginRec.Result().Cookies(), csrfCookieName))
	attachCookies(req, refreshCookies)
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
	if hasAction(entries, "create") {
		t.Fatal("session lifecycle also produced a generic create audit entry")
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
	attachCookies(req, loginRec.Result().Cookies())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	attachCookies(req, loginRec.Result().Cookies())
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
	attachCookies(req, loginRec.Result().Cookies())
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
	attachCookies(req, loginRec.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rotatedRefresh)
	csrf := findCookie(loginRec.Result().Cookies(), csrfCookieName)
	if csrf == nil {
		t.Fatal("csrf_token cookie missing after login")
	}
	req.AddCookie(csrf)
	req.Header.Set(csrfHeaderName, csrf.Value)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh from revoked family status = %d, want %d", rec.Code, http.StatusUnauthorized)
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
	attachCookies(req, cookies)
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
	attachCookies(req, cookies)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// 5. The whole family (including the rotated token) is now revoked.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rotatedRefresh)
	csrf := findCookie(cookies, csrfCookieName)
	if csrf == nil {
		t.Fatal("csrf_token cookie missing after login")
	}
	req.AddCookie(csrf)
	req.Header.Set(csrfHeaderName, csrf.Value)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("rotated refresh token still works after reuse-triggered family revocation")
	}
}
