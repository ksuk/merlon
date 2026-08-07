package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
)

type targetPreviewRequest struct {
	Operation      string            `json:"operation"`
	TargetMode     domain.TargetMode `json:"target_mode"`
	CustomerIDs    []string          `json:"customer_ids"`
	Filter         map[string]any    `json:"filter"`
	Criteria       string            `json:"criteria"`
	RuleSetID      string            `json:"rule_set_id"`
	RuleSetVersion int               `json:"rule_set_version"`
	Rationale      string            `json:"rationale"`
	IdempotencyKey string            `json:"idempotency_key"`
	TTLSeconds     int               `json:"ttl_seconds"`
}

func (s *Server) handlePreviewTargetManifest(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.wave3.(domain.TargetManifestRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "target manifest store not configured")
		return
	}
	var req targetPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.Operation == "" {
		req.Operation = "batch_score"
	}
	if req.TargetMode == "" {
		if len(req.CustomerIDs) > 0 {
			req.TargetMode = domain.TargetModeSelected
		} else {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "target_mode is required; an empty selection is not all customers")
			return
		}
	}
	if req.TargetMode != domain.TargetModeSelected && req.TargetMode != domain.TargetModeFilter && req.TargetMode != domain.TargetModeAll {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "target_mode must be selected, filter, or all")
		return
	}
	if req.TargetMode == domain.TargetModeSelected && len(req.CustomerIDs) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "selected target requires customer_ids")
		return
	}
	if req.TargetMode == domain.TargetModeFilter && len(req.Filter) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "filter target requires filter")
		return
	}
	if req.TargetMode == domain.TargetModeAll && len(req.CustomerIDs) > 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "all target cannot include customer_ids")
		return
	}
	for key := range req.Filter {
		switch key {
		case "risk_tier", "status", "country_code", "customer_type":
		default:
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "unsupported target filter: "+key)
			return
		}
	}
	ids := []string{}
	switch req.TargetMode {
	case domain.TargetModeSelected:
		ids = append(ids, req.CustomerIDs...)
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			canonical := domain.CanonicalUUID(id)
			if _, exists := seen[canonical]; exists {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "selected customer_ids must be unique")
				return
			}
			seen[canonical] = struct{}{}
			if _, err := s.customers.Get(r.Context(), id); err != nil {
				var notFound *domain.ErrNotFound
				if errors.As(err, &notFound) {
					writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
				} else {
					writeWave3Error(w, err)
				}
				return
			}
		}
	case domain.TargetModeFilter, domain.TargetModeAll:
		resolved, err := s.resolveTargetPopulation(r.Context(), req.TargetMode, req.Filter)
		if err != nil {
			var tooLarge *errTargetPopulationTooLarge
			if errors.As(err, &tooLarge) {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, tooLarge.Error())
				return
			}
			writeWave3Error(w, err)
			return
		}
		ids = resolved
	}
	if len(ids) > maxTargetMatches {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "target exceeds the maximum of 10000 customers")
		return
	}
	sample := append([]string(nil), ids...)
	if len(sample) > 20 {
		sample = sample[:20]
	}
	ttl := 15 * time.Minute
	if req.TTLSeconds > 0 {
		if req.TTLSeconds < 30 || req.TTLSeconds > 24*60*60 {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "ttl_seconds must be between 30 and 86400")
			return
		}
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	criteria := req.Criteria
	if criteria == "" {
		encoded, _ := json.Marshal(req.Filter)
		criteria = string(encoded)
	}
	token := generateID() + generateID()
	manifest := &domain.TargetManifest{ID: generateID(), Operation: req.Operation, TargetMode: req.TargetMode, CustomerIDs: ids, Filter: req.Filter, SampleCustomerIDs: sample, TargetCount: len(ids), Criteria: criteria, RuleSetID: req.RuleSetID, RuleSetVersion: req.RuleSetVersion, ConfigDigests: copyStringMap(s.configDigests), Token: token, IdempotencyKey: firstNonEmpty(req.IdempotencyKey, r.Header.Get("Idempotency-Key")), Rationale: req.Rationale, Status: "preview", Version: 1, ExpiresAt: time.Now().UTC().Add(ttl), CreatedBy: resolveAuditUserID(r), CreatedAt: time.Now().UTC()}
	if err := repo.CreateTargetManifest(r.Context(), manifest); err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

const (
	// targetResolutionPageSize is how many customers are read per page while
	// resolving a filter. It is a memory bound, not a limit on the answer.
	targetResolutionPageSize = 1000
	// maxTargetMatches is the largest population one manifest may cover.
	maxTargetMatches = 10000
	// maxTargetScan bounds the work one preview request may do. Reaching it
	// is reported as an error rather than answered with a partial population:
	// a manifest is a promise about exactly who a batch will touch.
	maxTargetScan = 200000
)

