package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) serveSPA(dir string) {
	fileServer := http.FileServer(http.Dir(dir))

	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})

	s.mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) SetUIDir(dir string) {
	if _, err := fs.Stat(os.DirFS(dir), "index.html"); err != nil {
		return
	}
	s.serveSPA(dir)
}
