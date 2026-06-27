package adapter

import (
	"testing"
)

func TestLoadAdapterConfigValid(t *testing.T) {
	cfg, err := LoadAdapterConfig("testdata/valid_adapter.yaml")
	if err != nil {
		t.Fatalf("LoadAdapterConfig() error = %v", err)
	}

	if cfg.Type != "rest" {
		t.Errorf("Type = %q, want %q", cfg.Type, "rest")
	}
	if cfg.BaseURL != "https://core.example.com/api/v1" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://core.example.com/api/v1")
	}
	if cfg.TimeoutSeconds != 15 {
		t.Errorf("TimeoutSeconds = %d, want 15", cfg.TimeoutSeconds)
	}
	if cfg.Auth.Type != "bearer" {
		t.Errorf("Auth.Type = %q, want %q", cfg.Auth.Type, "bearer")
	}
	if cfg.Auth.TokenEnv != "CORE_API_TOKEN" {
		t.Errorf("Auth.TokenEnv = %q, want %q", cfg.Auth.TokenEnv, "CORE_API_TOKEN")
	}

	ep, ok := cfg.Endpoints["fetch_customer"]
	if !ok {
		t.Fatal("fetch_customer endpoint not found")
	}
	if ep.Method != "GET" {
		t.Errorf("fetch_customer.Method = %q, want %q", ep.Method, "GET")
	}
	if ep.Path != "/customers/{id}" {
		t.Errorf("fetch_customer.Path = %q, want %q", ep.Path, "/customers/{id}")
	}
	if ep.FieldMapping["external_id"] != "$.customer_id" {
		t.Errorf("fetch_customer.FieldMapping[external_id] = %q, want %q", ep.FieldMapping["external_id"], "$.customer_id")
	}

	txn, ok := cfg.Endpoints["fetch_transactions"]
	if !ok {
		t.Fatal("fetch_transactions endpoint not found")
	}
	if txn.ResponseRoot != "$.transactions" {
		t.Errorf("fetch_transactions.ResponseRoot = %q, want %q", txn.ResponseRoot, "$.transactions")
	}
	if txn.Params["account_id"] != "{account_id}" {
		t.Errorf("fetch_transactions.Params[account_id] = %q, want %q", txn.Params["account_id"], "{account_id}")
	}
}

func TestLoadAdapterConfigMinimal(t *testing.T) {
	cfg, err := LoadAdapterConfig("testdata/minimal_adapter.yaml")
	if err != nil {
		t.Fatalf("LoadAdapterConfig() error = %v", err)
	}
	if cfg.Type != "rest" {
		t.Errorf("Type = %q, want %q", cfg.Type, "rest")
	}
	if cfg.Auth.Type != "none" {
		t.Errorf("Auth.Type = %q, want %q", cfg.Auth.Type, "none")
	}
}

func TestLoadAdapterConfigNotFound(t *testing.T) {
	_, err := LoadAdapterConfig("testdata/nonexistent.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestValidateValid(t *testing.T) {
	cfg, err := LoadAdapterConfig("testdata/valid_adapter.yaml")
	if err != nil {
		t.Fatalf("LoadAdapterConfig() error = %v", err)
	}
	t.Setenv("CORE_API_TOKEN", "test-token")
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestValidateMinimal(t *testing.T) {
	cfg, err := LoadAdapterConfig("testdata/minimal_adapter.yaml")
	if err != nil {
		t.Fatalf("LoadAdapterConfig() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestValidateNoBaseURL(t *testing.T) {
	cfg, err := LoadAdapterConfig("testdata/invalid_no_base_url.yaml")
	if err != nil {
		t.Fatalf("LoadAdapterConfig() error = %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for missing base_url")
	}
}

func TestValidateBadAuth(t *testing.T) {
	cfg, err := LoadAdapterConfig("testdata/invalid_bad_auth.yaml")
	if err != nil {
		t.Fatalf("LoadAdapterConfig() error = %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for bearer auth without token_env")
	}
}

func TestValidateUnsupportedType(t *testing.T) {
	cfg := &AdapterConfig{
		Type:    "graphql",
		BaseURL: "https://example.com",
		Auth:    AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{
			"test": {Method: "GET", Path: "/test", FieldMapping: map[string]string{"id": "$.id"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for unsupported type")
	}
}

func TestValidateNoEndpoints(t *testing.T) {
	cfg := &AdapterConfig{
		Type:      "rest",
		BaseURL:   "https://example.com",
		Auth:      AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for empty endpoints")
	}
}

func TestValidateEndpointNoMethod(t *testing.T) {
	cfg := &AdapterConfig{
		Type:    "rest",
		BaseURL: "https://example.com",
		Auth:    AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{
			"test": {Path: "/test", FieldMapping: map[string]string{"id": "$.id"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for endpoint without method")
	}
}

func TestValidateEndpointNoFieldMapping(t *testing.T) {
	cfg := &AdapterConfig{
		Type:    "rest",
		BaseURL: "https://example.com",
		Auth:    AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{
			"test": {Method: "GET", Path: "/test", FieldMapping: map[string]string{}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for endpoint without field mappings")
	}
}

func TestValidateDefaultTimeout(t *testing.T) {
	cfg := &AdapterConfig{
		Type:    "rest",
		BaseURL: "https://example.com",
		Auth:    AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{
			"test": {Method: "GET", Path: "/test", FieldMapping: map[string]string{"id": "$.id"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds = %d, want 30 (default)", cfg.TimeoutSeconds)
	}
}
