package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/casemgmt"
	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/metrics"
)

type UpdateAlertStatusRequest struct {
	Status     domain.AlertStatus `json:"status"`
	ResolvedBy string             `json:"resolved_by"`
	// ExpectedUpdatedAt enables optimistic locking (data-model.md §3.9,
	// WS-11 Task 8): when set, the update is rejected with 409 if the
	// alert's stored updated_at no longer matches. Omitted entirely, the
	// update proceeds unconditionally (legacy callers).
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

func alertCursor(a domain.Alert) Cursor {
	return Cursor{CreatedAt: a.CreatedAt, ID: a.ID}
}

// recordAlertCreated increments merlon_alerts_total (OPS-003, overview.md
// §4.4) for a single newly created alert. Call this exactly once per alert,
// right after its creation is confirmed, to avoid double-counting.
func recordAlertCreated(a *domain.Alert) {
	metrics.AlertsTotal.WithLabelValues(a.ScenarioID, string(a.Severity)).Inc()
}

// consolidateAlertIntoCase joins a into an existing open case for the same
// customer within casemgmt.DefaultConsolidationWindow, or creates a new one
// (transaction-monitoring.md「アラート統合ロジック」). Call this once per
// newly created alert, after it has been persisted. A failure here (e.g.
// the case store being unavailable) is logged and does not roll back or
// fail the alert creation itself; the alert still exists and can be
// consolidated later on retry/manual review (Fail-Alert: never lose an
// alert over a case-management side effect).
func (s *Server) consolidateAlertIntoCase(ctx context.Context, a *domain.Alert) {
	if s.cases == nil {
		return
	}
	if _, err := casemgmt.ConsolidateAlert(ctx, s.cases, a, casemgmt.DefaultConsolidationWindow); err != nil {
		slog.Error("failed to consolidate alert into case",
			"alert_id", a.ID, "customer_id", a.CustomerID, "error", err)
	}
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	customerID := r.URL.Query().Get("customer_id")

	if r.URL.Query().Get("cursor") != "" {
		s.handleListAlertsCursor(w, r, customerID)
		return
	}
	s.handleListAlertsOffset(w, r, customerID)
}

// handleListAlertsCursor serves api.md §1.1 cursor-based pagination.
func (s *Server) handleListAlertsCursor(w http.ResponseWriter, r *http.Request, customerID string) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fetchLimit := pageReq.Limit + 1
	after := toDomainCursor(pageReq.Cursor)

	var alerts []domain.Alert
	if customerID != "" {
		alerts, err = s.alerts.ListByCustomerCursor(r.Context(), customerID, fetchLimit, after)
	} else {
		alerts, err = s.alerts.ListOpenByCursor(r.Context(), fetchLimit, after)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(alerts, pageReq.Limit, alertCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListAlertsOffset preserves the pre-existing offset/limit contract
// (api.md §1.2 dual-support / deprecation period) while still returning the
// additive {"data", "pagination"} envelope.
func (s *Server) handleListAlertsOffset(w http.ResponseWriter, r *http.Request, customerID string) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var (
		alerts []domain.Alert
		err    error
	)

	if customerID != "" {
		alerts, err = s.alerts.ListByCustomer(r.Context(), customerID, limit+1, offset)
	} else {
		alerts, err = s.alerts.ListOpen(r.Context(), limit+1, offset)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	page, meta := BuildPaginationMeta(alerts, limit, alertCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
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

	if req.ExpectedUpdatedAt != nil {
		if err := s.alerts.UpdateStatusIfUnmodified(r.Context(), id, req.Status, req.ResolvedBy, *req.ExpectedUpdatedAt); err != nil {
			var conflict *domain.ErrConflict
			if errors.As(err, &conflict) {
				writeError(w, http.StatusConflict, conflict.Error())
				return
			}
			var notFound *domain.ErrNotFound
			if errors.As(err, &notFound) {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := s.alerts.UpdateStatus(r.Context(), id, req.Status, req.ResolvedBy); err != nil {
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
