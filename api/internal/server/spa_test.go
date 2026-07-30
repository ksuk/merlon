package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAServesIndexForUnknownRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>merlon</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer()
	s.SetUIDir(dir)

	req := httptest.NewRequest("GET", "/some/route", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "<html>merlon</html>" {
		t.Fatalf("expected index.html content, got %q", body)
	}
}

func TestSPAServesStaticFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>merlon</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "main.js"), []byte("console.log('ok')"), 0644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer()
	s.SetUIDir(dir)

	req := httptest.NewRequest("GET", "/assets/main.js", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" {
		t.Fatal("expected Cache-Control header for assets")
	}
	if body := w.Body.String(); body != "console.log('ok')" {
		t.Fatalf("expected static file content, got %q", body)
	}
}

func TestSPAInvalidPathsDoNotEscapeUIRoot(t *testing.T) {
	parentDir := t.TempDir()
	uiDir := filepath.Join(parentDir, "ui")
	if err := os.Mkdir(uiDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "index.html"), []byte("<html>merlon</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	const outsideContent = "outside-ui-root"
	if err := os.WriteFile(filepath.Join(parentDir, "secret.txt"), []byte(outsideContent), 0644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer()
	s.SetUIDir(uiDir)

	for _, requestPath := range []string{
		"/%2e%2e/secret.txt",
		"/assets/%2e%2e/%2e%2e/secret.txt",
		"/..%5csecret.txt",
		"/assets//secret.txt",
	} {
		t.Run(requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, requestPath, nil)
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				t.Fatalf("expected invalid path %q to be rejected", requestPath)
			}
			if strings.Contains(w.Body.String(), outsideContent) {
				t.Fatalf("request %q served a file outside the UI root", requestPath)
			}
		})
	}
}

func TestSPASkipsWhenNoIndexHTML(t *testing.T) {
	dir := t.TempDir()

	s := newTestServer()
	s.SetUIDir(dir)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	// Without index.html, SetUIDir should not register routes
	// The mux should return 404
	if w.Code == http.StatusOK {
		t.Fatal("expected non-200 when UI dir has no index.html")
	}
}

func TestSPAIsPublicButAPIRemainsProtectedWhenAuthIsEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>merlon</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "main.js"), []byte("console.log('ok')"), 0644); err != nil {
		t.Fatal(err)
	}

	s := testServerWithAuth()
	s.SetUIDir(dir)

	for _, requestPath := range []string{
		"/",
		"/login",
		"/setup",
		"/customers/customer-1",
		"/assets/main.js",
	} {
		t.Run("public "+requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, requestPath, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d, body: %s", requestPath, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}

	for _, requestPath := range []string{
		"/api/v1/customers",
		"/api/v1/does-not-exist",
		"/metrics",
	} {
		t.Run("protected "+requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, requestPath, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s status = %d, want %d, body: %s", requestPath, rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /login status = %d, want %d, body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestSPAReservedServerPathsNeverFallBackToIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>merlon</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer()
	s.SetUIDir(dir)

	for _, requestPath := range []string{
		"/api",
		"/api/v1/does-not-exist",
		"/healthz/does-not-exist",
		"/metrics/does-not-exist",
	} {
		t.Run(requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, requestPath, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want %d, body: %s", requestPath, rec.Code, http.StatusNotFound, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "merlon") {
				t.Fatalf("GET %s unexpectedly served index.html", requestPath)
			}
		})
	}
}

func newTestServer() *Server {
	return New(":0", Deps{})
}
