package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/events"
	"github.com/ksuk/merlon/api/internal/events/handlers"
	"github.com/ksuk/merlon/api/internal/screening"
)

const (
	maxAttributes   = 50
	maxAttrKeyLen   = 256
	maxAttrValueLen = 10000
)

type CreateCustomerRequest struct {
	ExternalID   string              `json:"external_id"`
	CustomerType domain.CustomerType `json:"customer_type"`
	CountryCode  string              `json:"country_code"`
	ProductTypes []string            `json:"product_types"`
	Attributes   map[string]any      `json:"attributes"`
	Identity     map[string]any      `json:"identity,omitempty"`
}

type UpdateCustomerRequest struct {
	CountryCode       *string                `json:"country_code,omitempty"`
	Status            *domain.CustomerStatus `json:"status,omitempty"`
	ProductTypes      *[]string              `json:"product_types,omitempty"`
	Attributes        map[string]any         `json:"attributes,omitempty"`
	Identity          map[string]any         `json:"identity,omitempty"`
	Rationale         string                 `json:"rationale,omitempty"`
	ExpectedUpdatedAt *time.Time             `json:"expected_updated_at,omitempty"`
}

func customerCursor(c domain.Customer) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
}

func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	if useCursorPagination(r) {
		s.handleListCustomersCursor(w, r)
		return
	}
	s.handleListCustomersOffset(w, r)
}

