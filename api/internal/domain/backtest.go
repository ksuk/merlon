package domain

type BacktestScenarioResult struct {
	ScenarioID          string   `json:"scenario_id"`
	AlertsGenerated     int      `json:"alerts_generated"`
	HighSeverityCount   int      `json:"high_severity_count"`
	MediumSeverityCount int      `json:"medium_severity_count"`
	LowSeverityCount    int      `json:"low_severity_count"`
	AffectedCustomerIDs []string `json:"affected_customer_ids"`
}

type BacktestResult struct {
	BacktestID        string                   `json:"backtest_id"`
	TotalTransactions int                      `json:"total_transactions"`
	TotalCustomers    int                      `json:"total_customers"`
	TotalAlerts       int                      `json:"total_alerts"`
	ScenarioResults   []BacktestScenarioResult `json:"scenario_results"`
	ExecutionTimeMs   float64                  `json:"execution_time_ms"`
}
