package server

import (
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const dashboardRecentTransactionWindow = 24 * time.Hour

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats := domain.DashboardStats{
		CustomersByRiskTier:           make(map[string]int),
		AlertsByStatus:                make(map[string]int),
		AlertsBySeverity:              make(map[string]int),
		CasesByStatus:                 make(map[string]int),
		RecentTransactionsWindowHours: int(dashboardRecentTransactionWindow / time.Hour),
	}

	customerDashboard, ok := s.customers.(domain.CustomerDashboardRepository)
	if !ok {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "customer dashboard aggregate is not configured")
		return
	}
	customerCounts, err := customerDashboard.DashboardRiskTierCounts(ctx)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	stats.CustomersByRiskTier = customerCounts
	for _, count := range customerCounts {
		stats.TotalCustomers += count
	}

	alertDashboard, ok := s.alerts.(domain.AlertDashboardRepository)
	if !ok {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "alert dashboard aggregate is not configured")
		return
	}
	alertStatusCounts, alertSeverityCounts, err := alertDashboard.DashboardUnresolvedCounts(ctx)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	stats.AlertsByStatus = alertStatusCounts
	stats.AlertsBySeverity = alertSeverityCounts
	for _, count := range alertStatusCounts {
		stats.TotalAlerts += count
	}

	if s.cases != nil {
		caseDashboard, ok := s.cases.(domain.CaseDashboardRepository)
		if !ok {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "case dashboard aggregate is not configured")
			return
		}
		caseStatusCounts, err := caseDashboard.DashboardUnresolvedCounts(ctx)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		stats.CasesByStatus = caseStatusCounts
		for _, count := range caseStatusCounts {
			stats.TotalCases += count
		}
	}

	if transactionDashboard, ok := s.transactions.(domain.TransactionDashboardRepository); ok {
		stats.RecentTransactions, err = transactionDashboard.CountExecutedSince(ctx, time.Now().UTC().Add(-dashboardRecentTransactionWindow))
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
	}

	if s.wave3 != nil || s.screeningListStore != nil || s.screeningFailureTracker != nil || len(s.screeningListIDs) > 0 {
		sources, sourceErr := s.screeningSourceStatuses(ctx, s.screeningListIDs, 0)
		if sourceErr != nil {
			sources = unavailableSourceStatuses(s.configuredScreeningSourceIDs(s.screeningListIDs), s.screeningSourceThresholds(0), "source status unavailable")
		}
		for _, source := range sources {
			stat := domain.ScreeningListFreshnessStat{ListID: source.ListID, ListType: source.ListType, OperationalState: source.OperationalState, LastAttemptAt: source.LastAttemptAt, LastSuccessAt: source.LastSuccessAt, AgeSeconds: source.AgeSeconds, Diagnostic: source.Diagnostic}
			if source.AgeSeconds != nil {
				stat.StaleDays = int(*source.AgeSeconds / 86400)
			}
			stat.NeedsOperationalAlert = source.OperationalState != domain.ScreeningSourceReady
			stats.ScreeningListFreshness = append(stats.ScreeningListFreshness, stat)
		}
		required := s.policies.ScreeningReadiness().Required
		stats.ScreeningDegradedSources = unreadyRequiredSources(sources, required)
		stats.ScreeningReady = len(stats.ScreeningDegradedSources) == 0
	} else {
		// Nothing to assess: no source directory is wired at all. Claiming
		// readiness here would be inventing a fact.
		stats.ScreeningReady = true
	}

	now := time.Now().UTC()
	workload, err := s.dashboardWorkload(r, now)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	stats.Workload = workload
	stats.Exceptions = s.dashboardExceptions(ctx, &stats, now)
	if s.reviews != nil {
		items, reviewErr := s.reviews.List(ctx, domain.CustomerReviewFilter{Statuses: []domain.CustomerReviewStatus{domain.CustomerReviewStatusScheduled, domain.CustomerReviewStatusDue, domain.CustomerReviewStatusOverdue}, AsOf: now, Limit: 10000})
		if reviewErr == nil {
			queue := &domain.CustomerReviewQueueStats{}
			for _, item := range items {
				switch item.Status {
				case domain.CustomerReviewStatusOverdue:
					queue.Overdue++
				case domain.CustomerReviewStatusDue:
					queue.Due++
				}
				if item.Cycle == 1 && item.PreviousScoreID == "" {
					queue.ColdStart++
				}
			}
			stats.CDDReviewQueue = queue
		}
	}

	writeJSON(w, http.StatusOK, stats)
}
