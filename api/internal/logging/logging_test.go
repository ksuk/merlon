package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestMaskingHandlerAppliesToKnownAttrKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	logger.Info("customer viewed",
		"customer_name", "田中太郎",
		"email", "keisuke@example.com",
		"phone", "090-1234-5678",
		"external_id", "cust-00123",
		"ip_address", "192.168.1.100",
		"action", "view",
	)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log output as JSON: %v (output: %s)", err, buf.String())
	}

	if record["customer_name"] != "田***" {
		t.Errorf("customer_name = %v, want 田***", record["customer_name"])
	}
	if record["email"] != "ke***@example.com" {
		t.Errorf("email = %v, want ke***@example.com", record["email"])
	}
	if record["phone"] != "***-***-5678" {
		t.Errorf("phone = %v, want ***-***-5678", record["phone"])
	}
	if record["external_id"] == "cust-00123" {
		t.Errorf("external_id was not masked: %v", record["external_id"])
	}
	if record["ip_address"] != "192.168.1.***" {
		t.Errorf("ip_address = %v, want 192.168.1.***", record["ip_address"])
	}
	if record["action"] != "view" {
		t.Errorf("action = %v, want it to pass through unmasked", record["action"])
	}
}

func TestNewLoggerProducesJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	logger.Info("startup", "component", "api")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("expected JSON output, got error: %v (output: %s)", err, buf.String())
	}
	if record["msg"] != "startup" {
		t.Errorf("msg = %v, want startup", record["msg"])
	}
}

func TestMaskingHandlerPassesThroughUnknownAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := MaskingHandler(base)
	logger := slog.New(handler)

	logger.Info("event", "some_field", "unmasked-value")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}
	if record["some_field"] != "unmasked-value" {
		t.Errorf("some_field = %v, want unmasked-value", record["some_field"])
	}
}
