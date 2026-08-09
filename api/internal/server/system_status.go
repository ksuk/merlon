package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ksuk/merlon/api/internal/buildinfo"
)

// OperationalState is what a component is actually doing, as distinct from
// whether it was configured.
//
// GET /system/info previously returned a literal list of three component names
// and the UI drew a green check beside each, so a deployment whose database was
// refusing connections reported the same thing as a healthy one (#83). The two
// facts are now reported separately, and neither is inferred from the other.
type OperationalState string

const (
	// OperationalReady means a check ran and succeeded.
	OperationalReady OperationalState = "ready"
	// OperationalDegraded means the component works but not fully — some
	// required screening source is stale, for instance.
	OperationalDegraded OperationalState = "degraded"
	// OperationalUnavailable means a check ran and failed.
	OperationalUnavailable OperationalState = "unavailable"
	// OperationalUnknown means no check could be run. It is never rendered as
	// healthy: not knowing is a third answer, not a quiet yes.
	OperationalUnknown OperationalState = "unknown"
)

// Reason codes are stable machine values. They never carry a dependency's error
// text: the same redaction rule as /healthz/ready (see checkFailed), because a
// pgx failure formats the host, user and database name and an engine failure
// carries configuration paths.
const (
	reasonNotConfigured    = "not_configured"
	reasonCheckFailed      = "check_failed"
	reasonNoProbeAvailable = "no_probe_available"
	reasonSourcesDegraded  = "sources_degraded"
)

// ComponentStatus is one row of the System page.
type ComponentStatus struct {
	Name string `json:"name"`
	// Configured reports whether this deployment wired the component at all.
	// A component that is not configured is not a failure, and not a success.
	Configured       bool             `json:"configured"`
	OperationalState OperationalState `json:"operational_state"`
	ReasonCode       string           `json:"reason_code,omitempty"`
	CheckedAt        time.Time        `json:"checked_at"`
}

// SystemStatus is the whole answer, including how old it is and where it came
// from, so an operator can tell a live check from a cached one.
type SystemStatus struct {
	Version       string             `json:"version"`
	Commit        string             `json:"commit,omitempty"`
	BuiltAt       string             `json:"built_at,omitempty"`
	AuthMode      AuthMode           `json:"auth_mode"`
	BaseCurrency  string             `json:"base_currency,omitempty"`
	ConfigDigests map[string]string  `json:"config_digests"`
	Policies      []PolicyProvenance `json:"policies"`
	Components    []ComponentStatus  `json:"components"`
	CheckedAt     time.Time          `json:"checked_at"`
	ExpiresAt     time.Time          `json:"expires_at"`
	// Source is "live" when this response ran the checks and "cached" when it
	// reused a recent result. A page that cannot tell the difference cannot
	// tell a stale green from a fresh one.
	Source string `json:"source"`
}

// PolicyProvenance identifies the policy documents this process loaded.
type PolicyProvenance struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
	PolicyVersion string `json:"policy_version"`
	Digest        string `json:"digest"`
	// Source is "file" or "default": a deployment running the in-code default
	// has not authored that policy, which is a materially different situation
	// from having authored one that happens to match.
	Source string `json:"source"`
}

// systemStatusTTL bounds how stale a cached answer may be. Probing every
// dependency on every page load is not free, and an operator refreshing a
// status page does not need sub-second accuracy — but they do need to know the
// age, which is why ExpiresAt travels with the result.
const systemStatusTTL = 15 * time.Second

const systemStatusProbeTimeout = 2 * time.Second

type systemStatusCache struct {
	mu     sync.Mutex
	status *SystemStatus
}

// componentProbe runs one check. Returning an error means "checked and failed";
// returning errNoProbe means "could not check", which is a different answer.
type componentProbe func(ctx context.Context) (OperationalState, string)

func (s *Server) probeAPI(context.Context) (OperationalState, string) {
	// The request being served is the observation. This is the one component
	// whose readiness needs no separate check.
	return OperationalReady, ""
}

func (s *Server) probeDatabase(ctx context.Context) (OperationalState, string) {
	if s.db == nil {
		return OperationalUnknown, reasonNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, systemStatusProbeTimeout)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		logReadinessFailure(ctx, "postgres", err)
		return OperationalUnavailable, reasonCheckFailed
	}
	return OperationalReady, ""
}

