package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

// ruleConfigType maps a domain.RuleType to the config_type string
// engine.ConfigEngine.ValidateConfig expects (engine/crates/merlon-engine/src/grpc/config_service.rs).
// "country_risk" validation is added to the engine in a later task; until
// then ValidateConfig will reject it, which is the conservative default.
func ruleConfigType(t domain.RuleType) string {
	switch t {
	case domain.RuleTypeTMScenario:
		return "tm_scenarios"
	case domain.RuleTypeCDDWeight:
		return "cdd_weights"
	case domain.RuleTypeScreeningConfig:
		return "screening_lists"
	case domain.RuleTypeCountryRisk:
		return "country_risk"
	default:
		return ""
	}
}

// validateRuleDefinition delegates schema/semantic validation to the Rust
// engine's ConfigService (rule-schema.md §5: rule definitions must be
// JSON-Schema validated; no in-process eval of rule content). It returns a
// *ruleValidationError (carrying the engine's structured errors) when
// validation ran and found problems.
func (s *Server) validateRuleDefinition(r *http.Request, ruleType domain.RuleType, definition json.RawMessage) error {
	if s.configEngine == nil {
		return nil
	}
	result, err := s.configEngine.ValidateConfig(r.Context(), ruleConfigType(ruleType), string(definition))
	if err != nil {
		return err
	}
	if !result.Valid {
		return &ruleValidationError{result: result}
	}
	return nil
}

// ruleValidationError carries the engine's structured validation errors so
// the handler can return them as the response body (400).
type ruleValidationError struct {
	result any
}

func (e *ruleValidationError) Error() string { return "rule definition failed validation" }

