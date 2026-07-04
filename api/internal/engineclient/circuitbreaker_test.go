package engineclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestCircuitBreaker builds a CircuitBreaker with short, deterministic
// durations and an injectable clock so tests don't need to sleep for the
// real 3s timeout / 30s open window (overview.md §4.4 Rust engine row).
func newTestCircuitBreaker(clock *fakeClock) *CircuitBreaker {
	return newCircuitBreaker(circuitBreakerConfig{
		timeout:      20 * time.Millisecond,
		maxRetries:   2,
		baseBackoff:  1 * time.Millisecond,
		openDuration: 100 * time.Millisecond,
		now:          clock.Now,
	})
}

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }
func (f *fakeClock) Advance(d time.Duration) { f.t = f.t.Add(d) }

var errBoom = errors.New("boom")

func alwaysFail(_ context.Context) error { return errBoom }
func alwaysSucceed(_ context.Context) error { return nil }

func TestCircuitBreaker_ClosedToOpenAfterTimeoutAndRetries(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	if cb.State() != StateClosed {
		t.Fatalf("initial state = %s, want closed", cb.State())
	}

	err := cb.Call(context.Background(), alwaysFail)
	if err == nil {
		t.Fatal("expected error from failing call")
	}

	if cb.State() != StateOpen {
		t.Fatalf("state after exhausted retries = %s, want open", cb.State())
	}
}

func TestCircuitBreaker_OpenRejectsImmediately(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	cb.Call(context.Background(), alwaysFail)
	if cb.State() != StateOpen {
		t.Fatalf("state = %s, want open", cb.State())
	}

	calls := 0
	err := cb.Call(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	})
	if err == nil {
		t.Fatal("expected Call to reject immediately while open")
	}
	if calls != 0 {
		t.Errorf("downstream fn was called %d times while open, want 0", calls)
	}
}

func TestCircuitBreaker_OpenToHalfOpenAfter30Seconds(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	cb.Call(context.Background(), alwaysFail)
	if cb.State() != StateOpen {
		t.Fatalf("state = %s, want open", cb.State())
	}

	clock.Advance(100 * time.Millisecond)

	if cb.State() != StateHalfOpen {
		t.Fatalf("state after open duration elapsed = %s, want half_open", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenAllowsOneRequest(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	cb.Call(context.Background(), alwaysFail)
	clock.Advance(100 * time.Millisecond)
	if cb.State() != StateHalfOpen {
		t.Fatalf("state = %s, want half_open", cb.State())
	}

	blocked := make(chan struct{})
	started := make(chan struct{})
	go func() {
		cb.Call(context.Background(), func(ctx context.Context) error {
			close(started)
			<-blocked
			return nil
		})
	}()
	<-started

	// A second call while the first half-open probe is still in flight must
	// be rejected without invoking the downstream fn.
	calls := 0
	err := cb.Call(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	})
	close(blocked)

	if err == nil {
		t.Fatal("expected second half-open call to be rejected")
	}
	if calls != 0 {
		t.Errorf("second half-open call invoked downstream fn %d times, want 0", calls)
	}
}

func TestCircuitBreaker_HalfOpenSuccessReturnsToClosed(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	cb.Call(context.Background(), alwaysFail)
	clock.Advance(100 * time.Millisecond)
	if cb.State() != StateHalfOpen {
		t.Fatalf("state = %s, want half_open", cb.State())
	}

	if err := cb.Call(context.Background(), alwaysSucceed); err != nil {
		t.Fatalf("half-open probe call: %v", err)
	}
	if cb.State() != StateClosed {
		t.Fatalf("state after successful half-open probe = %s, want closed", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReturnsToOpen(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	cb.Call(context.Background(), alwaysFail)
	clock.Advance(100 * time.Millisecond)
	if cb.State() != StateHalfOpen {
		t.Fatalf("state = %s, want half_open", cb.State())
	}

	if err := cb.Call(context.Background(), alwaysFail); err == nil {
		t.Fatal("expected error from failing half-open probe")
	}
	if cb.State() != StateOpen {
		t.Fatalf("state after failed half-open probe = %s, want open", cb.State())
	}
}

func TestCircuitBreaker_StateTransitionSequence(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	cb := newTestCircuitBreaker(clock)

	if cb.State() != StateClosed {
		t.Fatalf("1: state = %s, want closed", cb.State())
	}

	cb.Call(context.Background(), alwaysFail)
	if cb.State() != StateOpen {
		t.Fatalf("2: state = %s, want open", cb.State())
	}

	clock.Advance(100 * time.Millisecond)
	if cb.State() != StateHalfOpen {
		t.Fatalf("3: state = %s, want half_open", cb.State())
	}

	if err := cb.Call(context.Background(), alwaysSucceed); err != nil {
		t.Fatalf("half-open probe: %v", err)
	}
	if cb.State() != StateClosed {
		t.Fatalf("4: state = %s, want closed", cb.State())
	}
}
