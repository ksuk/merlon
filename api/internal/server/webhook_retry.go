package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/metrics"
)

const (
	// webhookMaxAttempts caps retries at 10 total delivery attempts
	// (api.md §3.1 "最大10回"); the 10th failure moves the event to the DLQ.
	webhookMaxAttempts = 10
	// webhookBaseBackoff is the delay before the 2nd attempt (api.md §3.1
	// "初回30秒").
	webhookBaseBackoff = 30 * time.Second
	// webhookMaxBackoff caps the exponential backoff (api.md §3.1 "最大6時間").
	webhookMaxBackoff = 6 * time.Hour

	// webhookDLQCapacity and webhookDLQWarningThreshold (80% of capacity):
	// neither notifications.md nor api.md specifies a DLQ capacity number, so
	// per ws08-notify-case.md Task 4 these are taken from the task document
	// itself (its designated fallback source of truth for this value).
	webhookDLQCapacity         = 10000
	webhookDLQWarningThreshold = 8000
)

// computeBackoff returns the delay to wait after `attempt` failed delivery
// attempts before trying again (attempt=1 -> 30s, doubling thereafter, capped
// at 6h). api.md §3.1: "配信失敗時は指数バックオフ（初回30秒、最大6時間、
// 最大10回）で再送する".
func computeBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := webhookBaseBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff > webhookMaxBackoff {
			return webhookMaxBackoff
		}
	}
	if backoff > webhookMaxBackoff {
		return webhookMaxBackoff
	}
	return backoff
}

// retryFailedDelivery re-sends d (attempt_count+1) using the same EventID so
// the receiver can deduplicate (api.md §4.2). On success it clears
// NextAttemptAt; on failure it either schedules the next attempt or, once
// webhookMaxAttempts is reached, moves the event to the DLQ.
func (s *Server) retryFailedDelivery(ctx context.Context, d *domain.WebhookDelivery) error {
	hook, err := s.webhooks.Get(ctx, d.WebhookID)
	if err != nil {
		return err
	}

	headers := webhookHeaders(*hook, d.Event, d.EventID, []byte(d.Payload))
	statusCode, sendErr := sendWebhookRequest(ctx, hook.URL, []byte(d.Payload), headers)

	d.AttemptCount++
	d.StatusCode = statusCode
	if sendErr == nil && statusCode >= 200 && statusCode < 300 {
		d.Success = true
		d.Error = ""
		d.NextAttemptAt = nil
		return s.webhooks.UpdateDelivery(ctx, d)
	}

	d.Success = false
	if sendErr != nil {
		d.Error = sendErr.Error()
	} else {
		d.Error = "non-2xx response"
	}

	if d.AttemptCount >= webhookMaxAttempts {
		d.NextAttemptAt = nil
		entry := &domain.DLQEntry{
			ID:           generateID(),
			WebhookID:    d.WebhookID,
			EventID:      d.EventID,
			Event:        d.Event,
			Payload:      d.Payload,
			AttemptCount: d.AttemptCount,
			LastError:    d.Error,
			FailedAt:     time.Now(),
		}
		if err := s.webhooks.CreateDLQEntry(ctx, entry); err != nil {
			return err
		}
		metrics.WebhookDLQDepth.Inc()
		s.checkDLQDepthWarning(ctx)
		return s.webhooks.UpdateDelivery(ctx, d)
	}

	next := time.Now().Add(computeBackoff(d.AttemptCount))
	d.NextAttemptAt = &next
	return s.webhooks.UpdateDelivery(ctx, d)
}

// dlqDepthWarning reports whether count has reached the 80% capacity warning
// threshold (ws08-notify-case.md Task 4).
func dlqDepthWarning(count int) bool {
	return count >= webhookDLQWarningThreshold
}

// checkDLQDepthWarning logs once the 80% capacity warning threshold is
// crossed, so operators are alerted before the DLQ fills up. The
// merlon_webhook_dlq_depth gauge itself is adjusted with Inc/Dec at the
// two call sites (DLQ entry created / reprocessed) rather than Set from a
// recount here, mirroring adjustCasesOpenGauge's pattern.
func (s *Server) checkDLQDepthWarning(ctx context.Context) {
	if s.webhooks == nil {
		return
	}
	count, err := s.webhooks.CountDLQEntries(ctx)
	if err != nil {
		return
	}
	if dlqDepthWarning(count) {
		slog.WarnContext(ctx, "webhook DLQ depth exceeds 80% capacity warning threshold",
			"count", count, "capacity", webhookDLQCapacity, "warning_threshold", webhookDLQWarningThreshold)
	}
}

// processDueRetries retries every delivery whose NextAttemptAt is due. It is
// called by RunWebhookRetryWorker on each tick.
func (s *Server) processDueRetries(ctx context.Context) {
	if s.webhooks == nil {
		return
	}
	due, err := s.webhooks.ListPendingRetries(ctx, time.Now())
	if err != nil {
		return
	}
	for i := range due {
		d := due[i]
		s.retryFailedDelivery(ctx, &d)
	}
}

// RunWebhookRetryWorker polls for due retries every interval until ctx is
// canceled. It persists retry state via webhook_deliveries.next_attempt_at
// (migrations/015_webhook_retry_dlq.sql) rather than an in-memory timer, so
// retry state survives a process restart.
func (s *Server) RunWebhookRetryWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processDueRetries(ctx)
		}
	}
}

// handleListDLQEntries serves GET /api/v1/webhooks/dlq: the events still
// awaiting reprocessing (api.md §3.1 "DLQ内イベントの再処理はUI上から実行可能").
func (s *Server) handleListDLQEntries(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	entries, err := s.webhooks.ListDLQEntries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	undelivered := []domain.DLQEntry{}
	for _, e := range entries {
		if e.ReprocessedAt == nil {
			undelivered = append(undelivered, e)
		}
	}

	writeJSON(w, http.StatusOK, undelivered)
}

type reprocessDLQEntryResponse struct {
	Success    bool `json:"success"`
	StatusCode int  `json:"status_code"`
}

// handleReprocessDLQEntry serves POST /api/v1/webhooks/dlq/{id}/reprocess:
// an immediate, manually-triggered redelivery attempt. The operation is
// recorded in the audit log via the standard auditMiddleware (api.md §3.1
// "操作を監査ログに記録する").
func (s *Server) handleReprocessDLQEntry(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")
	entry, err := s.webhooks.GetDLQEntry(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hook, err := s.webhooks.Get(r.Context(), entry.WebhookID)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	headers := webhookHeaders(*hook, entry.Event, entry.EventID, []byte(entry.Payload))
	statusCode, sendErr := sendWebhookRequest(r.Context(), hook.URL, []byte(entry.Payload), headers)
	success := sendErr == nil && statusCode >= 200 && statusCode < 300

	errMsg := ""
	if sendErr != nil {
		errMsg = sendErr.Error()
	}
	delivery := &domain.WebhookDelivery{
		ID:           generateID(),
		WebhookID:    hook.ID,
		Event:        entry.Event,
		EventID:      entry.EventID,
		Payload:      entry.Payload,
		StatusCode:   statusCode,
		Success:      success,
		Error:        errMsg,
		AttemptCount: entry.AttemptCount + 1,
		CreatedAt:    time.Now(),
	}
	if err := s.webhooks.CreateDelivery(r.Context(), delivery); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	if err := s.webhooks.MarkDLQEntryReprocessed(r.Context(), id, now); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.WebhookDLQDepth.Dec()

	writeJSON(w, http.StatusOK, reprocessDLQEntryResponse{Success: success, StatusCode: statusCode})
}
