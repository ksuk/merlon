package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/store"
)

// The durable manual batch run route had no test of any kind. These cover the
// lifecycle an operator depends on: the run exists before any customer is
// touched, a disconnect does not abandon it half-done, it can be stopped, and
// a rerun cannot skip the control that a first run had to pass.

type batchRunFixture struct {
	server    *Server
	runs      *store.MemoryBatchRunRepo
	wave3     *store.MemoryWave3Repo
	customers *store.MemoryCustomerRepo
	scoring   *gatedScoringEngine
}

// gatedScoringEngine lets a test hold a batch inside the customer loop, which
// is the only way to observe a run while it is still running.
type gatedScoringEngine struct {
	mu      sync.Mutex
	release chan struct{}
	scored  []string
}

func (e *gatedScoringEngine) ScoreCustomer(_ context.Context, c *domain.Customer, _ string) (*domain.ScoreRecord, error) {
	if e.release != nil {
		<-e.release
	}
	e.mu.Lock()
	e.scored = append(e.scored, c.ID)
	e.mu.Unlock()
	tier := domain.RiskTierLow
	return &domain.ScoreRecord{CustomerID: c.ID, Score: 1, Tier: tier, ScoredAt: time.Now().UTC()}, nil
}

func (e *gatedScoringEngine) scoredCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.scored)
}

