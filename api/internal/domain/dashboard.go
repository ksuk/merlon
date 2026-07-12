package domain

type DashboardStats struct {
	CustomersByRiskTier map[string]int `json:"customers_by_risk_tier"`
	TotalCustomers      int            `json:"total_customers"`
	AlertsByStatus      map[string]int `json:"alerts_by_status"`
	AlertsBySeverity    map[string]int `json:"alerts_by_severity"`
	TotalAlerts         int            `json:"total_alerts"`
	CasesByStatus       map[string]int `json:"cases_by_status"`
	TotalCases          int            `json:"total_cases"`
	RecentTransactions  int            `json:"recent_transactions"`

	// ScreeningListFreshness reports each configured sanctions/PEP list's
	// staleness (the screening workflow "リストの鮮度情報（最終更新日時）をダッシュボードに表示
	// する"). Empty when no list has completed an import yet.
	ScreeningListFreshness []ScreeningListFreshnessStat `json:"screening_list_freshness,omitempty"`
}

// ScreeningListFreshnessStat is one sanctions/PEP list's dashboard
// freshness display row.
type ScreeningListFreshnessStat struct {
	ListID                string `json:"list_id"`
	ListType              string `json:"list_type"`
	StaleDays             int    `json:"stale_days"`
	NeedsOperationalAlert bool   `json:"needs_operational_alert"`
}
