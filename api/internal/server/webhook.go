package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

type webhookPayload struct {
	Event     domain.WebhookEventType `json:"event"`
	Timestamp string                  `json:"timestamp"`
	Data      any                     `json:"data"`
}

func (s *Server) dispatchWebhook(ctx context.Context, event domain.WebhookEventType, data any) {
	if s.webhooks == nil {
		return
	}

	hooks, err := s.webhooks.ListByEvent(ctx, event)
	if err != nil || len(hooks) == 0 {
		return
	}

	payload := webhookPayload{
		Event:     event,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for _, hook := range hooks {
		go s.deliverWebhook(hook, event, body)
	}
}

func (s *Server) deliverWebhook(hook domain.Webhook, event domain.WebhookEventType, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		s.recordDelivery(hook.ID, event, string(body), 0, false, err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Merlon-Event", string(event))

	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Merlon-Signature", sig)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.recordDelivery(hook.ID, event, string(body), 0, false, err.Error())
		return
	}
	defer resp.Body.Close()

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	s.recordDelivery(hook.ID, event, string(body), resp.StatusCode, success, "")
}

func (s *Server) recordDelivery(webhookID string, event domain.WebhookEventType, payload string, statusCode int, success bool, errMsg string) {
	if s.webhooks == nil {
		return
	}
	d := &domain.WebhookDelivery{
		ID:         generateID(),
		WebhookID:  webhookID,
		Event:      event,
		Payload:    payload,
		StatusCode: statusCode,
		Success:    success,
		Error:      errMsg,
		CreatedAt:  time.Now(),
	}
	s.webhooks.CreateDelivery(context.Background(), d)
}

// Webhook CRUD handlers

type createWebhookRequest struct {
	URL    string                    `json:"url"`
	Events []domain.WebhookEventType `json:"events"`
	Secret string                    `json:"secret,omitempty"`
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "events is required")
		return
	}

	now := time.Now()
	hook := &domain.Webhook{
		ID:        generateID(),
		URL:       req.URL,
		Events:    req.Events,
		Secret:    req.Secret,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.webhooks.Create(r.Context(), hook); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, hook)
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	hooks, err := s.webhooks.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hooks == nil {
		hooks = []domain.Webhook{}
	}

	writeJSON(w, http.StatusOK, hooks)
}

func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")
	hook, err := s.webhooks.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, hook)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")
	if err := s.webhooks.Delete(r.Context(), id); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")

	_, err := s.webhooks.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	deliveries, err := s.webhooks.ListDeliveries(r.Context(), id, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if deliveries == nil {
		deliveries = []domain.WebhookDelivery{}
	}

	writeJSON(w, http.StatusOK, deliveries)
}
