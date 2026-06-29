package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func newTestAdapter(t *testing.T, serverURL string, endpoints map[string]EndpointConfig, auth AuthConfig) *RESTAdapter {
	t.Helper()
	cfg := &AdapterConfig{
		Type:           "rest",
		BaseURL:        serverURL,
		Auth:           auth,
		Endpoints:      endpoints,
		TimeoutSeconds: 5,
	}
	secCfg := SecurityConfig{
		OutboundAllowlist:    []string{},
		BlockPrivateIPRanges: false,
	}
	a, err := NewRESTAdapter(cfg, secCfg)
	if err != nil {
		t.Fatalf("NewRESTAdapter() error = %v", err)
	}
	return a
}

func TestFetchCustomer(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/customers/C001" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"customer_id": "C001",
			"full_name":   "Taro Yamada",
			"address":     map[string]any{"country_code": "JP"},
			"type":        "individual",
		})
	})
	defer ts.Close()

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method: "GET",
			Path:   "/customers/{id}",
			FieldMapping: map[string]string{
				"external_id":   "$.customer_id",
				"name":          "$.full_name",
				"country":       "$.address.country_code",
				"customer_type": "$.type",
			},
		},
	}, AuthConfig{Type: "none"})

	cust, err := a.FetchCustomer(context.Background(), "C001")
	if err != nil {
		t.Fatalf("FetchCustomer() error = %v", err)
	}
	if cust.ExternalID != "C001" {
		t.Errorf("ExternalID = %q, want %q", cust.ExternalID, "C001")
	}
	if cust.Name != "Taro Yamada" {
		t.Errorf("Name = %q, want %q", cust.Name, "Taro Yamada")
	}
	if cust.Country != "JP" {
		t.Errorf("Country = %q, want %q", cust.Country, "JP")
	}
	if cust.CustomerType != "individual" {
		t.Errorf("CustomerType = %q, want %q", cust.CustomerType, "individual")
	}
}

func TestFetchTransactions(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transactions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("since") != "2024-01-01" {
			t.Errorf("unexpected since param: %s", r.URL.Query().Get("since"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"transactions": []any{
				map[string]any{
					"txn_id":   "T001",
					"amount":   "15000.50",
					"currency": "JPY",
					"type":     "deposit",
				},
				map[string]any{
					"txn_id":   "T002",
					"amount":   "3000",
					"currency": "JPY",
					"type":     "withdrawal",
				},
			},
		})
	})
	defer ts.Close()

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_transactions": {
			Method:       "GET",
			Path:         "/transactions",
			Params:       map[string]string{"since": "{since}"},
			ResponseRoot: "$.transactions",
			FieldMapping: map[string]string{
				"external_id": "$.txn_id",
				"amount":      "$.amount",
				"currency":    "$.currency",
				"type":        "$.type",
			},
		},
	}, AuthConfig{Type: "none"})

	txns, err := a.FetchTransactions(context.Background(), map[string]string{"since": "2024-01-01"})
	if err != nil {
		t.Fatalf("FetchTransactions() error = %v", err)
	}
	if len(txns) != 2 {
		t.Fatalf("got %d transactions, want 2", len(txns))
	}
	if txns[0].ExternalID != "T001" {
		t.Errorf("txns[0].ExternalID = %q, want %q", txns[0].ExternalID, "T001")
	}
	if txns[1].ExternalID != "T002" {
		t.Errorf("txns[1].ExternalID = %q, want %q", txns[1].ExternalID, "T002")
	}
}

func TestBearerAuth(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-secret-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-secret-token")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "C001"})
	})
	defer ts.Close()

	t.Setenv("TEST_BEARER_TOKEN", "test-secret-token")

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "bearer", TokenEnv: "TEST_BEARER_TOKEN"})

	_, err := a.FetchCustomer(context.Background(), "C001")
	if err != nil {
		t.Fatalf("FetchCustomer() error = %v", err)
	}
}

func TestBasicAuth(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			t.Errorf("BasicAuth = (%q, %q, %v)", user, pass, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "C001"})
	})
	defer ts.Close()

	t.Setenv("TEST_USER", "admin")
	t.Setenv("TEST_PASS", "secret")

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "basic", UsernameEnv: "TEST_USER", PasswordEnv: "TEST_PASS"})

	_, err := a.FetchCustomer(context.Background(), "C001")
	if err != nil {
		t.Fatalf("FetchCustomer() error = %v", err)
	}
}

func TestCustomHeaderAuth(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "my-api-key" {
			t.Errorf("X-API-Key = %q", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "C001"})
	})
	defer ts.Close()

	t.Setenv("TEST_API_KEY", "my-api-key")

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "header", HeaderName: "X-API-Key", HeaderValEnv: "TEST_API_KEY"})

	_, err := a.FetchCustomer(context.Background(), "C001")
	if err != nil {
		t.Fatalf("FetchCustomer() error = %v", err)
	}
}

func TestNon2xxResponse(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "not found"}`))
	})
	defer ts.Close()

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "none"})

	_, err := a.FetchCustomer(context.Background(), "C001")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer ts.Close()

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{
		"fetch_customer": {
			Method:       "GET",
			Path:         "/customers/{id}",
			FieldMapping: map[string]string{"external_id": "$.id"},
		},
	}, AuthConfig{Type: "none"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.FetchCustomer(ctx, "C001")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestMissingEndpoint(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "C001"})
	})
	defer ts.Close()

	a := newTestAdapter(t, ts.URL, map[string]EndpointConfig{}, AuthConfig{Type: "none"})

	_, err := a.FetchCustomer(context.Background(), "C001")
	if err == nil {
		t.Fatal("expected error for missing fetch_customer endpoint")
	}
}

func TestCallEndpointValidatesURL(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "C001"})
	})
	defer ts.Close()

	cfg := &AdapterConfig{
		Type:    "rest",
		BaseURL: ts.URL,
		Auth:    AuthConfig{Type: "none"},
		Endpoints: map[string]EndpointConfig{
			"fetch_customer": {
				Method:       "GET",
				Path:         "/customers/{id}",
				FieldMapping: map[string]string{"external_id": "$.id"},
			},
		},
		TimeoutSeconds: 5,
	}
	secCfg := SecurityConfig{
		OutboundAllowlist:    []string{"allowed.example.com"},
		BlockPrivateIPRanges: false,
	}
	a, err := NewRESTAdapter(cfg, secCfg)
	if err != nil {
		t.Fatalf("NewRESTAdapter() error = %v", err)
	}

	_, err = a.FetchCustomer(context.Background(), "C001")
	if err == nil {
		t.Fatal("expected error: host not in allowlist")
	}
	if !strings.Contains(err.Error(), "not in the outbound allowlist") {
		t.Errorf("expected allowlist error, got: %v", err)
	}
}

func TestInterfaceCompliance(t *testing.T) {
	var _ Adapter = (*RESTAdapter)(nil)
}
