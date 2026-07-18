package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

	updated, err := s.customers.UpdateStatus(ctx, c.ID, req.Status, req.Reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.recordCustomerStatusAuditEntry(r, updated.ID, string(oldStatus), string(req.Status), req.Reason)

	// the data model §1.1.2: "顧客の死亡が判明した場合、基幹は status = frozen
	// を通知し、本システムは該当顧客の全アラートを severity = HIGH に引き上げ
	// てケース管理へエスカレーションする". The wire format has no dedicated
	// "cause" enum, so death is inferred from a free-text reason mentioning
	// it; other frozen causes (e.g. sanctions asset freezes) do not trigger
	// this blanket escalation.
	if req.Status == domain.CustomerStatusFrozen && oldStatus != domain.CustomerStatusFrozen && isDeathReason(req.Reason) {
		s.escalateCustomerAlertsOnDeath(r, updated.ID)
	}

	writeJSON(w, http.StatusOK, updated)
}

func isDeathReason(reason string) bool {
	return strings.Contains(strings.ToLower(reason), "death")
}

func (s *Server) recordCustomerStatusAuditEntry(r *http.Request, customerID, oldStatus, newStatus, reason string) {
	if s.audit == nil {
		return
	}
	entry := &domain.AuditEntry{
		UserID:       resolveAuditUserID(r),
		Action:       "customer_status_change",
		ResourceType: "customer",
		ResourceID:   customerID,
		Details: map[string]string{
			"old_status": oldStatus,
			"new_status": newStatus,
			"reason":     reason,
		},
		IPAddress: extractIP(r),
		UserAgent: r.UserAgent(),
		CreatedAt: time.Now(),
	}
	if err := s.audit.Create(r.Context(), entry); err != nil {
		slog.ErrorContext(r.Context(), "customer status change audit write failed", "customer_id", customerID, "error", err)
	}
}

// escalateCustomerAlertsOnDeath raises every alert belonging to customerID to
// HIGH severity and consolidates each into a case (the data model §1.1.2).
func (s *Server) escalateCustomerAlertsOnDeath(r *http.Request, customerID string) {
	if s.alerts == nil {
		return
	}
	alerts, err := s.alerts.ListByCustomer(r.Context(), customerID, maxBatchCustomers, 0)
	if err != nil {
		slog.ErrorContext(r.Context(), "death-freeze escalation: list alerts failed", "customer_id", customerID, "error", err)
		return
	}
	for i := range alerts {
		a := alerts[i]
		if err := s.alerts.EscalateSeverity(r.Context(), a.ID, domain.AlertSeverityHigh); err != nil {
			slog.ErrorContext(r.Context(), "death-freeze escalation: severity update failed", "alert_id", a.ID, "error", err)
			continue
		}
		a.Severity = domain.AlertSeverityHigh
		s.consolidateAlertIntoCase(r.Context(), &a)
	}
}
