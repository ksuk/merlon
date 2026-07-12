package screening

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func riskTier(t domain.RiskTier) *domain.RiskTier { return &t }

// --- TestRescreeningSchedule_*: pure function, table-driven per tier ---

func TestRescreeningSchedule_HighTierDaily(t *testing.T) {
	cases := []struct {
		elapsedDays int
		want        bool
	}{
		{0, false},
		{1, true},
		{2, true},
	}
	for _, tc := range cases {
		got := IsDueForRescreening(domain.RiskTierHigh, tc.elapsedDays)
		if got != tc.want {
			t.Errorf("IsDueForRescreening(High, %d) = %v, want %v", tc.elapsedDays, got, tc.want)
		}
	}
}

func TestRescreeningSchedule_MediumTierWeekly(t *testing.T) {
	cases := []struct {
		elapsedDays int
		want        bool
	}{
		{6, false},
		{7, true},
		{8, true},
	}
	for _, tc := range cases {
		got := IsDueForRescreening(domain.RiskTierMedium, tc.elapsedDays)
		if got != tc.want {
			t.Errorf("IsDueForRescreening(Medium, %d) = %v, want %v", tc.elapsedDays, got, tc.want)
		}
	}
}

func TestRescreeningSchedule_LowTierMonthly(t *testing.T) {
	cases := []struct {
		elapsedDays int
		want        bool
	}{
		{29, false},
		{30, true},
		{31, true},
	}
	for _, tc := range cases {
		got := IsDueForRescreening(domain.RiskTierLow, tc.elapsedDays)
		if got != tc.want {
			t.Errorf("IsDueForRescreening(Low, %d) = %v, want %v", tc.elapsedDays, got, tc.want)
		}
	}
}

// --- fakes for RunRescreeningBatch tests ---

type fakeCustomerRepo struct {
	mu        sync.Mutex
	data      map[string]*domain.Customer
	order     []string
	listCalls int
}

func newFakeCustomerRepo(customers ...domain.Customer) *fakeCustomerRepo {
	r := &fakeCustomerRepo{data: map[string]*domain.Customer{}}
	for _, c := range customers {
		cc := c
		r.data[c.ID] = &cc
		r.order = append(r.order, c.ID)
	}
	return r
}

func (r *fakeCustomerRepo) Get(_ context.Context, id string) (*domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer", ID: id}
	}
	cp := *c
	return &cp, nil
}

func (r *fakeCustomerRepo) GetByExternalID(_ context.Context, _ string) (*domain.Customer, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeCustomerRepo) List(_ context.Context, _, _ int) ([]domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	out := make([]domain.Customer, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, *r.data[id])
	}
	return out, nil
}

func (r *fakeCustomerRepo) ListByCursor(_ context.Context, _ int, _ *domain.Cursor) ([]domain.Customer, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeCustomerRepo) Create(_ context.Context, c *domain.Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *c
	r.data[c.ID] = &cp
	r.order = append(r.order, c.ID)
	return nil
}

func (r *fakeCustomerRepo) Update(_ context.Context, c *domain.Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *c
	r.data[c.ID] = &cp
	return nil
}

func (r *fakeCustomerRepo) SaveScoreRecord(_ context.Context, _ *domain.ScoreRecord) error {
	return nil
}

func (r *fakeCustomerRepo) ListScoreHistory(_ context.Context, _ string, _ int) ([]domain.ScoreRecord, error) {
	return nil, nil
}

func (r *fakeCustomerRepo) ListEDDPending(_ context.Context) ([]domain.Customer, error) {
	return nil, nil
}

func (r *fakeCustomerRepo) UpdateStatus(_ context.Context, id string, status domain.CustomerStatus, _ string) (*domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.data[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "customer", ID: id}
	}
	c.Status = status
	cp := *c
	return &cp, nil
}

// fakeScreeningEngine records the order and set of customers screened, and
// lets tests inject an onCall hook (e.g. to mutate the customer repo
// mid-batch) or return preset matches per customer.
type fakeScreeningEngine struct {
	mu      sync.Mutex
	calls   []string
	onCall  func(customerID string)
	matches map[string][]domain.ScreenMatch
}

func (e *fakeScreeningEngine) ScreenCustomer(_ context.Context, c *domain.Customer, listIDs []string) (*domain.ScreenResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, c.ID)
	e.mu.Unlock()

	if e.onCall != nil {
		e.onCall(c.ID)
	}

	matches := e.matches[c.ID]
	return &domain.ScreenResult{
		CustomerID:   c.ID,
		Hit:          len(matches) > 0,
		Matches:      matches,
		ListsChecked: len(listIDs),
		ScreenedAt:   time.Now(),
	}, nil
}

