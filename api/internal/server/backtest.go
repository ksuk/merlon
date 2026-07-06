package server

import (
	"encoding/json"
	"github.com/merlon-aml/merlon/api/internal/apierr"
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

const maxBacktestCustomers = 100

type RunBacktestRequest struct {
	CustomerIDs []string `json:"customer_ids"`
	ScenarioIDs []string `json:"scenario_ids"`
	Description string   `json:"description"`
}

func (s *Server) handleRunBacktest(w http.ResponseWriter, r *http.Request) {
	if s.backtest == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "backtest engine not configured")
		return
	}

	var req RunBacktestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if len(req.CustomerIDs) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_ids required")
		return
	}
	if len(req.CustomerIDs) > maxBacktestCustomers {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "too many customer_ids (max 100)")
		return
	}

	var customers []domain.Customer
	for _, id := range req.CustomerIDs {
		c, err := s.customers.Get(r.Context(), id)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeNotFound, "customer not found: "+id)
			return
		}
		customers = append(customers, *c)
	}

	var allTxns []domain.Transaction
	for _, c := range customers {
		txns, err := s.transactions.ListByCustomer(r.Context(), c.ID, 1000, 0)
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		allTxns = append(allTxns, txns...)
	}

	if len(allTxns) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "no transactions found for given customers")
		return
	}

	result, err := s.backtest.RunBacktest(r.Context(), customers, allTxns, req.ScenarioIDs, req.Description)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, apierr.CodeEngineError, "backtest engine error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
