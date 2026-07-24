package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIPResolverIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	resolver := newClientIPResolver([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.10")

	if got := resolver.resolve(req); got != "203.0.113.10" {
		t.Errorf("resolve() = %q, want direct peer", got)
	}
}

func TestClientIPResolverUsesFirstUntrustedHopFromRight(t *testing.T) {
	resolver := newClientIPResolver([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.3:443"
	req.Header.Set("X-Forwarded-For", "not-an-ip, 198.51.100.20, 10.0.0.2")

	if got := resolver.resolve(req); got != "198.51.100.20" {
		t.Errorf("resolve() = %q, want observed client hop", got)
	}
}

func TestClientIPResolverFallsBackWhenTrustedSideIsMalformed(t *testing.T) {
	resolver := newClientIPResolver([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.3:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.20, not-an-ip")

	if got := resolver.resolve(req); got != "10.0.0.3" {
		t.Errorf("resolve() = %q, want trusted peer fallback", got)
	}
}

func TestClientIPResolverIgnoresUnsupportedForwardingHeaders(t *testing.T) {
	resolver := newClientIPResolver([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.3:443"
	req.Header.Set("Forwarded", "for=198.51.100.20")
	req.Header.Set("X-Real-IP", "198.51.100.20")

	if got := resolver.resolve(req); got != "10.0.0.3" {
		t.Errorf("resolve() = %q, want trusted peer without X-Forwarded-For", got)
	}
}

func TestClientIPResolverSupportsIPv6AndRepeatedHeaders(t *testing.T) {
	resolver := newClientIPResolver([]netip.Prefix{
		netip.MustParsePrefix("2001:db8:ffff::/48"),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8:ffff::2]:443"
	req.Header.Add("X-Forwarded-For", "192.0.2.10")
	req.Header.Add("X-Forwarded-For", "2001:db8:ffff::1")

	if got := resolver.resolve(req); got != "192.0.2.10" {
		t.Errorf("resolve() = %q, want IPv4 client behind IPv6 proxies", got)
	}
}

func TestClientIPMiddlewareStoresResolvedAddress(t *testing.T) {
	s := &Server{
		clientIPs: newClientIPResolver([]netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/24"),
		}),
	}
	handler := s.clientIPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := extractIP(r); got != "198.51.100.25" {
			t.Errorf("extractIP() = %q, want resolved client", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.25")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
