package server

import (
	"net/http"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

func (s *Server) handleDiscoverBacktestRules(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule repository is not configured")
		return
	}
	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	rules, err := s.rules.List(r.Context(), domain.RuleTypeTMScenario, false, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeWave3Error(w, err)
		return
	}
	page, meta := BuildPaginationMeta(rules, pageReq.Limit, func(rule domain.RuleDefinition) Cursor { return Cursor{CreatedAt: rule.CreatedAt, ID: rule.ID} })
	writePaginatedJSON(w, http.StatusOK, page, meta)
}