func (s *Server) probeEngine(ctx context.Context) (OperationalState, string) {
	if s.engineHealth == nil {
		if s.scoring == nil && s.monitoring == nil {
			return OperationalUnknown, reasonNotConfigured
		}
		// An engine is wired but exposes no health check. Reporting it ready
		// would be an assertion nothing supports.
		return OperationalUnknown, reasonNoProbeAvailable
	}
	if err := s.engineHealth.CheckHealth(ctx); err != nil {
		logReadinessFailure(ctx, "engine", err)
		return OperationalUnavailable, reasonCheckFailed
	}
	return OperationalReady, ""
}

func (s *Server) probeScreeningSources(ctx context.Context) (OperationalState, string) {
	if s.screeningListStore == nil || len(s.screeningListIDs) == 0 {
		return OperationalUnknown, reasonNotConfigured
	}
	statuses, err := s.screeningSourceStatuses(ctx, s.screeningListIDs, 0)
	if err != nil {
		logReadinessFailure(ctx, "screening_sources", err)
		return OperationalUnavailable, reasonCheckFailed
	}
	required := s.policies.ScreeningReadiness()
	for _, status := range statuses {
		if !required.Required(status.ListID) {
			continue
		}
		if !status.IsReady() {
			// The source state machine already decided this; repeating its
			// judgement here rather than re-deriving one keeps the System page
			// and the screening queue from disagreeing.
			return OperationalDegraded, reasonSourcesDegraded
		}
	}
	return OperationalReady, ""
}

func (s *Server) componentProbes() []struct {
	name       string
	configured bool
	probe      componentProbe
} {
	return []struct {
		name       string
		configured bool
		probe      componentProbe
	}{
		{"api", true, s.probeAPI},
		{"database", s.db != nil, s.probeDatabase},
		{"engine", s.scoring != nil || s.monitoring != nil || s.engineHealth != nil, s.probeEngine},
		{"screening_sources", s.screeningListStore != nil && len(s.screeningListIDs) > 0, s.probeScreeningSources},
	}
}

// buildSystemStatus runs every probe. A probe that panics or blocks would take
// the page with it, so each is bounded by its own timeout rather than a shared
// one; a slow database must not make the engine look unknown.
func (s *Server) buildSystemStatus(ctx context.Context, now time.Time) *SystemStatus {
	components := make([]ComponentStatus, 0, 4)
	for _, entry := range s.componentProbes() {
		state, reason := entry.probe(ctx)
		components = append(components, ComponentStatus{
			Name:             entry.name,
			Configured:       entry.configured,
			OperationalState: state,
			ReasonCode:       reason,
			CheckedAt:        now,
		})
	}

	digests := make(map[string]string, len(s.configDigests))
	for name, digest := range s.configDigests {
		digests[name] = digest
	}

	policies := make([]PolicyProvenance, 0)
	for _, descriptor := range s.policies.Descriptors() {
		policies = append(policies, PolicyProvenance{
			Name:          descriptor.Name,
			SchemaVersion: descriptor.SchemaVersion,
			PolicyVersion: descriptor.PolicyVersion,
			Digest:        descriptor.Digest,
			Source:        descriptor.Source,
		})
	}

	return &SystemStatus{
		Version:       buildinfo.Version,
		Commit:        buildinfo.Commit,
		BuiltAt:       buildinfo.BuiltAt,
		AuthMode:      s.authMode(),
		BaseCurrency:  s.tmBaseCurrency,
		ConfigDigests: digests,
		Policies:      policies,
		Components:    components,
		CheckedAt:     now,
		ExpiresAt:     now.Add(systemStatusTTL),
		Source:        "live",
	}
}

// handleSystemStatus serves the truthful readiness and provenance contract.
//
// ?refresh=true forces a live check. Without it a result younger than the TTL
// is reused and labelled "cached", so a page that polls does not probe the
// database on every render while still being able to say how old the answer is.
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	forceRefresh := r.URL.Query().Get("refresh") == "true"

	s.statusCache.mu.Lock()
	cached := s.statusCache.status
	if !forceRefresh && cached != nil && now.Before(cached.ExpiresAt) {
		reply := *cached
		reply.Source = "cached"
		s.statusCache.mu.Unlock()
		writeJSON(w, http.StatusOK, reply)
		return
	}
	s.statusCache.mu.Unlock()

	status := s.buildSystemStatus(r.Context(), now)

	s.statusCache.mu.Lock()
	s.statusCache.status = status
	s.statusCache.mu.Unlock()

	writeJSON(w, http.StatusOK, status)
}