func newBatchRunFixture(t *testing.T, customerCount int, scoring *gatedScoringEngine) *batchRunFixture {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	runs := store.NewMemoryBatchRunRepo()
	wave3 := store.NewMemoryWave3Repo()
	if scoring == nil {
		scoring = &gatedScoringEngine{}
	}
	s := New(":0", Deps{
		Customers: customers, BatchRuns: runs, Wave3: wave3,
		Audit: store.NewMemoryAuditRepo(), EventOutbox: store.NewMemoryEventOutboxRepo(),
		Scoring: scoring, Monitoring: &engine.MockMonitoringEngine{},
	})
	for i := range customerCount {
		id := fmt.Sprintf("00000000000000000000000000000b%02d", i)
		if err := customers.Create(ctx, &domain.Customer{ID: id, ExternalID: fmt.Sprintf("batch-%d", i), CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	return &batchRunFixture{server: s, runs: runs, wave3: wave3, customers: customers, scoring: scoring}
}

// confirmedManifest stores a manifest already through preview and confirm.
func (f *batchRunFixture) confirmedManifest(t *testing.T, count int) *domain.TargetManifest {
	t.Helper()
	ids := make([]string, 0, count)
	for i := range count {
		ids = append(ids, fmt.Sprintf("00000000000000000000000000000b%02d", i))
	}
	confirmedAt := time.Now().UTC()
	manifest := &domain.TargetManifest{
		ID: generateID(), Operation: "score", TargetMode: domain.TargetModeSelected,
		CustomerIDs: ids, TargetCount: len(ids), Token: "batch-token", Status: "confirmed",
		Version: 2, ExpiresAt: confirmedAt.Add(time.Hour), CreatedBy: "analyst-1",
		CreatedAt: confirmedAt, ConfirmedAt: &confirmedAt,
	}
	if err := f.wave3.CreateTargetManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func (f *batchRunFixture) startRun(t *testing.T, manifestID string, extra string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"operation":"score","target_manifest_id":%q%s}`, manifestID, extra)
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/batch/runs", strings.NewReader(body)))
	return rec
}

func decodeBatchRun(t *testing.T, rec *httptest.ResponseRecorder) domain.BatchRun {
	t.Helper()
	var run domain.BatchRun
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return run
}

func waitForRunStatus(t *testing.T, f *batchRunFixture, id string, want domain.BatchRunStatus) *domain.BatchRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := f.runs.Get(context.Background(), id)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	run, _ := f.runs.Get(context.Background(), id)
	t.Fatalf("run %s never reached %q; last state = %+v", id, want, run)
	return nil
}

// The run row must be committed before any customer is processed, and the
// response must arrive without waiting for the work: executing on the request
// context meant a client disconnect cancelled both the execution and its
// finalisation, stranding the run at status=running.
func TestCreateBatchRunAcceptsAndExecutesIndependentlyOfTheRequest(t *testing.T) {
	scoring := &gatedScoringEngine{release: make(chan struct{})}
	f := newBatchRunFixture(t, 3, scoring)
	manifest := f.confirmedManifest(t, 3)

	rec := f.startRun(t, manifest.ID, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: the run is started, not finished", rec.Code)
	}
	run := decodeBatchRun(t, rec)
	if run.ID == "" || run.Status != domain.BatchRunStatusRunning {
		t.Fatalf("response = %+v, want a running run", run)
	}

	// The row exists while the work is still gated, so a crash here leaves a
	// resumable record rather than nothing.
	stored, err := f.runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("run row missing while executing: %v", err)
	}
	if stored.TargetManifestID != manifest.ID {
		t.Fatalf("stored run = %+v, want it bound to the manifest", stored)
	}

	close(scoring.release)
	done := waitForRunStatus(t, f, run.ID, domain.BatchRunStatusCompleted)
	if done.ResultCounts["succeeded"] != 3 {
		t.Fatalf("counts = %v, want all three customers scored", done.ResultCounts)
	}
	if len(done.ProcessedCustomerIDs) != 3 {
		t.Fatalf("checkpoint = %v, want one entry per customer", done.ProcessedCustomerIDs)
	}
	if done.ConfigDigests == nil {
		t.Fatal("config digests were not pinned onto the run")
	}
}

func TestCreateBatchRunRequiresAConfirmedManifest(t *testing.T) {
	f := newBatchRunFixture(t, 1, nil)
	manifest := &domain.TargetManifest{
		ID: generateID(), Operation: "score", TargetMode: domain.TargetModeSelected,
		CustomerIDs: []string{"00000000000000000000000000000b00"}, TargetCount: 1,
		Token: "unconfirmed", Status: "preview", Version: 1,
		ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC(),
	}
	if err := f.wave3.CreateTargetManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if rec := f.startRun(t, manifest.ID, ""); rec.Code != http.StatusConflict {
		t.Fatalf("unconfirmed manifest = %d, want 409", rec.Code)
	}
	if rec := f.startRun(t, "does-not-exist", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown manifest = %d, want 404", rec.Code)
	}
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/batch/runs", strings.NewReader(`{"operation":"score"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing target_manifest_id = %d, want 400", rec.Code)
	}
}

// A retried start with the same idempotency key returns the original run with
// 200, not a second run with 202.
func TestCreateBatchRunIdempotentRetryReturnsTheExistingRun(t *testing.T) {
	f := newBatchRunFixture(t, 2, nil)
	manifest := f.confirmedManifest(t, 2)

	first := f.startRun(t, manifest.ID, `,"idempotency_key":"key-1"`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first start = %d, want 202", first.Code)
	}
	original := decodeBatchRun(t, first)
	waitForRunStatus(t, f, original.ID, domain.BatchRunStatusCompleted)

	second := f.startRun(t, manifest.ID, `,"idempotency_key":"key-1"`)
	if second.Code != http.StatusOK {
		t.Fatalf("retry = %d, want 200 for an already-started run", second.Code)
	}
	retried := decodeBatchRun(t, second)
	if retried.ID != original.ID {
		t.Fatalf("retry created run %s, want the original %s", retried.ID, original.ID)
	}
	runs, err := f.runs.ListBatchRuns(context.Background(), domain.BatchRunFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want the retry to have created none", len(runs))
	}
}

// BatchRunStatusCancelled existed in the domain but nothing ever wrote it.
func TestCancelBatchRunStopsWorkAndKeepsWhatWasDone(t *testing.T) {
	scoring := &gatedScoringEngine{release: make(chan struct{})}
	f := newBatchRunFixture(t, 6, scoring)
	manifest := f.confirmedManifest(t, 6)

	rec := f.startRun(t, manifest.ID, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start = %d, body=%s", rec.Code, rec.Body.String())
	}
	run := decodeBatchRun(t, rec)

	// Let two customers through, then cancel while the third is gated.
	scoring.release <- struct{}{}
	scoring.release <- struct{}{}

	cancel := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(cancel, httptest.NewRequest(http.MethodPost, "/api/v1/batch/runs/"+run.ID+"/cancel", strings.NewReader(`{"reason":"wrong rule set"}`)))
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel = %d, body=%s", cancel.Code, cancel.Body.String())
	}
	cancelled := decodeBatchRun(t, cancel)
	if cancelled.Status != domain.BatchRunStatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	close(scoring.release)

	settled := waitForRunStatus(t, f, run.ID, domain.BatchRunStatusCancelled)
	if settled.ResultCounts["succeeded"] == 0 {
		t.Fatal("cancelling discarded the work already done")
	}
	if scoring.scoredCount() >= 6 {
		t.Fatalf("scored %d customers; cancellation did not stop the loop", scoring.scoredCount())
	}
	// Cancelled is terminal and distinct: the run must not later report
	// completed or failed.
	if settled.CompletedAt == nil {
		t.Fatal("a cancelled run was left without a completion timestamp")
	}
}

func TestCancelBatchRunRejectsATerminalRun(t *testing.T) {
	f := newBatchRunFixture(t, 1, nil)
	manifest := f.confirmedManifest(t, 1)
	rec := f.startRun(t, manifest.ID, "")
	run := decodeBatchRun(t, rec)
	waitForRunStatus(t, f, run.ID, domain.BatchRunStatusCompleted)

	late := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(late, httptest.NewRequest(http.MethodPost, "/api/v1/batch/runs/"+run.ID+"/cancel", strings.NewReader(`{}`)))
	if late.Code != http.StatusConflict {
		t.Fatalf("cancelling a completed run = %d, want 409", late.Code)
	}

	missing := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/api/v1/batch/runs/unknown/cancel", strings.NewReader(`{}`)))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("cancelling an unknown run = %d, want 404", missing.Code)
	}
}

// Rerun used to clone the manifest as already-confirmed and execute it,
// bypassing the preview-and-confirm control the target mechanism exists to
// provide. It now hands back a preview awaiting confirmation.
func TestRerunBatchRunReturnsAnUnconfirmedManifest(t *testing.T) {
	f := newBatchRunFixture(t, 2, nil)
	manifest := f.confirmedManifest(t, 2)
	rec := f.startRun(t, manifest.ID, "")
	run := decodeBatchRun(t, rec)
	waitForRunStatus(t, f, run.ID, domain.BatchRunStatusCompleted)

	rerun := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rerun, httptest.NewRequest(http.MethodPost, "/api/v1/batch/runs/"+run.ID+"/rerun", strings.NewReader(`{}`)))
	if rerun.Code != http.StatusCreated {
		t.Fatalf("rerun = %d, body=%s", rerun.Code, rerun.Body.String())
	}
	var body struct {
		TargetManifest domain.TargetManifest `json:"target_manifest"`
		Operation      string                `json:"operation"`
		RerunOf        string                `json:"rerun_of"`
	}
	if err := json.Unmarshal(rerun.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TargetManifest.Status != "preview" {
		t.Fatalf("cloned manifest status = %q, want preview", body.TargetManifest.Status)
	}
	if body.TargetManifest.ConfirmedAt != nil {
		t.Fatal("the cloned manifest arrived already confirmed")
	}
	if body.TargetManifest.Token == "" {
		t.Fatal("no token was returned; the caller cannot confirm the rerun")
	}
	if body.RerunOf != run.ID || body.Operation != "score" {
		t.Fatalf("body = %+v, want it to name the original run and operation", body)
	}

	// Starting a run against the unconfirmed clone is refused until it is
	// confirmed -- that is the control the old behaviour skipped.
	if blocked := f.startRun(t, body.TargetManifest.ID, ""); blocked.Code != http.StatusConflict {
		t.Fatalf("running an unconfirmed rerun = %d, want 409", blocked.Code)
	}

	confirm := httptest.NewRecorder()
	confirmBody := fmt.Sprintf(`{"token":%q,"rationale":"re-running after the rule fix","expected_version":1}`, body.TargetManifest.Token)
	f.server.Handler().ServeHTTP(confirm, httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/"+body.TargetManifest.ID+"/confirm", strings.NewReader(confirmBody)))
	if confirm.Code != http.StatusOK {
		t.Fatalf("confirming the rerun = %d, body=%s", confirm.Code, confirm.Body.String())
	}
	started := f.startRun(t, body.TargetManifest.ID, "")
	if started.Code != http.StatusAccepted {
		t.Fatalf("confirmed rerun = %d, want 202", started.Code)
	}
	waitForRunStatus(t, f, decodeBatchRun(t, started).ID, domain.BatchRunStatusCompleted)
}

func TestListAndGetBatchRuns(t *testing.T) {
	f := newBatchRunFixture(t, 2, nil)
	for range 3 {
		manifest := f.confirmedManifest(t, 2)
		rec := f.startRun(t, manifest.ID, "")
		waitForRunStatus(t, f, decodeBatchRun(t, rec).ID, domain.BatchRunStatusCompleted)
	}

	list := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/batch/runs?limit=2", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", list.Code, list.Body.String())
	}
	var page struct {
		Data       []domain.BatchRun `json:"data"`
		Pagination PaginationMeta    `json:"pagination"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 || !page.Pagination.HasMore {
		t.Fatalf("first page = %d rows has_more=%v, want 2 and true", len(page.Data), page.Pagination.HasMore)
	}

	filtered := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(filtered, httptest.NewRequest(http.MethodGet, "/api/v1/batch/runs?status=completed&operation=score", nil))
	var byStatus struct {
		Data []domain.BatchRun `json:"data"`
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &byStatus); err != nil {
		t.Fatal(err)
	}
	if len(byStatus.Data) != 3 {
		t.Fatalf("filtered = %d runs, want 3", len(byStatus.Data))
	}

	one := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(one, httptest.NewRequest(http.MethodGet, "/api/v1/batch/runs/"+page.Data[0].ID, nil))
	if one.Code != http.StatusOK {
		t.Fatalf("get = %d", one.Code)
	}
	missing := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/batch/runs/nope", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown run = %d, want 404", missing.Code)
	}
}

// A process that died mid-run leaves status=running. Resume must finish it
// without redoing the customers it already checkpointed.
func TestResumeManualBatchRunsSkipsProcessedCustomers(t *testing.T) {
	f := newBatchRunFixture(t, 3, nil)
	ctx := context.Background()
	manifest := f.confirmedManifest(t, 3)
	// Claim the manifest the way a started run does.
	if _, err := f.wave3.ClaimTargetManifest(ctx, manifest.ID, "run-resume", manifest.Version); err != nil {
		t.Fatal(err)
	}
	run := &domain.BatchRun{
		ID: "run-resume", JobType: "score", Operation: "score", Status: domain.BatchRunStatusRunning,
		TargetManifestID: manifest.ID, Parameters: map[string]any{}, Actor: "operator-1",
		ProcessedCustomerIDs: []string{"00000000000000000000000000000b00"},
		ResultCounts:         map[string]int{"succeeded": 1},
		StartedAt:            time.Now().UTC().Add(-time.Hour),
	}
	if err := f.runs.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	f.server.ResumeManualBatchRuns(ctx)

	settled, err := f.runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != domain.BatchRunStatusCompleted {
		t.Fatalf("status = %q, want the interrupted run finished", settled.Status)
	}
	if settled.ResultCounts["succeeded"] != 3 {
		t.Fatalf("counts = %v, want the checkpointed customer counted once and the rest processed", settled.ResultCounts)
	}
	for _, id := range f.scoring.scored {
		if id == "00000000000000000000000000000b00" {
			t.Fatal("resume re-scored a customer that was already checkpointed")
		}
	}
}
