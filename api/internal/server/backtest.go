package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

type createBacktestJobRequest struct {
	From               string                         `json:"from"`
	To                 string                         `json:"to"`
	CustomerIDs        []string                       `json:"customer_ids,omitempty"`
	CustomerFilter     *domain.BacktestCustomerFilter `json:"customer_filter,omitempty"`
	ScenarioIDs        []string                       `json:"scenario_ids,omitempty"`
	BaselineRuleSetID  string                         `json:"baseline_rule_set_id"`
	CandidateRuleSetID string                         `json:"candidate_rule_set_id"`
}

func parseBacktestUTC(value string) (time.Time, error) {
	if value == "" || !strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Errorf("timestamp must be UTC RFC3339 ending in Z")
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func (s *Server) handleCreateBacktestJob(w http.ResponseWriter, r *http.Request) {
	if s.backtestJobs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable backtest jobs not configured")
		return
	}
	var req createBacktestJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	from, err := parseBacktestUTC(req.From)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "from: "+err.Error())
		return
	}
	to, err := parseBacktestUTC(req.To)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "to: "+err.Error())
		return
	}
	if !to.After(from) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "to must be after from")
		return
	}
	if (len(req.CustomerIDs) == 0) == (req.CustomerFilter == nil) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "exactly one of customer_ids or customer_filter is required")
		return
	}
	seenIDs := make(map[string]struct{}, len(req.CustomerIDs))
	for i, id := range req.CustomerIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_ids must not contain empty IDs")
			return
		}
		req.CustomerIDs[i] = id
		if _, exists := seenIDs[id]; exists {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_ids must be unique")
			return
		}
		seenIDs[id] = struct{}{}
	}
	if req.CustomerFilter != nil {
		if req.CustomerFilter.RiskTier != "" && req.CustomerFilter.RiskTier != domain.RiskTierLow && req.CustomerFilter.RiskTier != domain.RiskTierMedium && req.CustomerFilter.RiskTier != domain.RiskTierHigh {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_filter.risk_tier is invalid")
			return
		}
		switch req.CustomerFilter.Status {
		case "", domain.CustomerStatusActive, domain.CustomerStatusDormant, domain.CustomerStatusFrozen, domain.CustomerStatusClosed:
		default:
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "customer_filter.status is invalid")
			return
		}
	}
	if strings.TrimSpace(req.CandidateRuleSetID) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "candidate_rule_set_id is required")
		return
	}
	if req.BaselineRuleSetID == "" {
		req.BaselineRuleSetID = "active"
	}
	resolveRule := func(id string) (json.RawMessage, int, error) {
		if id == "" || id == "active" {
			return nil, 0, nil
		}
		if s.rules == nil {
			return nil, 0, fmt.Errorf("rule repository is not configured")
		}
		rule, ruleErr := s.rules.GetActive(r.Context(), id)
		if ruleErr != nil {
			var notFound *domain.ErrNotFound
			if errors.As(ruleErr, &notFound) {
				return nil, 0, fmt.Errorf("rule set %q was not found", id)
			}
			return nil, 0, ruleErr
		}
		if rule == nil || rule.Type != domain.RuleTypeTMScenario {
			return nil, 0, fmt.Errorf("rule set %q must be an active TM_SCENARIO", id)
		}
		return append(json.RawMessage(nil), rule.Definition...), rule.Version, nil
	}
	baselineDefinition, baselineVersion, err := resolveRule(req.BaselineRuleSetID)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "baseline_rule_set_id: "+err.Error())
		return
	}
	candidateDefinition, candidateVersion, err := resolveRule(req.CandidateRuleSetID)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "candidate_rule_set_id: "+err.Error())
		return
	}
	now := time.Now().UTC()
	job := &domain.BacktestJob{ID: generateID(), Status: domain.BacktestJobQueued, From: from, To: to, CustomerIDs: req.CustomerIDs, CustomerFilter: req.CustomerFilter, ScenarioIDs: req.ScenarioIDs, BaselineRuleSetID: req.BaselineRuleSetID, CandidateRuleSetID: req.CandidateRuleSetID, BaselineRuleVersion: baselineVersion, CandidateRuleVersion: candidateVersion, BaselineRuleDefinition: baselineDefinition, CandidateRuleDefinition: candidateDefinition, ConfigDigests: copyStringMap(s.configDigests), SnapshotAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.backtestJobs.Create(r.Context(), job); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	w.Header().Set("Location", "/api/v1/backtests/"+job.ID)
	writeJSON(w, http.StatusAccepted, job)
}

func copyStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (s *Server) handleListBacktestJobs(w http.ResponseWriter, r *http.Request) {
	if s.backtestJobs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable backtest jobs not configured")
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
	jobs, err := s.backtestJobs.List(r.Context(), limit, offset)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": jobs, "pagination": map[string]any{"limit": limit, "offset": offset}})
}
func (s *Server) handleGetBacktestJob(w http.ResponseWriter, r *http.Request) {
	if s.backtestJobs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable backtest jobs not configured")
		return
	}
	job, err := s.backtestJobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}
func (s *Server) handleCancelBacktestJob(w http.ResponseWriter, r *http.Request) {
	if s.backtestJobs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable backtest jobs not configured")
		return
	}
	if err := s.backtestJobs.Cancel(r.Context(), r.PathValue("id")); err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	job, err := s.backtestJobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}
func (s *Server) handleBacktestAffectedCustomers(w http.ResponseWriter, r *http.Request) {
	if s.backtestJobs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "durable backtest jobs not configured")
		return
	}
	job, err := s.backtestJobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	var ids []string
	if job.Candidate != nil {
		for _, sr := range job.Candidate.ScenarioResults {
			ids = append(ids, sr.AffectedCustomerIDs...)
		}
	}
	sort.Strings(ids)
	ids = uniqueStrings(ids)
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset > len(ids) {
		offset = len(ids)
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": ids[offset:end], "pagination": map[string]any{"limit": limit, "offset": offset, "total": len(ids)}})
}

func writeBacktestRepositoryError(w http.ResponseWriter, err error) {
	var notFound *domain.ErrNotFound
	if errors.As(err, &notFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
		return
	}
	var conflict *domain.ErrConflict
	if errors.As(err, &conflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}

func uniqueStrings(in []string) []string {
	out := []string{}
	for _, v := range in {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

const maxBacktestCustomers = 100

type RunBacktestRequest struct {
	CustomerIDs []string `json:"customer_ids"`
	ScenarioIDs []string `json:"scenario_ids"`
	Description string   `json:"description"`
}

func (s *Server) handleRunBacktest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", "2026-10-01")
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