// handleListCustomersCursor serves the HTTP API contract §1.1 cursor-based pagination.
func (s *Server) handleListCustomersCursor(w http.ResponseWriter, r *http.Request) {
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	var customers []domain.Customer
	if search == "" {
		customers, err = s.customers.ListByCursor(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	} else {
		searchRepo, ok := s.customers.(domain.CustomerSearchRepository)
		if !ok {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "customer search is not configured")
			return
		}
		customers, err = searchRepo.ListByCursorSearch(r.Context(), pageReq.Limit+1, toDomainCursor(pageReq.Cursor), search)
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	page, meta := BuildPaginationMeta(customers, pageReq.Limit, customerCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

// handleListCustomersOffset preserves the pre-existing offset/limit contract
// (the HTTP API contract §1.2 dual-support / deprecation period) while still returning the
// additive {"data", "pagination"} envelope.
func (s *Server) handleListCustomersOffset(w http.ResponseWriter, r *http.Request) {
	offsetParam := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(offsetParam)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	var customers []domain.Customer
	var err error
	if search == "" {
		customers, err = s.customers.List(r.Context(), limit+1, offset)
	} else {
		searchRepo, ok := s.customers.(domain.CustomerSearchRepository)
		if !ok {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "customer search is not configured")
			return
		}
		customers, err = searchRepo.ListSearch(r.Context(), search, limit+1, offset)
	}
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	if offsetParam != "" {
		setOffsetDeprecationHeaders(w)
	}

	page, meta := BuildPaginationMeta(customers, limit, customerCursor)
	writePaginatedJSON(w, http.StatusOK, page, meta)
}

func (s *Server) handleGetCustomer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	s.writeCustomerWithKYC(w, http.StatusOK, c)
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	if req.ExternalID == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "external_id required")
		return
	}
	if !isValidCustomerType(req.CustomerType) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, customerTypeErrorMessage())
		return
	}
	attributes := mergeIdentityAttributes(req.Attributes, req.Identity)
	if err := validateAttributes(attributes); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	now := time.Now()
	c := &domain.Customer{
		ID:           generateID(),
		ExternalID:   req.ExternalID,
		CustomerType: req.CustomerType,
		CountryCode:  req.CountryCode,
		ProductTypes: req.ProductTypes,
		Status:       domain.CustomerStatusActive,
		Attributes:   attributes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Validated after the merge, so a create that supplies identity through
	// either `identity` or `attributes` is judged on what will be stored.
	if !s.enforceKYC(w, c) {
		return
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Customers.Create(r.Context(), c); err != nil {
			return err
		}
		if repos.IdentityHistory != nil {
			if err := repos.IdentityHistory.AppendCustomerIdentityHistory(r.Context(), &domain.CustomerIdentityHistoryEntry{
				ID: generateID(), CustomerID: c.ID,
				ChangedFields: map[string]any{"after": c.Attributes, "country_code": c.CountryCode, "status": c.EffectiveStatus()},
				Actor:         resolveAuditUserID(r), Rationale: "customer created", CreatedAt: time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("identity history persistence failed: %w", err)
			}
		}
		return appendRequiredMutationAudit(r.Context(), r, repos, "create", "customers", c.ID, map[string]string{
			"external_id": c.ExternalID,
		}, c.CreatedAt)
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}

	s.writeCustomerWithKYC(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req UpdateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	// Capture the pre-mutation snapshot before applying a partial update.  The
	// audit record is the canonical explanation of the change and must not
	// describe the already-mutated row as its "before" state.
	before := *c
	before.ProductTypes = append([]string(nil), c.ProductTypes...)
	before.Attributes = cloneAnyMap(c.Attributes)

	if req.CountryCode != nil {
		c.CountryCode = *req.CountryCode
	}
	if req.Status != nil {
		if !isValidCustomerStatus(*req.Status) {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "status must be one of: active, dormant, frozen, closed")
			return
		}
		c.Status = *req.Status
	}
	if req.ProductTypes != nil {
		c.ProductTypes = *req.ProductTypes
	}
	beforeAttributes := cloneAnyMap(c.Attributes)
	if req.Attributes != nil || req.Identity != nil {
		merged := mergeIdentityAttributes(c.Attributes, req.Attributes)
		merged = mergeIdentityAttributes(merged, req.Identity)
		if err := validateAttributes(merged); err != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
			return
		}
		c.Attributes = merged
	}
	// Judged on the merged result, not the request: a partial update that
	// removes the last value of a required field is exactly the case a
	// request-only check would miss.
	if !s.enforceKYC(w, c) {
		return
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if req.ExpectedUpdatedAt != nil {
			versioned, ok := repos.Customers.(domain.CustomerOptimisticRepository)
			if !ok {
				return errAtomicMutationUnavailable
			}
			if err := versioned.UpdateIfUnmodified(r.Context(), c, req.ExpectedUpdatedAt.UTC()); err != nil {
				return err
			}
		} else if err := repos.Customers.Update(r.Context(), c); err != nil {
			return err
		}
		if req.Attributes != nil || req.Identity != nil || req.CountryCode != nil || req.Status != nil || req.ProductTypes != nil {
			if repos.IdentityHistory == nil {
				// Older in-memory/test compositions do not opt into the additive
				// Wave 3 identity-history repository. Preserve their update contract;
				// the production wiring always supplies this repository when identity
				// history is requested.
			} else if err := repos.IdentityHistory.AppendCustomerIdentityHistory(r.Context(), &domain.CustomerIdentityHistoryEntry{
				ID: generateID(), CustomerID: c.ID,
				ChangedFields: map[string]any{
					"before": beforeAttributes, "after": c.Attributes,
					"before_country_code": before.CountryCode, "after_country_code": c.CountryCode,
					"before_status": before.EffectiveStatus(), "after_status": c.EffectiveStatus(),
					"before_product_types": before.ProductTypes, "after_product_types": c.ProductTypes,
				},
				Actor: resolveAuditUserID(r), Rationale: req.Rationale, CreatedAt: time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("identity history persistence failed: %w", err)
			}
		}
		return appendRequiredMutationAudit(r.Context(), r, repos, "update", "customers", c.ID, map[string]string{
			"before_country_code": before.CountryCode, "after_country_code": c.CountryCode,
			"before_external_id": before.ExternalID, "after_external_id": c.ExternalID,
		}, c.UpdatedAt)
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	s.writeCustomerWithKYC(w, http.StatusOK, c)
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeIdentityAttributes(base, additions map[string]any) map[string]any {
	if base == nil && additions == nil {
		return nil
	}
	out := cloneAnyMap(base)
	for key, value := range additions {
		if value == nil {
			delete(out, key)
		} else {
			out[key] = value
		}
	}
	return out
}

func (s *Server) handleGetScoreHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	records, err := s.customers.ListScoreHistory(r.Context(), id, limit)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if records == nil {
		records = []domain.ScoreRecord{}
	}

	writeJSON(w, http.StatusOK, records)
}

type ScoreCustomerRequest struct {
	RuleSetID        string         `json:"rule_set_id"`
	RuleSetVersion   int            `json:"rule_set_version,omitempty"`
	Rationale        string         `json:"rationale,omitempty"`
	OverrideEvidence map[string]any `json:"override_evidence,omitempty"`
	Confirmed        *bool          `json:"confirmed,omitempty"`
}

func (s *Server) handleScoreCustomer(w http.ResponseWriter, r *http.Request) {
	if s.scoring == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "scoring engine not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req ScoreCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	// `confirmed` is additive so legacy clients remain valid during the
	// contract-stability window. New operator clients send true; when they do,
	// the pre-run confirmation must include an auditable rationale.
	if req.Confirmed != nil && !*req.Confirmed {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "score confirmation is required")
		return
	}
	if req.Confirmed != nil && *req.Confirmed && strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required when score is confirmed")
		return
	}

	// cdd_rule_selection_v1 declares who picks the rule set. Under server
	// authority the policy's choice is the configured answer, so naming a
	// different one is a deliberate departure from the configuration and is
	// treated like any other override: allowed, but attributable (ADR-0019).
	//
	// Under client authority, and wherever the policy resolves to nothing (the
	// shipped default has no rules and no default_rule_set_id, meaning "use
	// the single active CDD_WEIGHT rule"), there is no policy choice to depart
	// from and selection stays ordinary use.
	selection := s.policies.CDDRuleSelection()
	policyChoice := ""
	if selection.ServerResolves() {
		policyChoice = strings.TrimSpace(selection.Resolve(c))
	}
	selectionOverride := policyChoice != "" &&
		strings.TrimSpace(req.RuleSetID) != "" &&
		!domain.SameIdentifier(policyChoice, req.RuleSetID)
	if selectionOverride && strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed,
			"rationale is required to score against a rule set other than the one the selection policy resolves")
		return
	}

	var record *domain.ScoreRecord
	var selectedRule *domain.RuleDefinition
	if strings.TrimSpace(req.RuleSetID) != "" {
		if versioned, ok := s.scoring.(engine.VersionedScoringEngine); ok && s.rules != nil {
			if req.RuleSetVersion > 0 {
				selectedRule, err = s.rules.GetVersion(r.Context(), req.RuleSetID, req.RuleSetVersion)
			} else {
				selectedRule, err = s.rules.GetActive(r.Context(), req.RuleSetID)
			}
			if err != nil {
				var notFound *domain.ErrNotFound
				if errors.As(err, &notFound) {
					writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
				} else {
					writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
				}
				return
			}
			if selectedRule.Type != domain.RuleTypeCDDWeight {
				writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rule_set_id must reference a CDD_WEIGHT rule")
				return
			}
			if !selectedRule.IsActive {
				writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "selected CDD rule set is not active")
				return
			}
			record, err = versioned.ScoreCustomerWithRuleSet(r.Context(), c, selectedRule.Name, selectedRule.Definition)
			if err == nil {
				record.RuleSetVersion = selectedRule.Version
			}
		} else if req.RuleSetVersion > 0 {
			writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "selected rule-set scoring is not supported by the configured engine")
			return
		} else {
			record, err = s.scoring.ScoreCustomer(r.Context(), c, req.RuleSetID)
		}
	} else {
		record, err = s.scoring.ScoreCustomer(r.Context(), c, req.RuleSetID)
	}
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, apierr.CodeEngineError, "scoring engine error: "+err.Error())
		return
	}

	record.ID = generateID()
	record.Actor = resolveAuditUserID(r)
	if strings.TrimSpace(req.RuleSetID) != "" {
		record.RuleSetID = req.RuleSetID
	}
	if strings.TrimSpace(req.Rationale) != "" {
		record.Rationale = req.Rationale
	}
	// An override is a proposal, not a result. Validating its shape and
	// parking it for approval is what stops one person moving a customer out
	// of High tier -- the tier that decides EDD, monitoring thresholds and
	// rescreening frequency -- with an unstructured note and no second
	// signature (ADR-0019).
	var pendingOverride *domain.CDDScoreOverride
	if len(req.OverrideEvidence) > 0 {
		reason, proposedTier, documents, parseErr := domain.ParseOverrideEvidence(req.OverrideEvidence)
		if parseErr != nil {
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, parseErr.Error())
			return
		}
		if _, ok := s.customers.(domain.CDDScoreOverrideRepository); !ok {
			writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "score overrides require an approval store")
			return
		}
		record.OverrideEvidence = req.OverrideEvidence
		pendingOverride = &domain.CDDScoreOverride{
			ID: generateID(), CustomerID: c.ID, ScoreRecordID: record.ID,
			ProposedTier: proposedTier, ComputedTier: record.Tier, ComputedScore: record.Score,
			Reason: reason, SupportingDocuments: documents, Evidence: req.OverrideEvidence,
			Status: domain.CDDOverridePendingApproval, RequestedBy: resolveAuditUserID(r),
			RequestedAt: time.Now().UTC(), Version: 1,
		}
	}
	// A machine-produced rescore needs no narrative, and naming the rule set
	// to score against is ordinary use, not a deviation. What must be
	// attributable is a deliberate departure from the current configuration:
	// an override, or pinning a specific historical rule-set version.
	if (pendingOverride != nil || req.RuleSetVersion > 0) && strings.TrimSpace(req.Rationale) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required when overriding a score or pinning a rule set version")
		return
	}

	oldTier := c.RiskTier

	// Update customer risk score
	c.RiskScore = &record.Score
	c.RiskTier = &record.Tier
	now := record.ScoredAt
	c.LastScoredAt = &now

	// EDD escalation window (the case-management workflow §EDD未実施継続時の段階的
	// 措置): entering High tier starts the clock (kept if already running,
	// so a re-score at High doesn't reset stage progress); leaving High tier
	// closes the window, since EDD is no longer required.
	if record.Tier == domain.RiskTierHigh {
		if c.EddRequestedAt == nil {
			eddAt := record.ScoredAt
			c.EddRequestedAt = &eddAt
		}
	} else if c.EddRequestedAt != nil && c.EddClosedAt == nil {
		// Leaving High tier closes the window, but under the default
		// retain_evidence policy it does not erase it. Nulling the four stage
		// timestamps destroyed the record that EDD had been requested and how
		// far it had escalated -- evidence a routine rescore has no business
		// deleting. The window is marked closed instead, with the reason.
		closedAt := record.ScoredAt
		c.EddClosedAt = &closedAt
		c.EddCloseReason = "tier_downgrade"
		if !s.policies.EDD().RetainOnDowngrade() {
			c.EddRequestedAt = nil
			c.EddStage1LastSentAt = nil
			c.EddStage2NotifiedAt = nil
			c.EddStage3NotifiedAt = nil
		}
	}

	if err := s.runAtomic(r.Context(), func(repos domain.AtomicMutationRepositories) error {
		if repos.Customers == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if err := repos.Customers.Update(r.Context(), c); err != nil {
			return err
		}
		if err := repos.Customers.SaveScoreRecord(r.Context(), record); err != nil {
			return err
		}
		if pendingOverride != nil {
			overrides, ok := repos.Customers.(domain.CDDScoreOverrideRepository)
			if !ok {
				overrides, ok = s.customers.(domain.CDDScoreOverrideRepository)
			}
			if !ok {
				return errAtomicMutationUnavailable
			}
			if err := overrides.CreateCDDScoreOverride(r.Context(), pendingOverride); err != nil {
				return fmt.Errorf("record score override proposal: %w", err)
			}
		}
		scoreAudit := map[string]string{
			"rule_set_id": record.RuleSetID,
			"score":       strconv.FormatFloat(record.Score, 'f', -1, 64),
			"tier":        string(record.Tier),
		}
		if selectionOverride {
			// Both readings, not just the winner: a reviewer must be able to
			// see that the configured policy would have scored this customer
			// under a different rule set.
			scoreAudit["cdd_rule_set_policy_choice"] = policyChoice
			scoreAudit["cdd_rule_set_selected"] = record.RuleSetID
			scoreAudit["cdd_rule_set_selection_policy_version"] = selection.Version()
			scoreAudit["cdd_rule_set_override_rationale"] = strings.TrimSpace(req.Rationale)
		}
		if err := appendRequiredMutationAudit(r.Context(), r, repos, "score_customer", "customers", c.ID, scoreAudit, record.ScoredAt); err != nil {
			return err
		}
		if s.eventOutbox != nil {
			return s.enqueueTierChange(r.Context(), repos, c.ID, oldTier, record.Tier, record.ScoredAt)
		}
		return nil
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}

	// Immediate sanctions rescreen at the new tier's frequency
	// (the screening workflow "CDD ティア昇格時（Medium → High 等、新ティアの頻度を即時適用）").
	if isTierPromotion(oldTier, record.Tier) && s.screening != nil {
		deps := screening.SchedulerDeps{
			Customers:        s.customers,
			Screening:        s.screening,
			Results:          s.screeningResults,
			Workflow:         s.wave3,
			ConfigDigests:    s.configDigests,
			Actor:            resolveAuditUserID(r),
			TargetCustomerID: c.ID,
		}
		if s.wave3 != nil {
			deps.PersistWorkflow = func(ctx context.Context, run *domain.ScreeningRun, results []domain.ScreeningResultRecord) error {
				return s.persistScreeningRunAtomic(ctx, r, run, results)
			}
		}
		if _, err := screening.RunRescreeningBatch(r.Context(), deps, screening.TriggerTierPromoted); err != nil {
			slog.Error("tier-promotion immediate rescreen failed", "customer_id", c.ID, "error", err)
		}
	}

	// Publish a tier-change event (Task 8, CDD-009) so
	// events/handlers.TierChangeHandler can trigger the transaction-monitoring design's
	// 24h retroactive TM re-evaluation on upgrades. Independent of the
	// screening rescreen above (different downstream consumer).
	if s.eventOutbox == nil {
		if err := s.publishTierChange(r.Context(), c.ID, oldTier, record.Tier, record.ScoredAt); err != nil {
			slog.ErrorContext(r.Context(), "tier-change event publish failed", "customer_id", c.ID, "error", err)
		}
	}

	if pendingOverride != nil {
		// The score is recorded and the customer's tier reflects the computed
		// value; the proposed tier does not take effect until a second person
		// approves it.
		w.Header().Set("Warning", "299 - tier override recorded as a proposal awaiting approval")
		writeJSON(w, http.StatusOK, map[string]any{"score": record, "pending_override": pendingOverride})
		return
	}
	writeJSON(w, http.StatusOK, record)
}

