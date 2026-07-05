// Package notify implements NOTIF-001 (email) and NOTIF-003 (severity/
// scenario routing) notifications. Package and exported type names are a
// cross-WS interface contract (ws08-notify-case.md): other work streams may
// depend on api/internal/notify.Notifier.
package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// Notification is the payload passed to Notifier.Send. It deliberately
// excludes customer names, amounts, or any other PII (notifications.md §1
// "通知本文には機微情報（PII）を含めず、ケース/アラートIDと本システムへの
// リンクのみを記載することを原則とする") — there is no field a caller could
// use to leak PII through even by mistake.
type Notification struct {
	AlertID    string
	CaseID     string
	Severity   domain.AlertSeverity
	ScenarioID string
	LinkURL    string
}

// Notifier sends a Notification through some channel (email, Webhook, etc).
type Notifier interface {
	Send(ctx context.Context, n Notification) error
}

// SMTPConfig holds the mail server settings (notifications.md §1: "SMTP設定
// （ホスト、ポート、認証、TLS）はシステム設定で構成する").
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	// UseTLS selects implicit TLS (typically port 465). When false,
	// smtp.SendMail is used, which opportunistically negotiates STARTTLS
	// with servers that advertise it (typically port 587).
	UseTLS bool
}

// Mailer implements Notifier over SMTP.
type Mailer struct {
	cfg SMTPConfig
	// transport is overridden in tests to avoid a real SMTP connection
	// (sandboxed environments cannot reach an SMTP server).
	transport func(cfg SMTPConfig, msg []byte) error
}

func NewMailer(cfg SMTPConfig) *Mailer {
	return &Mailer{cfg: cfg, transport: sendSMTP}
}

// Send composes and delivers a plain-text email containing only the IDs and
// link carried by n (notifications.md §1).
func (m *Mailer) Send(_ context.Context, n Notification) error {
	msg := buildMessage(m.cfg.From, m.cfg.To, n)
	return m.transport(m.cfg, msg)
}

func buildMessage(from string, to []string, n Notification) []byte {
	subject := fmt.Sprintf("[Merlon] %s alert", n.Severity)

	var body strings.Builder
	if n.AlertID != "" {
		fmt.Fprintf(&body, "Alert ID: %s\n", n.AlertID)
	}
	if n.CaseID != "" {
		fmt.Fprintf(&body, "Case ID: %s\n", n.CaseID)
	}
	if n.ScenarioID != "" {
		fmt.Fprintf(&body, "Scenario: %s\n", n.ScenarioID)
	}
	fmt.Fprintf(&body, "Severity: %s\n", n.Severity)
	if n.LinkURL != "" {
		fmt.Fprintf(&body, "View in Merlon: %s\n", n.LinkURL)
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("\r\n")
	msg.WriteString(body.String())
	return []byte(msg.String())
}

func sendSMTP(cfg SMTPConfig, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	if cfg.UseTLS {
		return sendSMTPImplicitTLS(cfg, addr, auth, msg)
	}
	return smtp.SendMail(addr, auth, cfg.From, cfg.To, msg)
}

// sendSMTPImplicitTLS handles the implicit-TLS case (e.g. port 465), which
// smtp.SendMail does not support directly.
func sendSMTPImplicitTLS(cfg SMTPConfig, addr string, auth smtp.Auth, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return err
	}
	for _, rcpt := range cfg.To {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = w.Write(msg)
	return err
}
