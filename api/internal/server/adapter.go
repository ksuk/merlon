package server

import (
	"net/http"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
)

func (s *Server) handleAdapterDryRun(w http.ResponseWriter, r *http.Request) {
	role, ok := auth.RoleFromContext(r.Context())
	if !ok || role != "admin" {
		writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "admin role required")
		return
	}
	if s.adapter == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "adapter is not configured")
		return
	}
	result, err := s.adapter.DryRun(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, apierr.CodeEngineError, "adapter dry-run failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