// isTierPromotion reports whether newTier is a promotion (e.g. Medium ->
// High) relative to oldTier. A first-ever score (oldTier == nil) is not a
// promotion.
func isTierPromotion(oldTier *domain.RiskTier, newTier domain.RiskTier) bool {
	if oldTier == nil {
		return false
	}
	return tierRank(newTier) > tierRank(*oldTier)
}

func tierRank(t domain.RiskTier) int {
	switch t {
	case domain.RiskTierLow:
		return 0
	case domain.RiskTierMedium:
		return 1
	case domain.RiskTierHigh:
		return 2
	default:
		return -1
	}
}

// publishTierChange emits a "cdd.tier_changed" event (Task 8, CDD-009) when
// scoring changed the customer's risk tier, so
// events/handlers.TierChangeHandler can trigger the transaction-monitoring design's
// retroactive re-evaluation on upgrades. It is a no-op if no event bus is
// configured or the tier did not change.
func (s *Server) publishTierChange(ctx context.Context, customerID string, oldTier *domain.RiskTier, newTier domain.RiskTier, scoredAt time.Time) error {
	if s.events == nil {
		return nil
	}
	event, err := s.newTierChangeEvent(customerID, oldTier, newTier, scoredAt)
	if err != nil || event.ID == "" {
		return err
	}
	return s.events.Publish(ctx, event)
}

