package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/merlon-aml/merlon/api/internal/domain"
)

// newTestUUID generates a random 32 hex-digit identifier accepted by
// PostgreSQL's uuid input parser (server.generateID uses the same shape but
// is unexported and lives in a different package).
func newTestUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// newTestPgPool connects to MERLON_DATABASE_URL for integration tests. It
// skips (not fails) when the variable is unset, matching main.go's
// treatment of MERLON_DATABASE_URL as an optional dependency.
func newTestPgPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping Postgres integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("pool.Ping: %v", err)
	}
	return pool
}

// seedTestCustomer inserts a minimal customer row so pending_evaluations'
// customer_id foreign key is satisfiable, and returns its id.
func seedTestCustomer(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO customers (external_id, customer_type, country_code, product_types, attributes)
		VALUES ($1, 'individual', 'JP', '{}', '{}') RETURNING id`,
		"pending-eval-test-"+newTestUUID(),
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM customers WHERE id = $1`, id)
	})
	return id
}

func TestPostgresPendingEvaluationRepo_CreateAndGet(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgPendingEvaluationRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)

	pe := &domain.PendingEvaluation{
		ID:             newTestUUID(),
		CustomerID:     customerID,
		TransactionIDs: nil,
		Status:         domain.PendingEvaluationStatusPendingReview,
		Reason:         "engine unavailable: deadline exceeded",
	}
	if err := repo.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM pending_evaluations WHERE id = $1`, pe.ID)
	})

	got, err := repo.Get(ctx, pe.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CustomerID != customerID {
		t.Errorf("CustomerID = %s, want %s", got.CustomerID, customerID)
	}
	if got.Status != domain.PendingEvaluationStatusPendingReview {
		t.Errorf("Status = %s, want PENDING_REVIEW", got.Status)
	}
	if got.Reason != pe.Reason {
		t.Errorf("Reason = %s, want %s", got.Reason, pe.Reason)
	}
}

func TestPostgresPendingEvaluationRepo_ListByStatusFiltersCorrectly(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgPendingEvaluationRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)

	makePe := func(status domain.PendingEvaluationStatus) *domain.PendingEvaluation {
		pe := &domain.PendingEvaluation{
			ID:         newTestUUID(),
			CustomerID: customerID,
			Status:     status,
			Reason:     "test",
		}
		if err := repo.Create(ctx, pe); err != nil {
			t.Fatalf("Create: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM pending_evaluations WHERE id = $1`, pe.ID)
		})
		return pe
	}

	p1 := makePe(domain.PendingEvaluationStatusPendingReview)
	p2 := makePe(domain.PendingEvaluationStatusPendingReview)
	makePe(domain.PendingEvaluationStatusResolved)

	pending, err := repo.ListByStatus(ctx, domain.PendingEvaluationStatusPendingReview, 50, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}

	found := map[string]bool{}
	for _, pe := range pending {
		found[pe.ID] = true
		if pe.Status != domain.PendingEvaluationStatusPendingReview {
			t.Errorf("ListByStatus(PENDING_REVIEW) returned status %s", pe.Status)
		}
	}
	if !found[p1.ID] || !found[p2.ID] {
		t.Errorf("ListByStatus(PENDING_REVIEW) missing expected records, got %d", len(pending))
	}
}

func TestPostgresPendingEvaluationRepo_UpdateStatusAndIncrementRetry(t *testing.T) {
	pool := newTestPgPool(t)
	repo := NewPgPendingEvaluationRepo(pool)
	ctx := context.Background()
	customerID := seedTestCustomer(t, pool)

	pe := &domain.PendingEvaluation{
		ID:         newTestUUID(),
		CustomerID: customerID,
		Status:     domain.PendingEvaluationStatusPendingReview,
		Reason:     "test",
	}
	if err := repo.Create(ctx, pe); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM pending_evaluations WHERE id = $1`, pe.ID)
	})

	if err := repo.IncrementRetry(ctx, pe.ID); err != nil {
		t.Fatalf("IncrementRetry: %v", err)
	}
	if err := repo.UpdateStatus(ctx, pe.ID, domain.PendingEvaluationStatusResolved); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.Get(ctx, pe.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", got.RetryCount)
	}
	if got.Status != domain.PendingEvaluationStatusResolved {
		t.Errorf("Status = %s, want RESOLVED", got.Status)
	}
	if got.ResolvedAt == nil {
		t.Error("ResolvedAt should be set")
	}
}