func (e *fakeScreeningEngine) callOrder() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.calls))
	copy(out, e.calls)
	return out
}

func TestRunRescreeningBatch_SnapshotFixedAtStart(t *testing.T) {
	custA := domain.Customer{ID: "cust-a", RiskTier: riskTier(domain.RiskTierHigh)}
	repo := newFakeCustomerRepo(custA)

	engine := &fakeScreeningEngine{
		onCall: func(customerID string) {
			if customerID == "cust-a" {
				// Simulate a new customer being onboarded mid-batch; it must
				// not be picked up by the batch already in flight.
				_ = repo.Create(context.Background(), &domain.Customer{ID: "cust-b", RiskTier: riskTier(domain.RiskTierHigh)})
			}
		},
	}

	deps := SchedulerDeps{
		Customers: repo,
		Screening: engine,
		Results:   store.NewMemoryScreeningResultRepo(),
		Now:       func() time.Time { return time.Date(2026, 7, 5, 3, 0, 0, 0, time.UTC) },
	}

	result, err := RunRescreeningBatch(context.Background(), deps, TriggerListUpdated)
	if err != nil {
		t.Fatalf("RunRescreeningBatch: %v", err)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].CustomerID != "cust-a" {
		t.Fatalf("outcomes = %+v, want only cust-a (snapshot fixed at batch start)", result.Outcomes)
	}
	if repo.listCalls != 1 {
		t.Errorf("Customers.List called %d times, want exactly 1 (no mid-batch re-query)", repo.listCalls)
	}
}

func TestRunRescreeningBatch_NameChangeDuringBatchTakesPriority(t *testing.T) {
	batchStart := time.Date(2026, 7, 5, 3, 0, 0, 0, time.UTC)
	custA := domain.Customer{ID: "cust-a", RiskTier: riskTier(domain.RiskTierHigh)}
	custB := domain.Customer{ID: "cust-b", RiskTier: riskTier(domain.RiskTierHigh)}
	repo := newFakeCustomerRepo(custA, custB)
	engine := &fakeScreeningEngine{}

	results := store.NewMemoryScreeningResultRepo()
	// cust-a already has a screening_results row screened AFTER batchStart:
	// an immediate rescreen (e.g. name change) beat the batch to it.
	if err := results.Create(context.Background(), &domain.ScreeningResultRecord{
		ID: "sr-1", CustomerID: "cust-a", ListID: "mof_japan", ListType: "sanctions",
		EntryID: "MOF-001", MatchedName: "x", Similarity: 0.9,
		Status: domain.ScreeningResultStatusNew, ScreenedAt: batchStart.Add(time.Minute), CreatedAt: batchStart.Add(time.Minute),
	}); err != nil {
		t.Fatalf("seed screening result: %v", err)
	}

	deps := SchedulerDeps{
		Customers: repo,
		Screening: engine,
		Results:   results,
		Now:       func() time.Time { return batchStart },
	}

	result, err := RunRescreeningBatch(context.Background(), deps, TriggerListUpdated)
	if err != nil {
		t.Fatalf("RunRescreeningBatch: %v", err)
	}

	var aOutcome, bOutcome *CustomerScreenOutcome
	for i := range result.Outcomes {
		switch result.Outcomes[i].CustomerID {
		case "cust-a":
			aOutcome = &result.Outcomes[i]
		case "cust-b":
			bOutcome = &result.Outcomes[i]
		}
	}
	if aOutcome == nil || !aOutcome.Skipped || aOutcome.SkipReason != "immediate_rescreen_duplicate" {
		t.Errorf("cust-a outcome = %+v, want skipped due to immediate rescreen duplicate", aOutcome)
	}
	if bOutcome == nil || !bOutcome.Screened {
		t.Errorf("cust-b outcome = %+v, want screened normally", bOutcome)
	}
	for _, id := range engine.callOrder() {
		if id == "cust-a" {
			t.Error("engine.ScreenCustomer was called for cust-a, want it skipped in favor of the immediate rescreen")
		}
	}
}

func TestRunRescreeningBatch_PriorityOrderHighMediumLow(t *testing.T) {
	// Deliberately scrambled input order.
	low := domain.Customer{ID: "cust-low", RiskTier: riskTier(domain.RiskTierLow)}
	high := domain.Customer{ID: "cust-high", RiskTier: riskTier(domain.RiskTierHigh)}
	medium := domain.Customer{ID: "cust-medium", RiskTier: riskTier(domain.RiskTierMedium)}
	repo := newFakeCustomerRepo(low, high, medium)
	engine := &fakeScreeningEngine{}

	deps := SchedulerDeps{
		Customers: repo,
		Screening: engine,
		Results:   store.NewMemoryScreeningResultRepo(),
		Now:       func() time.Time { return time.Now() },
	}

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerListUpdated); err != nil {
		t.Fatalf("RunRescreeningBatch: %v", err)
	}

	want := []string{"cust-high", "cust-medium", "cust-low"}
	got := engine.callOrder()
	if len(got) != len(want) {
		t.Fatalf("callOrder = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("callOrder = %v, want %v (High -> Medium -> Low)", got, want)
		}
	}
}

