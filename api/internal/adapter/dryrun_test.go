package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDryRunReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer ts.Close()

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "none"})

	result, err := a.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if !result.ConfigValid {
		t.Error("expected ConfigValid = true")
	}
	if !result.Reachable {
		t.Errorf("expected Reachable = true, errors: %v", result.Errors)
	}
	if _, ok := result.EndpointResults["fetch_customer"]; !ok {
		t.Error("expected fetch_customer in EndpointResults")
	}
}

func TestDryRunUnreachable(t *testing.T) {
	a := newTestAdapter(t, "http://127.0.0.1:1", map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "none"})

	result, err := a.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if !result.ConfigValid {
		t.Error("expected ConfigValid = true")
	}
	if result.Reachable {
		t.Error("expected Reachable = false for unreachable host")
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors for unreachable host")
	}
}

func TestDryRunInvalidConfig(t *testing.T) {
	cfg := &AdapterConfig{
		Type:      "rest",
		BaseURL:   "",
		Auth:      AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{},
	}
	secCfg := SecurityConfig{BlockPrivateIPRanges: false}
	a := &RESTAdapter{
		config:    cfg,
		client:    http.DefaultClient,
		validator: NewURLValidator(secCfg),
		auth:      noAuth{},
	}

	result, err := a.DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if result.ConfigValid {
		t.Error("expected ConfigValid = false for invalid config")
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors for invalid config")
	}
}
