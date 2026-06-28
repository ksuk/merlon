package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type UpdateAlertStatusRequest struct {
	Status     domain.AlertStatus `json:"status"`
	ResolvedBy string             `json:"resolved_by"`
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	customerID := r.URL.Query().Get("customer_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var (
		alerts []domain.Alert
		err    error
	)

	if customerID != "" {
		alerts, err = s.alerts.ListByCustomer(r.Context(), customerID, limit, offset)
	} else {
		alerts, err = s.alerts.ListOpen(r.Context(), limit, offset)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if alerts == nil {
		alerts = []domain.Alert{}
	}

	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.alerts.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleUpdateAlertStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req UpdateAlertStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status required")
		return
	}

	if err := s.alerts.UpdateStatus(r.Context(), id, req.Status, req.ResolvedBy); err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a, _ := s.alerts.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, a)
}
