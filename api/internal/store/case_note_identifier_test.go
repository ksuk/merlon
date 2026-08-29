package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func hyphenatedTestUUID() string {
	id := newTestUUID()
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func TestMemoryCaseRepoAddNoteCanonicalizesUUIDShapedCaseID(t *testing.T) {
	repo := NewMemoryCaseRepo()
	ctx := context.Background()
	caseID := hyphenatedTestUUID()
	createdAt := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	kase := &domain.Case{
		ID:         caseID,
		CustomerID: newTestUUID(),
		Status:     domain.CaseStatusOpen,
		Priority:   domain.CasePriorityMedium,
		Summary:    "synthetic case note identifier test",
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
	if err := repo.Create(ctx, kase); err != nil {
		t.Fatalf("Create: %v", err)
	}

	note := &domain.CaseNote{
		ID:        "note-" + newTestUUID(),
		Author:    "synthetic-reviewer",
		Content:   "synthetic case note",
		CreatedAt: createdAt.Add(time.Minute),
	}
	if err := repo.AddNote(ctx, caseID, note); err != nil {
		t.Fatalf("AddNote with UUID-shaped case ID: %v", err)
	}

	got, err := repo.Get(ctx, caseID)
	if err != nil {
		t.Fatalf("Get with UUID-shaped case ID: %v", err)
	}
	if len(got.Notes) != 1 || got.Notes[0].ID != note.ID {
		t.Fatalf("Notes = %+v, want note %q", got.Notes, note.ID)
	}
}

func TestPostgresCaseRepoAddNoteUsesCanonicalCaseID(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgCaseRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	createdAt := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name   string
		caseID string
	}{
		{name: "UUID-shaped TEXT identifier", caseID: hyphenatedTestUUID()},
		{name: "non-UUID TEXT identifier", caseID: "synthetic-case-" + newTestUUID()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kase := &domain.Case{
				ID:         tc.caseID,
				CustomerID: customerID,
				Status:     domain.CaseStatusOpen,
				Priority:   domain.CasePriorityMedium,
				Summary:    "synthetic case note identifier test",
				CreatedAt:  createdAt,
				UpdatedAt:  createdAt,
			}
			if err := repo.Create(ctx, kase); err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM case_notes WHERE case_id = $1`, kase.ID)
				pool.Exec(context.Background(), `DELETE FROM cases WHERE id = $1`, kase.ID)
			})

			note := &domain.CaseNote{
				ID:        "note-" + newTestUUID(),
				Author:    "synthetic-reviewer",
				Content:   "synthetic case note",
				CreatedAt: createdAt.Add(time.Minute),
			}
			if err := repo.AddNote(ctx, tc.caseID, note); err != nil {
				t.Fatalf("AddNote(%q): %v", tc.caseID, err)
			}

			got, err := repo.Get(ctx, tc.caseID)
			if err != nil {
				t.Fatalf("Get(%q): %v", tc.caseID, err)
			}
			if len(got.Notes) != 1 || got.Notes[0].ID != note.ID {
				t.Fatalf("Notes = %+v, want note %q", got.Notes, note.ID)
			}
		})
	}
}
