package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
)

type errorResponse struct {
	Error string      `json:"error"`
	Code  apierr.Code `json:"error_code,omitempty"`
}

// deprecatedOffsetSunsetWindow is the minimum migration period the HTTP API contract §1.2
// requires between deprecating a parameter and removing it.
const deprecatedOffsetSunsetWindow = 6 * 30 * 24 * time.Hour

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Deprecated: use writeErrorCode so the response carries a stable
// error_code alongside the message (Contract Stability: clients branch on
// error_code, not the message string, which may be reworded or translated).
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeErrorCode(w http.ResponseWriter, status int, code apierr.Code, msg string) {
	writeJSON(w, status, errorResponse{Error: msg, Code: code})
}

// paginatedResponse is the additive {"data": [...], "pagination": {...}}
// envelope the HTTP API contract §1.1 specifies for list endpoints.
type paginatedResponse[T any] struct {
	Data       []T            `json:"data"`
	Pagination PaginationMeta `json:"pagination"`
}

func writePaginatedJSON[T any](w http.ResponseWriter, status int, data []T, meta PaginationMeta) {
	if data == nil {
		data = []T{}
	}
	writeJSON(w, status, paginatedResponse[T]{Data: data, Pagination: meta})
}

// setOffsetDeprecationHeaders marks the legacy offset/limit pagination
// parameters as deprecated per the HTTP API contract §1.2 (Deprecation header + Sunset date
// at least 6 months out).
func setOffsetDeprecationHeaders(w http.ResponseWriter) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", time.Now().Add(deprecatedOffsetSunsetWindow).UTC().Format(http.TimeFormat))
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
