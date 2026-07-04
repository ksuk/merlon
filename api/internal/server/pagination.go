package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/merlon-aml/merlon/api/internal/domain"
)

const (
	defaultPageLimit = 50
	maxPageLimit      = 200
	// cursorSeparator is a control character that cannot appear in a UUID or
	// an RFC3339Nano timestamp, so it safely delimits the two cursor fields.
	cursorSeparator = "\x1f"
)

// Cursor holds the sort key (created_at, id) used for keyset pagination.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// EncodeCursor converts a Cursor into an opaque base64-encoded string.
func EncodeCursor(c Cursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + cursorSeparator + c.ID
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor decodes an opaque string produced by EncodeCursor back into a Cursor.
func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	parts := strings.SplitN(string(raw), cursorSeparator, 2)
	if len(parts) != 2 || parts[1] == "" {
		return Cursor{}, errors.New("invalid cursor format")
	}

	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor timestamp: %w", err)
	}

	return Cursor{CreatedAt: t, ID: parts[1]}, nil
}

// PageRequest is a pagination request parsed from query parameters.
type PageRequest struct {
	Limit  int
	Cursor *Cursor
}

// ParsePageRequest interprets the limit/cursor query parameters of an http.Request.
// An unspecified or non-positive limit defaults to 50; limits over 200 are
// clamped to 200. An invalid cursor value returns an error.
func ParsePageRequest(r *http.Request) (PageRequest, error) {
	limit := defaultPageLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	pr := PageRequest{Limit: limit}

	if raw := r.URL.Query().Get("cursor"); raw != "" {
		c, err := DecodeCursor(raw)
		if err != nil {
			return PageRequest{}, err
		}
		pr.Cursor = &c
	}

	return pr, nil
}

// PaginationMeta is the "pagination" field of a paginated list response.
type PaginationMeta struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// toDomainCursor converts a server.Cursor to the domain package's equivalent,
// which repository interfaces use so domain does not depend on server.
func toDomainCursor(c *Cursor) *domain.Cursor {
	if c == nil {
		return nil
	}
	return &domain.Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
}

// fromDomainCursor converts a domain.Cursor back to server.Cursor.
func fromDomainCursor(c domain.Cursor) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
}

// BuildPaginationMeta trims a slice fetched with limit+1 rows down to at most
// limit items and reports whether a further page exists. items must have been
// fetched with a limit of limit+1 (a one-row lookahead used to detect has_more).
func BuildPaginationMeta[T any](items []T, limit int, cursorOf func(T) Cursor) ([]T, PaginationMeta) {
	if len(items) <= limit {
		return items, PaginationMeta{HasMore: false}
	}

	trimmed := items[:limit]
	next := cursorOf(trimmed[len(trimmed)-1])
	return trimmed, PaginationMeta{NextCursor: EncodeCursor(next), HasMore: true}
}