// errTargetPopulationTooLarge is returned when the book is too large to
// resolve within maxTargetScan. It is deliberately distinct from "too many
// matches": the operator's remedy is different (narrow the filter so fewer
// rows must be examined, rather than so fewer match).
type errTargetPopulationTooLarge struct{ scanned int }

func (e *errTargetPopulationTooLarge) Error() string {
	return fmt.Sprintf("customer population is too large to resolve in one manifest (scanned %d rows); narrow the filter", e.scanned)
}

// resolveTargetPopulation pages through the whole customer book applying the
// filter.
//
// It previously read a single page of 10,001 customers and filtered that. On a
// book of 50,000 customers, a filter matching 200 of them produced a manifest
// holding only the matches that happened to fall in the first 10,001 rows --
// with no error, no warning, and a target_count the operator would reasonably
// read as "these are all of them". The batch then ran against a silently
// truncated population.
func (s *Server) resolveTargetPopulation(ctx context.Context, mode domain.TargetMode, filter map[string]any) ([]string, error) {
	ids := []string{}
	scanned := 0
	var cursor *domain.Cursor
	for {
		page, err := s.customers.ListByCursor(ctx, targetResolutionPageSize, cursor)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return ids, nil
		}
		for _, c := range page {
			scanned++
			if mode == domain.TargetModeFilter && !matchesCustomerFilter(c, filter) {
				continue
			}
			ids = append(ids, c.ID)
			if len(ids) > maxTargetMatches {
				// The caller reports this as the match-count error; returning
				// early keeps a runaway filter from reading the whole book.
				return ids, nil
			}
		}
		if scanned >= maxTargetScan {
			return nil, &errTargetPopulationTooLarge{scanned: scanned}
		}
		if len(page) < targetResolutionPageSize {
			return ids, nil
		}
		last := page[len(page)-1]
		cursor = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func matchesCustomerFilter(c domain.Customer, filter map[string]any) bool {
	for key, value := range filter {
		switch key {
		case "risk_tier":
			if c.RiskTier == nil || string(*c.RiskTier) != fmt.Sprint(value) {
				return false
			}
		case "status":
			if string(c.EffectiveStatus()) != fmt.Sprint(value) {
				return false
			}
		case "country_code":
			if !strings.EqualFold(c.CountryCode, fmt.Sprint(value)) {
				return false
			}
		case "customer_type":
			if string(c.CustomerType) != fmt.Sprint(value) {
				return false
			}
		}
	}
	return true
}

// largeBatchThreshold is where a batch stops being an operator's routine
// correction and becomes a change to the book. Above it, confirmation needs
// both the batch:execute:large permission and a second person.
const largeBatchThreshold = 1000

// checkLargeBatchAuthorization returns a status and message when the caller may
// not confirm this manifest, or 0 when they may. Small manifests are
// unaffected, so the everyday workflow keeps working exactly as before.
func checkLargeBatchAuthorization(r *http.Request, manifest *domain.TargetManifest) (int, string) {
	if manifest == nil || manifest.TargetCount <= largeBatchThreshold {
		return 0, ""
	}
	// An unauthenticated deployment has no roles to check. Failing closed here
	// would break every single-tenant install that runs without auth, so the
	// role check applies only where roles exist; the separation-of-duties check
	// below still does.
	if role, ok := auth.RoleFromContext(r.Context()); ok && !auth.HasPermission(role, auth.PermBatchExecuteLarge) {
		return http.StatusForbidden, fmt.Sprintf("confirming a batch over %d customers requires the %s permission", largeBatchThreshold, auth.PermBatchExecuteLarge)
	}
	if manifest.CreatedBy != "" && resolveAuditUserID(r) == manifest.CreatedBy {
		// The same rule whitelist approval already applies: the person who
		// framed the target is not the person who authorises it.
		return http.StatusForbidden, "the operator who previewed a large batch cannot confirm it"
	}
	return 0, ""
}

type confirmTargetRequest struct {
	Token           string `json:"token"`
	Rationale       string `json:"rationale"`
	IdempotencyKey  string `json:"idempotency_key"`
	ExpectedVersion int    `json:"expected_version"`
}

func (s *Server) handleConfirmTargetManifest(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.wave3.(domain.TargetManifestRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "target manifest store not configured")
		return
	}
	var req confirmTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.Token == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "token is required")
		return
	}
	if req.ExpectedVersion <= 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "expected_version is required")
		return
	}
	if strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required for target confirmation")
		return
	}
	// The dual-control checks need the manifest's size and author, which route
	// middleware cannot see, so they run here before the mutation opens.
	if existing, err := repo.GetTargetManifest(r.Context(), r.PathValue("id")); err == nil {
		if status, message := checkLargeBatchAuthorization(r, existing); status != 0 {
			writeErrorCode(w, status, apierr.CodeForbidden, message)
			return
		}
	}
	idempotencyKey := firstNonEmpty(req.IdempotencyKey, r.Header.Get("Idempotency-Key"))
	var m *domain.TargetManifest
	mutate := func(repos domain.AtomicMutationRepositories) error {
		wf, ok := repos.Wave3.(domain.TargetManifestRepository)
		if !ok {
			wf, ok = repo.(domain.TargetManifestRepository)
		}
		if !ok {
			return errAtomicMutationUnavailable
		}
		before, err := wf.GetTargetManifest(r.Context(), r.PathValue("id"))
		if err != nil {
			return err
		}
		idempotentRetry := (before.Status == "confirmed" || before.Status == "consumed") && idempotencyKey != "" && idempotencyKey == before.IdempotencyKey
		m, err = wf.ConfirmTargetManifest(r.Context(), r.PathValue("id"), req.Token, resolveAuditUserID(r), req.Rationale, idempotencyKey, req.ExpectedVersion)
		if err != nil {
			return err
		}
		if idempotentRetry {
			markAtomicAuditHandled(r)
			return nil
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{UserID: resolveAuditUserID(r), Action: "target_manifest_confirmed", ResourceType: "target_manifests", ResourceID: r.PathValue("id"), Details: map[string]string{"operation": m.Operation, "target_count": strconv.Itoa(m.TargetCount), "rationale": req.Rationale, "correlation_id": correlationID(r)}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append target confirmation audit: %w", err)
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			eventManifest := *m
			eventManifest.Token = ""
			payload, err := json.Marshal(&eventManifest)
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(r.Context(), &domain.DurableEvent{ID: generateID(), Topic: "target.manifest.confirmed", Payload: payload, ChainID: correlationID(r), CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue target confirmation event: %w", err)
			}
		}
		markAtomicAuditHandled(r)
		return nil
	}
	var err error
	if s.atomic != nil {
		err = s.runAtomic(r.Context(), mutate)
	} else {
		err = mutate(domain.AtomicMutationRepositories{Wave3: s.wave3, Audit: s.audit, EventOutbox: s.eventOutbox})
	}
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	m.Token = ""
	writeJSON(w, http.StatusOK, m)
}
func (s *Server) handleGetTargetManifest(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.wave3.(domain.TargetManifestRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "target manifest store not configured")
		return
	}
	m, err := repo.GetTargetManifest(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	// The raw confirmation token is write-only: it is returned once from the
	// preview response and never from a read or a later confirmation response.
	m.Token = ""
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleListPendingEvaluations(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.pendingEvals.(domain.PendingEvaluationWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "pending evaluation workflow not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	filter := domain.PendingEvaluationFilter{CustomerID: r.URL.Query().Get("customer_id"), BatchRunID: r.URL.Query().Get("batch_run_id"), Cursor: toDomainCursor(pageReq.Cursor)}
	if raw := firstNonEmpty(r.URL.Query().Get("created_from"), r.URL.Query().Get("from")); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "created_from must be RFC3339")
			return
		}
		filter.CreatedFrom = &parsed
	}
	if raw := firstNonEmpty(r.URL.Query().Get("created_to"), r.URL.Query().Get("to")); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "created_to must be RFC3339")
			return
		}
		filter.CreatedTo = &parsed
	}
	if raw := r.URL.Query().Get("min_age_days"); raw != "" {
		days, parseErr := strconv.Atoi(raw)
		if parseErr != nil || days < 0 {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "min_age_days must be a non-negative integer")
			return
		}
		cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
		if filter.CreatedTo == nil || cutoff.Before(*filter.CreatedTo) {
			filter.CreatedTo = &cutoff
		}
	}
	if raw := r.URL.Query().Get("max_age_days"); raw != "" {
		days, parseErr := strconv.Atoi(raw)
		if parseErr != nil || days < 0 {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "max_age_days must be a non-negative integer")
			return
		}
		cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
		if filter.CreatedFrom == nil || cutoff.After(*filter.CreatedFrom) {
			filter.CreatedFrom = &cutoff
		}
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && filter.CreatedFrom.After(*filter.CreatedTo) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "pending evaluation time range is invalid")
		return
	}
	if raw := r.URL.Query().Get("status"); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			filter.Status = append(filter.Status, domain.PendingEvaluationStatus(strings.TrimSpace(item)))
		}
	}
	items, err := repo.ListPendingEvaluations(r.Context(), filter, pageReq.Limit+1)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	page, meta := BuildPaginationMeta(items, pageReq.Limit, func(item domain.PendingEvaluation) Cursor { return Cursor{CreatedAt: item.CreatedAt, ID: item.ID} })
	writePaginatedJSON(w, http.StatusOK, page, meta)
}
func (s *Server) handleGetPendingEvaluation(w http.ResponseWriter, r *http.Request) {
	if s.pendingEvals == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "pending evaluation store not configured")
		return
	}
	pe, err := s.pendingEvals.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pe)
}
func (s *Server) handleListPendingHistory(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.pendingEvals.(domain.PendingEvaluationWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "pending evaluation workflow not configured")
		return
	}
	items, err := repo.ListPendingHistory(r.Context(), r.PathValue("id"), 50)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) handleTransitionPending(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.pendingEvals.(domain.PendingEvaluationWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "pending evaluation workflow not configured")
		return
	}
	var body struct {
		Reason          string `json:"reason"`
		ExpectedVersion int    `json:"expected_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if body.ExpectedVersion <= 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "expected_version is required")
		return
	}
	var item *domain.PendingEvaluation
	mutate := func(repos domain.AtomicMutationRepositories) error {
		workflow := repos.PendingEvaluations
		ok := workflow != nil
		if !ok {
			workflow, ok = s.pendingEvals.(domain.PendingEvaluationWorkflowRepository)
		}
		if !ok || workflow == nil {
			return errAtomicMutationUnavailable
		}
		var err error
		item, err = workflow.TransitionPendingEvaluation(r.Context(), r.PathValue("id"), r.PathValue("action"), resolveAuditUserID(r), body.Reason, body.ExpectedVersion)
		if err != nil {
			return err
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{UserID: resolveAuditUserID(r), Action: "pending_evaluation_" + r.PathValue("action"), ResourceType: "pending_evaluations", ResourceID: r.PathValue("id"), Details: map[string]string{"reason": body.Reason, "correlation_id": correlationID(r)}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append pending evaluation audit: %w", err)
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			payload, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(r.Context(), &domain.DurableEvent{ID: generateID(), Topic: "pending.evaluation.transitioned", Payload: payload, ChainID: correlationID(r), CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue pending evaluation event: %w", err)
			}
		}
		markAtomicAuditHandled(r)
		return nil
	}
	var err error
	if s.atomic != nil {
		err = s.runAtomic(r.Context(), mutate)
	} else {
		err = mutate(domain.AtomicMutationRepositories{PendingEvaluations: repo, Audit: s.audit, EventOutbox: s.eventOutbox})
	}
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type manualBatchRunRequest struct {
	Operation        string         `json:"operation"`
	TargetManifestID string         `json:"target_manifest_id"`
	Parameters       map[string]any `json:"parameters"`
	Rationale        string         `json:"rationale"`
	IdempotencyKey   string         `json:"idempotency_key"`
	RerunOf          string         `json:"rerun_of"`
}

func (s *Server) handleCreateBatchRun(w http.ResponseWriter, r *http.Request) {
	if s.batchRuns == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "batch run store not configured")
		return
	}
	var req manualBatchRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.Operation == "" {
		req.Operation = "score"
	}
	if req.TargetManifestID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "target_manifest_id is required")
		return
	}
	wf, ok := s.wave3.(domain.TargetManifestRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "target manifest store not configured")
		return
	}
	manifest, err := wf.GetTargetManifest(r.Context(), req.TargetManifestID)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	if manifest.Status != "confirmed" {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "target manifest must be confirmed")
		return
	}
	key := firstNonEmpty(req.IdempotencyKey, r.Header.Get("Idempotency-Key"))
	if finder, ok := s.batchRuns.(domain.BatchRunWorkflowRepository); ok && key != "" {
		if existing, findErr := finder.FindBatchRunByIdempotency(r.Context(), req.Operation, key); findErr == nil && existing != nil {
			writeJSON(w, http.StatusOK, existing)
			return
		}
	}
	params := req.Parameters
	if params == nil {
		params = map[string]any{}
	}
	if key != "" {
		params["idempotency_key"] = key
	}
	if strings.TrimSpace(req.Rationale) != "" {
		params["rationale"] = strings.TrimSpace(req.Rationale)
	}
	run := &domain.BatchRun{ID: generateID(), JobType: req.Operation, Operation: req.Operation, Status: domain.BatchRunStatusRunning, Parameters: params, TargetManifestID: manifest.ID, ConfigDigests: copyStringMap(s.configDigests), Actor: resolveAuditUserID(r), RerunOf: req.RerunOf, StartedAt: time.Now().UTC()}
	startRun := func(repos domain.AtomicMutationRepositories) error {
		batchRepo := repos.BatchRuns
		if batchRepo == nil {
			batchRepo = s.batchRuns
		}
		workflow := repos.Wave3
		if workflow == nil {
			workflow = s.wave3
		}
		targetRepo, ok := workflow.(domain.TargetManifestRepository)
		if !ok || batchRepo == nil {
			return errAtomicMutationUnavailable
		}
		if err := batchRepo.Create(r.Context(), run); err != nil {
			return err
		}
		claimed, err := targetRepo.ClaimTargetManifest(r.Context(), manifest.ID, run.ID, manifest.Version)
		if err != nil {
			return err
		}
		manifest = claimed
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{UserID: run.Actor, Action: "batch_run_started", ResourceType: "batch_runs", ResourceID: run.ID, Details: map[string]string{"operation": run.Operation, "target_manifest_id": manifest.ID, "rationale": req.Rationale, "correlation_id": correlationID(r)}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append batch run audit: %w", err)
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			payload, err := json.Marshal(run)
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(r.Context(), &domain.DurableEvent{ID: generateID(), Topic: "batch.run.started", Payload: payload, ChainID: correlationID(r), CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue batch run event: %w", err)
			}
		}
		markAtomicAuditHandled(r)
		return nil
	}
	err = nil
	if s.atomic != nil {
		err = s.runAtomic(r.Context(), startRun)
	} else {
		err = startRun(domain.AtomicMutationRepositories{Wave3: s.wave3, BatchRuns: s.batchRuns, Audit: s.audit, EventOutbox: s.eventOutbox})
	}
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	counts, failure := s.executeManualBatchRun(r.Context(), run, manifest, nil, nil)
	status := domain.BatchRunStatusCompleted
	if failure != "" {
		if counts["succeeded"] > 0 {
			status = domain.BatchRunStatusPartial
		} else {
			status = domain.BatchRunStatusFailed
		}
	}
	if err := s.finalizeManualBatchRun(r.Context(), r, run, status, counts, failure); err != nil {
		writeWave3Error(w, err)
		return
	}
	if persisted, err := s.batchRuns.Get(r.Context(), run.ID); err == nil {
		run = persisted
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) executeManualBatchRun(ctx context.Context, run *domain.BatchRun, manifest *domain.TargetManifest, alreadyProcessed map[string]bool, initialCounts map[string]int) (map[string]int, string) {
	counts := cloneIntMap(initialCounts)
	if counts == nil {
		counts = map[string]int{}
	}
	if alreadyProcessed == nil {
		alreadyProcessed = make(map[string]bool, len(run.ProcessedCustomerIDs))
		for _, id := range run.ProcessedCustomerIDs {
			alreadyProcessed[domain.CanonicalIdentifier(id)] = true
		}
	}
	if _, ok := counts["succeeded"]; !ok {
		counts["succeeded"] = 0
	}
	if _, ok := counts["failed"]; !ok {
		counts["failed"] = 0
	}
	if _, ok := counts["alerts"]; !ok {
		counts["alerts"] = 0
	}
	var failures []string
	for _, id := range manifest.CustomerIDs {
		canonicalID := domain.CanonicalIdentifier(id)
		if alreadyProcessed[canonicalID] {
			continue
		}
		processed, alertCount, failureReason := s.processManualBatchCustomer(ctx, run, id, counts)
		if failureReason != "" {
			counts["failed"]++
			failures = append(failures, id+": "+failureReason)
			continue
		}
		if !processed {
			// Another recovery worker claimed this customer while this worker
			// was evaluating it. Its transaction owns the durable result.
			alreadyProcessed[canonicalID] = true
			continue
		}
		counts["succeeded"]++
		counts["alerts"] += alertCount
		alreadyProcessed[canonicalID] = true
	}
	return counts, strings.Join(failures, "; ")
}

// processManualBatchCustomer evaluates outside the write transaction, then
// claims and persists the customer's result, checkpoint, progress, audit, and
// outbox event in one atomic unit. Claiming first under the transaction lock
// prevents duplicate work during restart/concurrent recovery.
func (s *Server) processManualBatchCustomer(ctx context.Context, run *domain.BatchRun, customerID string, counts map[string]int) (bool, int, string) {
	recordFailure := func(reason string) (bool, int, string) {
		if outcomeErr := s.recordManualBatchOutcome(ctx, run, customerID, domain.BatchRunCustomerOutcomeStatus(domain.BatchRunCustomerFailed), nil, reason); outcomeErr != nil {
			reason += "; persist customer outcome: " + outcomeErr.Error()
		}
		return false, 0, reason
	}
	customer, err := s.customers.Get(ctx, customerID)
	if err != nil {
		return recordFailure("customer lookup: " + err.Error())
	}

	var score *domain.ScoreRecord
	var alerts []domain.Alert
	switch run.Operation {
	case "score", "batch_score":
		if s.scoring == nil {
			return recordFailure("scoring unavailable")
		}
		ruleSetID := "default"
		if raw, ok := run.Parameters["rule_set_id"].(string); ok && raw != "" {
			ruleSetID = raw
		}
		score, err = s.scoring.ScoreCustomer(ctx, customer, ruleSetID)
		if err != nil {
			return recordFailure(err.Error())
		}
		if score == nil {
			return recordFailure("scoring returned no result")
		}
		if score.ID == "" {
			score.ID = generateID()
		}
		score.CustomerID = customerID
		score.Actor = run.Actor
		if rationale, ok := run.Parameters["rationale"].(string); ok {
			score.Rationale = rationale
		}
	case "monitor", "batch_monitor":
		if s.monitoring == nil || s.alerts == nil || s.transactions == nil {
			return recordFailure("monitoring, transaction, or alert repository unavailable")
		}
		txns, txnErr := s.transactions.ListByCustomer(ctx, customerID, 1000, 0)
		if txnErr != nil {
			return recordFailure("transactions: " + txnErr.Error())
		}
		alerts, err = s.evaluateMonitoring(ctx, customer, txns, engine.EvaluationModeBatch, nil)
		if err != nil {
			return recordFailure(err.Error())
		}
	default:
		return recordFailure("unsupported operation")
	}

	createdAlerts := 0
	createdAlertIDs := []string{}
	claimed := false
	mutate := func(repos domain.AtomicMutationRepositories) error {
		batchRepo := repos.BatchRuns
		if batchRepo == nil {
			batchRepo = s.batchRuns
		}
		if batchRepo == nil {
			return errAtomicMutationUnavailable
		}
		if progress, ok := batchRepo.(domain.BatchRunProgressRepository); ok {
			var claimErr error
			claimed, claimErr = progress.AppendProcessedCustomerIfAbsent(ctx, run.ID, customerID)
			if claimErr != nil {
				return claimErr
			}
		} else {
			if err := batchRepo.AppendProcessedCustomer(ctx, run.ID, customerID); err != nil {
				return err
			}
			claimed = true
		}
		if !claimed {
			return nil
		}

		if score != nil {
			customer.RiskScore = &score.Score
			customer.RiskTier = &score.Tier
			scoredAt := score.ScoredAt
			if scoredAt.IsZero() {
				scoredAt = time.Now().UTC()
				score.ScoredAt = scoredAt
			}
			customer.LastScoredAt = &scoredAt
			customerRepo := repos.Customers
			if customerRepo == nil {
				customerRepo = s.customers
			}
			if customerRepo == nil {
				return errAtomicMutationUnavailable
			}
			if err := customerRepo.Update(ctx, customer); err != nil {
				return fmt.Errorf("persist score: %w", err)
			}
			if err := customerRepo.SaveScoreRecord(ctx, score); err != nil {
				return fmt.Errorf("persist score history: %w", err)
			}
		}

		if len(alerts) > 0 {
			alertRepo := repos.Alerts
			if alertRepo == nil {
				alertRepo = s.alerts
			}
			if alertRepo == nil {
				return errAtomicMutationUnavailable
			}
			for i := range alerts {
				alert := alerts[i]
				alert.ID = generateID()
				now := time.Now().UTC()
				alert.CreatedAt = now
				alert.UpdatedAt = now
				if alert.DetectedAt.IsZero() {
					alert.DetectedAt = now
				}
				if _, err := s.applyWhitelistSuppression(ctx, &alert); err != nil {
					return fmt.Errorf("apply alert suppression: %w", err)
				}
				created, existing, createErr := alertRepo.CreateIfNotDuplicate(ctx, &alert)
				if createErr != nil {
					return fmt.Errorf("persist alert: %w", createErr)
				}
				if created {
					createdAlerts++
					createdAlertIDs = append(createdAlertIDs, alert.ID)
				} else if existing != nil {
					createdAlertIDs = append(createdAlertIDs, existing.ID)
				}
			}
		}
		outcomes, ok := batchRepo.(domain.BatchRunWorkflowRepository)
		if !ok {
			return errAtomicMutationUnavailable
		}
		if err := outcomes.RecordBatchRunOutcome(ctx, run.ID, domain.BatchRunCustomerOutcome{CustomerID: customerID, Status: domain.BatchRunCustomerSucceeded, AlertIDs: createdAlertIDs, Attempt: 1, UpdatedAt: time.Now().UTC()}); err != nil {
			return fmt.Errorf("persist customer outcome: %w", err)
		}

		nextCounts := cloneIntMap(counts)
		nextCounts["succeeded"]++
		nextCounts["alerts"] += createdAlerts
		if progress, ok := batchRepo.(domain.BatchRunWorkflowRepository); ok {
			if err := progress.UpdateBatchRun(ctx, run.ID, domain.BatchRunStatusRunning, nextCounts, ""); err != nil {
				return fmt.Errorf("persist batch progress: %w", err)
			}
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: run.Actor, Action: "batch_customer_processed", ResourceType: "batch_runs", ResourceID: run.ID, Details: map[string]string{"customer_id": customerID, "operation": run.Operation, "alerts_created": strconv.Itoa(createdAlerts)}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append batch customer audit: %w", err)
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			payload, err := json.Marshal(map[string]any{"run_id": run.ID, "customer_id": customerID, "operation": run.Operation, "alerts_created": createdAlerts})
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: generateID(), Topic: "batch.customer.processed", Payload: payload, ChainID: run.ID, CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue batch customer event: %w", err)
			}
		}
		return nil
	}
	if s.atomic != nil {
		err = s.runAtomic(ctx, mutate)
	} else {
		err = mutate(domain.AtomicMutationRepositories{Customers: s.customers, Transactions: s.transactions, Alerts: s.alerts, Audit: s.audit, EventOutbox: s.eventOutbox, BatchRuns: s.batchRuns})
	}
	if err != nil {
		return recordFailure(err.Error())
	}
	return claimed, createdAlerts, ""
}

// recordManualBatchOutcome keeps a customer-level failure durable even when
// evaluation fails before the per-customer business mutation can be claimed.
// The outcome, audit row, and optional outbox event share one transaction.
func (s *Server) recordManualBatchOutcome(ctx context.Context, run *domain.BatchRun, customerID string, status domain.BatchRunCustomerOutcomeStatus, alertIDs []string, reason string) error {
	outcome := domain.BatchRunCustomerOutcome{CustomerID: customerID, Status: status, AlertIDs: append([]string(nil), alertIDs...), Error: reason, UpdatedAt: time.Now().UTC()}
	mutate := func(repos domain.AtomicMutationRepositories) error {
		batchRepo := repos.BatchRuns
		if batchRepo == nil {
			batchRepo = s.batchRuns
		}
		workflow, ok := batchRepo.(domain.BatchRunWorkflowRepository)
		if !ok {
			return errAtomicMutationUnavailable
		}
		if err := workflow.RecordBatchRunOutcome(ctx, run.ID, outcome); err != nil {
			return err
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: run.Actor, Action: "batch_customer_outcome", ResourceType: "batch_runs", ResourceID: run.ID, Details: map[string]string{"customer_id": customerID, "status": string(status), "error": reason}, CreatedAt: createdAt}); err != nil {
			return err
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			payload, err := json.Marshal(outcome)
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: generateID(), Topic: "batch.customer.outcome", Payload: payload, ChainID: run.ID, CreatedAt: createdAt}); err != nil {
				return err
			}
		}
		return nil
	}
	if s.atomic != nil {
		return s.runAtomic(ctx, mutate)
	}
	return mutate(domain.AtomicMutationRepositories{BatchRuns: s.batchRuns, Audit: s.audit, EventOutbox: s.eventOutbox})
}

func timePtr(value time.Time) *time.Time { return &value }

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *Server) finalizeManualBatchRun(ctx context.Context, request *http.Request, run *domain.BatchRun, status domain.BatchRunStatus, counts map[string]int, failure string) error {
	mutate := func(repos domain.AtomicMutationRepositories) error {
		batchRepo := repos.BatchRuns
		if batchRepo == nil {
			batchRepo = s.batchRuns
		}
		if updater, ok := batchRepo.(domain.BatchRunWorkflowRepository); ok {
			if err := updater.UpdateBatchRun(ctx, run.ID, status, counts, failure); err != nil {
				return err
			}
		} else if status == domain.BatchRunStatusCompleted {
			if err := batchRepo.Complete(ctx, run.ID); err != nil {
				return err
			}
		} else if err := batchRepo.Fail(ctx, run.ID); err != nil {
			return err
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		actor := run.Actor
		correlation := ""
		if request != nil {
			actor = resolveAuditUserID(request)
			correlation = correlationID(request)
		}
		if err := repos.Audit.Create(ctx, &domain.AuditEntry{UserID: actor, Action: "batch_run_" + string(status), ResourceType: "batch_runs", ResourceID: run.ID, Details: map[string]string{"failure": failure, "correlation_id": correlation}, CreatedAt: createdAt}); err != nil {
			return fmt.Errorf("append batch completion audit: %w", err)
		}
		if s.eventOutbox != nil {
			if repos.EventOutbox == nil {
				return errAtomicMutationUnavailable
			}
			payload, err := json.Marshal(map[string]any{"run_id": run.ID, "status": status, "result_counts": counts, "failure": failure})
			if err != nil {
				return err
			}
			if err := repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{ID: generateID(), Topic: "batch.run.completed", Payload: payload, ChainID: correlation, CreatedAt: createdAt}); err != nil {
				return fmt.Errorf("enqueue batch completion event: %w", err)
			}
		}
		if request != nil {
			markAtomicAuditHandled(request)
		}
		return nil
	}
	if s.atomic != nil {
		return s.runAtomic(ctx, mutate)
	}
	return mutate(domain.AtomicMutationRepositories{Wave3: s.wave3, BatchRuns: s.batchRuns, Audit: s.audit, EventOutbox: s.eventOutbox})
}

// ResumeManualBatchRuns continues runs that were durably marked running when
// the previous API process stopped. Progress is checkpointed after each
// successful customer, so a restart only re-evaluates the unprocessed suffix.
// The batch row is the durable source of truth; a missing target manifest is
// surfaced in logs and leaves the run running for operator intervention.
func (s *Server) ResumeManualBatchRuns(ctx context.Context) {
	runs, ok := s.batchRuns.(domain.BatchRunWorkflowRepository)
	if !ok || runs == nil {
		return
	}
	manifests, ok := s.wave3.(domain.TargetManifestRepository)
	if !ok || manifests == nil {
		return
	}
	items, err := runs.ListBatchRuns(ctx, domain.BatchRunFilter{Status: domain.BatchRunStatusRunning}, 1001)
	if err != nil {
		slog.Error("resume manual batch runs: list", "error", err)
		return
	}
	for i := range items {
		run := items[i]
		manifest, getErr := manifests.GetTargetManifest(ctx, run.TargetManifestID)
		if getErr != nil {
			slog.Error("resume manual batch run: target manifest", "run_id", run.ID, "target_manifest_id", run.TargetManifestID, "error", getErr)
			continue
		}
		counts, failure := s.executeManualBatchRun(ctx, &run, manifest, nil, run.ResultCounts)
		status := domain.BatchRunStatusCompleted
		if failure != "" {
			if counts["succeeded"] > 0 {
				status = domain.BatchRunStatusPartial
			} else {
				status = domain.BatchRunStatusFailed
			}
		}
		if finalizeErr := s.finalizeManualBatchRun(ctx, nil, &run, status, counts, failure); finalizeErr != nil {
			slog.Error("resume manual batch run: finalize", "run_id", run.ID, "error", finalizeErr)
		}
	}
}

func (s *Server) handleListBatchRuns(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.batchRuns.(domain.BatchRunWorkflowRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "batch run workflow not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	items, err := repo.ListBatchRuns(r.Context(), domain.BatchRunFilter{Operation: r.URL.Query().Get("operation"), Status: domain.BatchRunStatus(r.URL.Query().Get("status")), Cursor: toDomainCursor(pageReq.Cursor)}, pageReq.Limit+1)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	page, meta := BuildPaginationMeta(items, pageReq.Limit, func(item domain.BatchRun) Cursor { return Cursor{CreatedAt: item.StartedAt, ID: item.ID} })
	writePaginatedJSON(w, http.StatusOK, page, meta)
}
func (s *Server) handleGetBatchRun(w http.ResponseWriter, r *http.Request) {
	if s.batchRuns == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "batch run store not configured")
		return
	}
	run, err := s.batchRuns.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
func (s *Server) handleRerunBatchRun(w http.ResponseWriter, r *http.Request) {
	if s.batchRuns == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "batch run store not configured")
		return
	}
	old, err := s.batchRuns.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	manifestRepo, ok := s.wave3.(domain.TargetManifestRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "target manifest store not configured")
		return
	}
	manifest, err := manifestRepo.GetTargetManifest(r.Context(), old.TargetManifestID)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	clone := *manifest
	clone.ID = generateID()
	clone.Token = generateID() + generateID()
	clone.IdempotencyKey = ""
	clone.Status = "confirmed"
	clone.Version = 1
	clone.RunID = ""
	clone.CreatedBy = resolveAuditUserID(r)
	clone.CreatedAt = time.Now().UTC()
	confirmedAt := clone.CreatedAt
	clone.ConfirmedAt = &confirmedAt
	clone.ExpiresAt = clone.CreatedAt.Add(15 * time.Minute)
	clone.Rationale = "rerun of " + old.ID
	if err := manifestRepo.CreateTargetManifest(r.Context(), &clone); err != nil {
		writeWave3Error(w, err)
		return
	}
	params := cloneAnyMap(old.Parameters)
	delete(params, "idempotency_key")
	body, _ := json.Marshal(manualBatchRunRequest{Operation: old.Operation, TargetManifestID: clone.ID, Parameters: params, Rationale: "rerun", RerunOf: old.ID})
	req := r.Clone(r.Context())
	req.Body = io.NopCloser(bytes.NewReader(body))
	s.handleCreateBatchRun(w, req)
}
