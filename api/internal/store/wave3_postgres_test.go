package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

func TestPostgresWave3ScreeningReviewCASAndRecovery(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	runID, resultID := newTestUUID(), newTestUUID()
	repo := NewPgWave3Repo(pool)
	if err := repo.PersistScreeningRun(ctx, &domain.ScreeningRun{
		ID: runID, CustomerID: customerID, ListIDs: []string{"mof"},
		ConfigDigests: map[string]string{"screening": "sha256:test"},
		Status:        domain.ScreeningRunCompleted, Actor: "operator", CreatedAt: time.Now().UTC(),
	}, []domain.ScreeningResultRecord{{
		ID: resultID, CustomerID: customerID, ListID: "mof", ListType: "sanctions",
		EntryID: "entry-1", MatchedName: "Example", Status: domain.ScreeningResultStatusReviewing,
	}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM screening_result_history WHERE screening_result_id=$1`, resultID)
		_, _ = pool.Exec(ctx, `DELETE FROM screening_results WHERE id=$1`, resultID)
		_, _ = pool.Exec(ctx, `DELETE FROM screening_runs WHERE id=$1`, runID)
	})

	conn1 := acquirePgConn(t, pool)
	conn2 := acquirePgConn(t, pool)
	repo1 := NewPgWave3Repo(conn1)
	repo2 := NewPgWave3Repo(conn2)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, repo := range []*PgWave3Repo{repo1, repo2} {
		wg.Add(1)
		go func(i int, repo *PgWave3Repo) {
			defer wg.Done()
			_, err := repo.ReviewScreeningResult(ctx, resultID, domain.ScreeningResultStatusTruePositive, "confirmed", "operator-"+string(rune('a'+i)), 1)
			errs <- err
		}(i, repo)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		var c *domain.ErrConflict
		if !errors.As(err, &c) {
			t.Fatalf("unexpected concurrent review error: %v", err)
		}
		conflict++
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent review results = success %d, conflict %d; want 1/1", success, conflict)
	}
	got, err := repo.GetScreeningResult(ctx, resultID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ScreeningResultStatusTruePositive || got.Version != 2 || got.CaseID == "" {
		t.Fatalf("reloaded result = %+v, want true-positive version 2 with case", got)
	}
	history, err := repo.ListScreeningResultHistory(ctx, resultID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Version != 2 {
		t.Fatalf("history = %+v, want one version-2 entry", history)
	}
	var caseCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cases WHERE id=$1`, got.CaseID).Scan(&caseCount); err != nil {
		t.Fatal(err)
	}
	if caseCount != 1 {
		t.Fatalf("critical case count = %d, want 1", caseCount)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM cases WHERE id=$1`, got.CaseID)
}

func TestPostgresWave3TargetManifestConfirmationCAS(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	repo := NewPgWave3Repo(pool)
	manifest := &domain.TargetManifest{
		ID: newTestUUID(), Operation: "screening", TargetMode: domain.TargetModeSelected,
		CustomerIDs: []string{newTestUUID()}, SampleCustomerIDs: []string{},
		Token: "wave3-target-token", Status: "preview", Version: 1,
		ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC(),
	}
	if err := repo.CreateTargetManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM target_manifests WHERE id=$1`, manifest.ID) })
	conn1 := acquirePgConn(t, pool)
	conn2 := acquirePgConn(t, pool)
	repo1 := NewPgWave3Repo(conn1)
	repo2 := NewPgWave3Repo(conn2)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, client := range []*PgWave3Repo{repo1, repo2} {
		wg.Add(1)
		go func(i int, client *PgWave3Repo) {
			defer wg.Done()
			_, err := client.ConfirmTargetManifest(ctx, manifest.ID, manifest.Token, "operator", "approved", "confirm-"+string(rune('a'+i)), 1)
			errs <- err
		}(i, client)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		var c *domain.ErrConflict
		if !errors.As(err, &c) {
			t.Fatalf("unexpected target confirmation error: %v", err)
		}
		conflict++
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("target confirmation results = success %d, conflict %d; want 1/1", success, conflict)
	}
	confirmed, err := repo.GetTargetManifest(ctx, manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != "confirmed" || confirmed.Version != 2 {
		t.Fatalf("manifest = %+v, want confirmed version 2", confirmed)
	}
}

