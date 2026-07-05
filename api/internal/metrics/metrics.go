// Package metrics defines Merlon's Prometheus-compatible metric registry
// (OPS-003, overview.md §4.4). Metric names and labels here must match the
// overview.md §4.4 "OPS-003 メトリクス一覧" table exactly; it is the
// contract other tooling (Grafana dashboards, WS-4's alerting) is built on.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AlertsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "merlon_alerts_total",
		Help: "Total number of alerts raised, by scenario and severity.",
	}, []string{"scenario_id", "severity"})

	CasesOpen = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "merlon_cases_open",
		Help: "Number of currently open cases, by status.",
	}, []string{"status"})

	ScreeningHitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "merlon_screening_hits_total",
		Help: "Total number of screening hits, by list type and disposition status.",
	}, []string{"list_type", "status"})

	CDDTierDistribution = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "merlon_cdd_tier_distribution",
		Help: "Number of customers currently in each CDD risk tier.",
	}, []string{"tier"})

	CDDTierAnomalyTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "merlon_cdd_tier_anomaly_total",
		Help: "Total number of anomalous CDD tier transitions detected.",
	})

	TxMissingFiatEquivalent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "merlon_tx_missing_fiat_equivalent_total",
		Help: "Total number of transactions missing a fiat currency equivalent.",
	})

	ScreeningListStaleDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "merlon_screening_list_stale_days",
		Help: "Days since the sanctions/PEP list was last refreshed, by list type.",
	}, []string{"list_type"})

	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "merlon_api_request_duration_seconds",
		Help: "REST API response time in seconds.",
	}, []string{"method", "path", "status"})

	GRPCRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "merlon_grpc_request_duration_seconds",
		Help: "gRPC call duration in seconds, as observed by the Go client.",
	}, []string{"method", "status"})

	DBPoolActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "merlon_db_pool_active_connections",
		Help: "Number of active PostgreSQL connections in the pool.",
	})

	WebhookDLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "merlon_webhook_dlq_depth",
		Help: "Number of events currently held in the webhook dead-letter queue.",
	})

	BatchEvaluationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "merlon_batch_evaluation_duration_seconds",
		Help: "Batch evaluation job execution time in seconds, by job type.",
	}, []string{"job_type"})

	CDDEventChainTruncatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "merlon_cdd_event_chain_truncated_total",
		Help: "Total number of CDD event chains truncated after exceeding the hop limit (cdd-scoring.md safety valve 4).",
	})

	// AuditIntegrityCheckFailedTotal is incremented by merlon-audit verify
	// (audit.md §7) whenever it detects an audit_logs anomaly (id gap,
	// created_at regression, or daily count drop). merlon-audit is a
	// one-shot CLI, not a long-running process scraped by Prometheus, so
	// this counter is only meaningful if the deployment pushes it to a
	// Pushgateway after each run; that wiring is a self-hosting operational
	// concern documented alongside the recommended daily cron
	// (docs/compliance/data-retention.md), not implemented here.
	AuditIntegrityCheckFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		// Name matches audit.md §6/§7 verbatim (merlon_audit_integrity_check_failed,
		// no _total suffix) — that name is the operational-alerting contract
		// documented for self-hosting deployments.
		Name: "merlon_audit_integrity_check_failed",
		Help: "Total number of merlon-audit verify runs that detected an audit_logs integrity anomaly.",
	})
)

// knownAlertSeverities/knownCaseStatuses/etc. seed zero-value series for
// known label combinations at process start, so /metrics shows every metric
// name (overview.md §4.4 OPS-003) even before any matching business event
// has occurred. Labels with unbounded cardinality (scenario_id, list_type)
// are seeded with a representative placeholder value rather than every
// possible value.
var (
	knownAlertSeverities  = []string{"low", "medium", "high", "critical"}
	knownOpenCaseStatuses = []string{"open", "investigating", "escalated"}
	knownListTypes        = []string{"sanctions", "pep"}
	knownScreeningStatus  = []string{"pending", "confirmed", "false_positive"}
	knownRiskTiers        = []string{"low", "medium", "high"}
	knownBatchJobTypes    = []string{"score", "monitor"}
)

func init() {
	for _, severity := range knownAlertSeverities {
		AlertsTotal.WithLabelValues("unspecified", severity).Add(0)
	}
	for _, status := range knownOpenCaseStatuses {
		CasesOpen.WithLabelValues(status).Add(0)
	}
	for _, listType := range knownListTypes {
		for _, status := range knownScreeningStatus {
			ScreeningHitsTotal.WithLabelValues(listType, status).Add(0)
		}
		ScreeningListStaleDays.WithLabelValues(listType).Set(0)
	}
	for _, tier := range knownRiskTiers {
		CDDTierDistribution.WithLabelValues(tier).Set(0)
	}
	APIRequestDuration.WithLabelValues("GET", "/healthz", "200").Observe(0)
	GRPCRequestDuration.WithLabelValues("unspecified", "ok").Observe(0)
	for _, jobType := range knownBatchJobTypes {
		BatchEvaluationDuration.WithLabelValues(jobType).Observe(0)
	}
}
