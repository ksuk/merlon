package server

import "net/http"

type Server struct {
	mux  *http.ServeMux
	addr string
}

func New(addr string) *Server {
	s := &Server{
		mux:  http.NewServeMux(),
		addr: addr,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.mux)
}
