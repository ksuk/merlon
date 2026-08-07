package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ksuk/merlon/api/internal/domain"
)

// Permission is a fine-grained authorization grant, distinct from the
// coarse method/path role check in server.hasPermission (the authentication model §3).
type Permission string

const (
	PermWhitelistRequest Permission = "whitelist:request"
	PermWhitelistApprove Permission = "whitelist:approve"
	PermAuditRead        Permission = "audit:read"
	// PermRuleWrite gates rule definition create/update/activate/deactivate/
	// import (the HTTP API contract §1.4). Unlike the coarse method-based check in
	// server.hasPermission (which lets Analyst write most resources), rule
	// changes affect scoring/monitoring behavior system-wide, so the HTTP API contract
	// restricts them to Admin specifically.
	PermRuleWrite Permission = "rule:write"
	// PermBatchExecuteLarge is required to confirm a target manifest covering
	// more than largeBatchThreshold customers. A mis-scoped bulk rescore or
	// re-evaluation is one of the few actions in the system an operator cannot
	// undo by editing a record afterwards, so the scale of the blast radius,
	// not just the operation, decides who may authorise it.
	PermBatchExecuteLarge Permission = "batch:execute:large"
	// PermCDDScore gates re-scoring a customer. The CDD score decides EDD
	// requirements, monitoring thresholds and rescreening frequency
	// (ADR-0004), so producing one is a control action, not a read.
	PermCDDScore Permission = "cdd:score"
	// PermCDDOverrideApprove gates approving a proposed override of a computed
	// tier. Held by Admin only, and never by the person who proposed it.
	PermCDDOverrideApprove Permission = "cdd:override:approve"
)

// RolePermissions maps each role to its granted permissions (the authentication model §3).
// Admin holds every permission; Analyst may request whitelist entries but
// may not approve them or read the audit log (segregation of duties);
// Viewer holds none: a role that may only read must not be able to move a
// customer's risk tier (ADR-0019).
var RolePermissions = map[domain.Role][]Permission{
	domain.RoleAdmin:   {PermWhitelistRequest, PermWhitelistApprove, PermAuditRead, PermRuleWrite, PermBatchExecuteLarge, PermCDDScore, PermCDDOverrideApprove},
	domain.RoleAnalyst: {PermWhitelistRequest, PermCDDScore},
	domain.RoleViewer:  {},
}

// HasPermission reports whether role has been granted permission p.
func HasPermission(role domain.Role, p Permission) bool {
	for _, perm := range RolePermissions[role] {
		if perm == p {
			return true
		}
	}
	return false
}

type roleContextKey struct{}

// WithRole attaches the authenticated principal's role to ctx. It is set by
// server.authMiddleware (JWT or API key path) after authentication succeeds,
// and read by RequirePermission.
func WithRole(ctx context.Context, role domain.Role) context.Context {
	return context.WithValue(ctx, roleContextKey{}, role)
}

// RoleFromContext retrieves the role set by WithRole, if any.
func RoleFromContext(ctx context.Context) (domain.Role, bool) {
	role, ok := ctx.Value(roleContextKey{}).(domain.Role)
	return role, ok
}

// RequirePermission is an http middleware that 403s unless the authenticated
// role (set in the request context by the preceding authMiddleware) holds
// permission p.
func RequirePermission(p Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFromContext(r.Context())
			if !ok || !HasPermission(role, p) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "insufficient permissions"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
