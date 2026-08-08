package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
)

// AuthMode names how this deployment authenticates callers. It is reported
// rather than inferred: ADR-0024 (DR-17) decided that stating "this deployment
// authenticates nobody" is more accurate than guessing a role that does not
// exist, and the UI uses the value to keep an evaluation deployment visually
// distinct from an authenticated production session (#81).
type AuthMode string

const (
	// AuthModeDisabled is MERLON_AUTH_ENABLED=false: authMiddleware passes
	// every request through and no role reaches the request context.
	AuthModeDisabled AuthMode = "disabled"
	// AuthModeAPIKeyOnly is authentication without a JWT signing key. API keys
	// authenticate machine callers but no human can log in, so an operator
	// looking for a login screen should be told it does not exist here.
	AuthModeAPIKeyOnly AuthMode = "api_key_only"
	// AuthModeSession is the full deployment: user login, refresh and logout.
	AuthModeSession AuthMode = "session"
)

// CapabilityAvailability is the CAP-01 vocabulary. The full set is defined
// here because it is the contract; handleCapabilities currently emits
// available, not_configured and forbidden. degraded and unavailable are
// reserved for runtime readiness (#83) and unsupported for a capability a
// deployment topology cannot host — none of which this endpoint can yet
// observe, and inventing an observation is exactly what #83 exists to stop.
type CapabilityAvailability string

const (
	CapabilityAvailable     CapabilityAvailability = "available"
	CapabilityNotConfigured CapabilityAvailability = "not_configured"
	CapabilityForbidden     CapabilityAvailability = "forbidden"
	CapabilityUnsupported   CapabilityAvailability = "unsupported"
	CapabilityDegraded      CapabilityAvailability = "degraded"
	CapabilityUnavailable   CapabilityAvailability = "unavailable"
)

// Reason codes are stable machine values; the UI localizes them. They never
// carry configuration detail, endpoint names or credentials (SEC-C4).
const (
	reasonDependencyNotConfigured = "dependency_not_configured"
	reasonPermissionRequired      = "permission_required"
	reasonAuthenticationRequired  = "authentication_required"
)

// CapabilityDescriptor tells an operator what a function is for, whether it is
// available, why it is not, who may use it, and whether the supported
// interaction is a screen, an endpoint, or both (CAP-01).
type CapabilityDescriptor struct {
	ID                 string                 `json:"id"`
	Availability       CapabilityAvailability `json:"availability"`
	RequiredPermission string                 `json:"required_permission,omitempty"`
	Surfaces           []string               `json:"surfaces"`
	ReasonCode         string                 `json:"reason_code,omitempty"`
	DocsURL            string                 `json:"docs_url,omitempty"`
	CheckedAt          time.Time              `json:"checked_at"`
	ExpiresAt          *time.Time             `json:"expires_at,omitempty"`
}

const (
	surfaceUI  = "ui"
	surfaceAPI = "api"
)

// capabilityEntry is one row of the catalog. permission is the fine-grained
// grant from auth.RolePermissions — deliberately the same map the routes
// enforce, so a capability can never advertise authority the server does not
// check. present reports whether this deployment configured the dependency the
// capability needs at all.
type capabilityEntry struct {
	id         string
	permission auth.Permission
	surfaces   []string
	docsURL    string
	present    func(s *Server) bool
}

// capabilityCatalog is the single operator-facing inventory of administrative
// and permission-gated functions. A capability whose supported surface is api
// only is a valid product decision (#80); an unexplained omission is not, which
// is why every row carries a docs_url.
var capabilityCatalog = []capabilityEntry{
	{
		id:       "api_keys.manage",
		surfaces: []string{surfaceUI, surfaceAPI},
		docsURL:  "/docs/auth",
		present:  func(s *Server) bool { return s.apikeys != nil },
	},
	{
		id:       "users.manage",
		surfaces: []string{surfaceUI, surfaceAPI},
		docsURL:  "/docs/auth",
		present:  func(s *Server) bool { return s.users != nil },
	},
	{
		id:       "webhooks.manage",
		surfaces: []string{surfaceUI, surfaceAPI},
		docsURL:  "/docs/adapter-guide",
		present:  func(s *Server) bool { return s.webhooks != nil },
	},
	{
		// Retention periods are edited rarely, by an administrator, and are
		// audited. The endpoints exist and are documented; no screen is
		// planned, so the omission is stated rather than left to be discovered.
		id:       "retention.manage",
		surfaces: []string{surfaceAPI},
		docsURL:  "/docs/compliance/data-retention",
		present:  func(s *Server) bool { return s.retention != nil },
	},
	{
		// Account linking is an ingestion-time concern driven by the adapter
		// layer, not an analyst workflow.
		id:       "accounts.manage",
		surfaces: []string{surfaceAPI},
		docsURL:  "/docs/adapter-guide",
		present:  func(s *Server) bool { return s.accounts != nil },
	},
	{
		id:       "screening.review",
		surfaces: []string{surfaceUI, surfaceAPI},
		docsURL:  "/docs/case-management",
		present:  func(s *Server) bool { return s.screeningResults != nil },
	},
	{
		id:         "rules.write",
		permission: auth.PermRuleWrite,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/rule-authoring",
		present:    func(s *Server) bool { return s.rules != nil },
	},
	{
		id:       "config.validate",
		surfaces: []string{surfaceUI, surfaceAPI},
		docsURL:  "/docs/configuration",
		present:  func(s *Server) bool { return s.configEngine != nil },
	},
	{
		id:         "whitelist.request",
		permission: auth.PermWhitelistRequest,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/case-management",
		present:    func(s *Server) bool { return s.whitelist != nil },
	},
	{
		id:         "whitelist.approve",
		permission: auth.PermWhitelistApprove,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/case-management",
		present:    func(s *Server) bool { return s.whitelist != nil },
	},
	{
		id:         "audit.read",
		permission: auth.PermAuditRead,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/auth",
		present:    func(s *Server) bool { return s.audit != nil },
	},
	{
		id:         "cdd.score",
		permission: auth.PermCDDScore,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/configuration",
		present:    func(s *Server) bool { return s.scoring != nil },
	},
	{
		id:         "cdd.override.approve",
		permission: auth.PermCDDOverrideApprove,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/configuration",
		present:    func(s *Server) bool { return s.wave3 != nil },
	},
	{
		id:         "batch.execute.large",
		permission: auth.PermBatchExecuteLarge,
		surfaces:   []string{surfaceUI, surfaceAPI},
		docsURL:    "/docs/operations/worker-mode",
		present:    func(s *Server) bool { return s.batchRuns != nil },
	},
}

