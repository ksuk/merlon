package adapter

import (
	"fmt"
	"net"
	"net/url"
)

type SecurityConfig struct {
	OutboundAllowlist    []string `yaml:"outbound_allowlist"`
	BlockPrivateIPRanges bool     `yaml:"block_private_ip_ranges"`
}

type URLValidator struct {
	allowlist    map[string]bool
	blockPrivate bool
	AllowHTTP    bool
}

func NewURLValidator(cfg SecurityConfig) *URLValidator {
	allow := make(map[string]bool, len(cfg.OutboundAllowlist))
	for _, h := range cfg.OutboundAllowlist {
		allow[h] = true
	}
	return &URLValidator{
		allowlist:    allow,
		blockPrivate: cfg.BlockPrivateIPRanges,
	}
}

func (v *URLValidator) Validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid URL: %q", rawURL)
	}

	if u.Scheme != "https" {
		if u.Scheme == "http" && v.AllowHTTP {
			// allowed in dev mode
		} else {
			return fmt.Errorf("scheme %q is not allowed, use https", u.Scheme)
		}
	}

	host := u.Hostname()
	if !v.allowlist[host] {
		return fmt.Errorf("host %q is not in the outbound allowlist", host)
	}

	if v.blockPrivate {
		if ip := net.ParseIP(host); ip != nil {
			if isPrivateIP(host) {
				return fmt.Errorf("host %q resolves to a private IP address", host)
			}
		} else {
			ips, err := net.LookupIP(host)
			if err != nil {
				return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
			}
			for _, ip := range ips {
				if isPrivateIP(ip.String()) {
					return fmt.Errorf("host %q resolves to private IP %s", host, ip)
				}
			}
		}
	}

	return nil
}

func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	return false
}
