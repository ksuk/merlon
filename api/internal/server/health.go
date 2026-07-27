package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

var Version = "dev"

const healthzReadyDBPingTimeout = 2 * time.Second

// checkFailed is what /healthz/ready reports for a failing dependency.
//
// The probe is unauthenticated by necessity (auth.go exempts it, and the
// OpenAPI document declares it with an empty security requirement), so its
// body is readable by anyone who can reach the port. Dependency errors are not
// safe to put there: a pgx connection error is formatted
// "failed to connect to `host=... user=... database=...`", and engine errors
// carry configuration file paths. The check's name already tells an operator
// which dependency is down, which is all the probe needs to convey; the detail
// goes to the server log, where reaching it requires access to the host.
const checkFailed = "error"

// logReadinessFailure records the detail that the response deliberately omits.
func logReadinessFailure(ctx context.Context, check string, err error) {
	slog.WarnContext(ctx, "readiness check failed", "check", check, "error", err)
}

// handleHealth is the original combined health endpoint (engine availability
// as a best-effort signal). Kept unchanged for Contract Stability; new
// deployments should use the /healthz/live and /healthz/ready split below
// (the operational design §4.4).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.engineHealth != nil {
		if err := s.engineHealth.CheckHealth(r.Context()); err != nil {
			// Redacted for the same reason as /healthz/ready below: this
			// endpoint is unauthenticated too, and engine errors carry
			// configuration file paths.
			logReadinessFailure(r.Context(), "engine", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"engine": checkFailed,
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
// and while PostgreSQL or the configured engine check reports an error. Each
// dependency's outcome is reported under "checks" so
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
			logReadinessFailure(r.Context(), "setup", err)
			checks["setup"] = checkFailed
			healthy = false
		case count == 0:
			// Not redacted, unlike the cases around it. This one reports the
			// application's own state rather than a dependency's error, it is
			// what makes the probe hold a fresh deployment out of a load
			// balancer until an administrator completes setup, and the
			// disclosure is an accepted risk (docs/security/accepted-risks).
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
			logReadinessFailure(ctx, "postgres", err)
			checks["postgres"] = checkFailed
			healthy = false
		} else {
			checks["postgres"] = "ok"
		}
	}

	if s.engineHealth != nil {
		if err := s.engineHealth.CheckHealth(r.Context()); err != nil {
			logReadinessFailure(r.Context(), "engine", err)
			checks["engine"] = checkFailed
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
