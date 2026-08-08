package server

import (
	"context"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/screening"
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

	if s.screeningListStore != nil && s.screeningFailureTracker != nil {
		stats.ScreeningListFreshness = s.screeningListFreshness(ctx)
	}

	writeJSON(w, http.StatusOK, stats)
}

// screeningListFreshness reports each configured list's staleness
// (the screening workflow "リストの鮮度情報（最終更新日時）をダッシュボードに表示する"). A list
// that has never completed an import yet is omitted rather than shown as
// freshly imported.
func (s *Server) screeningListFreshness(ctx context.Context) []domain.ScreeningListFreshnessStat {
	statuses := make([]screening.ListImportStatus, 0, len(s.screeningListIDs))
	for _, listID := range s.screeningListIDs {
		data, err := s.screeningListStore.GetList(ctx, listID)
		if err != nil {
			continue
		}
		lastSuccess, err := s.screeningFailureTracker.LastSuccessAt(ctx, listID)
		if err != nil {
			continue
		}
		statuses = append(statuses, screening.ListImportStatus{
			ListID: listID, ListType: data.ListType, LastSuccessAt: lastSuccess,
		})
	}

	out := make([]domain.ScreeningListFreshnessStat, 0, len(statuses))
	for _, f := range screening.ComputeListFreshness(statuses) {
		out = append(out, domain.ScreeningListFreshnessStat{
			ListID:                f.ListID,
			ListType:              f.ListType,
			StaleDays:             f.StaleDays,
			NeedsOperationalAlert: f.NeedsOperationalAlert,
		})
	}
	return out
}
