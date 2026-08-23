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

	// Workload answers the start-of-day questions the page could not: what is
	// assigned to me, what is unassigned, what is oldest, what is overdue.
	// Nil when the caller has no identity to scope "mine" against, which is
	// itself worth showing rather than reporting zero.
	Workload *DashboardWorkload `json:"workload,omitempty"`

	// Exceptions summarises operational work that failed or degraded. An empty
	// list means nothing is failing; a nil one means nothing was checked.
	Exceptions []DashboardException `json:"exceptions"`

	// CDDReviewQueue is populated when the durable review store is configured;
	// omitting it keeps older deployments' dashboard contract unchanged.
	CDDReviewQueue *CustomerReviewQueueStats `json:"cdd_review_queue,omitempty"`
}

type CustomerReviewQueueStats struct {
	Due       int `json:"due"`
	Overdue   int `json:"overdue"`
	ColdStart int `json:"cold_start"`
}

// DashboardWorkload is the operator-facing queue summary. Every count states
// the scope it was taken over, because "12 alerts" means nothing without
// knowing whose.
type DashboardWorkload struct {
	// Scope names whose work these figures cover: the operator identity used
	// for "mine", or empty when the deployment has no identity to use.
	Scope  string         `json:"scope"`
	Alerts WorkloadCounts `json:"alerts"`
	Cases  WorkloadCounts `json:"cases"`
	// SLA carries the policy that produced the due figures, so a reader can
	// tell an unconfigured deployment from one meeting every deadline.
	SLA DashboardSLA `json:"sla"`
	// EvaluatedAt is when these counts were taken.
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// WorkloadCounts is one queue's ownership and age picture.
type WorkloadCounts struct {
	Open       int `json:"open"`
	Mine       int `json:"mine"`
	Unassigned int `json:"unassigned"`
	// OldestOpenAt is nil on an empty queue. An age of zero would read as
	// "something arrived just now".
	OldestOpenAt     *time.Time `json:"oldest_open_at,omitempty"`
	OldestAgeSeconds *int64     `json:"oldest_age_seconds,omitempty"`
	// AgeBuckets counts open work by how long it has been open. The bucket
	// boundaries are reported alongside so the UI never restates them.
	AgeBuckets []AgeBucket `json:"age_buckets"`
	// Overdue is only meaningful when an SLA policy is configured; it is nil
	// otherwise, which is different from zero.
	Overdue *int `json:"overdue,omitempty"`
	DueSoon *int `json:"due_soon,omitempty"`
}

// AgeBucket is one documented age band and its count.
type AgeBucket struct {
	Label     string `json:"label"`
	FromHours int    `json:"from_hours"`
	// ToHours is 0 for the final open-ended bucket.
	ToHours int `json:"to_hours,omitempty"`
	Count   int `json:"count"`
}

// DashboardSLA states whether deadlines exist at all before any count is read.
type DashboardSLA struct {
	// State is not_configured when this deployment declared no rules. The UI
	// must render that distinctly: it is neither zero nor healthy
	// (ADR-0024, DR-07).
	State         string `json:"state"`
	PolicyVersion string `json:"policy_version"`
	// DueSoonWithinHours documents the window "due soon" was taken over.
	DueSoonWithinHours int `json:"due_soon_within_hours,omitempty"`
}

// DashboardException is one operational failure an operator has to act on.
type DashboardException struct {
	// Kind is a stable machine value the UI localizes.
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	// Href is the pre-filtered queue that explains the count. A number an
	// operator cannot open is a number they cannot act on.
	Href string `json:"href"`
	// State distinguishes a failure from a degradation from an unknown.
	State string `json:"state"`
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
