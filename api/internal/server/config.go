package server

import (
	"encoding/json"
	"net/http"
)

type validateConfigRequest struct {
	ConfigType  string `json:"config_type"`
	YAMLContent string `json:"yaml_content"`
}

func (s *Server) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	if s.configEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "config validation not configured")
		return
	}

	var req validateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.ConfigType == "" {
		writeError(w, http.StatusBadRequest, "config_type is required")
		return
	}
	if req.YAMLContent == "" {
		writeError(w, http.StatusBadRequest, "yaml_content is required")
		return
	}

	result, err := s.configEngine.ValidateConfig(r.Context(), req.ConfigType, req.YAMLContent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		},
	}

	writeJSON(w, http.StatusOK, info)
}
