package server

import "net/http"

// handleHealth is the original combined health endpoint (engine reachability
// as a best-effort signal). Kept unchanged for Contract Stability; new
// deployments should use the /healthz/live and /healthz/ready split below
// (overview.md §4.4).
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","version":"dev"}`))
}

// handleHealthLive is the liveness probe (overview.md §4.4): only whether
// this process is alive and able to respond, independent of dependency
// health or initial-setup state (acceptance criterion 1).
func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleHealthReady is the readiness probe: unhealthy until initial setup
// (overview.md §4.5) completes, and while the engine is unreachable.
// Postgres/Redis connectivity checks are added in WS-3 (OPS-002); this
// endpoint checks what WS-1 owns (setup gating, engine reachability).
func (s *Server) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	if s.users != nil {
		count, err := s.users.Count(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"setup":  err.Error(),
			})
			return
		}
		if count == 0 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"setup":  "initial setup not completed",
			})
			return
		}
	}

	if s.engineHealth != nil {
		if err := s.engineHealth.CheckHealth(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"engine": err.Error(),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