func TestPostgresWave3TransactionTravelRuleEmptyReasonSurvivesReload(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	txnID := newTestUUID()
	idempotencyKey := "wave3-travel-rule-" + txnID
	applicable := true
	now := time.Now().UTC()
	txn := &domain.Transaction{
		ID: txnID, CustomerID: customerID, ExternalID: "wave3-travel-rule-" + txnID,
		Amount: 100, Currency: "JPY", Direction: domain.DirectionOutbound,
		CounterpartyCountry: "JP", Metadata: map[string]any{"source": "wave3"},
		IdempotencyKey: &idempotencyKey, TravelRuleApplicable: &applicable,
		TravelRuleEvidence: map[string]any{"originator": "verified"},
		ExecutedAt:         now, CreatedAt: now,
	}
	if err := NewPgTransactionRepo(pool).Create(ctx, txn); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM transactions WHERE id=$1`, txnID) })
	got, err := NewPgTransactionRepo(pool).Get(ctx, txnID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IdempotencyKey == nil || *got.IdempotencyKey != idempotencyKey || got.TravelRuleApplicable == nil || !*got.TravelRuleApplicable {
		t.Fatalf("reloaded transaction parity = %+v", got)
	}
	if got.TravelRuleNotApplicableReason != "" || got.Metadata["source"] != "wave3" || got.TravelRuleEvidence["originator"] != "verified" {
		t.Fatalf("reloaded travel-rule fields = %+v", got)
	}
}

func TestPostgresWave3PendingTransitionCASAndHistory(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	pendingID := newTestUUID()
	repo := NewPgPendingEvaluationRepo(pool)
	pending := &domain.PendingEvaluation{ID: pendingID, CustomerID: customerID, Status: domain.PendingEvaluationStatusPendingReview, Reason: "engine unavailable"}
	if err := repo.Create(ctx, pending); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pending_evaluation_history WHERE pending_evaluation_id=$1`, pendingID)
		_, _ = pool.Exec(ctx, `DELETE FROM pending_evaluations WHERE id=$1`, pendingID)
	})
	conn1 := acquirePgConn(t, pool)
	conn2 := acquirePgConn(t, pool)
	repo1 := NewPgPendingEvaluationRepo(conn1)
	repo2 := NewPgPendingEvaluationRepo(conn2)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, client := range []*PgPendingEvaluationRepo{repo1, repo2} {
		wg.Add(1)
		go func(i int, client *PgPendingEvaluationRepo) {
			defer wg.Done()
			_, err := client.TransitionPendingEvaluation(ctx, pendingID, "retry", "operator-"+string(rune('a'+i)), "retry", 1)
			errs <- err
		}(i, client)
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		var c *domain.ErrConflict
		if !errors.As(err, &c) {
			t.Fatalf("unexpected pending transition error: %v", err)
		}
		conflict++
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("pending transition results = success %d, conflict %d; want 1/1", success, conflict)
	}
	got, err := repo.Get(ctx, pendingID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.RetryCount != 1 || got.NextRetryAt == nil {
		t.Fatalf("pending = %+v, want version 2 with retry schedule", got)
	}
	history, err := repo.ListPendingHistory(ctx, pendingID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Action != "retry" {
		t.Fatalf("history = %+v, want one retry entry", history)
	}
}

func TestPostgresWave3AppendOnlyHistoryRejectsUpdateAndDelete(t *testing.T) {
	pool := newTestPgPool(t)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)
	for _, mutation := range []string{"UPDATE customer_identity_history SET rationale='changed' WHERE id=$1", "DELETE FROM customer_identity_history WHERE id=$1"} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		entryID := newTestUUID()
		if _, err := tx.Exec(ctx, `INSERT INTO customer_identity_history(id,customer_id,changed_fields,actor,rationale) VALUES($1,$2,'{}','operator','original')`, entryID, customerID); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, mutation, entryID); err == nil {
			tx.Rollback(ctx)
			t.Fatalf("mutation %q unexpectedly succeeded", mutation)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
