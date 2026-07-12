// Package engineclient wraps api/internal/engine.Client with a circuit
// breaker so that a stalled or crashed Rust engine degrades the API into
// PENDING_REVIEW queueing (OPS-005, Fail-Alert) instead of blocking every
// caller on a hung gRPC call.
package engineclient

import (
	"context"
	"errors"
	"sync"
	"time"
)

type CircuitState string

const (
	StateClosed   CircuitState = "closed"
	StateOpen     CircuitState = "open"
	StateHalfOpen CircuitState = "half_open"
)

// ErrCircuitOpen is returned by Call when the breaker is open (or half-open
// with a probe already in flight) and the downstream call is skipped
// entirely.
var ErrCircuitOpen = errors.New("engineclient: circuit breaker open")

// circuitBreakerConfig holds the numeric constants the operational design §4.4's
// "障害対応マトリクス" Rust engine row specifies, plus a clock hook so tests
// can advance time deterministically instead of sleeping for real.
type circuitBreakerConfig struct {
	timeout      time.Duration
	maxRetries   int
	baseBackoff  time.Duration
	openDuration time.Duration
	now          func() time.Time
}

// CircuitBreaker guards calls to the Rust engine. Per the operational design §4.4: gRPC
// calls get a 3s timeout and 2 retries with exponential backoff; if all
// attempts fail the breaker opens for 30s, then allows exactly one
// half-open probe request before deciding whether to close or reopen.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	openedAt         time.Time
	halfOpenInFlight bool

	cfg circuitBreakerConfig
}

func newCircuitBreaker(cfg circuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{state: StateClosed, cfg: cfg}
}

// NewCircuitBreaker builds a breaker with the production constants: 3s
// timeout, 2 retries (exponential backoff starting at 100ms), 30s open
// window, single half-open probe.
func NewCircuitBreaker() *CircuitBreaker {
	return newCircuitBreaker(circuitBreakerConfig{
		timeout:      3 * time.Second,
		maxRetries:   2,
		baseBackoff:  100 * time.Millisecond,
		openDuration: 30 * time.Second,
		now:          time.Now,
	})
}

// State reports the breaker's current state, first applying the
// Open-to-Half-Open transition if the open window has elapsed.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.maybeExpireOpenLocked()
	return cb.state
}

func (cb *CircuitBreaker) maybeExpireOpenLocked() {
	if cb.state == StateOpen && cb.cfg.now().Sub(cb.openedAt) >= cb.cfg.openDuration {
		cb.state = StateHalfOpen
		cb.halfOpenInFlight = false
	}
}

// Call runs fn under the breaker. While closed, fn is retried up to
// maxRetries times (exponential backoff) under a per-attempt timeout; any
// failure after retries are exhausted opens the breaker. While open, Call
// returns ErrCircuitOpen immediately without invoking fn. While half-open,
// exactly one caller's fn is allowed through; concurrent callers are
// rejected until that probe completes, and its result decides whether the
// breaker closes (success) or reopens (failure).
func (cb *CircuitBreaker) Call(ctx context.Context, fn func(ctx context.Context) error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := cb.attemptWithRetry(ctx, fn)
	cb.recordResult(err)
	return err
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.maybeExpireOpenLocked()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		return false
	case StateHalfOpen:
		if cb.halfOpenInFlight {
			return false
		}
		cb.halfOpenInFlight = true
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateHalfOpen:
		cb.halfOpenInFlight = false
		if err != nil {
			cb.state = StateOpen
			cb.openedAt = cb.cfg.now()
		} else {
			cb.state = StateClosed
		}
	case StateClosed:
		if err != nil {
			cb.state = StateOpen
			cb.openedAt = cb.cfg.now()
		}
	}
}

// attemptWithRetry performs up to 1+maxRetries attempts of fn, each bounded
// by cfg.timeout, waiting cfg.baseBackoff*2^n between attempts.
func (cb *CircuitBreaker) attemptWithRetry(ctx context.Context, fn func(ctx context.Context) error) error {
	backoff := cb.cfg.baseBackoff
	var err error
	for attempt := 0; attempt <= cb.cfg.maxRetries; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, cb.cfg.timeout)
		err = fn(callCtx)
		cancel()
		if err == nil {
			return nil
		}
		if attempt < cb.cfg.maxRetries {
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
			backoff *= 2
		}
	}
	return err
}
