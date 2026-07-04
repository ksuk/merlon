package server

import "net/http"

// handleHealth reports this API process's basic liveness, plus (OPS-002)
// engine reachability when an engine.HealthChecker is configured. This is
// distinct from WS-1's planned /healthz/live and /healthz/ready split
// (overview.md §4.4): until that lands, this single endpoint folds engine
// reachability in as a best-effort signal.
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
