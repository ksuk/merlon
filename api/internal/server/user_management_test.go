package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func testServerWithUserLifecycle(t *testing.T) (*Server, *store.MemoryUserRepo, *store.MemoryRefreshTokenRepo, *store.MemoryAuditRepo) {
	t.Helper()
	issuer, err := auth.NewHS256Issuer("test-only-secret-not-for-production")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}
	users := store.NewMemoryUserRepo()
	audit := store.NewMemoryAuditRepo()
	tokens := store.NewMemoryRefreshTokenRepoWithAuditAndUsers(audit, users)
	lifecycle := store.NewMemoryUserLifecycleRepo(users, tokens, audit)
	s := New(":0", Deps{
		Customers:     store.NewMemoryCustomerRepo(),
		Audit:         audit,
		APIKeys:       store.NewMemoryAPIKeyRepo(),
		Users:         users,
		RefreshTokens: tokens,
		UserLifecycle: lifecycle,
		TokenIssuer:   issuer,
		Denylist:      auth.NewInMemoryDenylist(),
	})
	return s, users, tokens, audit
}

func doUserManagementRequest(t *testing.T, s *Server, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	attachCookies(req, cookies)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCreateUserSupportsAnalystAndViewerLogin(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	admin := doLogin(t, s, "admin@example.com", testUserPassword)

	for _, role := range []domain.Role{domain.RoleAnalyst, domain.RoleViewer} {
		email := string(role) + "@example.com"
		body := `{"email":"` + email + `","password":"correct-horse-battery-staple","role":"` + string(role) + `"}`
		rec := doUserManagementRequest(t, s, http.MethodPost, "/api/v1/admin/users", body, admin.Result().Cookies())
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s status = %d, body = %s", role, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "correct-horse") {
			t.Fatal("create response contains password material")
		}
		doLogin(t, s, email, "correct-horse-battery-staple")
	}
}

func TestCreateUserRejectsDuplicateInvalidRoleAndWeakPassword(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	createTestUser(t, users, "existing@example.com", testUserPassword, domain.RoleViewer)
	admin := doLogin(t, s, "admin@example.com", testUserPassword)

	tests := []struct {
		name string
		body string
		code int
	}{
		{"duplicate", `{"email":"existing@example.com","password":"correct-horse-battery-staple","role":"viewer"}`, http.StatusConflict},
		{"invalid role", `{"email":"new@example.com","password":"correct-horse-battery-staple","role":"owner"}`, http.StatusBadRequest},
		{"weak password", `{"email":"new@example.com","password":"short","role":"viewer"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doUserManagementRequest(t, s, http.MethodPost, "/api/v1/admin/users", tt.body, admin.Result().Cookies())
			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.code, rec.Body.String())
			}
		})
	}
}

func TestUserManagementRejectsNonAdminIndependentlyOfUI(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	createTestUser(t, users, "analyst@example.com", testUserPassword, domain.RoleAnalyst)
	analyst := doLogin(t, s, "analyst@example.com", testUserPassword)
	rec := doUserManagementRequest(t, s, http.MethodPost, "/api/v1/admin/users", `{"email":"new@example.com","password":"correct-horse-battery-staple","role":"viewer"}`, analyst.Result().Cookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUserManagementCapabilityIsForbiddenForNonAdmin(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	createTestUser(t, users, "viewer@example.com", testUserPassword, domain.RoleViewer)
	viewer := doLogin(t, s, "viewer@example.com", testUserPassword)
	body := fetchCapabilities(t, s, viewer.Result().Cookies())
	capability := capabilityByID(t, body, "users.manage")
	if capability.Availability != CapabilityForbidden || capability.RequiredPermission != string(auth.PermUserManage) {
		t.Fatalf("capability = %#v", capability)
	}
}

func TestUpdateUserAuthorityRevokesExistingSession(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	adminUser := createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	target := createTestUser(t, users, "analyst@example.com", testUserPassword, domain.RoleAnalyst)
	admin := doLogin(t, s, adminUser.Email, testUserPassword)
	targetLogin := doLogin(t, s, target.Email, testUserPassword)

	rec := doUserManagementRequest(t, s, http.MethodPatch, "/api/v1/admin/users/"+target.ID, `{"role":"viewer","active":true}`, admin.Result().Cookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updated domain.User
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil || updated.Role != domain.RoleViewer {
		t.Fatalf("updated = %#v, err = %v", updated, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, targetLogin.Result().Cookies())
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session status = %d, want 401", rec.Code)
	}

	rec = doUserManagementRequest(t, s, http.MethodPatch, "/api/v1/admin/users/"+target.ID, `{"role":"viewer","active":false}`, admin.Result().Cookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", rec.Code, rec.Body.String())
	}
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"analyst@example.com","password":"`+testUserPassword+`"}`))
	loginRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusUnauthorized {
		t.Fatalf("inactive login status = %d, want 401", loginRec.Code)
	}
}

