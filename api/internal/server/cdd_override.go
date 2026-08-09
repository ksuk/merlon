package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

// handleListCDDScoreOverrides shows the proposals raised against a customer's
// computed tier, decided or not.
func (s *Server) handleListCDDScoreOverrides(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.customers.(domain.CDDScoreOverrideRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "score override store not configured")
		return
	}
	overrides, err := repo.ListCDDScoreOverrides(r.Context(), r.PathValue("id"), 50)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	if overrides == nil {
		overrides = []domain.CDDScoreOverride{}
	}
	writeJSON(w, http.StatusOK, overrides)
}

type approveOverrideRequest struct {
	Rationale       string `json:"rationale"`
	Reject          bool   `json:"reject,omitempty"`
	ExpectedVersion int    `json:"expected_version"`
}

// handleApproveCDDScoreOverride settles a proposal, and on approval moves the
// customer's tier to the proposed one.
//
// The approver must not be the requester. Overriding a computed tier changes
// which EDD, monitoring and rescreening rules a customer is subject to, so the
// same separation of duties that governs whitelist approval governs this.
func (s *Server) handleApproveCDDScoreOverride(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.customers.(domain.CDDScoreOverrideRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "score override store not configured")
		return
	}
	var req approveOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	if req.ExpectedVersion <= 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "expected_version is required")
		return
	}
	rationale := strings.TrimSpace(req.Rationale)
	if rationale == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required to decide a score override")
		return
	}

	overrideID := r.PathValue("overrideID")
	override, err := repo.GetCDDScoreOverride(r.Context(), overrideID)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	if !domain.SameIdentifier(override.CustomerID, r.PathValue("id")) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "override does not belong to this customer")
		return
	}
	approver := resolveAuditUserID(r)
	if approver == override.RequestedBy {
		writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "requester cannot approve their own score override")
		return
	}

	status := domain.CDDOverrideApproved
	if req.Reject {
		status = domain.CDDOverrideRejected
	}

	var decided *domain.CDDScoreOverride
	mutate := func(repos domain.AtomicMutationRepositories) error {
		overrideRepo := repo
		if bound, ok := repos.Customers.(domain.CDDScoreOverrideRepository); ok {
			overrideRepo = bound
		}
		var err error
		decided, err = overrideRepo.DecideCDDScoreOverride(r.Context(), overrideID, status, approver, rationale, req.ExpectedVersion)
		if err != nil {
			return err
		}
		if status == domain.CDDOverrideApproved {
			customerRepo := repos.Customers
			if customerRepo == nil {
				customerRepo = s.customers
			}
			customer, err := customerRepo.Get(r.Context(), decided.CustomerID)
			if err != nil {
				return err
			}
			// Only now does the customer's tier move. Until this point the
			// record carried the computed tier, which is what makes the
			// proposal a proposal.
			tier := decided.ProposedTier
			customer.RiskTier = &tier
			if err := customerRepo.Update(r.Context(), customer); err != nil {
				return err
			}
		}
		if repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		createdAt := time.Now().UTC()
		if err := repos.Audit.Create(r.Context(), &domain.AuditEntry{
			UserID: approver, Action: "cdd_score_override_" + string(status),
			ResourceType: "cdd_score_overrides", ResourceID: overrideID,
			Details: map[string]string{
				"customer_id": decided.CustomerID, "requested_by": decided.RequestedBy,
				"computed_tier": string(decided.ComputedTier), "proposed_tier": string(decided.ProposedTier),
				"rationale": rationale, "correlation_id": correlationID(r),
			}, CreatedAt: createdAt,
		}); err != nil {
			return fmt.Errorf("append score override audit: %w", err)
		}
		markAtomicAuditHandled(r)
		return nil
	}
	if err := s.runAtomic(r.Context(), mutate); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decided)
}

// handleListCDDRuleSets reports which CDD rule sets apply to a customer and
// which one the policy recommends.
//
// The UI previously showed whichever active rule set the API happened to list
// first (customer-detail.tsx read `rules.data[0].name`), which made rule-set
// applicability a positional accident of the listing endpoint rather than a
// stated policy.
func (s *Server) handleListCDDRuleSets(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule repository is not configured")
		return
	}
	customer, err := s.customers.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	selection := s.policies.CDDRuleSelection()
	candidates := selection.Applicable(customer)

	rules, err := s.rules.List(r.Context(), domain.RuleTypeCDDWeight, false, 100, nil)
	if err != nil {
		writeWave3Error(w, err)
		return
	}

	type ruleSetCandidate struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Version     int    `json:"version"`
		IsActive    bool   `json:"is_active"`
		Digest      string `json:"digest"`
		MatchedOn   string `json:"matched_on,omitempty"`
		Priority    int    `json:"priority,omitempty"`
		Recommended bool   `json:"recommended"`
	}
	byRuleSet := map[string]int{}
	for i, candidate := range candidates {
		byRuleSet[candidate.RuleSetID] = i
	}

	out := make([]ruleSetCandidate, 0, len(rules))
	for _, rule := range rules {
		item := ruleSetCandidate{
			ID: rule.ID, Name: rule.Name, Version: rule.Version,
			IsActive: rule.IsActive, Digest: stableDigest(rule.Definition),
		}
		if i, ok := byRuleSet[rule.ID]; ok {
			item.MatchedOn = candidates[i].MatchedOn
			item.Priority = candidates[i].Priority
			item.Recommended = candidates[i].Recommended
		}
		out = append(out, item)
	}
	// With no policy rule matching, the active rule set is the recommendation:
	// that is the honest statement of what will be used, rather than leaving
	// the caller to infer it from list order.
	if len(candidates) == 0 {
		for i := range out {
			if out[i].IsActive {
				out[i].Recommended = true
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":                out,
		"policy_version":      selection.Version(),
		"selection_authority": selection.ServerResolves(),
	})
}
