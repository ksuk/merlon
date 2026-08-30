package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func validateLoopbackBaseURL(raw string) (*url.URL, error) {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return nil, errors.New("base URL must be non-empty and contain no surrounding whitespace")
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if err := validateLoopbackDestination(target); err != nil {
		return nil, err
	}
	if target.Path != "" && target.Path != "/" {
		return nil, errors.New("base URL must not contain a path")
	}
	if target.RawPath != "" || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("base URL must not contain an escaped path, query, or fragment")
	}
	target.Path = ""
	return target, nil
}

func validateLoopbackDestination(target *url.URL) error {
	if target == nil {
		return errors.New("destination URL is nil")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return errors.New("destination scheme must be http or https")
	}
	if target.Host == "" {
		return errors.New("destination host is required")
	}
	if target.User != nil {
		return errors.New("credentials must not be embedded in the destination URL")
	}

	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("destination host %q is not a loopback address", target.Hostname())
	}
	return nil
}

func newLoopbackHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A localhost-only verifier must not honor HTTP_PROXY or HTTPS_PROXY. The
	// request has to reach the loopback service directly or fail closed.
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if err := validateLoopbackDestination(req.URL); err != nil {
				return fmt.Errorf("reject redirect: %w", err)
			}
			return nil
		},
	}
}