func (s *Server) newTierChangeEvent(customerID string, oldTier *domain.RiskTier, newTier domain.RiskTier, scoredAt time.Time) (events.Event, error) {
	if s.events == nil || (oldTier != nil && *oldTier == newTier) {
		return events.Event{}, nil
	}

	tc := handlers.TierChangeEvent{
		CustomerID: customerID,
		OldTier:    oldTier,
		NewTier:    newTier,
		ChainID:    generateID(),
		ScoredAt:   scoredAt,
	}
	payload, err := json.Marshal(tc)
	if err != nil {
		return events.Event{}, err
	}
	return events.Event{
		ID:        generateID(),
		Topic:     "cdd.tier_changed",
		Payload:   payload,
		ChainID:   tc.ChainID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *Server) enqueueTierChange(ctx context.Context, repos domain.AtomicMutationRepositories, customerID string, oldTier *domain.RiskTier, newTier domain.RiskTier, scoredAt time.Time) error {
	if s.eventOutbox == nil || s.events == nil || (oldTier != nil && *oldTier == newTier) {
		return nil
	}
	if repos.EventOutbox == nil {
		return errAtomicMutationUnavailable
	}
	event, err := s.newTierChangeEvent(customerID, oldTier, newTier, scoredAt)
	if err != nil {
		return err
	}
	return repos.EventOutbox.Enqueue(ctx, &domain.DurableEvent{
		ID:            event.ID,
		Topic:         event.Topic,
		Payload:       event.Payload,
		ChainID:       event.ChainID,
		ChainHopCount: event.ChainHopCount,
		CreatedAt:     event.CreatedAt,
	})
}

type ScreenCustomerRequest struct {
	ListIDs []string `json:"list_ids"`
}

func (s *Server) handleScreenCustomer(w http.ResponseWriter, r *http.Request) {
	if s.screening == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "screening engine not configured")
		return
	}

	id := r.PathValue("id")
	c, err := s.customers.Get(r.Context(), id)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req ScreenCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}

	result, err := s.screening.ScreenCustomer(r.Context(), c, req.ListIDs)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, apierr.CodeEngineError, "screening engine error: "+err.Error())
		return
	}

	// Wave 3 makes the customer-triggered screen durable.  Keep the legacy
	// response fields intact and add the run/result identities so the UI can
	// reload the same evidence instead of treating this as a transient check.
	if s.wave3 != nil {
		listIDs := append([]string(nil), req.ListIDs...)
		if len(listIDs) == 0 {
			listIDs = append(listIDs, s.screeningListIDs...)
		}
		screenedAt := result.ScreenedAt
		if screenedAt.IsZero() {
			screenedAt = time.Now().UTC()
			result.ScreenedAt = screenedAt
		}
		run := &domain.ScreeningRun{
			ID: generateID(), CustomerID: c.ID, ListIDs: listIDs,
			ConfigDigests: copyStringMap(s.configDigests), Status: domain.ScreeningRunCompleted,
			StartedAt: screenedAt, CreatedAt: screenedAt, Actor: resolveAuditUserID(r),
		}
		records := make([]domain.ScreeningResultRecord, 0, len(result.Matches))
		for _, match := range result.Matches {
			records = append(records, domain.ScreeningResultRecord{
				ID: generateID(), CustomerID: c.ID, ListID: match.ListID, ListType: match.ListType,
				EntryID: match.EntryID, MatchedName: match.MatchedName, Similarity: match.Similarity,
				Status: domain.ScreeningResultStatusNew, ScreenedAt: screenedAt, CreatedAt: screenedAt,
				MatchEvidence: map[string]any{"source": match.Source},
			})
		}
		if err := s.persistScreeningRunAtomic(r.Context(), r, run, records); err != nil {
			// A downstream write failure is actionable and must not be reported as
			// a successful screen.  The repository transaction has rolled back.
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, "screening persistence failed: "+err.Error())
			return
		}
		result.RunID = run.ID
		result.ResultIDs = make([]string, 0, len(records))
		for _, record := range records {
			result.ResultIDs = append(result.ResultIDs, record.ID)
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func isValidCustomerType(ct domain.CustomerType) bool {
	for _, valid := range validCustomerTypes() {
		if ct == valid {
			return true
		}
	}
	return false
}

func validateAttributes(attrs map[string]any) error {
	if len(attrs) > maxAttributes {
		return fmt.Errorf("too many attributes: %d (max %d)", len(attrs), maxAttributes)
	}
	for k, v := range attrs {
		if len(k) > maxAttrKeyLen {
			return fmt.Errorf("attribute key too long: %d chars (max %d)", len(k), maxAttrKeyLen)
		}
		// Scalar string values are checked directly; structured values
		// (e.g. attributes.trust_parties, the data model §1.1.1) are checked
		// by their serialized JSON size instead.
		size := 0
		if s, ok := v.(string); ok {
			size = len(s)
		} else if encoded, err := json.Marshal(v); err == nil {
			size = len(encoded)
		}
		if size > maxAttrValueLen {
			return fmt.Errorf("attribute value too long for key %q: %d chars (max %d)", k, size, maxAttrValueLen)
		}
	}
	return nil
}
