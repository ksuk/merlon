// Package webhook implements the durable, authenticated inbound webhook
// transport.  It deliberately knows nothing about PostgreSQL or the HTTP
// server: repositories and the record ingestor are narrow interfaces so the
// same protocol is used by memory mode, workers, and production.
package webhook

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxBodyBytes      = 10 << 20
	DefaultMaxRecords        = 1000
	DefaultTimestampSkew     = 5 * time.Minute
	DefaultRetryInterval     = 30 * time.Second
	DefaultMaxRetryAge       = time.Hour
	DefaultMaxAttempts       = 8
	DefaultVisibilityTimeout = 5 * time.Minute
)

type Kind string

const (
	KindCustomers    Kind = "customers"
	KindTransactions Kind = "transactions"
)

func (k Kind) Valid() bool { return k == KindCustomers || k == KindTransactions }

type EventStatus string

const (
	StatusAccepted  EventStatus = "accepted"
	StatusRunning   EventStatus = "running"
	StatusCompleted EventStatus = "completed"
	StatusFailed    EventStatus = "failed"
	StatusDLQ       EventStatus = "dlq"
)

type RecordStatus string

const (
	RecordAccepted          RecordStatus = "accepted"
	RecordUpdated           RecordStatus = "updated"
	RecordSkipped           RecordStatus = "skipped"
	RecordWaitingDependency RecordStatus = "waiting_dependency"
	RecordRejected          RecordStatus = "rejected"
)

var (
	ErrNotFound         = errors.New("inbound webhook event not found")
	ErrConflict         = errors.New("inbound webhook event conflicts with an existing event")
	ErrUnauthorized     = errors.New("inbound webhook authentication failed")
	ErrInvalidTimestamp = errors.New("inbound webhook timestamp is invalid or outside the allowed skew")
	ErrInvalidSignature = errors.New("inbound webhook signature is invalid")
	ErrPayloadTooLarge  = errors.New("inbound webhook payload is too large")
	ErrInvalidPayload   = errors.New("inbound webhook payload is invalid")
	ErrDependency       = errors.New("inbound webhook dependency is not ready")
)

// Error wraps a protocol error while retaining errors.Is compatibility with
// the sentinel values above.  HTTP callers can use Code to choose a status.
type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.Cause }

const (
	CodeUnauthorized     = "unauthorized"
	CodeInvalidTimestamp = "invalid_timestamp"
	CodeInvalidSignature = "invalid_signature"
	CodePayloadTooLarge  = "payload_too_large"
	CodeInvalidPayload   = "invalid_payload"
	CodeConflict         = "conflict"
	CodeNotFound         = "not_found"
	CodeDependency       = "dependency"
)

// Authenticator verifies the fixed inbound signature contract.  The secret is
// never persisted and raw bodies are only handed to the service after this
// check succeeds.
type Authenticator struct {
	Secret  []byte
	Clock   func() time.Time
	MaxSkew time.Duration
}

// Signature computes the wire value used by X-Merlon-Signature.  It is
// exported for core-system integration tests and examples; callers should
// still send the raw, unmodified request bytes to Accept.
func Signature(secret []byte, eventID, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(eventID))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func (a Authenticator) Verify(eventID, timestamp, signature string, body []byte) error {
	if len(a.Secret) == 0 || strings.TrimSpace(eventID) == "" {
		return &Error{Code: CodeUnauthorized, Cause: ErrUnauthorized}
	}
	when, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(timestamp))
	if err != nil {
		return &Error{Code: CodeInvalidTimestamp, Cause: ErrInvalidTimestamp}
	}
	now := time.Now().UTC()
	if a.Clock != nil {
		now = a.Clock().UTC()
	}
	skew := a.MaxSkew
	if skew <= 0 {
		skew = DefaultTimestampSkew
	}
	if d := now.Sub(when); d > skew || d < -skew {
		return &Error{Code: CodeInvalidTimestamp, Cause: ErrInvalidTimestamp}
	}
	if !strings.HasPrefix(signature, "v1=") {
		return &Error{Code: CodeInvalidSignature, Cause: ErrInvalidSignature}
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "v1="))
	if err != nil || len(provided) != sha256.Size {
		return &Error{Code: CodeInvalidSignature, Cause: ErrInvalidSignature}
	}
	expected := strings.TrimPrefix(Signature(a.Secret, eventID, timestamp, body), "v1=")
	expectedBytes, _ := hex.DecodeString(expected)
	if subtle.ConstantTimeCompare(provided, expectedBytes) != 1 {
		return &Error{Code: CodeInvalidSignature, Cause: ErrInvalidSignature}
	}
	return nil
}

