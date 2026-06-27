package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type RESTAdapter struct {
	config    *AdapterConfig
	client    *http.Client
	validator *URLValidator
	auth      authProvider
}

type authProvider interface {
	Apply(req *http.Request)
}

type noAuth struct{}

func (noAuth) Apply(*http.Request) {}

type bearerAuth struct{ token string }

func (a bearerAuth) Apply(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.token)
}

type basicAuth struct{ user, pass string }

func (a basicAuth) Apply(req *http.Request) {
	req.SetBasicAuth(a.user, a.pass)
}

type headerAuth struct{ name, value string }

func (a headerAuth) Apply(req *http.Request) {
	req.Header.Set(a.name, a.value)
}

func NewRESTAdapter(cfg *AdapterConfig, secCfg SecurityConfig) (*RESTAdapter, error) {
	auth, err := resolveAuth(cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("resolve auth: %w", err)
	}

	validator := NewURLValidator(secCfg)
	validator.AllowHTTP = true

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := newSafeTransport(secCfg)

	return &RESTAdapter{
		config:    cfg,
		client:    &http.Client{Timeout: timeout, Transport: transport},
		validator: validator,
		auth:      auth,
	}, nil
}

func resolveAuth(cfg AuthConfig) (authProvider, error) {
	switch cfg.Type {
	case "none", "":
		return noAuth{}, nil
	case "bearer":
		token := os.Getenv(cfg.TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("env var %q is empty", cfg.TokenEnv)
		}
		return bearerAuth{token: token}, nil
	case "basic":
		user := os.Getenv(cfg.UsernameEnv)
		pass := os.Getenv(cfg.PasswordEnv)
		if user == "" || pass == "" {
			return nil, fmt.Errorf("env vars %q/%q must be set", cfg.UsernameEnv, cfg.PasswordEnv)
		}
		return basicAuth{user: user, pass: pass}, nil
	case "header":
		val := os.Getenv(cfg.HeaderValEnv)
		if val == "" {
			return nil, fmt.Errorf("env var %q is empty", cfg.HeaderValEnv)
		}
		return headerAuth{name: cfg.HeaderName, value: val}, nil
	default:
		return nil, fmt.Errorf("unsupported auth type %q", cfg.Type)
	}
}

func newSafeTransport(secCfg SecurityConfig) *http.Transport {
	if !secCfg.BlockPrivateIPRanges {
		return http.DefaultTransport.(*http.Transport).Clone()
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("DNS resolution failed for %q: %w", host, err)
			}
			for _, ip := range ips {
				if isPrivateIP(ip.IP.String()) {
					return nil, fmt.Errorf("host %q resolves to private IP %s", host, ip.IP)
				}
			}
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
}

func (a *RESTAdapter) FetchCustomer(ctx context.Context, id string) (*CustomerData, error) {
	ep, ok := a.config.Endpoints["fetch_customer"]
	if !ok {
		return nil, fmt.Errorf("endpoint \"fetch_customer\" is not configured")
	}

	raw, err := a.callEndpoint(ctx, ep, map[string]string{"id": id}, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch customer %q: %w", id, err)
	}

	fields := ApplyFieldMapping(raw, ep.FieldMapping)
	return &CustomerData{
		ExternalID:   toString(fields["external_id"]),
		Name:         toString(fields["name"]),
		Country:      toString(fields["country"]),
		CustomerType: toString(fields["customer_type"]),
		RawFields:    fields,
	}, nil
}

func (a *RESTAdapter) FetchTransactions(ctx context.Context, params map[string]string) ([]TransactionData, error) {
	ep, ok := a.config.Endpoints["fetch_transactions"]
	if !ok {
		return nil, fmt.Errorf("endpoint \"fetch_transactions\" is not configured")
	}

	raw, err := a.callEndpoint(ctx, ep, nil, params)
	if err != nil {
		return nil, fmt.Errorf("fetch transactions: %w", err)
	}

	var items []any
	if ep.ResponseRoot != "" {
		root, ok := ExtractField(raw, ep.ResponseRoot)
		if !ok {
			return nil, fmt.Errorf("response_root %q not found in response", ep.ResponseRoot)
		}
		items, ok = root.([]any)
		if !ok {
			return nil, fmt.Errorf("response_root %q is not an array", ep.ResponseRoot)
		}
	} else {
		return nil, fmt.Errorf("response_root is required for fetch_transactions")
	}

	result := make([]TransactionData, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fields := ApplyFieldMapping(m, ep.FieldMapping)
		result = append(result, TransactionData{
			ExternalID: toString(fields["external_id"]),
			Amount:     toString(fields["amount"]),
			Currency:   toString(fields["currency"]),
			Type:       toString(fields["type"]),
			RawFields:  fields,
		})
	}

	return result, nil
}

func (a *RESTAdapter) callEndpoint(ctx context.Context, ep EndpointConfig, pathParams, queryParams map[string]string) (map[string]any, error) {
	path := ep.Path
	for k, v := range pathParams {
		path = strings.ReplaceAll(path, "{"+k+"}", v)
	}

	fullURL := strings.TrimRight(a.config.BaseURL, "/") + path

	req, err := http.NewRequestWithContext(ctx, ep.Method, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	q := req.URL.Query()
	for k, tmpl := range ep.Params {
		val := tmpl
		for pk, pv := range queryParams {
			val = strings.ReplaceAll(val, "{"+pk+"}", pv)
		}
		q.Set(k, val)
	}
	req.URL.RawQuery = q.Encode()

	a.auth.Apply(req)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, preview)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode JSON response: %w", err)
	}

	return result, nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%g", val)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}