func TestUpdateUserAuthorityProtectsLastActiveAdministrator(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	adminUser := createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	admin := doLogin(t, s, adminUser.Email, testUserPassword)
	rec := doUserManagementRequest(t, s, http.MethodPatch, "/api/v1/admin/users/"+adminUser.ID, `{"role":"viewer","active":true}`, admin.Result().Cookies())
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
}

func TestResetUserPasswordRevokesSessionsAndKeepsSecretsOutOfAudit(t *testing.T) {
	s, users, _, audit := testServerWithUserLifecycle(t)
	adminUser := createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	target := createTestUser(t, users, "viewer@example.com", testUserPassword, domain.RoleViewer)
	admin := doLogin(t, s, adminUser.Email, testUserPassword)
	targetLogin := doLogin(t, s, target.Email, testUserPassword)
	newPassword := "replacement-password-123"

	rec := doUserManagementRequest(t, s, http.MethodPost, "/api/v1/admin/users/"+target.ID+"/reset-password", `{"password":"`+newPassword+`"}`, admin.Result().Cookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), newPassword) || strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatal("reset response contains password material")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers", nil)
	attachCookies(req, targetLogin.Result().Cookies())
	oldSession := httptest.NewRecorder()
	s.Handler().ServeHTTP(oldSession, req)
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("old session status = %d, want 401", oldSession.Code)
	}
	doLogin(t, s, target.Email, newPassword)

	entries, err := audit.List(context.Background(), domain.AuditListFilter{ResourceType: "users", ResourceID: target.ID})
	if err != nil || len(entries) != 1 || entries[0].Action != "user_password_reset" {
		t.Fatalf("audit entries = %#v, err = %v", entries, err)
	}
	encoded, _ := json.Marshal(entries[0])
	if strings.Contains(string(encoded), newPassword) {
		t.Fatal("audit contains new password")
	}
}

func TestResetUserPasswordRejectsWeakPassword(t *testing.T) {
	s, users, _, _ := testServerWithUserLifecycle(t)
	adminUser := createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	target := createTestUser(t, users, "viewer@example.com", testUserPassword, domain.RoleViewer)
	admin := doLogin(t, s, adminUser.Email, testUserPassword)
	rec := doUserManagementRequest(t, s, http.MethodPost, "/api/v1/admin/users/"+target.ID+"/reset-password", `{"password":"short"}`, admin.Result().Cookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateUserDoesNotExposeLifecycleFailureDetails(t *testing.T) {
	s, users, _, audit := testServerWithUserLifecycle(t)
	createTestUser(t, users, "admin@example.com", testUserPassword, domain.RoleAdmin)
	admin := doLogin(t, s, "admin@example.com", testUserPassword)
	audit.SetCreateFailure(errors.New("private database failure detail"))

	rec := doUserManagementRequest(t, s, http.MethodPost, "/api/v1/admin/users", `{"email":"new@example.com","password":"correct-horse-battery-staple","role":"viewer"}`, admin.Result().Cookies())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "private database failure detail") {
		t.Fatalf("response exposes lifecycle failure detail: %s", rec.Body.String())
	}
}
