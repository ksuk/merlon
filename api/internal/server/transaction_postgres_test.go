package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/batch"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

func issue150PostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MERLON_DATABASE_URL")
	if dsn == "" {
		t.Skip("MERLON_DATABASE_URL not set, skipping PostgreSQL integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return pool
}

type issue150PendingRecoveryScope struct {
	*store.PgPendingEvaluationRepo
	id string
}

func (r issue150PendingRecoveryScope) ListByStatus(ctx context.Context, status domain.PendingEvaluationStatus, limit, offset int) ([]domain.PendingEvaluation, error) {
	if offset > 0 || limit <= 0 {
		return nil, nil
	}
	pe, err := r.Get(ctx, r.id)
	if err != nil {
		return nil, err
	}
	if pe.Status != status {
		return nil, nil
	}
	return []domain.PendingEvaluation{*pe}, nil
}

func TestPostgresTransactionMonitoringUnavailableQueuesOnceAndRecovers(t *testing.T) {
	ctx := context.Background()
	pool := issue150PostgresPool(t)
	customers := store.NewPgCustomerRepo(pool, nil)
	transactions := store.NewPgTransactionRepo(pool)
	pending := store.NewPgPendingEvaluationRepo(pool)
	alerts := store.NewPgAlertRepo(pool)
	audit := store.NewPgAuditRepo(pool)
	atomic := store.NewPgAtomicMutationRepo(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := &domain.Customer{
		ID: generateID(), ExternalID: "issue-150-" + generateID(), CustomerType: domain.CustomerTypeIndividual,
		CountryCode: "JP", Status: domain.CustomerStatusActive, Attributes: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := customers.Create(ctx, customer); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pending_evaluation_history WHERE pending_evaluation_id IN (SELECT id FROM pending_evaluations WHERE customer_id=$1)`, customer.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM pending_evaluations WHERE customer_id=$1`, customer.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE resource_id=$1 OR details->>'customer_id'=$1`, customer.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM transactions WHERE customer_id=$1`, customer.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM customers WHERE id=$1`, customer.ID)
	})

	s := New(":0", Deps{
		Customers: customers, Transactions: transactions, Alerts: alerts, Audit: audit, Atomic: atomic,
		Scoring: &engine.MockScoringEngine{Score: 2.5, Tier: domain.RiskTierMedium}, PendingEvaluations: pending,
	})
	body := `{"customer_id":"` + customer.ID + `","external_id":"issue-150-tx-` + generateID() + `","amount":100,"currency":"JPY","direction":"inbound","executed_at":"2026-01-02T03:04:05Z"}`
	responses := make([]transactionMonitoringResponse, 0, 2)
	for i, wantStatus := range []int{http.StatusCreated, http.StatusOK} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "issue-150-"+customer.ID)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("request %d status = %d, want %d, body: %s", i+1, rec.Code, wantStatus, rec.Body.String())
		}
		var response transactionMonitoringResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if responses[0].MonitoringEvaluation == nil || responses[1].MonitoringEvaluation == nil || responses[0].MonitoringEvaluation.PendingEvaluationID != responses[1].MonitoringEvaluation.PendingEvaluationID {
		t.Fatalf("idempotent responses disagree: first=%+v second=%+v", responses[0], responses[1])
	}

	var transactionCount, pendingCount, queuedAuditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE customer_id=$1`, customer.ID).Scan(&transactionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pending_evaluations WHERE customer_id=$1`, customer.ID).Scan(&pendingCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='pending_evaluation_queued' AND resource_id=$1`, responses[0].MonitoringEvaluation.PendingEvaluationID).Scan(&queuedAuditCount); err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 || pendingCount != 1 || queuedAuditCount != 1 {
		t.Fatalf("transaction/pending/audit counts = %d/%d/%d, want 1/1/1", transactionCount, pendingCount, queuedAuditCount)
	}

	recoveryPending := issue150PendingRecoveryScope{PgPendingEvaluationRepo: pending, id: responses[0].MonitoringEvaluation.PendingEvaluationID}
	recovery := batch.NewRecoveryJob(recoveryPending, &engine.MockMonitoringEngine{}, alerts, transactions, customers)
	recovery.SetPersistence(atomic, audit, nil)
	processed, err := recovery.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	resolved, err := pending.GetLatestByTransaction(ctx, responses[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != domain.PendingEvaluationStatusResolved {
		t.Fatalf("status after recovery = %s, want %s", resolved.Status, domain.PendingEvaluationStatusResolved)
	}
	var recoveredAuditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='pending_evaluation_recovered' AND resource_id=$1`, resolved.ID).Scan(&recoveredAuditCount); err != nil {
		t.Fatal(err)
	}
	if recoveredAuditCount != 1 {
		t.Fatalf("pending_evaluation_recovered audit count = %d, want 1", recoveredAuditCount)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+responses[0].ID, nil)
	getRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body: %s", getRec.Code, getRec.Body.String())
	}
	var loaded transactionMonitoringResponse
	if err := json.NewDecoder(getRec.Body).Decode(&loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.MonitoringEvaluation == nil || loaded.MonitoringEvaluation.Status != domain.PendingEvaluationStatusResolved || loaded.MonitoringEvaluation.PendingEvaluationID != resolved.ID {
		t.Fatalf("GET monitoring_evaluation = %+v, want resolved %s", loaded.MonitoringEvaluation, resolved.ID)
	}
}
