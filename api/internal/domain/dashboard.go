package domain

import "time"

type DashboardStats struct {
	CustomersByRiskTier map[string]int `json:"customers_by_risk_tier"`
	TotalCustomers      int            `json:"total_customers"`
	AlertsByStatus      map[string]int `json:"alerts_by_status"`
	AlertsBySeverity    map[string]int `json:"alerts_by_severity"`
	TotalAlerts         int            `json:"total_alerts"`
	CasesByStatus       map[string]int `json:"cases_by_status"`
	TotalCases          int            `json:"total_cases"`
	RecentTransactions  int            `json:"recent_transactions"`
	// RecentTransactionsWindowHours documents the fixed rolling window used
	// for RecentTransactions. Keeping the definition on the response prevents
	// the UI from presenting an unlabeled aggregate with an ambiguous scope.
	RecentTransactionsWindowHours int `json:"recent_transactions_window_hours"`

	// ScreeningListFreshness reports each configured sanctions/PEP list's
	// staleness (the screening workflow "リストの鮮度情報（最終更新日時）をダッシュボードに表示
	// する"). Empty when no list has completed an import yet.
	ScreeningListFreshness []ScreeningListFreshnessStat `json:"screening_list_freshness,omitempty"`

	// ScreeningReady is false when any source the screening_readiness policy
	// marks required is not usable. Results produced in that state are
	// recorded degraded, so the dashboard must say so rather than leave the
	// operator to infer it from a row of freshness numbers.
	ScreeningReady           bool     `json:"screening_ready"`
	ScreeningDegradedSources []string `json:"screening_degraded_sources,omitempty"`
}

// ScreeningListFreshnessStat is one sanctions/PEP list's dashboard
// freshness display row.
type ScreeningListFreshnessStat struct {
	ListID                string               `json:"list_id"`
	ListType              string               `json:"list_type"`
	StaleDays             int                  `json:"stale_days"`
	NeedsOperationalAlert bool                 `json:"needs_operational_alert"`
	OperationalState      ScreeningSourceState `json:"operational_state,omitempty"`
	LastAttemptAt         *time.Time           `json:"last_attempt_at,omitempty"`
	LastSuccessAt         *time.Time           `json:"last_success_at,omitempty"`
	AgeSeconds            *int64               `json:"age_seconds,omitempty"`
	Diagnostic            string               `json:"diagnostic,omitempty"`
}
