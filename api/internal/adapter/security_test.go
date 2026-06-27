package adapter

import (
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"192.168.0.1", true},
		{"192.168.1.100", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"169.254.1.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isPrivateIP(tt.ip); got != tt.private {
				t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestURLValidatorEmptyAllowlist(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{},
		BlockPrivateIPRanges: true,
	})
	if err := v.Validate("https://core.example.com/api"); err == nil {
		t.Error("expected error for URL with empty allowlist")
	}
}

func TestURLValidatorAllowlistedHost(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"core.example.com"},
		BlockPrivateIPRanges: false,
	})
	if err := v.Validate("https://core.example.com/api/v1/customers"); err != nil {
		t.Errorf("unexpected error for allowlisted host: %v", err)
	}
}

func TestURLValidatorNonAllowlistedHost(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"core.example.com"},
		BlockPrivateIPRanges: false,
	})
	if err := v.Validate("https://evil.example.com/api"); err == nil {
		t.Error("expected error for non-allowlisted host")
	}
}

func TestURLValidatorHostWithPort(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"core.example.com"},
		BlockPrivateIPRanges: false,
	})
	if err := v.Validate("https://core.example.com:8443/api"); err != nil {
		t.Errorf("unexpected error for allowlisted host with port: %v", err)
	}
}

func TestURLValidatorMalformedURL(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"example.com"},
		BlockPrivateIPRanges: true,
	})
	if err := v.Validate("://not-a-url"); err == nil {
		t.Error("expected error for malformed URL")
	}
}

func TestURLValidatorHTTPRejectedByDefault(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"example.com"},
		BlockPrivateIPRanges: false,
	})
	if err := v.Validate("http://example.com/api"); err == nil {
		t.Error("expected error for http scheme")
	}
}

func TestURLValidatorHTTPAllowedInDevMode(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"example.com"},
		BlockPrivateIPRanges: false,
	})
	v.AllowHTTP = true
	if err := v.Validate("http://example.com/api"); err != nil {
		t.Errorf("unexpected error for http in dev mode: %v", err)
	}
}

func TestURLValidatorPrivateIPBlocked(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"127.0.0.1"},
		BlockPrivateIPRanges: true,
	})
	if err := v.Validate("https://127.0.0.1/api"); err == nil {
		t.Error("expected error for private IP even if allowlisted")
	}
}

func TestURLValidatorPrivateIPAllowedWhenDisabled(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"127.0.0.1"},
		BlockPrivateIPRanges: false,
	})
	v.AllowHTTP = true
	if err := v.Validate("http://127.0.0.1/api"); err != nil {
		t.Errorf("unexpected error when private IP blocking disabled: %v", err)
	}
}

func TestURLValidatorLocalhostBlocked(t *testing.T) {
	v := NewURLValidator(SecurityConfig{
		OutboundAllowlist:    []string{"localhost"},
		BlockPrivateIPRanges: true,
	})
	err := v.Validate("https://localhost/api")
	if err == nil {
		t.Error("expected error for localhost (resolves to 127.0.0.1)")
	}
}
