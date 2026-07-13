package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ksuk/merlon/api/internal/apierr"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ksuk/merlon/api/internal/domain"
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
// engine's ConfigService (the rule schema §5: rule definitions must be
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
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
		return
	}

	ruleType := domain.RuleType(r.URL.Query().Get("type"))
	activeOnly := r.URL.Query().Get("is_active") == "true"

	pageReq, err := ParsePageRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	items, err := s.rules.List(r.Context(), ruleType, activeOnly, pageReq.Limit+1, toDomainCursor(pageReq.Cursor))
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	trimmed, meta := BuildPaginationMeta(items, pageReq.Limit, func(rd domain.RuleDefinition) Cursor {
		return Cursor{CreatedAt: rd.CreatedAt, ID: rd.ID}
	})

	writePaginatedJSON(w, http.StatusOK, trimmed, meta)
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
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
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "version must be an integer")
			return
		}
		rd, err = s.rules.GetVersion(r.Context(), id, version)
	} else {
		rd, err = s.rules.Get(r.Context(), id)
	}

	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rd)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
		return
	}

	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if req.Type == "" || req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "type and name are required")
		return
	}
	if req.IsActive {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "new rules require independent activation")
		return
	}
	if len(req.Definition) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "definition is required")
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
		IsActive:    false,
		CreatedBy:   resolveAuditUserID(r),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.rules.Create(r.Context(), rd); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
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
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")
	existing, err := s.rules.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	var req updateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if len(req.Definition) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "definition is required")
		return
	}
	if req.IsActive {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "new rule versions require independent activation")
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
		IsActive:    false,
		CreatedBy:   resolveAuditUserID(r),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.rules.CreateNewVersion(r.Context(), updated); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
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
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")
	actor := resolveAuditUserID(r)
	if activeRule, err := s.rules.GetActive(r.Context(), id); err == nil {
		if activeRule.CreatedBy != "" && activeRule.CreatedBy == actor {
			writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "the rule author cannot change its active state")
			return
		}
	} else {
		var nf *domain.ErrNotFound
		if !errors.As(err, &nf) {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		if active {
			latest, latestErr := s.rules.Get(r.Context(), id)
			if latestErr != nil {
				if errors.As(latestErr, &nf) {
					writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, latestErr.Error())
					return
				}
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, latestErr.Error())
				return
			}
			if latest.CreatedBy != "" && latest.CreatedBy == actor {
				writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "the rule author cannot change its active state")
				return
			}
		}
	}
	if err := s.rules.SetActive(r.Context(), id, active); err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	rd, err := s.rules.Get(r.Context(), id)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rd)
}