// authMode reports the deployment's authentication state. It reads the same
// fields authMiddleware branches on, so the reported mode cannot drift from the
// enforced one.
func (s *Server) authMode() AuthMode {
	switch {
	case s.apikeys == nil:
		return AuthModeDisabled
	case s.tokenIssuer == nil || s.users == nil || s.refreshTokens == nil:
		return AuthModeAPIKeyOnly
	default:
		return AuthModeSession
	}
}

// rolePermissions returns the grants held by role, always non-nil so the JSON
// carries [] rather than null: an empty grant set is a fact about the role, not
// missing data.
func rolePermissions(role domain.Role) []string {
	perms := auth.RolePermissions[role]
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	sort.Strings(out)
	return out
}

// describeCapabilities resolves the catalog against this deployment and the
// caller's role.
//
// The dependency check comes before the permission check on purpose. ADR-0024
// grants every capability when authentication is disabled, because a
// deployment without roles cannot refuse one — but that reasoning is about
// authority, not about existence. Reporting an absent dependency as available
// would claim a feature this deployment does not have, which is the class of
// untruth #83 exists to remove.
func (s *Server) describeCapabilities(role domain.Role, authenticated bool, now time.Time) []CapabilityDescriptor {
	out := make([]CapabilityDescriptor, 0, len(capabilityCatalog))
	for _, entry := range capabilityCatalog {
		d := CapabilityDescriptor{
			ID:                 entry.id,
			RequiredPermission: string(entry.permission),
			Surfaces:           append([]string(nil), entry.surfaces...),
			DocsURL:            entry.docsURL,
			CheckedAt:          now,
		}

		switch {
		case !entry.present(s):
			d.Availability = CapabilityNotConfigured
			d.ReasonCode = reasonDependencyNotConfigured
		case entry.permission == "" || !authenticated:
			d.Availability = CapabilityAvailable
		case !auth.HasPermission(role, entry.permission):
			d.Availability = CapabilityForbidden
			if role == "" {
				d.ReasonCode = reasonAuthenticationRequired
			} else {
				d.ReasonCode = reasonPermissionRequired
			}
		default:
			d.Availability = CapabilityAvailable
		}

		out = append(out, d)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type capabilitiesResponse struct {
	AuthMode    AuthMode               `json:"auth_mode"`
	UserID      string                 `json:"user_id,omitempty"`
	Role        domain.Role            `json:"role,omitempty"`
	Permissions []string               `json:"permissions"`
	CheckedAt   time.Time              `json:"checked_at"`
	Data        []CapabilityDescriptor `json:"data"`
}

// handleCapabilities serves the server-sourced availability contract the UI
// uses to hide, disable or explain a control (#80, #81).
//
// Hiding a control is not authorization: every capability named here is
// enforced again on the route that performs the action (SEC-C1). This endpoint
// exists so an operator learns *why* a function is absent, not so the client
// becomes the decision-maker.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	mode := s.authMode()

	var role domain.Role
	var userID string
	if principal, ok := r.Context().Value(ctxKeyPrincipal).(Principal); ok {
		role = principal.Role
		userID = principal.UserID
	}

	authenticated := mode != AuthModeDisabled

	writeJSON(w, http.StatusOK, capabilitiesResponse{
		AuthMode:    mode,
		UserID:      userID,
		Role:        role,
		Permissions: rolePermissions(role),
		CheckedAt:   now,
		Data:        s.describeCapabilities(role, authenticated, now),
	})
}