// Event is the durable envelope.  PayloadCiphertext is intentionally omitted
// from JSON responses; only the digest and per-record outcomes are exposed.
type Event struct {
	ID                string      `json:"id"`
	Kind              Kind        `json:"kind"`
	PayloadDigest     string      `json:"payload_digest"`
	PayloadCiphertext string      `json:"-"`
	RecordCount       int         `json:"record_count"`
	Status            EventStatus `json:"status"`
	AttemptCount      int         `json:"attempt_count"`
	NextAttemptAt     time.Time   `json:"next_attempt_at"`
	FirstReceivedAt   time.Time   `json:"first_received_at"`
	LastAttemptAt     *time.Time  `json:"last_attempt_at,omitempty"`
	CompletedAt       *time.Time  `json:"completed_at,omitempty"`
	LastError         string      `json:"last_error,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

type RecordOutcome struct {
	Index      int          `json:"index"`
	EntityType string       `json:"entity_type"`
	ExternalID string       `json:"external_id,omitempty"`
	EntityID   string       `json:"entity_id,omitempty"`
	Status     RecordStatus `json:"status"`
	Reason     string       `json:"reason,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

type EventView struct {
	Event    Event           `json:"event"`
	Outcomes []RecordOutcome `json:"outcomes"`
}

// EventRepository is implemented by both the memory and PostgreSQL stores.
// Update and SaveOutcomes are called after an event has been durably accepted;
// a process crash therefore leaves a retryable event rather than losing body
// data.
type EventRepository interface {
	CreateEvent(context.Context, *Event) error
	GetEvent(context.Context, string) (*Event, error)
	UpdateEvent(context.Context, *Event) error
	ListDueEvents(context.Context, time.Time, int) ([]Event, error)
	ListOutcomes(context.Context, string) ([]RecordOutcome, error)
	SaveOutcomes(context.Context, string, []RecordOutcome) error
}

// PayloadCipher is the field-level encryption boundary.  Production wires the
// repository's configured key-ring encryptor; the service default is an
// ephemeral AES-GCM cipher suitable for memory mode and tests.
type PayloadCipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

type RecordHandler func(context.Context, Kind, int, json.RawMessage) (RecordOutcome, error)

type Config struct {
	Repository        EventRepository
	Secret            []byte
	Cipher            PayloadCipher
	Handler           RecordHandler
	Clock             func() time.Time
	MaxBodyBytes      int
	MaxRecords        int
	MaxSkew           time.Duration
	RetryInterval     time.Duration
	MaxRetryAge       time.Duration
	MaxAttempts       int
	VisibilityTimeout time.Duration
}

type Service struct {
	repo                       EventRepository
	auth                       Authenticator
	cipher                     PayloadCipher
	handler                    RecordHandler
	clock                      func() time.Time
	maxBodyBytes, maxRecords   int
	retryInterval, maxRetryAge time.Duration
	maxAttempts                int
	visibilityTimeout          time.Duration
}

func NewService(repo EventRepository, secret []byte) *Service {
	return NewServiceWithConfig(Config{Repository: repo, Secret: secret})
}

func NewServiceWithConfig(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = DefaultMaxBodyBytes
	}
	maxRecords := cfg.MaxRecords
	if maxRecords <= 0 {
		maxRecords = DefaultMaxRecords
	}
	interval := cfg.RetryInterval
	if interval <= 0 {
		interval = DefaultRetryInterval
	}
	maxAge := cfg.MaxRetryAge
	if maxAge <= 0 {
		maxAge = DefaultMaxRetryAge
	}
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultMaxAttempts
	}
	visibilityTimeout := cfg.VisibilityTimeout
	if visibilityTimeout <= 0 {
		visibilityTimeout = DefaultVisibilityTimeout
	}
	cipherImpl := cfg.Cipher
	if cipherImpl == nil {
		cipherImpl = newEphemeralCipher()
	}
	return &Service{
		repo:   cfg.Repository,
		auth:   Authenticator{Secret: append([]byte(nil), cfg.Secret...), Clock: clock, MaxSkew: cfg.MaxSkew},
		cipher: cipherImpl, handler: cfg.Handler, clock: clock,
		maxBodyBytes: maxBody, maxRecords: maxRecords,
		retryInterval: interval, maxRetryAge: maxAge, maxAttempts: attempts, visibilityTimeout: visibilityTimeout,
	}
}

