// Package apierr defines the stable machine-readable error codes returned
// alongside the human-readable "error" message in API responses
// (api.md Contract Stability: clients must be able to branch on error_code
// without depending on message wording, which may change or be translated).
package apierr

type Code string

const (
	CodeNotFound               Code = "not_found"
	CodeValidationFailed       Code = "validation_failed"
	CodeInvalidStateTransition Code = "invalid_state_transition"
	CodeUnauthorized           Code = "unauthorized"
	CodeForbidden              Code = "forbidden"
	CodeConflict               Code = "conflict"
	CodeRateLimited            Code = "rate_limited"
	CodePayloadTooLarge        Code = "payload_too_large"
	CodeInternal               Code = "internal_error"
	CodeServiceUnavailable     Code = "service_unavailable"
	CodeEngineError            Code = "engine_error"

	// CodeRetentionShortenForbidden is reserved for the WS-9 retention
	// policy feature (rejecting a retention period shortened below the
	// regulatory minimum). No handler emits it yet.
	CodeRetentionShortenForbidden Code = "retention_shorten_forbidden"
)
