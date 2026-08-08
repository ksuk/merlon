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

type eddActionRequest struct {
	Rationale         string     `json:"rationale"`
	CaseID            string     `json:"case_id,omitempty"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

// handleCustomerEDDAction closes or reopens an EDD window.
//
// There was no completion path at all: a window could be opened by a High-tier
// score and escalated by the job, but never finished. An operator who had done
// the enhanced due diligence had nowhere to say so, so the customer stayed
// outstanding, the escalation clock kept running, and the only way the window
// ever ended was a tier downgrade that erased its history.
func (s *Server) handleCustomerEDDAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "complete" && action != "reopen" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "action must be complete or reopen")
		return
	}
	id := r.PathValue("id")
	customer, err := s.customers.Get(r.Context(), id)
	if err != nil {
		writeWave3Error(w, err)
		return
	}

	var req eddActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	edd := s.policies.EDD()
	rationale := strings.TrimSpace(req.Rationale)
	// Completing EDD is a judgement about a customer's risk, so the policy can
	// require it to be attributed. Reopening always requires one: it overrides
	// a decision someone already recorded.
	if rationale == "" && (action == "reopen" || edd.RequiresRationale()) {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "rationale is required to "+action+" an EDD window")
		return
	}
	if action == "complete" && edd.RequiresCaseLink() && strings.TrimSpace(req.CaseID) == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "case_id is required to complete an EDD window")
		return
	}

	if customer.EddRequestedAt == nil {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "this customer has no EDD window")
		return
	}
	switch action {
	case "complete":
		if customer.EddCompletedAt != nil {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "this EDD window is already complete")
			return
		}
	case "reopen":
		if customer.EddCompletedAt == nil {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, "this EDD window is not complete")
			return
		}
	}

	now := time.Now().UTC()
	eventType := domain.EDDEventCompleted
	if action == "complete" {
		customer.EddCompletedAt = &now
		customer.EddClosedAt = &now
		customer.EddCloseReason = "completed"
		if strings.TrimSpace(req.CaseID) != "" {
			customer.EddCaseID = strings.TrimSpace(req.CaseID)
		}
	} else {
		eventType = domain.EDDEventReopened
		customer.EddCompletedAt = nil
		customer.EddClosedAt = nil
		customer.EddCloseReason = ""
	}

	mutate := func(repos domain.AtomicMutationRepositories) error {
		customerRepo := repos.Customers
		if customerRepo == nil {
			customerRepo = s.customers
		}
		if customerRepo == nil || repos.Audit == nil {
			return errAtomicMutationUnavailable
		}
		if req.ExpectedUpdatedAt != nil {
			versioned, ok := customerRepo.(domain.CustomerOptimisticRepository)
			if !ok {
				return errAtomicMutationUnavailable
			}
			if err := versioned.UpdateIfUnmodified(r.Context(), customer, req.ExpectedUpdatedAt.UTC()); err != nil {
				return err
			}
		} else if err := customerRepo.Update(r.Context(), customer); err != nil {
			return err
		}
		// The event is the durable answer to "was EDD ever completed, by whom,
		// and on what grounds" after the customer's tier has moved on.
		if events, ok := repos.Customers.(domain.CustomerEDDEventRepository); ok {
			if err := events.AppendCustomerEDDEvent(r.Context(), &domain.CustomerEDDEvent{
				ID: generateID(), CustomerID: customer.ID, EventType: eventType,
				Rationale: rationale, CaseID: customer.EddCaseID, Actor: resolveAuditUserID(r),
				PolicyVersion: edd.Version(), CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("append EDD event: %w", err)
			}
		} else if events, ok := s.customers.(domain.CustomerEDDEventRepository); ok {
			if err := events.AppendCustomerEDDEvent(r.Context(), &domain.CustomerEDDEvent{
				ID: generateID(), CustomerID: customer.ID, EventType: eventType,
				Rationale: rationale, CaseID: customer.EddCaseID, Actor: resolveAuditUserID(r),
				PolicyVersion: edd.Version(), CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("append EDD event: %w", err)
			}
		}
		return appendRequiredMutationAudit(r.Context(), r, repos, "edd_"+action, "customers", customer.ID, map[string]string{
			"rationale": rationale, "case_id": customer.EddCaseID, "policy_version": edd.Version(),
		}, now)
	}
	if err := s.runAtomic(r.Context(), mutate); err != nil {
		writeAtomicMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, buildInvestigationEDDPanel(customer, edd))
}

// handleListCustomerEDDEvents serves the window's history.
func (s *Server) handleListCustomerEDDEvents(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.customers.(domain.CustomerEDDEventRepository)
	if !ok {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "EDD event history not configured")
		return
	}
	events, err := repo.ListCustomerEDDEvents(r.Context(), r.PathValue("id"), 100)
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	if events == nil {
		events = []domain.CustomerEDDEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}