func (s *Service) SetHandler(handler RecordHandler) { s.handler = handler }

// SetHandlerIfMissing wires the server's repository-backed ingestor while
// preserving a caller-supplied handler (used by specialised workers/tests).
func (s *Service) SetHandlerIfMissing(handler RecordHandler) {
	if s.handler == nil {
		s.handler = handler
	}
}

// Accept verifies and persists a body.  Existing event IDs are idempotent only
// when their digest and kind match; a mismatched replay is a conflict.
func (s *Service) Accept(ctx context.Context, kind Kind, headers http.Header, body []byte) (*Event, error) {
	if s.repo == nil {
		return nil, errors.New("inbound webhook repository is not configured")
	}
	if !kind.Valid() {
		return nil, &Error{Code: CodeInvalidPayload, Cause: ErrInvalidPayload}
	}
	if len(body) > s.maxBodyBytes {
		return nil, &Error{Code: CodePayloadTooLarge, Cause: ErrPayloadTooLarge}
	}
	eventID := strings.TrimSpace(headers.Get("X-Merlon-Event-Id"))
	timestamp := strings.TrimSpace(headers.Get("X-Merlon-Timestamp"))
	signature := strings.TrimSpace(headers.Get("X-Merlon-Signature"))
	if err := s.auth.Verify(eventID, timestamp, signature, body); err != nil {
		return nil, err
	}
	records, err := DecodeRecords(body, s.maxRecords)
	if err != nil {
		return nil, err
	}
	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	if existing, getErr := s.repo.GetEvent(ctx, eventID); getErr == nil {
		if existing.PayloadDigest != digest || existing.Kind != kind {
			return nil, &Error{Code: CodeConflict, Cause: ErrConflict}
		}
		return existing, nil
	} else if !errors.Is(getErr, ErrNotFound) {
		return nil, getErr
	}
	ciphertext, err := s.cipher.Encrypt(string(body))
	if err != nil {
		return nil, fmt.Errorf("encrypt inbound webhook payload: %w", err)
	}
	now := s.clock().UTC()
	event := &Event{ID: eventID, Kind: kind, PayloadDigest: digest, PayloadCiphertext: ciphertext,
		RecordCount: len(records), Status: StatusAccepted, NextAttemptAt: now.Add(s.retryInterval),
		FirstReceivedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateEvent(ctx, event); err != nil {
		if errors.Is(err, ErrConflict) {
			existing, getErr := s.repo.GetEvent(ctx, eventID)
			if getErr == nil && existing.PayloadDigest == digest && existing.Kind == kind {
				return existing, nil
			}
		}
		return nil, err
	}
	return event, nil
}

func DecodeRecords(body []byte, maxRecords int) ([]json.RawMessage, error) {
	var envelope json.RawMessage
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&envelope); err != nil {
		return nil, &Error{Code: CodeInvalidPayload, Message: "invalid JSON", Cause: ErrInvalidPayload}
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, &Error{Code: CodeInvalidPayload, Message: "request body must contain one JSON value", Cause: ErrInvalidPayload}
	} else if !errors.Is(err, io.EOF) {
		return nil, &Error{Code: CodeInvalidPayload, Message: "invalid trailing JSON", Cause: ErrInvalidPayload}
	}
	var records []json.RawMessage
	if len(envelope) > 0 && envelope[0] == '[' {
		if err := json.Unmarshal(envelope, &records); err != nil {
			return nil, &Error{Code: CodeInvalidPayload, Cause: ErrInvalidPayload}
		}
	} else {
		var object struct {
			Records []json.RawMessage `json:"records"`
		}
		if err := json.Unmarshal(envelope, &object); err != nil || object.Records == nil {
			return nil, &Error{Code: CodeInvalidPayload, Message: "payload must be an array or an object with records", Cause: ErrInvalidPayload}
		}
		records = object.Records
	}
	if len(records) == 0 {
		return nil, &Error{Code: CodeInvalidPayload, Message: "records must not be empty", Cause: ErrInvalidPayload}
	}
	if maxRecords > 0 && len(records) > maxRecords {
		return nil, &Error{Code: CodePayloadTooLarge, Message: fmt.Sprintf("at most %d records are allowed", maxRecords), Cause: ErrPayloadTooLarge}
	}
	for _, record := range records {
		var object map[string]json.RawMessage
		if len(record) == 0 || record[0] != '{' || json.Unmarshal(record, &object) != nil {
			return nil, &Error{Code: CodeInvalidPayload, Message: "each record must be a JSON object", Cause: ErrInvalidPayload}
		}
	}
	return records, nil
}

