package server

import (
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		stats.TotalCases = len(cases)
		for _, c := range cases {
			stats.CasesByStatus[string(c.Status)]++
		}
	}

	writeJSON(w, http.StatusOK, stats)
}
