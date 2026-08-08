package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
	// cursorSeparator is a control character that cannot appear in a UUID or
	// an RFC3339Nano timestamp, so it safely delimits cursor fields.
	cursorSeparator = "\x1f"
)

// Cursor holds the sort key (created_at, id) used for keyset pagination.
type Cursor struct {
	CreatedAt time.Time
	ID        string
	Sort      string
	Rank      int
	Filter    string
}

// EncodeCursor converts a Cursor into an opaque base64-encoded string.
func EncodeCursor(c Cursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + cursorSeparator + c.ID
	if c.Sort == "risk" {
		raw = "risk" + cursorSeparator + strconv.Itoa(c.Rank) + cursorSeparator + raw
	}
	if c.Filter != "" {
		raw = "filter" + cursorSeparator + c.Filter + cursorSeparator + raw
	}
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor decodes an opaque string produced by EncodeCursor back into a Cursor.
func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	parts := strings.Split(string(raw), cursorSeparator)
	var timestamp, id, sort string
	filter := ""
	rank := 0
	if len(parts) >= 2 && parts[0] == "filter" {
		filter = parts[1]
		parts = parts[2:]
	}
	switch {
	case len(parts) == 2:
		timestamp, id = parts[0], parts[1]
	case len(parts) == 4 && parts[0] == "risk":
		var rankErr error
		rank, rankErr = strconv.Atoi(parts[1])
		if rankErr != nil || rank < 0 || rank > 4 {
			return Cursor{}, errors.New("invalid risk cursor rank")
		}
		timestamp, id, sort = parts[2], parts[3], "risk"
	default:
		return Cursor{}, errors.New("invalid cursor format")
	}
	if id == "" {
		return Cursor{}, errors.New("invalid cursor format")
	}

	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor timestamp: %w", err)
	}

	return Cursor{CreatedAt: t, ID: id, Sort: sort, Rank: rank, Filter: filter}, nil
}

// PageRequest is a pagination request parsed from query parameters.
type PageRequest struct {
	Limit  int
	Cursor *Cursor
}

// useCursorPagination selects the stable keyset contract unless a caller
// explicitly supplies the deprecated offset parameter. This lets a first
// request use cursor pagination without inventing a sentinel cursor value.
func useCursorPagination(r *http.Request) bool {
	return r.URL.Query().Get("cursor") != "" || r.URL.Query().Get("offset") == ""
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
	return &domain.Cursor{CreatedAt: c.CreatedAt, ID: c.ID, Sort: c.Sort, Rank: c.Rank, Filter: c.Filter}
}

// fromDomainCursor converts a domain.Cursor back to server.Cursor.
func fromDomainCursor(c domain.Cursor) Cursor {
	return Cursor{CreatedAt: c.CreatedAt, ID: c.ID, Sort: c.Sort, Rank: c.Rank, Filter: c.Filter}
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
