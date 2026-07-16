package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestMemoryCustomerRepo_ListByCursor_MatchesListTraversal(t *testing.T) {
	repo := NewMemoryCustomerRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	const n = 5
	ids := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		c := &domain.Customer{
			ID:         generateTestID(i),
			ExternalID: generateTestID(i),
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
			UpdatedAt:  base.Add(time.Duration(i) * time.Minute),
		}
		ids[c.ID] = true
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// Full offset traversal.
	offsetIDs := map[string]bool{}
	offset := 0
	for {
		page, err := repo.List(ctx, 2, offset)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, c := range page {
			offsetIDs[c.ID] = true
		}
		offset += len(page)
	}

	// Full cursor traversal, fetching limit+1 as handlers do to detect has_more.
	cursorIDs := map[string]bool{}
	var after *domain.Cursor
	for {
		page, err := repo.ListByCursor(ctx, 3, after)
		if err != nil {
			t.Fatalf("ListByCursor: %v", err)
		}
		trimmed, meta := trimForTest(page, 2)
		for _, c := range trimmed {
			cursorIDs[c.ID] = true
		}
		if !meta {
			break
		}
		last := trimmed[len(trimmed)-1]
		after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	if len(offsetIDs) != n {
		t.Errorf("offset traversal found %d, want %d", len(offsetIDs), n)
	}
	if len(cursorIDs) != n {
		t.Errorf("cursor traversal found %d, want %d", len(cursorIDs), n)
	}
	for id := range ids {
		if !offsetIDs[id] {
			t.Errorf("offset traversal missing %s", id)
		}
		if !cursorIDs[id] {
			t.Errorf("cursor traversal missing %s", id)
		}
	}
}

func TestMemoryAlertRepo_ListOpenByCursor_HasMore(t *testing.T) {
	repo := NewMemoryAlertRepo()
	ctx := context.Background()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		a := &domain.Alert{
			ID:         generateTestID(i),
			CustomerID: "cust-1",
			Status:     domain.AlertStatusOpen,
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
			UpdatedAt:  base.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.Create(ctx, a); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	page, err := repo.ListOpenByCursor(ctx, 2, nil)
	if err != nil {
		t.Fatalf("ListOpenByCursor: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("len = %d, want 2", len(page))
	}
	// Newest first (created_at DESC): index 2, then index 1.
	if page[0].ID != generateTestID(2) || page[1].ID != generateTestID(1) {
		t.Errorf("unexpected order: %+v", page)
	}

	after := &domain.Cursor{CreatedAt: page[1].CreatedAt, ID: page[1].ID}
	rest, err := repo.ListOpenByCursor(ctx, 2, after)
	if err != nil {
		t.Fatalf("ListOpenByCursor page2: %v", err)
	}
	if len(rest) != 1 || rest[0].ID != generateTestID(0) {
		t.Errorf("unexpected second page: %+v", rest)
	}
}

func generateTestID(i int) string {
	return "id-" + string(rune('a'+i))
}

// trimForTest mirrors server.BuildPaginationMeta's has_more detection without
// importing the server package (which would create an import cycle back into
// store via domain).
func trimForTest(items []domain.Customer, limit int) ([]domain.Customer, bool) {
	if len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}
