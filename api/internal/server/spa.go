package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveSPA(uiFS fs.FS) {
	fileServer := http.FileServerFS(uiFS)

	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if isReservedServerPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		name, ok := spaFileName(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if info, err := fs.Stat(uiFS, name); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFileFS(w, r, uiFS, "index.html")
	})

	s.mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, ok := spaFileName(r.URL.Path); !ok {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// isReservedServerPath prevents a typo or unknown endpoint under a server
// namespace from being answered with the SPA's index.html. Besides producing
// the correct 404 for authenticated callers, authMiddleware uses the same
// predicate so these paths can never inherit the SPA's public-shell exemption.
func isReservedServerPath(urlPath string) bool {
	return urlPath == "/api" ||
		strings.HasPrefix(urlPath, "/api/") ||
		urlPath == "/healthz" ||
		strings.HasPrefix(urlPath, "/healthz/") ||
		urlPath == "/metrics" ||
		strings.HasPrefix(urlPath, "/metrics/")
}

func spaFileName(urlPath string) (string, bool) {
	if urlPath == "/" {
		return ".", true
	}
	if !strings.HasPrefix(urlPath, "/") {
		return "", false
	}

	name := strings.TrimPrefix(urlPath, "/")
	name = strings.TrimSuffix(name, "/")
	if !fs.ValidPath(name) || strings.ContainsRune(name, '\\') {
		return "", false
	}

	localName, err := filepath.Localize(name)
	if err != nil || !filepath.IsLocal(localName) {
		return "", false
	}
	return name, true
}

func (s *Server) SetUIDir(dir string) {
	uiFS := os.DirFS(dir)
	info, err := fs.Stat(uiFS, "index.html")
	if err != nil || info.IsDir() {
		return
	}
	s.serveSPA(uiFS)
}