// TestDormantCustomerContinuesScreeningSkipsTMWithoutTransaction verifies the
// screening half of the data model §1.1.2's per-status table: closed
// customers stop periodic rescreening entirely, while dormant customers
// continue to be screened (undetected sanctions listing during a dormant
// period is exactly what this cadence exists to catch). The TM half — that
// dormant is evaluated only "取引発生時" rather than on the scheduled batch —
// is covered separately in
// api/internal/batch/tm_batch_job_test.go:TestRunTMBatchEvaluation_SkipsClosedAndDormantCustomers.
func TestDormantCustomerContinuesScreeningSkipsTMWithoutTransaction(t *testing.T) {
	active := domain.Customer{ID: "cust-active", RiskTier: riskTier(domain.RiskTierLow), Status: domain.CustomerStatusActive}
	dormant := domain.Customer{ID: "cust-dormant", RiskTier: riskTier(domain.RiskTierLow), Status: domain.CustomerStatusDormant}
	closed := domain.Customer{ID: "cust-closed", RiskTier: riskTier(domain.RiskTierLow), Status: domain.CustomerStatusClosed}
	repo := newFakeCustomerRepo(active, dormant, closed)
	engine := &fakeScreeningEngine{}

	deps := SchedulerDeps{
		Customers: repo,
		Screening: engine,
		Results:   store.NewMemoryScreeningResultRepo(),
		Now:       func() time.Time { return time.Now() },
	}

	if _, err := RunRescreeningBatch(context.Background(), deps, TriggerListUpdated); err != nil {
		t.Fatalf("RunRescreeningBatch: %v", err)
	}

	screened := map[string]bool{}
	for _, id := range engine.callOrder() {
		screened[id] = true
	}
	if !screened["cust-active"] {
		t.Error("active customer was not screened, want screened")
	}
	if !screened["cust-dormant"] {
		t.Error("dormant customer was not screened, want screened (periodic rescreening continues while dormant)")
	}
	if screened["cust-closed"] {
		t.Error("closed customer was screened, want excluded")
	}
}

// --- Scheduler: serialized queue, no concurrent/duplicate batch runs ---

func TestRunRescreeningBatch_ListUpdateDuringBatchQueuesNotRestarts(t *testing.T) {
	var running int32
	var maxConcurrent int32
	var runCount int32
	unblock := make(chan struct{})
	firstCallStarted := make(chan struct{})

	runFn := func(_ context.Context, _ SchedulerDeps, _ TriggerType) (BatchResult, error) {
		n := atomic.AddInt32(&running, 1)
		for {
			cur := atomic.LoadInt32(&maxConcurrent)
			if n <= cur || atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
				break
			}
		}
		call := atomic.AddInt32(&runCount, 1)
		if call == 1 {
			close(firstCallStarted)
			<-unblock
		}
		atomic.AddInt32(&running, -1)
		return BatchResult{}, nil
	}

	sched := newTestScheduler(runFn)

	done := make(chan struct{})
	go func() {
		sched.Trigger(context.Background(), TriggerListUpdated)
		close(done)
	}()

	<-firstCallStarted
	// Multiple list updates arrive while the batch is still running; none
	// of them should start a second concurrent batch.
	sched.Trigger(context.Background(), TriggerListUpdated)
	sched.Trigger(context.Background(), TriggerListUpdated)
	sched.Trigger(context.Background(), TriggerListUpdated)

	close(unblock)
	<-done

	if got := atomic.LoadInt32(&maxConcurrent); got > 1 {
		t.Errorf("max concurrent batch runs = %d, want at most 1", got)
	}
	if got := atomic.LoadInt32(&runCount); got != 2 {
		t.Errorf("total batch runs = %d, want exactly 2 (initial + one collapsed queued run)", got)
	}
}

func TestScheduler_RunPeriodicStopsOnContextCancellation(t *testing.T) {
	runFn := func(_ context.Context, _ SchedulerDeps, _ TriggerType) (BatchResult, error) {
		return BatchResult{}, nil
	}
	sched := newTestScheduler(runFn)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		sched.RunPeriodic(ctx, time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPeriodic did not return after context cancellation")
	}
}
