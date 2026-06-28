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
}
