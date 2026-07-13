package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestMetricsAllRegistered verifies the operational design §4.4 OPS-003's metric list is
// all registered and has at least one series exposed at process start (zero
// value), so /metrics shows every metric name even before any business event
// occurs.
func TestMetricsAllRegistered(t *testing.T) {
	cases := []struct {
		name string
		c    prometheus.Collector
	}{
		{"merlon_alerts_total", AlertsTotal},
		{"merlon_cases_open", CasesOpen},
		{"merlon_screening_hits_total", ScreeningHitsTotal},
		{"merlon_cdd_tier_distribution", CDDTierDistribution},
		{"merlon_cdd_tier_anomaly_total", CDDTierAnomalyTotal},
		{"merlon_tx_missing_fiat_equivalent_total", TxMissingFiatEquivalent},
		{"merlon_screening_list_stale_days", ScreeningListStaleDays},
		{"merlon_api_request_duration_seconds", APIRequestDuration},
		{"merlon_grpc_request_duration_seconds", GRPCRequestDuration},
		{"merlon_db_pool_active_connections", DBPoolActiveConnections},
		{"merlon_webhook_dlq_depth", WebhookDLQDepth},
		{"merlon_batch_evaluation_duration_seconds", BatchEvaluationDuration},
		{"merlon_alert_persistence_failures_total", AlertPersistenceFailuresTotal},
		{"merlon_pending_evaluation_failures_total", PendingEvaluationFailuresTotal},
		{"merlon_cdd_event_chain_truncated_total", CDDEventChainTruncatedTotal},
	}

	for _, tc := range cases {
		if got := testutil.CollectAndCount(tc.c); got == 0 {
			t.Errorf("%s: expected at least one registered series (zero-value seeded), got 0", tc.name)
		}
	}
}
