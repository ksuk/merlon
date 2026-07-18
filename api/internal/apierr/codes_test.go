package apierr

import "testing"

func TestCodesAreStableStrings(t *testing.T) {
	cases := map[Code]string{
		CodeNotFound:                  "not_found",
		CodeValidationFailed:          "validation_failed",
		CodeInvalidStateTransition:    "invalid_state_transition",
		CodeUnauthorized:              "unauthorized",
		CodeForbidden:                 "forbidden",
		CodeConflict:                  "conflict",
		CodeRateLimited:               "rate_limited",
		CodePayloadTooLarge:           "payload_too_large",
		CodeInternal:                  "internal_error",
		CodeServiceUnavailable:        "service_unavailable",
		CodeEngineError:               "engine_error",
		CodeRetentionShortenForbidden: "retention_shorten_forbidden",
	}

	for code, want := range cases {
		if string(code) != want {
			t.Errorf("code %v: got %q, want %q", code, string(code), want)
		}
	}
}