type createRuleRequest struct {
	Type        domain.RuleType `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Definition  json.RawMessage `json:"definition"`
	IsActive    bool            `json:"is_active"`
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeError(w, http.StatusServiceUnavailable, "rule management not configured")
		return
	}

	ruleType := domain.RuleType(r.URL.Query().Get("type"))
	activeOnly := r.URL.Query().Get("is_active") == "true"

	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, err := s.rules.List(r.Context(), ruleType, activeOnly, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	trimmed, meta := BuildPaginationMeta(items, pageReq.Limit, func(rd domain.RuleDefinition) Cursor {
		return Cursor{CreatedAt: rd.CreatedAt, ID: rd.ID}
	})

	writePaginatedJSON(w, http.StatusOK, trimmed, meta)
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeError(w, http.StatusServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")

	var (
		rd  *domain.RuleDefinition
		err error
	)
	if raw := r.URL.Query().Get("version"); raw != "" {
		version, convErr := strconv.Atoi(raw)
		if convErr != nil {
			writeError(w, http.StatusBadRequest, "version must be an integer")
			return
		}
		rd, err = s.rules.GetVersion(r.Context(), id, version)
	} else {
		rd, err = s.rules.Get(r.Context(), id)
	}

	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rd)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeError(w, http.StatusServiceUnavailable, "rule management not configured")
		return
	}

	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Type == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "type and name are required")
		return
	}
	if len(req.Definition) == 0 {
		writeError(w, http.StatusBadRequest, "definition is required")
		return
	}

	if err := s.validateRuleDefinition(r, req.Type, req.Definition); err != nil {
		s.writeRuleValidationError(w, err)
		return
	}

	now := time.Now()
	rd := &domain.RuleDefinition{
		ID:          generateID(),
		Type:        req.Type,
		Name:        req.Name,
		Description: req.Description,
		Definition:  req.Definition,
		IsActive:    req.IsActive,
		CreatedBy:   resolveAuditUserID(r),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.rules.Create(r.Context(), rd); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, rd)
}

type updateRuleRequest struct {
	Description string          `json:"description,omitempty"`
	Definition  json.RawMessage `json:"definition"`
	IsActive    bool            `json:"is_active"`
}

// handleUpdateRule appends a new version rather than overwriting the
// existing row (Auditability First) and records the before/after Definition
// diff on the audit log entry (ALD-003).
func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeError(w, http.StatusServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")
	existing, err := s.rules.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req updateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Definition) == 0 {
		writeError(w, http.StatusBadRequest, "definition is required")
		return
	}

	if err := s.validateRuleDefinition(r, existing.Type, req.Definition); err != nil {
		s.writeRuleValidationError(w, err)
		return
	}

	description := req.Description
	if description == "" {
		description = existing.Description
	}

	now := time.Now()
	updated := &domain.RuleDefinition{
		ID:          generateID(),
		Type:        existing.Type,
		Name:        existing.Name,
		Description: description,
		Definition:  req.Definition,
		IsActive:    req.IsActive,
		CreatedBy:   resolveAuditUserID(r),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.rules.CreateNewVersion(r.Context(), updated); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if diff, err := diffRuleDefinitions(existing.Definition, updated.Definition); err == nil {
		setAuditDetail(r, "diff", diff)
	}

	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleActivateRule(w http.ResponseWriter, r *http.Request) {
	s.setRuleActive(w, r, true)
}

func (s *Server) handleDeactivateRule(w http.ResponseWriter, r *http.Request) {
	s.setRuleActive(w, r, false)
}

func (s *Server) setRuleActive(w http.ResponseWriter, r *http.Request, active bool) {
	if s.rules == nil {
		writeError(w, http.StatusServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")
	if err := s.rules.SetActive(r.Context(), id, active); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rd, err := s.rules.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rd)
}

func (s *Server) handleExportRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeError(w, http.StatusServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")
	rd, err := s.rules.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeError(w, http.StatusNotFound, nf.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeExportedRule(w, r, exportedRule{
		Type:        rd.Type,
		Name:        rd.Name,
		Description: rd.Description,
		Definition:  rd.Definition,
	})
}

func (s *Server) writeRuleValidationError(w http.ResponseWriter, err error) {
	var ve *ruleValidationError
	if errors.As(err, &ve) {
		writeJSON(w, http.StatusBadRequest, ve.result)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// exportedRule is the interchange format for GET .../export and POST
// .../import (api.md §1.4): a rule's content without the DB-managed
// id/version/is_active bookkeeping fields, so re-importing an export
// produces a semantically equivalent rule regardless of storage history.
type exportedRule struct {
	Type        domain.RuleType `json:"type" yaml:"type"`
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Definition  json.RawMessage `json:"definition" yaml:"definition"`
}

// writeExportedRule writes er as JSON. A later task adds YAML output via the
// ?format=yaml query parameter.
func writeExportedRule(w http.ResponseWriter, _ *http.Request, er exportedRule) {
	writeJSON(w, http.StatusOK, er)
}

// diffRuleDefinitions produces a flat, top-level key diff between two rule
// Definition documents (ALD-003). A full structural/nested diff is not
// required by rule-schema.md; a per-key before/after comparison is
// sufficient for audit purposes.
func diffRuleDefinitions(before, after json.RawMessage) (string, error) {
	var beforeMap, afterMap map[string]any
	if err := json.Unmarshal(before, &beforeMap); err != nil {
		return "", err
	}
	if err := json.Unmarshal(after, &afterMap); err != nil {
		return "", err
	}

	type change struct {
		Before any `json:"before,omitempty"`
		After  any `json:"after,omitempty"`
	}
	changes := make(map[string]change)

	for k, av := range afterMap {
		bv, existed := beforeMap[k]
		if !existed {
			changes[k] = change{After: av}
			continue
		}
		if !reflect.DeepEqual(bv, av) {
			changes[k] = change{Before: bv, After: av}
		}
	}
	for k, bv := range beforeMap {
		if _, existed := afterMap[k]; !existed {
			changes[k] = change{Before: bv}
		}
	}

	out, err := json.Marshal(changes)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
