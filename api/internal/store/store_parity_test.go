package store

import (
	"context"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

// The memory store is what handler tests run against and what a deployment
// without MERLON_DATABASE_URL uses. Where it answers a question differently
// from PostgreSQL, a test can pass everywhere and the product still be wrong.

// B7: customer search matched any attribute key or value in memory, but only
// four whitelisted keys in PostgreSQL. Besides the parity break, the memory
// behaviour let a search term reach arbitrary PII-bearing attributes -- an
// occupation, a nationality, a free-text note -- that the SQL side
// deliberately excludes.
func TestMemoryCustomerSearchMatchesOnlyTheSearchableAttributes(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryCustomerRepo()
	now := time.Now().UTC()
	if err := repo.Create(ctx, &domain.Customer{
		ID: "00000000000000000000000000000f01", ExternalID: "parity-1",
		CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP",
		Status: domain.CustomerStatusActive, CreatedAt: now, UpdatedAt: now,
		Attributes: map[string]any{
			"name":       "Yamada Taro",
			"name_kana":  "ヤマダタロウ",
			"address":    "Tokyo",
			"occupation": "cryptozoologist",
		},
	}); err != nil {
		t.Fatal(err)
	}

	searchable := []string{"yamada", "ヤマダ", "tokyo", "parity-1", "JP"}
	for _, needle := range searchable {
		found, err := repo.ListSearch(ctx, needle, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 {
			t.Errorf("search %q returned %d customers, want 1", needle, len(found))
		}
	}

	// occupation is not one of the searchable attributes in SQL, so it must
	// not be one here either.
	found, err := repo.ListSearch(ctx, "cryptozoologist", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("search on a non-searchable attribute returned %d customers, want 0: the memory store must not reach attributes PostgreSQL excludes", len(found))
	}
}

// B8: ReviewScreeningResult reported case_created=true and handed back a case
// id even when no case repository was wired, so a caller was told a critical
// case existed for a confirmed sanctions hit when none had been created.
func TestMemoryScreeningReviewDoesNotClaimAnUncreatedCase(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryWave3Repo() // deliberately without SetCaseRepository
	now := time.Now().UTC()
	result := &domain.ScreeningResultRecord{
		ID: "00000000000000000000000000000f10", CustomerID: "00000000000000000000000000000f01",
		ListID: "ofac", ListType: "sanctions", EntryID: "E-1", MatchedName: "Test Match",
		Status: domain.ScreeningResultStatusNew, ScreenedAt: now, CreatedAt: now,
		UpdatedAt: now, Version: 1,
	}
	run := &domain.ScreeningRun{ID: wave3ID(), CustomerID: result.CustomerID, Status: domain.ScreeningRunCompleted, ResultCount: 1, StartedAt: now, CompletedAt: &now, CreatedAt: now}
	if err := repo.PersistScreeningRun(ctx, run, []domain.ScreeningResultRecord{*result}); err != nil {
		t.Fatal(err)
	}
	reviewing, err := repo.ReviewScreeningResult(ctx, result.ID, domain.ScreeningResultStatusReviewing, "", "analyst", result.Version)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.ReviewScreeningResult(ctx, result.ID, domain.ScreeningResultStatusTruePositive, "confirmed", "analyst", reviewing.Result.Version)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CaseCreated {
		t.Error("case_created = true with no case repository configured; a confirmed hit must not report a case that was never written")
	}
	if outcome.CaseID != "" {
		t.Errorf("case_id = %q with no case repository configured, want empty", outcome.CaseID)
	}
	if outcome.Result.CaseID != "" {
		t.Errorf("result.case_id = %q with no case repository configured, want empty", outcome.Result.CaseID)
	}
}

// The wired path must still create the case, or the fix above would have
// turned a false claim into a missing one.
func TestMemoryScreeningReviewCreatesCaseWhenRepositoryIsWired(t *testing.T) {
	ctx := context.Background()
	cases := NewMemoryCaseRepo()
	repo := NewMemoryWave3Repo()
	repo.SetCaseRepository(cases)
	now := time.Now().UTC()
	result := &domain.ScreeningResultRecord{
		ID: "00000000000000000000000000000f11", CustomerID: "00000000000000000000000000000f01",
		ListID: "ofac", ListType: "sanctions", EntryID: "E-2", MatchedName: "Test Match",
		Status: domain.ScreeningResultStatusNew, ScreenedAt: now, CreatedAt: now,
		UpdatedAt: now, Version: 1,
	}
	run := &domain.ScreeningRun{ID: wave3ID(), CustomerID: result.CustomerID, Status: domain.ScreeningRunCompleted, ResultCount: 1, StartedAt: now, CompletedAt: &now, CreatedAt: now}
	if err := repo.PersistScreeningRun(ctx, run, []domain.ScreeningResultRecord{*result}); err != nil {
		t.Fatal(err)
	}
	reviewing, err := repo.ReviewScreeningResult(ctx, result.ID, domain.ScreeningResultStatusReviewing, "", "analyst", result.Version)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.ReviewScreeningResult(ctx, result.ID, domain.ScreeningResultStatusTruePositive, "confirmed", "analyst", reviewing.Result.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.CaseCreated || outcome.CaseID == "" {
		t.Fatalf("case_created=%v case_id=%q, want a case to be created", outcome.CaseCreated, outcome.CaseID)
	}
	created, err := cases.Get(ctx, outcome.CaseID)
	if err != nil {
		t.Fatalf("reported case %s is not retrievable: %v", outcome.CaseID, err)
	}
	if created.Priority != domain.CasePriorityCritical {
		t.Errorf("case priority = %q, want critical", created.Priority)
	}
}
