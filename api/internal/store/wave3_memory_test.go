package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func requireWave3Conflict(t *testing.T, err error) {
	t.Helper()
	var conflict *domain.ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}

func TestMemoryWave3ScreeningReviewCASCreatesOneCriticalCase(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryWave3Repo()
	cases := NewMemoryCaseRepo()
	repo.SetCaseRepository(cases)
	customerID := "00000000000000000000000000000001"
	resultID := "00000000000000000000000000000002"
	if err := repo.PersistScreeningRun(ctx, &domain.ScreeningRun{
		ID: "00000000000000000000000000000003", CustomerID: customerID,
		Status: domain.ScreeningRunCompleted, CreatedAt: time.Now().UTC(),
	}, []domain.ScreeningResultRecord{{
		ID: resultID, CustomerID: customerID, ListID: "mof", ListType: "sanctions",
		EntryID: "entry-1", MatchedName: "Example", Status: domain.ScreeningResultStatusReviewing,
	}}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, actor := range []string{"analyst-a", "analyst-b"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			_, err := repo.ReviewScreeningResult(ctx, resultID, domain.ScreeningResultStatusTruePositive, "confirmed", actor, 1)
			errs <- err
		}(actor)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireWave3Conflict(t, err)
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("review results = success %d, conflict %d; want 1/1", success, conflict)
	}
	got, err := repo.GetScreeningResult(ctx, resultID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CaseID == "" || got.Version != 2 {
		t.Fatalf("result after review = %+v, want one case and version 2", got)
	}
	createdCases, err := cases.ListByCustomer(ctx, customerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(createdCases) != 1 || createdCases[0].Priority != domain.CasePriorityCritical {
		t.Fatalf("cases = %+v, want one critical case", createdCases)
	}
	history, err := repo.ListScreeningResultHistory(ctx, resultID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ToStatus != domain.ScreeningResultStatusTruePositive {
		t.Fatalf("history = %+v, want one true-positive entry", history)
	}
}

func TestMemoryWave3TargetManifestConfirmationAndClaimAreCASAndIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryWave3Repo()
	manifest := &domain.TargetManifest{
		ID: "00000000000000000000000000000011", Operation: "screening",
		TargetMode: domain.TargetModeSelected, CustomerIDs: []string{"c1"},
		Token: "one-time-token", Status: "preview", Version: 1,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := repo.CreateTargetManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	confirmed, err := repo.ConfirmTargetManifest(ctx, manifest.ID, manifest.Token, "analyst", "reviewed", "confirm-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != "confirmed" || confirmed.Version != 2 {
		t.Fatalf("confirmed = %+v, want version 2", confirmed)
	}
	retry, err := repo.ConfirmTargetManifest(ctx, manifest.ID, manifest.Token, "analyst", "reviewed", "confirm-1", 1)
	if err != nil {
		t.Fatalf("idempotent confirmation: %v", err)
	}
	if retry.Version != confirmed.Version || retry.Status != confirmed.Status {
		t.Fatalf("idempotent retry = %+v, want %+v", retry, confirmed)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, runID := range []string{"00000000000000000000000000000012", "00000000000000000000000000000013"} {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			_, err := repo.ClaimTargetManifest(ctx, manifest.ID, runID, 2)
			errs <- err
		}(runID)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireWave3Conflict(t, err)
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("claim results = success %d, conflict %d; want 1/1", success, conflict)
	}
}

func TestMemoryWave3PendingEvaluationTransitionCASAndHistory(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryPendingEvaluationRepo()
	pending := &domain.PendingEvaluation{ID: "pending-wave3", CustomerID: "customer-wave3", Status: domain.PendingEvaluationStatusPendingReview, Reason: "engine unavailable"}
	if err := repo.Create(ctx, pending); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, actor := range []string{"operator-a", "operator-b"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			_, err := repo.TransitionPendingEvaluation(ctx, pending.ID, "retry", actor, "retrying", 1)
			errs <- err
		}(actor)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireWave3Conflict(t, err)
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("pending transition results = success %d, conflict %d; want 1/1", success, conflict)
	}
	got, err := repo.Get(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.RetryCount != 1 || got.NextRetryAt == nil {
		t.Fatalf("pending = %+v, want version 2 with retry schedule", got)
	}
	history, err := repo.ListPendingHistory(ctx, pending.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Action != "retry" {
		t.Fatalf("pending history = %+v, want one retry entry", history)
	}
}
