package server

import (
	"context"
	"net/http"
	"time"
)

var Version = "dev"

const healthzReadyDBPingTimeout = 2 * time.Second

// handleHealth is the original combined health endpoint (engine reachability
// as a best-effort signal). Kept unchanged for Contract Stability; new
// deployments should use the /healthz/live and /healthz/ready split below
// (the operational design §4.4).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.engineHealth != nil {
		if err := s.engineHealth.CheckHealth(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"engine": err.Error(),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

// handleHealthLive is the liveness probe (the operational design §4.4): only whether
// this process is alive and able to respond, independent of dependency
// health or initial-setup state (acceptance criterion 1).
func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleHealthReady is the readiness probe (the operational design §4.4 "ヘルスチェッ
// クの粒度"): unhealthy until initial setup (the operational design §4.5) completes,
// and while PostgreSQL or the Rust engine's grpc.health.v1 check is
// unreachable. Each dependency's outcome is reported under "checks" so
// operators can tell which one is failing; a dependency not configured
// (e.g. no DB pool in in-memory dev mode) is simply omitted rather than
// reported as failing.
func (s *Server) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	healthy := true

	if s.users != nil {
		count, err := s.users.Count(r.Context())
		switch {
		case err != nil:
			checks["setup"] = "error: " + err.Error()
			healthy = false
		case count == 0:
			checks["setup"] = "error: initial setup not completed"
			healthy = false
		default:
			checks["setup"] = "ok"
		}
	}

	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthzReadyDBPingTimeout)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			checks["postgres"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["postgres"] = "ok"
		}
	}

	if s.engineHealth != nil {
		if err := s.engineHealth.CheckHealth(r.Context()); err != nil {
			checks["engine"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["engine"] = "ok"
		}
	}

	status := http.StatusOK
	statusText := "healthy"
	if !healthy {
		status = http.StatusServiceUnavailable
		statusText = "unhealthy"
	}

	writeJSON(w, status, map[string]any{
		"status": statusText,
		"checks": checks,
	})
}
