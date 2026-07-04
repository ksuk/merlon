package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

func TestHasPermission(t *testing.T) {
	tests := []struct {
		name string
		role domain.Role
		perm Permission
		want bool
	}{
		{"admin can read audit", domain.RoleAdmin, PermAuditRead, true},
		{"analyst cannot read audit", domain.RoleAnalyst, PermAuditRead, false},
		{"analyst can request whitelist", domain.RoleAnalyst, PermWhitelistRequest, true},
		{"analyst cannot approve whitelist", domain.RoleAnalyst, PermWhitelistApprove, false},
		{"viewer has no permissions (audit)", domain.RoleViewer, PermAuditRead, false},
		{"viewer has no permissions (whitelist request)", domain.RoleViewer, PermWhitelistRequest, false},
		{"admin can approve whitelist", domain.RoleAdmin, PermWhitelistApprove, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPermission(tt.role, tt.perm); got != tt.want {
				t.Errorf("HasPermission(%s, %s) = %v, want %v", tt.role, tt.perm, got, tt.want)
			}
		})
	}
}

func TestRequirePermission_Allows(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequirePermission(PermAuditRead)(next)

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req = req.WithContext(WithRole(req.Context(), domain.RoleAdmin))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatal("next handler was not called for permitted role")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequirePermission_Denies403(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequirePermission(PermWhitelistApprove)(next)

	req := httptest.NewRequest(http.MethodPost, "/whatever", nil)
	req = req.WithContext(WithRole(req.Context(), domain.RoleAnalyst))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handlerCalled {
		t.Fatal("next handler was called for a forbidden role")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestRequirePermission_DeniesWhenRoleMissing(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called without an authenticated role")
	})

	mw := RequirePermission(PermAuditRead)(next)

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRoleFromContext_RoundTrip(t *testing.T) {
	ctx := WithRole(context.Background(), domain.RoleAnalyst)
	role, ok := RoleFromContext(ctx)
	if !ok {
		t.Fatal("RoleFromContext returned ok=false after WithRole")
	}
	if role != domain.RoleAnalyst {
		t.Errorf("role = %s, want %s", role, domain.RoleAnalyst)
	}
}