func (s *Service) Get(ctx context.Context, id string) (*EventView, error) {
	event, err := s.repo.GetEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	outcomes, err := s.repo.ListOutcomes(ctx, id)
	if err != nil {
		return nil, err
	}
	return &EventView{Event: *event, Outcomes: outcomes}, nil
}

func (s *Service) Replay(ctx context.Context, id string) (*Event, error) {
	event, err := s.repo.GetEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	event.Status, event.AttemptCount, event.NextAttemptAt, event.LastError = StatusAccepted, 0, now, ""
	event.CompletedAt, event.LastAttemptAt, event.UpdatedAt = nil, nil, now
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *Service) Process(ctx context.Context, id string) (*Event, error) {
	event, err := s.repo.GetEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	if event.Status == StatusCompleted {
		return event, nil
	}
	now := s.clock().UTC()
	if now.Sub(event.FirstReceivedAt) > s.maxRetryAge {
		return s.moveToDLQ(ctx, event, "retry age exceeded")
	}
	event.AttemptCount++
	event.Status = StatusRunning
	// A worker crash leaves the row running. NextAttemptAt is its visibility
	// deadline, after which another worker may safely retry it.
	event.NextAttemptAt = now.Add(s.visibilityTimeout)
	event.LastAttemptAt = &now
	event.UpdatedAt = now
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	plain, err := s.cipher.Decrypt(event.PayloadCiphertext)
	if err != nil {
		return s.retryOrDLQ(ctx, event, fmt.Sprintf("decrypt payload: %v", err))
	}
	records, err := DecodeRecords([]byte(plain), s.maxRecords)
	if err != nil {
		return s.retryOrDLQ(ctx, event, err.Error())
	}
	outcomes := make([]RecordOutcome, 0, len(records))
	retry := false
	for index, record := range records {
		var outcome RecordOutcome
		if s.handler == nil {
			outcome = RecordOutcome{Index: index, EntityType: string(event.Kind), Status: RecordRejected, Reason: "ingestor_unavailable"}
			retry = true
		} else {
			outcome, err = s.handler(ctx, event.Kind, index, record)
			outcome.Index = index
			if outcome.EntityType == "" {
				outcome.EntityType = strings.TrimSuffix(string(event.Kind), "s")
			}
			if err != nil {
				if errors.Is(err, ErrDependency) {
					outcome.Status, outcome.Reason, retry = RecordWaitingDependency, err.Error(), true
				} else {
					outcome.Status, outcome.Reason = RecordRejected, err.Error()
				}
			}
		}
		if outcome.Status == RecordWaitingDependency {
			retry = true
		}
		if outcome.Status == "" {
			outcome.Status, outcome.Reason = RecordRejected, "ingestor_returned_no_status"
		}
		if outcome.CreatedAt.IsZero() {
			outcome.CreatedAt = now
		}
		outcomes = append(outcomes, outcome)
	}
	if err := s.repo.SaveOutcomes(ctx, event.ID, outcomes); err != nil {
		return s.retryOrDLQ(ctx, event, fmt.Sprintf("save outcomes: %v", err))
	}
	if retry {
		return s.retryOrDLQ(ctx, event, "one or more records require retry")
	}
	event.Status, event.CompletedAt, event.LastError, event.UpdatedAt = StatusCompleted, &now, "", now
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *Service) retryOrDLQ(ctx context.Context, event *Event, reason string) (*Event, error) {
	now := s.clock().UTC()
	event.LastError, event.UpdatedAt = reason, now
	if event.AttemptCount >= s.maxAttempts || now.Sub(event.FirstReceivedAt) >= s.maxRetryAge {
		return s.moveToDLQ(ctx, event, reason)
	}
	backoff := s.retryInterval
	for n := 1; n < event.AttemptCount; n++ {
		if backoff >= time.Hour/2 {
			backoff = time.Hour
			break
		}
		backoff *= 2
	}
	if backoff > time.Hour {
		backoff = time.Hour
	}
	event.Status, event.NextAttemptAt = StatusFailed, now.Add(backoff)
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *Service) moveToDLQ(ctx context.Context, event *Event, reason string) (*Event, error) {
	now := s.clock().UTC()
	event.Status, event.LastError, event.UpdatedAt = StatusDLQ, reason, now
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

// RunWorker polls due events.  A poll interval is injectable for tests; the
// production caller passes the documented 30-second cadence.
func (s *Service) RunWorker(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = DefaultRetryInterval
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			events, err := s.repo.ListDueEvents(ctx, now.UTC(), 100)
			if err != nil {
				slog.Error("list due inbound webhook events", "error", err)
				continue
			}
			for i := range events {
				if _, err := s.Process(ctx, events[i].ID); err != nil && ctx.Err() == nil {
					slog.Error("process inbound webhook event", "event_id", events[i].ID, "error", err)
				}
			}
		}
	}
}

