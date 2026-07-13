package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewareGeneratesAndPropagatesID(t *testing.T) {
	s := New(":0", Deps{})
	req := httptest.NewRequest(http.MethodGet, "/healthz/live", nil)
	resp := httptest.NewRecorder()
	s.Handler().ServeHTTP(resp, req)
	if got := resp.Header().Get(requestIDHeader); got == "" {
		t.Fatal("expected generated X-Request-ID response header")
	}
}

func TestRequestIDMiddlewareRejectsUnsafeInput(t *testing.T) {
	s := New(":0", Deps{})
	req := httptest.NewRequest(http.MethodGet, "/healthz/live", nil)
	req.Header.Set(requestIDHeader, "bad value\n")
	resp := httptest.NewRecorder()
	s.Handler().ServeHTTP(resp, req)
	if got := resp.Header().Get(requestIDHeader); got == "bad value\n" || got == "" {
		t.Fatalf("expected replacement request ID, got %q", got)
	}
}
