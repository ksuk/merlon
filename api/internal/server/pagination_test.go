package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

// decodeListResponse decodes the additive {"data", "pagination"} envelope
// that list endpoints return (Task 2), used by resource-specific list tests.
func decodeListResponse[T any](t *testing.T, body io.Reader) ([]T, PaginationMeta) {
	t.Helper()
	var resp struct {
		Data       []T            `json:"data"`
		Pagination PaginationMeta `json:"pagination"`
	}
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return resp.Data, resp.Pagination
}

func TestEncodeDecodeCursor_RoundTrip(t *testing.T) {
	c := Cursor{CreatedAt: time.Date(2026, 7, 1, 12, 30, 0, 123456789, time.UTC), ID: "abc-123"}

	encoded := EncodeCursor(c)
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}

	if !decoded.CreatedAt.Equal(c.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", decoded.CreatedAt, c.CreatedAt)
	}
	if decoded.ID != c.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, c.ID)
	}
}

func TestDecodeCursor_InvalidBase64(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64 cursor")
	}
}

func TestParsePageRequest(t *testing.T) {
	validCursor := EncodeCursor(Cursor{CreatedAt: time.Now(), ID: "x"})

	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantCursor bool
		wantErr    bool
	}{
		{name: "limit unspecified defaults to 50", query: "", wantLimit: 50},
		{name: "limit over max clamps to 200", query: "limit=500", wantLimit: 200},
		{name: "limit zero defaults to 50", query: "limit=0", wantLimit: 50},
		{name: "invalid cursor errors", query: "cursor=not-valid-base64!!!", wantErr: true},
		{name: "cursor omitted is nil", query: "limit=10", wantLimit: 10, wantCursor: false},
		{name: "valid cursor is parsed", query: "cursor=" + validCursor, wantLimit: 50, wantCursor: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?"+tt.query, nil)
			pr, err := ParsePageRequest(req)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pr.Limit != tt.wantLimit {
				t.Errorf("Limit = %d, want %d", pr.Limit, tt.wantLimit)
			}
			if tt.wantCursor && pr.Cursor == nil {
				t.Error("expected non-nil cursor")
			}
			if !tt.wantCursor && pr.Cursor != nil {
				t.Error("expected nil cursor")
			}
		})
	}
}

func TestBuildPaginationMeta(t *testing.T) {
	mk := func(n int) []Cursor {
		items := make([]Cursor, n)
		base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for i := range items {
			items[i] = Cursor{CreatedAt: base.Add(time.Duration(i) * time.Minute), ID: fmt.Sprintf("id-%d", i)}
		}
		return items
	}
	cursorOf := func(c Cursor) Cursor { return c }

	t.Run("count less than limit has no more pages", func(t *testing.T) {
		items := mk(2)
		got, meta := BuildPaginationMeta(items, 5, cursorOf)
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
		if meta.HasMore {
			t.Error("HasMore = true, want false")
		}
		if meta.NextCursor != "" {
			t.Error("NextCursor should be empty")
		}
	})

	t.Run("count equal to limit plus one has more pages", func(t *testing.T) {
		items := mk(6)
		got, meta := BuildPaginationMeta(items, 5, cursorOf)
		if len(got) != 5 {
			t.Errorf("len = %d, want 5", len(got))
		}
		if !meta.HasMore {
			t.Error("HasMore = false, want true")
		}
		wantNext := EncodeCursor(items[4])
		if meta.NextCursor != wantNext {
			t.Errorf("NextCursor = %q, want %q", meta.NextCursor, wantNext)
		}
	})
}
