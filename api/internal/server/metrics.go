package server

import (
	"net/http"

	// Blank-imported so its init() registers all OPS-003 metrics (and seeds
	// their zero-value series) into the default Prometheus registry that
	// promhttp.Handler() below serves, even if no handler in this package
	// references a metrics.* variable directly yet.
	_ "github.com/ksuk/merlon/api/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// handleMetrics exposes the Prometheus text exposition format (OPS-003,
// the operational design §4.4) by delegating to the global promauto registry that
// api/internal/metrics registers into.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}
