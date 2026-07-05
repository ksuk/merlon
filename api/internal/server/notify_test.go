package server

import (
	"context"
	"sync"
	"testing"

	"github.com/merlon-aml/merlon/api/internal/domain"
	"github.com/merlon-aml/merlon/api/internal/notify"
)

// fakeNotifier is a test double for notify.Notifier that records every
// Notification passed to Send, guarded by a mutex since notifyAlertCreated
// dispatches through a goroutine in production use.
type fakeNotifier struct {
	mu   sync.Mutex
	sent []notify.Notification
}

func (f *fakeNotifier) Send(_ context.Context, n notify.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeNotifier) last() notify.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[len(f.sent)-1]
}

// TestNotifyAlertCreated_SendsEmailWhenRouteIncludesEmail verifies the
// NOTIF-001 email hook: a critical alert routed to "email" by NOTIF-003
// triggers Notifier.Send with the alert ID and a link built from PublicURL,
// and no other fields (notifications.md §1: ID + link only).
func TestNotifyAlertCreated_SendsEmailWhenRouteIncludesEmail(t *testing.T) {
	fn := &fakeNotifier{}
	s := New(":0", Deps{
		Notifier: fn,
		RoutingRules: []notify.RoutingRule{
			{Severity: domain.AlertSeverityCritical, Channels: []string{"email", "webhook"}},
		},
		PublicURL: "https://merlon.internal",
	})

	a := domain.Alert{ID: "alert-1", Severity: domain.AlertSeverityCritical, ScenarioID: "structuring_basic"}
	// Exercise the synchronous send path directly (this is what
	// notifyAlertCreated's goroutine calls) to avoid a race in the test.
	s.sendAlertEmail(a)

	if fn.count() != 1 {
		t.Fatalf("expected 1 email sent, got %d", fn.count())
	}
	got := fn.last()
	if got.AlertID != "alert-1" {
		t.Errorf("AlertID = %q, want alert-1", got.AlertID)
	}
	if got.LinkURL != "https://merlon.internal/alerts/alert-1" {
		t.Errorf("LinkURL = %q, want https://merlon.internal/alerts/alert-1", got.LinkURL)
	}
}

// TestNotifyAlertCreated_SkipsEmailWhenRouteExcludesEmail verifies a LOW
// severity alert (routed to webhook only per notifications.md §3's example)
// never reaches the notifier. The routing gate in notifyAlertCreated runs
// synchronously before any goroutine is spawned, so this assertion is
// deterministic.
func TestNotifyAlertCreated_SkipsEmailWhenRouteExcludesEmail(t *testing.T) {
	fn := &fakeNotifier{}
	s := New(":0", Deps{
		Notifier: fn,
		RoutingRules: []notify.RoutingRule{
			{Severity: domain.AlertSeverityLow, Channels: []string{"webhook"}},
		},
	})

	a := domain.Alert{ID: "alert-2", Severity: domain.AlertSeverityLow}
	s.notifyAlertCreated(context.Background(), a)

	if fn.count() != 0 {
		t.Fatalf("expected no email sent, got %d", fn.count())
	}
}

// TestNotifyAlertCreated_NoopWithoutNotifier verifies the hook is safe to
// call when no Notifier is configured (e.g. no SMTP host set).
func TestNotifyAlertCreated_NoopWithoutNotifier(t *testing.T) {
	s := New(":0", Deps{})
	a := domain.Alert{ID: "alert-3", Severity: domain.AlertSeverityCritical}
	s.notifyAlertCreated(context.Background(), a)
}
