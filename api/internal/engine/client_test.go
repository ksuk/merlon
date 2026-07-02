package engine

import "testing"

func TestWithTLSMissingCertificateFailsClosed(t *testing.T) {
	_, err := NewClient("127.0.0.1:1", WithTLS("does-not-exist.pem", "engine.local"))
	if err == nil {
		t.Fatal("expected missing TLS certificate to fail before dialing")
	}
}
