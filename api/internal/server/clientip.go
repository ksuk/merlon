package server

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
)

type clientIPContextKey struct{}

type clientIPResolver struct {
	trustedProxyCIDRs []netip.Prefix
}

func newClientIPResolver(prefixes []netip.Prefix) clientIPResolver {
	trusted := make([]netip.Prefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		if prefix.IsValid() {
			trusted = append(trusted, prefix.Masked())
		}
	}
	return clientIPResolver{trustedProxyCIDRs: trusted}
}

func (resolver clientIPResolver) resolve(r *http.Request) string {
	peer, ok := parsePeerAddr(r.RemoteAddr)
	if !ok {
		return r.RemoteAddr
	}
	if !resolver.isTrusted(peer) {
		return peer.String()
	}

	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return peer.String()
	}

	hops := make([]string, 0, len(values))
	for _, value := range values {
		hops = append(hops, strings.Split(value, ",")...)
	}

	resolved := peer
	for i := len(hops) - 1; i >= 0; i-- {
		hop, ok := parseForwardedAddr(hops[i])
		if !ok {
			// The trusted side of the chain is malformed. Do not use any
			// caller-controlled value from the header.
			return peer.String()
		}
		resolved = hop
		if !resolver.isTrusted(hop) {
			// Stop at the first untrusted hop. Values farther left can have
			// been supplied by the original caller and must not override it.
			return hop.String()
		}
	}

	// Every forwarded hop was trusted. This can occur for internal clients;
	// use the leftmost address while requiring operators to configure only
	// narrow proxy ranges.
	return resolved.String()
}

func (resolver clientIPResolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range resolver.trustedProxyCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parsePeerAddr(value string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return normalizeAddr(addrPort.Addr()), true
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return normalizeAddr(addr), true
	}
	return netip.Addr{}, false
}

func parseForwardedAddr(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return normalizeAddr(addr), true
	}
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return normalizeAddr(addrPort.Addr()), true
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		if addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")); err == nil {
			return normalizeAddr(addr), true
		}
	}
	return netip.Addr{}, false
}

func normalizeAddr(addr netip.Addr) netip.Addr {
	return addr.Unmap().WithZone("")
}

func (s *Server) clientIPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.clientIPs.resolve(r)
		ctx := context.WithValue(r.Context(), clientIPContextKey{}, ip)
		// Preserve the request pointer so net/http can publish the matched
		// ServeMux pattern back to outer metrics middleware.
		*r = *r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

func extractIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPContextKey{}).(string); ok && ip != "" {
		return ip
	}
	if peer, ok := parsePeerAddr(r.RemoteAddr); ok {
		return peer.String()
	}
	return r.RemoteAddr
}
