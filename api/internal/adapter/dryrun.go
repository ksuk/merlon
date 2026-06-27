package adapter

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"
)

func (a *RESTAdapter) DryRun(ctx context.Context) (*DryRunResult, error) {
	result := &DryRunResult{
		EndpointResults: make(map[string]string),
	}

	if err := a.config.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("config validation: %s", err))
		return result, nil
	}
	result.ConfigValid = true

	u, err := url.Parse(a.config.BaseURL)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("invalid base_url: %s", err))
		return result, nil
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("connectivity: %s", err))
		return result, nil
	}
	conn.Close()
	result.Reachable = true

	result.AuthValid = true

	for name := range a.config.Endpoints {
		result.EndpointResults[name] = "configured"
	}

	return result, nil
}
