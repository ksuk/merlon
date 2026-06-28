package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func newTestServer() *Server {
	return New(":0", Deps{})
}
