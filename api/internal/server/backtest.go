package server

import (
	"encoding/json"
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type RunBacktestRequest struct {
	CustomerIDs []string `json:"customer_ids"`
	ScenarioIDs []string `json:"scenario_ids"`
	Description string   `json:"description"`
}

func (s *Server) handleRunBacktest(w http.ResponseWriter, r *http.Request) {
	if s.backtest == nil {
		writeError(w, http.StatusServiceUnavailable, "backtest engine not configured")
		return
	}

	var req RunBacktestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if len(req.CustomerIDs) == 0 {
		writeError(w, http.StatusBadRequest, "customer_ids required")
		return
	}

	var customers []domain.Customer
	for _, id := range req.CustomerIDs {
		c, err := s.customers.Get(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, "customer not found: "+id)
			return
		}
		customers = append(customers, *c)
	}

	var allTxns []domain.Transaction
	for _, c := range customers {
		txns, err := s.transactions.ListByCustomer(r.Context(), c.ID, 10000, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		allTxns = append(allTxns, txns...)
	}

	if len(allTxns) == 0 {
		writeError(w, http.StatusBadRequest, "no transactions found for given customers")
		return
	}

	result, err := s.backtest.RunBacktest(r.Context(), customers, allTxns, req.ScenarioIDs, req.Description)
	if err != nil {
		writeError(w, http.StatusBadGateway, "backtest engine error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
