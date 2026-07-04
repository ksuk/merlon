package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/screening"
)

// ScreeningCheckRequest is the body for POST /api/v1/screening/check, the
// explicit-request immediate rescreen trigger (screening.md 即時再照合契機
// "基幹からの明示的リクエスト").
type ScreeningCheckRequest struct {
	CustomerID string   `json:"customer_id"`
	ListIDs    []string `json:"list_ids"`
}

func (s *Server) handleScreeningCheck(w http.ResponseWriter, r *http.Request) {
	if s.screening == nil {
		writeError(w, http.StatusServiceUnavailable, "screening engine not configured")
		return
	}

	var req ScreeningCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.CustomerID == "" {
		writeError(w, http.StatusBadRequest, "customer_id is required")
		return
	}

	deps := screening.SchedulerDeps{
		Customers:        s.customers,
		Screening:        s.screening,
		Results:          s.screeningResults,
		ListIDs:          req.ListIDs,
		TargetCustomerID: req.CustomerID,
	}

	result, err := screening.RunRescreeningBatch(r.Context(), deps, screening.TriggerAPIRequest)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
