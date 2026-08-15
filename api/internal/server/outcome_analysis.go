package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

func (s *Server) handleBacktestOutcomes(w http.ResponseWriter, r *http.Request) {
	if s.backtestJobs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable backtest jobs not configured")
		return
	}
	repository, ok := s.backtestJobs.(domain.BacktestOutcomeRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "backtest outcome analysis is not configured")
		return
	}
	jobID := r.PathValue("id")
	if _, err := s.backtestJobs.Get(r.Context(), jobID); err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	page, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	filter := domain.BacktestOutcomeFilter{JobID: jobID, Variant: domain.OutcomeVariant(strings.TrimSpace(r.URL.Query().Get("variant"))), ScenarioID: strings.TrimSpace(r.URL.Query().Get("scenario_id")), Label: strings.TrimSpace(r.URL.Query().Get("label")), Cursor: toDomainCursor(page.Cursor), Limit: page.Limit + 1}
	if filter.Variant != "" && filter.Variant != domain.OutcomeVariantBaseline && filter.Variant != domain.OutcomeVariantCandidate && filter.Variant != domain.OutcomeVariantDelta {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "variant is invalid")
		return
	}
	if filter.Label != "" && filter.Label != "TP" && filter.Label != "FP" && filter.Label != "unlabeled" && filter.Label != "unevaluable" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "label is invalid")
		return
	}
	items, err := repository.ListBacktestOutcomeDetails(r.Context(), filter)
	if err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	data, pagination := BuildPaginationMeta(items, page.Limit, func(item domain.BacktestOutcomeDetail) Cursor { return Cursor{CreatedAt: item.CreatedAt, ID: item.ID} })
	analysis, analysisErr := repository.GetBacktestOutcomeAnalysis(r.Context(), jobID)
	if analysisErr != nil {
		writeBacktestRepositoryError(w, analysisErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"outcome_analysis": analysis, "data": data, "pagination": pagination})
}

type createCoverageAnalysisRequest struct {
	ScenarioIDs []string `json:"scenario_ids,omitempty"`
	CustomerIDs []string `json:"customer_ids,omitempty"`
	SnapshotAt  string   `json:"snapshot_at,omitempty"`
}

func (s *Server) handleCreateCoverageAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.coverageAnalyses == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "coverage analysis is not configured")
		return
	}
	var req createCoverageAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	snapshot := time.Now().UTC()
	if strings.TrimSpace(req.SnapshotAt) != "" {
		parsed, err := time.Parse(time.RFC3339, req.SnapshotAt)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "snapshot_at must be RFC3339")
			return
		}
		snapshot = parsed.UTC()
	}
	analysis, err := s.coverageAnalyses.Create(r.Context(), &domain.CoverageAnalysis{ScenarioIDs: uniqueNonEmpty(req.ScenarioIDs), CustomerIDs: uniqueNonEmpty(req.CustomerIDs), SnapshotAt: snapshot})
	if err != nil {
		writeCoverageError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/coverage-analyses/"+analysis.ID)
	writeJSON(w, http.StatusAccepted, analysis)
}

func (s *Server) handleListCoverageAnalyses(w http.ResponseWriter, r *http.Request) {
	if s.coverageAnalyses == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "coverage analysis is not configured")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	status := domain.CoverageAnalysisStatus(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && status != domain.CoverageAnalysisQueued && status != domain.CoverageAnalysisRunning && status != domain.CoverageAnalysisCompleted && status != domain.CoverageAnalysisFailed {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "status is invalid")
		return
	}
	items, err := s.coverageAnalyses.List(r.Context(), domain.CoverageAnalysisFilter{Status: status, Limit: limit + 1, Offset: offset})
	if err != nil {
		writeCoverageError(w, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "pagination": map[string]any{"limit": limit, "offset": offset, "has_more": hasMore}})
}

func (s *Server) handleGetCoverageAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.coverageAnalyses == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "coverage analysis is not configured")
		return
	}
	item, err := s.coverageAnalyses.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCoverageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleListCoverageMatters(w http.ResponseWriter, r *http.Request) {
	if s.coverageAnalyses == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "coverage analysis is not configured")
		return
	}
	page, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	items, err := s.coverageAnalyses.Matters(r.Context(), domain.CoverageMatterFilter{AnalysisID: r.PathValue("id"), ScenarioID: strings.TrimSpace(r.URL.Query().Get("scenario_id")), Label: strings.TrimSpace(r.URL.Query().Get("label")), Cursor: toDomainCursor(page.Cursor), Limit: page.Limit + 1})
	if err != nil {
		writeCoverageError(w, err)
		return
	}
	data, pagination := BuildPaginationMeta(items, page.Limit, func(item domain.CoverageMatterResult) Cursor { return Cursor{CreatedAt: item.CreatedAt, ID: item.ID} })
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "pagination": pagination})
}

func writeCoverageError(w http.ResponseWriter, err error) {
	writeBacktestRepositoryError(w, err)
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
