// Package apierr defines the stable machine-readable error codes returned
// alongside the human-readable "error" message in API responses
// (the HTTP API contract Contract Stability: clients must be able to branch on error_code
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

	// CodeRetentionShortenForbidden is reserved for legacy/custom deployments
	// that configure a minimum retention period. No handler emits it yet.
	CodeRetentionShortenForbidden Code = "retention_shorten_forbidden"
)

// Retryable reports whether repeating an identical request could plausibly
// succeed.
//
// This is a property of the failure class alone. A validation error will fail
// again unchanged; a dependency outage or a rate limit may not. Whether it is
// *safe* to retry is a separate question that depends on whether the request
// mutated anything, and only the caller knows that (ERR-01).
func Retryable(code Code) bool {
	switch code {
	case CodeServiceUnavailable, CodeRateLimited, CodeInternal, CodeEngineError:
		return true
	default:
		return false
	}
}
