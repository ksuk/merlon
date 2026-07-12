package server

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/notify"
)

// notifyAlertCreated resolves NOTIF-003 routing for a newly created alert
// and, when the resolved channels include "email", sends a PII-free email
// notification (NOTIF-001, notifications.md §1/§3). Email delivery runs in
// its own goroutine so a slow or unreachable SMTP server never blocks the
// alert-creation path, mirroring dispatchWebhook's fire-and-forget model.
func (s *Server) notifyAlertCreated(ctx context.Context, a domain.Alert) {
	if s.notifier == nil {
		return
	}
	channels := notify.ResolveRoute(s.routingRules, a.Severity, a.ScenarioID)
	if !hasChannel(channels, "email") {
		return
	}
	go s.sendAlertEmail(a)
}

// sendAlertEmail performs the Notifier.Send call for a single alert. It is
// split out from notifyAlertCreated so tests can invoke it synchronously
// without racing the goroutine dispatch above.
func (s *Server) sendAlertEmail(a domain.Alert) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	n := notify.Notification{
		AlertID:    a.ID,
		Severity:   a.Severity,
		ScenarioID: a.ScenarioID,
		LinkURL:    s.alertLinkURL(a.ID),
	}
	if err := s.notifier.Send(ctx, n); err != nil {
		slog.Error("email notification failed", "alert_id", a.ID, "error", err)
	}
}

// alertLinkURL builds the system link carried in the notification email
// (notifications.md §1: "ケース/アラートIDと本システムへのリンクのみを記載
// する"). Returns "" when no PublicURL is configured, in which case the
// mailer simply omits the link line (mailer.go buildMessage).
func (s *Server) alertLinkURL(alertID string) string {
	if s.publicURL == "" || alertID == "" {
		return ""
	}
	return strings.TrimRight(s.publicURL, "/") + "/alerts/" + alertID
}

func hasChannel(channels []string, want string) bool {
	for _, c := range channels {
		if c == want {
			return true
		}
	}
	return false
}
