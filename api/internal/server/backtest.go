package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
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
	Rationale          string                         `json:"rationale"`
	RerunOf            string                         `json:"rerun_of,omitempty"`
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
	if req.BaselineRuleSetID == req.CandidateRuleSetID {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "baseline_rule_set_id and candidate_rule_set_id must differ")
		return
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
	_, hasMetadata := s.wave3.(domain.BacktestMetadataRepository)
	var metadata *domain.BacktestMetadata
	if _, ok := s.wave3.(domain.BacktestMetadataRepository); ok {
		preview := map[string]any{"count": len(req.CustomerIDs), "sample_customer_ids": append([]string(nil), req.CustomerIDs...)}
		// Keep the cohort preview useful to an operator: the customer count alone
		// cannot distinguish an intentionally empty result from a cohort with no
		// transactions.  This is additive and remains best-effort for adapters
		// that do not expose the transaction repository.
		if s.transactions != nil && len(req.CustomerIDs) > 0 {
			transactionCount := 0
			for _, customerID := range req.CustomerIDs {
				if transactions, transactionErr := s.transactions.ListByCustomer(r.Context(), customerID, 1001, 0); transactionErr == nil {
					transactionCount += len(transactions)
				}
			}
			preview["transaction_count"] = transactionCount
		}
		if len(req.CustomerIDs) > 20 {
			preview["sample_customer_ids"] = req.CustomerIDs[:20]
		}
		metadata = &domain.BacktestMetadata{JobID: job.ID, Rationale: req.Rationale, CohortPreview: preview, BaselineSnapshot: map[string]any{"rule_set_id": job.BaselineRuleSetID, "version": job.BaselineRuleVersion, "digest": stableDigest(baselineDefinition)}, CandidateSnapshot: map[string]any{"rule_set_id": job.CandidateRuleSetID, "version": job.CandidateRuleVersion, "digest": stableDigest(candidateDefinition)}, RerunOf: req.RerunOf, CreatedAt: now}
	}
	persist := func(repos domain.AtomicMutationRepositories) error {
		jobRepo := s.backtestJobs
		if repos.BacktestJobs != nil {
			jobRepo = repos.BacktestJobs
		}
		if err := jobRepo.Create(r.Context(), job); err != nil {
			return err
		}
		if metadata != nil {
			wave3Repo, ok := repos.Wave3.(domain.BacktestMetadataRepository)
			if !ok {
				return fmt.Errorf("backtest metadata repository is not transaction-bound")
			}
			if err := wave3Repo.SaveBacktestMetadata(r.Context(), metadata); err != nil {
				return fmt.Errorf("backtest metadata persistence failed: %w", err)
			}
		}
		return nil
	}
	var persistErr error
	if s.atomic != nil && (hasMetadata || s.backtestJobs != nil) {
		persistErr = s.runAtomic(r.Context(), persist)
	} else {
		persistErr = persist(domain.AtomicMutationRepositories{})
	}
	if persistErr != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, persistErr.Error())
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
	if metadataRepo, ok := s.wave3.(domain.BacktestMetadataRepository); ok {
		if metadata, metadataErr := metadataRepo.GetBacktestMetadata(r.Context(), job.ID); metadataErr == nil {
			job.Metadata = metadata
		}
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
	jobID := r.PathValue("id")
	job, err := s.backtestJobs.Get(r.Context(), jobID)
	if err != nil {
		writeBacktestRepositoryError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	scenarioID := strings.TrimSpace(r.URL.Query().Get("scenario_id"))

	rows, total, err := s.backtestAffectedCustomers(r.Context(), job, scenarioID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	// data stays a plain id array: this route predates Wave 3 and clients read
	// it that way. delta_kinds is the additive half, keyed by the ids in data.
	ids := make([]string, 0, len(rows))
	for _, id := range orderedCustomerIDs(rows) {
		ids = append(ids, id)
	}
	kinds := domain.AggregateBacktestDeltaKinds(rows)
	response := map[string]any{
		"data":        ids,
		"delta_kinds": kinds,
		"rows":        rows,
		"pagination":  map[string]any{"limit": limit, "total": total, "has_more": len(ids) == limit && total > 0},
	}
	if len(ids) == limit {
		response["pagination"].(map[string]any)["next_cursor"] = ids[len(ids)-1]
	}
	writeJSON(w, http.StatusOK, response)
}

// backtestAffectedCustomers reads one keyset page of durable outcome rows.
// Jobs completed before migration 051 have no rows, so the pre-existing
// derivation from the stored result is kept as a fallback: an old job must not
// start reporting an empty population.
func (s *Server) backtestAffectedCustomers(ctx context.Context, job *domain.BacktestJob, scenarioID, cursor string, limit int) ([]domain.BacktestAffectedCustomer, int, error) {
	repo, ok := s.backtestJobs.(domain.BacktestAffectedCustomerRepository)
	if ok {
		filter := domain.BacktestAffectedCustomerFilter{JobID: job.ID, ScenarioID: scenarioID, AfterCustomerID: cursor}
		total, err := repo.CountBacktestAffectedCustomers(ctx, filter)
		if err != nil {
			return nil, 0, err
		}
		if total > 0 {
			// A customer can hold several scenario rows, so read generously and
			// trim on the customer boundary rather than the row boundary.
			rows, err := repo.ListBacktestAffectedCustomers(ctx, filter, limit*maxScenariosPerCustomerPage)
			if err != nil {
				return nil, 0, err
			}
			return trimToCustomerLimit(rows, limit), total, nil
		}
	}
	rows := domain.BacktestAffectedCustomersFrom(job.ID, job.Candidate, job.Delta)
	if scenarioID != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.ScenarioID == scenarioID {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	total := len(orderedCustomerIDs(rows))
	if cursor != "" {
		after := rows[:0]
		for _, row := range rows {
			if row.CustomerID > cursor {
				after = append(after, row)
			}
		}
		rows = after
	}
	return trimToCustomerLimit(rows, limit), total, nil
}

// maxScenariosPerCustomerPage bounds the over-read that keeps whole customers
// on one page. A customer matching more scenarios than this still appears;
// only some of its scenario rows spill to the next page.
const maxScenariosPerCustomerPage = 8

// trimToCustomerLimit cuts a row page at a customer boundary so a customer's
// scenario rows are never split in a way that would let the cursor skip them.
func trimToCustomerLimit(rows []domain.BacktestAffectedCustomer, limit int) []domain.BacktestAffectedCustomer {
	seen := 0
	var current string
	for i, row := range rows {
		if row.CustomerID != current {
			if seen == limit {
				return rows[:i]
			}
			current = row.CustomerID
			seen++
		}
	}
	return rows
}

// orderedCustomerIDs lists each customer once, in row order.
func orderedCustomerIDs(rows []domain.BacktestAffectedCustomer) []string {
	out := make([]string, 0, len(rows))
	var current string
	for _, row := range rows {
		if row.CustomerID != current {
			out = append(out, row.CustomerID)
			current = row.CustomerID
		}
	}
	return out
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
