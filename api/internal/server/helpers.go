package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ksuk/merlon/api/internal/apierr"
)

// errorResponse is the ERR-01 envelope. Error and Code keep their meaning and
// position; the rest is additive.
//
// RequestID existed only as a response header, so an operator reading a
// screenshot of a failure had no correlation identifier to quote and support
// had nothing to search the log with (#85).
type errorResponse struct {
	Error string      `json:"error"`
	Code  apierr.Code `json:"error_code,omitempty"`
	// RequestID correlates this response with the server log. It is opaque and
	// carries no diagnostic content of its own.
	RequestID string `json:"request_id,omitempty"`
	// Retryable is a property of the failure class, not of the operation: a
	// validation error will fail again unchanged, a dependency outage may not.
	// Whether it is *safe* to retry additionally depends on whether the request
	// mutated anything, which only the caller knows.
	Retryable bool `json:"retryable"`
}

// deprecatedOffsetSunsetWindow is the minimum migration period the HTTP API contract §1.2
// requires between deprecating a parameter and removing it.
const deprecatedOffsetSunsetWindow = 6 * 30 * 24 * time.Hour

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErrorCode(w http.ResponseWriter, status int, code apierr.Code, msg string) {
	writeJSON(w, status, newErrorResponse(w, code, msg))
}

// newErrorResponse reads the correlation identifier from the response header
// the request-ID middleware already set, so every error body carries it without
// threading the request through six hundred call sites.
func newErrorResponse(w http.ResponseWriter, code apierr.Code, msg string) errorResponse {
	return errorResponse{
		Error:     msg,
		Code:      code,
		RequestID: w.Header().Get(requestIDHeader),
		Retryable: apierr.Retryable(code),
	}
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
