package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/casemgmt"
	"github.com/ksuk/merlon/api/internal/domain"
)

// customerStatusWebhookRequest is the body of the core system's
// customer_status_changed notification (the data model §1.1.2). The core
// system is the system of record for the customer's identity, so it is
// addressed by external_id rather than this system's internal UUID.
type customerStatusWebhookRequest struct {
	ExternalID string                `json:"external_id"`
	Status     domain.CustomerStatus `json:"status"`
	Reason     string                `json:"reason"`
}

func isValidCustomerStatus(s domain.CustomerStatus) bool {
	switch s {
	case domain.CustomerStatusActive, domain.CustomerStatusDormant, domain.CustomerStatusFrozen, domain.CustomerStatusClosed:
		return true
	default:
		return false
	}
}

// handleCustomerStatusWebhook implements
// POST /api/v1/webhooks/inbound/customer-status. This system does not judge
// whether the transition is valid (the data model §1.1.2: "本システムは通知
// を受けて status を更新するのみで、状態遷移の可否判定は行わない") — it
// records whatever status the core system reports.
func (s *Server) handleCustomerStatusWebhook(w http.ResponseWriter, r *http.Request) {
	var req customerStatusWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "external_id required")
		return
	}
	if !isValidCustomerStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "status must be one of: active, dormant, frozen, closed")
		return
	}

	ctx := r.Context()

	c, err := s.customers.GetByExternalID(ctx, req.ExternalID)
	if err != nil {
		var notFound *domain.ErrNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, "customer not found: "+req.ExternalID)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	oldStatus := c.EffectiveStatus()

	// the data model §1.1.2: "顧客の死亡が判明した場合、基幹は status = frozen
	// を通知し、本システムは該当顧客の全アラートを severity = HIGH に引き上げ
	// てケース管理へエスカレーションする". The wire format has no dedicated
	// "cause" enum, so death is inferred from a free-text reason mentioning
	// it; other frozen causes (e.g. sanctions asset freezes) do not trigger
	// this blanket escalation.
	deathFreeze := req.Status == domain.CustomerStatusFrozen && oldStatus != domain.CustomerStatusFrozen && isDeathReason(req.Reason)
	var updated *domain.Customer
	if err := s.runAtomic(ctx, func(repos domain.AtomicMutationRepositories) error {
		var err error
		updated, err = repos.Customers.UpdateStatus(ctx, c.ID, req.Status, req.Reason)
		if err != nil {
			return err
		}
		if err := appendRequiredCustomerStatusAudit(ctx, r, repos, updated.ID, string(oldStatus), string(req.Status), req.Reason, updated.UpdatedAt); err != nil {
			return err
		}
		if deathFreeze {
			return s.escalateCustomerAlertsOnDeathAtomic(ctx, r, repos, updated.ID)
		}
		return nil
	}); err != nil {
		writeAtomicMutationError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func isDeathReason(reason string) bool {
	return strings.Contains(strings.ToLower(reason), "death")
}

// escalateCustomerAlertsOnDeath raises every alert belonging to customerID to
// HIGH severity and consolidates each into a case (the data model §1.1.2).
func (s *Server) escalateCustomerAlertsOnDeathAtomic(ctx context.Context, r *http.Request, repos domain.AtomicMutationRepositories, customerID string) error {
	if repos.Alerts == nil || repos.Cases == nil || repos.Investigation == nil {
		return errAtomicMutationUnavailable
	}
	alerts, err := repos.Alerts.ListByCustomer(ctx, customerID, maxBatchCustomers, 0)
	if err != nil {
		return err
	}
	for i := range alerts {
		a := alerts[i]
		before := a
		if a.Severity != domain.AlertSeverityHigh {
			if err := repos.Alerts.EscalateSeverity(ctx, a.ID, domain.AlertSeverityHigh); err != nil {
				return err
			}
			updated, err := repos.Alerts.Get(ctx, a.ID)
			if err != nil {
				return err
			}
			a = *updated
			if err := repos.Audit.Create(ctx, &domain.AuditEntry{
				UserID: resolveAuditUserID(r), Action: "alert_severity_escalated", ResourceType: "alerts", ResourceID: a.ID,
				Details: map[string]string{
					"before": string(before.Severity), "after": string(a.Severity),
					"reason": "customer death freeze", "correlation_id": correlationID(r),
				}, IPAddress: extractIP(r), UserAgent: r.UserAgent(), CreatedAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
			markAtomicAuditHandled(r)
		}

		priorityResolver := casemgmt.PriorityResolver(func(ctx context.Context, customerID string) (domain.CasePriority, error) {
			customer, err := repos.Customers.Get(ctx, customerID)
			if err != nil {
				return "", err
			}
			return s.priorityPolicy.PriorityFor(customer), nil
		})
		caseRecord, err := casemgmt.ConsolidateAlertWithLifecycleAndPriority(ctx, repos.Cases, repos.CaseAlertLifecycle, &a, casemgmt.DefaultConsolidationWindow, priorityResolver)
		if err != nil {
			return err
		}
		if caseRecord == nil {
			return errors.New("death-freeze escalation did not produce a case")
		}
		if err := appendRequiredCaseEvent(ctx, r, repos, &domain.CaseEvent{
			CaseID: caseRecord.ID, EventType: "alert_consolidated", Reason: "customer death freeze",
			After: caseEventState(*caseRecord), RelatedAlertIDs: []string{a.ID},
		}); err != nil {
			return err
		}
	}
	return nil
}
