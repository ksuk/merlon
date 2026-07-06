package server

import (
	"context"
	"github.com/merlon-aml/merlon/api/internal/apierr"
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/screening"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats := domain.DashboardStats{
		CustomersByRiskTier: make(map[string]int),
		AlertsByStatus:      make(map[string]int),
		AlertsBySeverity:    make(map[string]int),
		CasesByStatus:       make(map[string]int),
	}

	customers, err := s.customers.List(ctx, 10000, 0)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	stats.TotalCustomers = len(customers)
	for _, c := range customers {
		tier := "unscored"
		if c.RiskTier != nil {
			tier = string(*c.RiskTier)
		}
		stats.CustomersByRiskTier[tier]++
	}

	openAlerts, err := s.alerts.ListOpen(ctx, 10000, 0)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	for _, a := range openAlerts {
		stats.AlertsByStatus[string(a.Status)]++
		stats.AlertsBySeverity[string(a.Severity)]++
		stats.TotalAlerts++
	}

	if s.cases != nil {
		cases, err := s.cases.ListOpen(ctx, 10000, 0)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		stats.TotalCases = len(cases)
		for _, c := range cases {
			stats.CasesByStatus[string(c.Status)]++
		}
	}

	if s.screeningListStore != nil && s.screeningFailureTracker != nil {
		stats.ScreeningListFreshness = s.screeningListFreshness(ctx)
	}

	writeJSON(w, http.StatusOK, stats)
}

// screeningListFreshness reports each configured list's staleness
// (screening.md "リストの鮮度情報（最終更新日時）をダッシュボードに表示する"). A list
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
