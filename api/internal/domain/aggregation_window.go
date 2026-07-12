package domain

import "time"

// DailyAggregationWindowStart returns the UTC midnight boundary of the day
// containing t. It is used as Alert.AggregationWindowStart for scenarios
// whose aggregation period is a calendar day
// (the transaction-monitoring design「アラート統合ロジック」/「バッチ評価のスケジューリング」:
// 24h = 前日00:00:00〜23:59:59). Both the realtime evaluation path
// (server.handleBatchMonitor) and the daily batch job (batch.RunTMBatchEvaluation)
// call this with the alert's detection time, so a same-day scenario alert
// dedups correctly under the (customer_id, scenario_id,
// aggregation_window_start) constraint regardless of which path creates it
// first. UTC is used rather than a configurable system timezone so both
// paths always agree on the boundary without threading timezone config
// through every alert-creation call site.
func DailyAggregationWindowStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
