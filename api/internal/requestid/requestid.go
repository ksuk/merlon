// Package requestid carries a bounded request identifier across process
// boundaries without trusting arbitrary user input.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type contextKey struct{}

const maxLength = 128

func New() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "generated-unavailable"
	}
	return hex.EncodeToString(b)
}

func Valid(value string) bool {
	if value == "" || len(value) > maxLength || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(contextKey{}).(string); ok && Valid(id) {
		return id
	}
	return ""
}