// ephemeralCipher keeps unauthenticated bodies out of the event repository in
// memory mode as well.  Production replaces it with the configured key-ring
// cipher, whose key survives restarts.
type ephemeralCipher struct{ key []byte }

func newEphemeralCipher() PayloadCipher {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("generate inbound webhook encryption key: " + err.Error())
	}
	return &ephemeralCipher{key: key}
}

func (c *ephemeralCipher) Encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *ephemeralCipher) Decrypt(encoded string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// MemoryEventRepository is useful for database-free development and unit
// tests.  It follows the same conflict and outcome semantics as the PG repo.
type MemoryEventRepository struct {
	mu       sync.RWMutex
	events   map[string]*Event
	outcomes map[string][]RecordOutcome
}

func NewMemoryEventRepository() *MemoryEventRepository {
	return &MemoryEventRepository{events: map[string]*Event{}, outcomes: map[string][]RecordOutcome{}}
}

func cloneEvent(event *Event) *Event {
	if event == nil {
		return nil
	}
	cp := *event
	return &cp
}

func (r *MemoryEventRepository) CreateEvent(_ context.Context, event *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.events[event.ID]; ok {
		return ErrConflict
	}
	r.events[event.ID] = cloneEvent(event)
	return nil
}
func (r *MemoryEventRepository) GetEvent(_ context.Context, id string) (*Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	event, ok := r.events[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneEvent(event), nil
}
func (r *MemoryEventRepository) UpdateEvent(_ context.Context, event *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.events[event.ID]; !ok {
		return ErrNotFound
	}
	r.events[event.ID] = cloneEvent(event)
	return nil
}
func (r *MemoryEventRepository) ListDueEvents(_ context.Context, now time.Time, limit int) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Event
	for _, event := range r.events {
		if (event.Status == StatusAccepted || event.Status == StatusFailed || event.Status == StatusRunning) && !event.NextAttemptAt.After(now) {
			result = append(result, *cloneEvent(event))
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}
func (r *MemoryEventRepository) ListOutcomes(_ context.Context, id string) ([]RecordOutcome, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.events[id]; !ok {
		return nil, ErrNotFound
	}
	return append([]RecordOutcome(nil), r.outcomes[id]...), nil
}
func (r *MemoryEventRepository) SaveOutcomes(_ context.Context, id string, outcomes []RecordOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.events[id]; !ok {
		return ErrNotFound
	}
	r.outcomes[id] = append([]RecordOutcome(nil), outcomes...)
	return nil
}
