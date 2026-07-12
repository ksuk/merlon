package logging

import (
	"context"
	"io"
	"log/slog"
)

// maskers maps known PII attribute keys (security.md §4.1) to their masking
// function. Keys not present here are logged unmasked.
var maskers = map[string]func(string) string{
	"customer_name": MaskCustomerName,
	"email":         MaskEmail,
	"phone":         MaskPhone,
	"external_id":   MaskExternalID,
	"ip_address":    MaskIP,
}

// NewLogger returns a JSON slog.Logger that automatically masks known PII
// attribute keys (security.md §4.1) before writing to w. It does not affect
// data stored in the database, and must not be used for audit log writes
// (api/internal/server/audit.go), which require full, unmasked recording.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(MaskingHandler(slog.NewJSONHandler(w, nil)))
}

// MaskingHandler wraps next so that Attrs matching a known PII key
// (security.md §4.1) have their string value masked before being passed
// through. Non-string values and unknown keys are left untouched.
func MaskingHandler(next slog.Handler) slog.Handler {
	return &maskingHandler{next: next}
}

type maskingHandler struct {
	next slog.Handler
}

func (h *maskingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *maskingHandler) Handle(ctx context.Context, record slog.Record) error {
	masked := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(a slog.Attr) bool {
		masked.AddAttrs(maskAttr(a))
		return true
	})
	return h.next.Handle(ctx, masked)
}

func (h *maskingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = maskAttr(a)
	}
	return &maskingHandler{next: h.next.WithAttrs(masked)}
}

func (h *maskingHandler) WithGroup(name string) slog.Handler {
	return &maskingHandler{next: h.next.WithGroup(name)}
}

func maskAttr(a slog.Attr) slog.Attr {
	mask, ok := maskers[a.Key]
	if !ok || a.Value.Kind() != slog.KindString {
		return a
	}
	return slog.String(a.Key, mask(a.Value.String()))
}
