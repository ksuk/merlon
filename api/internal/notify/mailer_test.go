package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
)

func testMailer(t *testing.T) (*Mailer, *[]byte) {
	t.Helper()
	var captured []byte
	m := NewMailer(SMTPConfig{
		Host: "smtp.example.com", Port: 587, From: "merlon@example.com",
		To: []string{"compliance@example.com"},
	})
	m.transport = func(cfg SMTPConfig, msg []byte) error {
		captured = msg
		return nil
	}
	return m, &captured
}

// TestMailer_Send_ContainsNoPII verifies notifications.md §1: the message
// must never contain customer names or transaction amounts, only IDs and a
// link. Notification has no field that could carry such data, but this test
// guards against a future regression that adds one and forgets to keep the
// mailer PII-free.
func TestMailer_Send_ContainsNoPII(t *testing.T) {
	m, captured := testMailer(t)

	n := Notification{
		AlertID:    "alert-123",
		CaseID:     "case-456",
		Severity:   domain.AlertSeverityCritical,
		ScenarioID: "structuring_basic",
		LinkURL:    "https://merlon.internal/cases/case-456",
	}
	if err := m.Send(context.Background(), n); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := string(*captured)
	knownPII := []string{
		"山田太郎", "Taro Yamada", "1,000,000円", "customer_name", "amount",
	}
	for _, pii := range knownPII {
		if strings.Contains(body, pii) {
			t.Errorf("message body contains PII marker %q:\n%s", pii, body)
		}
	}
}

// TestMailer_Send_ContainsIDAndLink verifies the message body carries only
// the case/alert ID and the system link (notifications.md §1).
func TestMailer_Send_ContainsIDAndLink(t *testing.T) {
	m, captured := testMailer(t)

	n := Notification{
		AlertID:  "alert-789",
		Severity: domain.AlertSeverityHigh,
		LinkURL:  "https://merlon.internal/alerts/alert-789",
	}
	if err := m.Send(context.Background(), n); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := string(*captured)
	if !strings.Contains(body, "alert-789") {
		t.Errorf("message body missing alert ID:\n%s", body)
	}
	if !strings.Contains(body, "https://merlon.internal/alerts/alert-789") {
		t.Errorf("message body missing link URL:\n%s", body)
	}
}
