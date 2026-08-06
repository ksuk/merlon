package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

func screeningResultCursor(result domain.ScreeningResultRecord) Cursor {
	return Cursor{CreatedAt: result.CreatedAt, ID: result.ID}
}

func screeningRunCursor(run domain.ScreeningRun) Cursor {
	return Cursor{CreatedAt: run.CreatedAt, ID: run.ID}
}

func (s *Server) handleListScreeningResults(w http.ResponseWriter, r *http.Request) {
	workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable screening workflow not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		customerID = r.PathValue("id")
	}
	filter := domain.ScreeningResultFilter{CustomerID: customerID, ListID: r.URL.Query().Get("list_id")}
	filter.Status = domain.ScreeningResultStatus(r.URL.Query().Get("status"))
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "from must be RFC3339")
			return
		}
		filter.From = &parsed
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "to must be RFC3339")
			return
		}
		filter.To = &parsed
	}
	filter.Cursor = toDomainCursor(pageReq.Cursor)
	items, err := workflow.ListScreeningResults(r.Context(), filter, pageReq.Limit+1)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	page, meta := BuildPaginationMeta(items, pageReq.Limit, screeningResultCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleGetScreeningResult(w http.ResponseWriter, r *http.Request) {
	workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable screening workflow not configured")
		return
	}
	result, err := workflow.GetScreeningResult(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListScreeningResultHistory(w http.ResponseWriter, r *http.Request) {
	workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable screening workflow not configured")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	history, err := workflow.ListScreeningResultHistory(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	if history == nil {
		history = []domain.ScreeningResultHistoryEntry{}
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleListScreeningRuns(w http.ResponseWriter, r *http.Request) {
	workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable screening workflow not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	items, err := workflow.ListScreeningRuns(r.Context(), r.URL.Query().Get("customer_id"), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	page, meta := BuildPaginationMeta(items, pageReq.Limit, screeningRunCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleGetScreeningRun(w http.ResponseWriter, r *http.Request) {
	workflow, ok := s.wave3.(domain.ScreeningWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable screening workflow not configured")
		return
	}
	run, err := workflow.GetScreeningRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListScreeningSources(w http.ResponseWriter, r *http.Request) {
	ids := append([]string(nil), s.screeningListIDs...)
	if raw := strings.TrimSpace(r.URL.Query().Get("source_ids")); raw != "" {
		ids = strings.Split(raw, ",")
	}
	threshold := 72 * time.Hour
	if raw := r.URL.Query().Get("freshness_threshold_seconds"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "freshness_threshold_seconds must be a positive integer")
			return
		}
		threshold = time.Duration(n) * time.Second
	}
	items, err := s.screeningSourceStatuses(r.Context(), ids, threshold)
	if err != nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "configured_count": len(ids), "ready_count": countSourceState(items, domain.ScreeningSourceReady), "unready_count": len(items) - countSourceState(items, domain.ScreeningSourceReady)})
}

func countSourceState(items []domain.ScreeningSourceStatus, state domain.ScreeningSourceState) int {
	n := 0
	for _, item := range items {
		if item.OperationalState == state {
			n++
		}
	}
	return n
}
