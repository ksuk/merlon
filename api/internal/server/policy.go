package server

import (
	"net/http"

	"github.com/ksuk/merlon/api/internal/apierr"
)

// handleListPolicies lists every policy document with its version, digest,
// and whether it came from a file or the in-code default. The UI reads this
// surface so no page has to restate a policy value that would then drift from
// the one the server actually applies.
func (s *Server) handleListPolicies(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": s.policies.Descriptors(),
	})
}

// handleGetPolicy returns one policy document in full. The documents carry
// institutional rules, not customer data, so they are readable wherever the
// rule catalogue is.
func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("policy")
	document, ok := s.policies.Document(name)
	if !ok {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, "unknown policy")
		return
	}
	descriptor, _ := s.policies.Describe(name)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":           descriptor.Name,
		"schema_version": descriptor.SchemaVersion,
		"policy_version": descriptor.PolicyVersion,
		"digest":         descriptor.Digest,
		"source":         descriptor.Source,
		"document":       document,
	})
}
