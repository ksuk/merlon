package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

type webhookPayload struct {
	Event     domain.WebhookEventType `json:"event"`
	Timestamp string                  `json:"timestamp"`
	Data      any                     `json:"data"`
}

// DispatchWebhook exposes dispatchWebhook to out-of-package callers (e.g.
// batch.RunEDDEscalationJob in internal/batch, which cannot depend on the
// server package to avoid an import cycle).
func (s *Server) DispatchWebhook(ctx context.Context, event domain.WebhookEventType, data any) {
	s.dispatchWebhook(ctx, event, data)
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

	// eventID is generated once per dispatched event and reused across every
	// hook and every retry attempt (the HTTP API contract §4.2 webhook output idempotency).
	eventID := generateID()
	for _, hook := range hooks {
		go s.deliverWebhook(hook, event, eventID, body)
	}
}

// webhookHeaders builds the headers the HTTP API contract §3 requires on every delivery
// attempt: X-Merlon-Event-Id and X-Merlon-Timestamp stay identical across
// retries (only the timestamp is refreshed to reflect the actual send time),
// while X-Merlon-Signature is recomputed from the (unchanged) body each time.
func webhookHeaders(hook domain.Webhook, event domain.WebhookEventType, eventID string, body []byte) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("X-Merlon-Event", string(event))
	h.Set("X-Merlon-Event-Id", eventID)
	h.Set("X-Merlon-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(body)
		h.Set("X-Merlon-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	return h
}

// sendWebhookRequest POSTs body to rawURL with the given headers and returns
// the response status code (0 if the request could not be sent at all).
func sendWebhookRequest(ctx context.Context, rawURL string, body []byte, headers http.Header) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header = headers
	resp, err := webhookHTTPClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// deliverWebhook first persists a pending delivery intent. If the process
// stops before the network request, the retry worker can resume the row after
// restart. The event ID is stable across that retry, so receivers can safely
// deduplicate an at-least-once delivery.
func (s *Server) deliverWebhook(hook domain.Webhook, event domain.WebhookEventType, eventID string, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	createdAt := time.Now().UTC()
	nextAttempt := createdAt
	d := &domain.WebhookDelivery{
		ID:            generateID(),
		WebhookID:     hook.ID,
		Event:         event,
		EventID:       eventID,
		Payload:       string(body),
		StatusCode:    0,
		Success:       false,
		AttemptCount:  0,
		CreatedAt:     createdAt,
		NextAttemptAt: &nextAttempt,
	}
	if s.webhooks == nil {
		return
	}
	if err := s.webhooks.CreateDelivery(ctx, d); err != nil {
		slog.ErrorContext(ctx, "webhook delivery intent persistence failed", "webhook_id", hook.ID, "event_id", eventID, "error", err)
		return
	}

	headers := webhookHeaders(hook, event, eventID, body)
	statusCode, err := sendWebhookRequest(ctx, hook.URL, body, headers)

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	d.StatusCode = statusCode
	d.Success = err == nil && statusCode >= 200 && statusCode < 300
	d.Error = errMsg
	d.AttemptCount = 1
	if !d.Success {
		next := time.Now().Add(computeBackoff(1))
		d.NextAttemptAt = &next
	} else {
		d.NextAttemptAt = nil
	}
	if err := s.webhooks.UpdateDelivery(ctx, d); err != nil {
		slog.ErrorContext(ctx, "webhook delivery result persistence failed", "webhook_id", hook.ID, "event_id", eventID, "error", err)
	}
}

func postWebhook(ctx context.Context, rawURL string, body []byte, headers http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := webhookHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func webhookHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !isPublicIP(ip) {
				return nil, fmt.Errorf("webhook URL resolved to private or loopback address")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return validatePublicHTTPURL(req.URL)
		},
	}
}

// Webhook CRUD handlers

type createWebhookRequest struct {
	URL    string                    `json:"url"`
	Events []domain.WebhookEventType `json:"events"`
	Secret string                    `json:"secret,omitempty"`
}

type createWebhookResponse struct {
	ID        string                    `json:"id"`
	URL       string                    `json:"url"`
	Events    []domain.WebhookEventType `json:"events"`
	Active    bool                      `json:"active"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "webhooks not configured")
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if req.URL == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "url is required")
		return
	}
	if err := validateWebhookURL(req.URL); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if len(req.Events) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "events is required")
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
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createWebhookResponse{
		ID:        hook.ID,
		URL:       hook.URL,
		Events:    hook.Events,
		Active:    hook.Active,
		CreatedAt: hook.CreatedAt,
		UpdatedAt: hook.UpdatedAt,
	})
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "webhooks not configured")
		return
	}

	hooks, err := s.webhooks.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if hooks == nil {
		hooks = []domain.Webhook{}
	}

	writeJSON(w, http.StatusOK, hooks)
}

func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")
	hook, err := s.webhooks.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, hook)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")
	if err := s.webhooks.Delete(r.Context(), id); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "webhooks not configured")
		return
	}

	id := r.PathValue("id")

	_, err := s.webhooks.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	deliveries, err := s.webhooks.ListDeliveries(r.Context(), id, 50)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	if deliveries == nil {
		deliveries = []domain.WebhookDelivery{}
	}

	writeJSON(w, http.StatusOK, deliveries)
}

func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid URL")
	}
	return validatePublicHTTPURL(u)
}

func validatePublicHTTPURL(u *url.URL) error {
	if u == nil || u.Host == "" {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("URL scheme must be http or https")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" {
		return fmt.Errorf("webhook URL must not target localhost")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("webhook URL must not target private or loopback addresses")
		}
	}
	return nil
}

// webhookAllowLoopbackForTests disables the SSRF private-IP guard so unit
// tests can exercise real webhook delivery against httptest.NewServer, which
// always binds to loopback. Production code never touches this; only test
// files in this package set it, scoped with t.Cleanup.
var webhookAllowLoopbackForTests = false

func isPublicIP(ip net.IP) bool {
	if webhookAllowLoopbackForTests {
		return true
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified())
}