func (s *Server) handleExportRule(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
		return
	}

	id := r.PathValue("id")
	rd, err := s.rules.Get(r.Context(), id)
	if err != nil {
		var nf *domain.ErrNotFound
		if errors.As(err, &nf) {
			writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, nf.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
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
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}

// exportedRule is the interchange format for GET .../export and POST
// .../import (the HTTP API contract §1.4): a rule's content without the DB-managed
// id/version/is_active bookkeeping fields, so re-importing an export
// produces a semantically equivalent rule regardless of storage history.
type exportedRule struct {
	Type        domain.RuleType `json:"type" yaml:"type"`
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Definition  json.RawMessage `json:"definition" yaml:"definition"`
}

// writeExportedRule writes er as JSON, or as YAML when the request asks for
// ?format=yaml (the HTTP API contract §1.4).
func writeExportedRule(w http.ResponseWriter, r *http.Request, er exportedRule) {
	if r.URL.Query().Get("format") != "yaml" {
		writeJSON(w, http.StatusOK, er)
		return
	}

	var definition any
	if err := json.Unmarshal(er.Definition, &definition); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	out := struct {
		Type        domain.RuleType `yaml:"type"`
		Name        string          `yaml:"name"`
		Description string          `yaml:"description,omitempty"`
		Definition  any             `yaml:"definition"`
	}{Type: er.Type, Name: er.Name, Description: er.Description, Definition: definition}

	raw, err := yaml.Marshal(out)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(raw)
}

// importRuleItem is the interchange shape POST /api/v1/rules/import accepts,
// one per rule in the batch (JSON array, or a YAML sequence when
// Content-Type mentions yaml). Definition is decoded generically (rather
// than as json.RawMessage) because gopkg.in/yaml.v3 has no special handling
// for it; both JSON and YAML paths converge here and are re-marshaled to
// JSON for storage.
type importRuleItem struct {
	Type        domain.RuleType `json:"type" yaml:"type"`
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Definition  any             `json:"definition" yaml:"definition"`
}

func decodeImportItems(r *http.Request) ([]importRuleItem, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var items []importRuleItem
	if strings.Contains(r.Header.Get("Content-Type"), "yaml") {
		err = yaml.Unmarshal(body, &items)
	} else {
		err = json.Unmarshal(body, &items)
	}
	if err != nil {
		return nil, err
	}
	return items, nil
}

// handleImportRules bulk-creates rules from a JSON or YAML array (CNT-001/002).
// It validates every item (existence checks + engine.ConfigEngine schema
// validation, CNT-003) before creating any of them: one invalid item rejects
// the whole batch (the HTTP API contract §1.4 "1件でも失敗したら全体を拒否"). Note: the
// RuleRepository interface has no multi-row transactional Create, so this
// atomicity is enforced by validating everything up front rather than by a
// DB transaction — a failure in the create loop itself (e.g. a name racing
// in concurrently) can still leave a partial batch on Postgres.
func (s *Server) handleImportRules(w http.ResponseWriter, r *http.Request) {
	if s.rules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "rule management not configured")
		return
	}

	items, err := decodeImportItems(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}
	if len(items) == 0 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "at least one rule is required")
		return
	}

	now := time.Now()
	userID := resolveAuditUserID(r)
	seenNames := make(map[string]bool, len(items))
	prepared := make([]*domain.RuleDefinition, 0, len(items))

	for i, item := range items {
		if item.Type == "" || item.Name == "" {
			writeErrorCode(w, http.StatusConflict, apierr.CodeValidationFailed, fmt.Sprintf("item %d: type and name are required", i))
			return
		}
		if seenNames[item.Name] {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, fmt.Sprintf("item %d: duplicate rule name %q in import batch", i, item.Name))
			return
		}
		seenNames[item.Name] = true

		if _, err := s.rules.Get(r.Context(), item.Name); err == nil {
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, fmt.Sprintf("item %d: rule %q already exists", i, item.Name))
			return
		} else {
			var nf *domain.ErrNotFound
			if !errors.As(err, &nf) {
				writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
				return
			}
		}

		definition, err := json.Marshal(item.Definition)
		if err != nil {
			writeErrorCode(w, http.StatusConflict, apierr.CodeValidationFailed, fmt.Sprintf("item %d: invalid definition: %v", i, err))
			return
		}

		if verr := s.validateRuleDefinition(r, item.Type, definition); verr != nil {
			var ve *ruleValidationError
			if errors.As(verr, &ve) {
				writeJSON(w, http.StatusConflict, map[string]any{"item": i, "errors": ve.result})
				return
			}
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, verr.Error())
			return
		}

		prepared = append(prepared, &domain.RuleDefinition{
			ID:          generateID(),
			Type:        item.Type,
			Name:        item.Name,
			Description: item.Description,
			Definition:  definition,
			IsActive:    false,
			CreatedBy:   userID,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	created := make([]*domain.RuleDefinition, 0, len(prepared))
	for _, rd := range prepared {
		if err := s.rules.Create(r.Context(), rd); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		created = append(created, rd)
	}

	writeJSON(w, http.StatusCreated, created)
}

// diffRuleDefinitions produces a flat, top-level key diff between two rule
// Definition documents (ALD-003). A full structural/nested diff is not
// required by the rule schema; a per-key before/after comparison is
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
