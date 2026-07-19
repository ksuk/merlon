package server

import (
	"encoding/json"
	"github.com/ksuk/merlon/api/internal/apierr"
	"net/http"
)

type validateConfigRequest struct {
	ConfigType  string `json:"config_type"`
	YAMLContent string `json:"yaml_content"`
}

func (s *Server) handleConfigDigests(w http.ResponseWriter, _ *http.Request) {
	digests := make(map[string]string, len(s.configDigests))
	for name, digest := range s.configDigests {
		digests[name] = digest
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config_digests": digests,
		"base_currency":  s.tmBaseCurrency,
	})
}

func (s *Server) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	if s.configEngine == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "config validation not configured")
		return
	}

	var req validateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, err.Error())
		return
	}

	if req.ConfigType == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "config_type is required")
		return
	}
	if req.YAMLContent == "" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "yaml_content is required")
		return
	}
	if len(req.YAMLContent) > 512*1024 {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "yaml_content too large (max 512KB)")
		return
	}

	result, err := s.configEngine.ValidateConfig(r.Context(), req.ConfigType, req.YAMLContent)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, _ *http.Request) {
	info := map[string]any{
		"version":    "1.0.0",
		"components": []string{"api", "engine", "database"},
		"endpoints":  36,
		"features": map[string]bool{
			"auth":       s.apikeys != nil,
			"audit":      s.audit != nil,
			"cases":      s.cases != nil,
			"webhooks":   s.webhooks != nil,
			"rate_limit": s.limiter != nil,
			"scoring":    s.scoring != nil,
			"monitoring": s.monitoring != nil,
			"screening":  s.screening != nil,
			"backtest":   s.backtest != nil,
			"config":     s.configEngine != nil,
			"demo_data":  s.demoDataEnabled,
		},
	}

	writeJSON(w, http.StatusOK, info)
}
