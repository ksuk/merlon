package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

type affectedCustomersResponse struct {
	Data       []string                            `json:"data"`
	DeltaKinds map[string]domain.BacktestDeltaKind `json:"delta_kinds"`
	Rows       []domain.BacktestAffectedCustomer   `json:"rows"`
	Pagination struct {
		Limit      int    `json:"limit"`
		Total      int    `json:"total"`
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"pagination"`
}

// completedBacktestJob drives a job through the repository lifecycle so the
// durable affected-customer rows are written exactly as the worker writes them.
func completedBacktestJob(t *testing.T, jobs *store.MemoryBacktestJobRepo, candidate, delta *domain.BacktestResult) string {
	t.Helper()
	ctx := context.Background()
	job := &domain.BacktestJob{ID: generateID(), Status: domain.BacktestJobQueued, From: time.Now().Add(-24 * time.Hour), To: time.Now(), BaselineRuleSetID: "active", CandidateRuleSetID: "candidate"}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Complete(ctx, job.ID, &domain.BacktestResult{}, candidate, delta); err != nil {
		t.Fatal(err)
	}
	return job.ID
}

func customerID(n int) string { return fmt.Sprintf("cust-%03d", n) }

func getAffected(t *testing.T, s *Server, path string) affectedCustomersResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, body=%s", path, rec.Code, rec.Body.String())
	}
	var out affectedCustomersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBacktestAffectedCustomersReportsDeltaKinds(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), BacktestJobs: jobs})

	candidate := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "structuring", AffectedCustomerIDs: []string{customerID(1), customerID(2)}},
		{ScenarioID: "rapid_movement", AffectedCustomerIDs: []string{customerID(2)}},
	}}
	delta := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "structuring", AddedCustomerIDs: []string{customerID(2)}, RemovedCustomerIDs: []string{customerID(3)}},
	}}
	jobID := completedBacktestJob(t, jobs, candidate, delta)

	got := getAffected(t, s, "/api/v1/backtests/"+jobID+"/affected-customers")
	// customer 3 alerted only under the baseline, so it appears in no candidate
	// scenario result and would be invisible without the delta's removed set.
	want := []string{customerID(1), customerID(2), customerID(3)}
	if len(got.Data) != len(want) {
		t.Fatalf("data = %v, want %v", got.Data, want)
	}
	for i := range want {
		if got.Data[i] != want[i] {
			t.Fatalf("data = %v, want %v in customer order", got.Data, want)
		}
	}
	if got.DeltaKinds[customerID(1)] != domain.BacktestDeltaUnchanged {
		t.Errorf("customer 1 kind = %q, want unchanged", got.DeltaKinds[customerID(1)])
	}
	if got.DeltaKinds[customerID(2)] != domain.BacktestDeltaAdded {
		t.Errorf("customer 2 kind = %q, want added: it is new under structuring", got.DeltaKinds[customerID(2)])
	}
	if got.DeltaKinds[customerID(3)] != domain.BacktestDeltaRemoved {
		t.Errorf("customer 3 kind = %q, want removed", got.DeltaKinds[customerID(3)])
	}
	if got.Pagination.Total != 3 {
		t.Errorf("total = %d, want 3 distinct customers", got.Pagination.Total)
	}
}

func TestBacktestAffectedCustomersFilterByScenario(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), BacktestJobs: jobs})

	candidate := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "structuring", AffectedCustomerIDs: []string{customerID(1)}},
		{ScenarioID: "rapid_movement", AffectedCustomerIDs: []string{customerID(2)}},
	}}
	jobID := completedBacktestJob(t, jobs, candidate, &domain.BacktestResult{})

	got := getAffected(t, s, "/api/v1/backtests/"+jobID+"/affected-customers?scenario_id=rapid_movement")
	if len(got.Data) != 1 || got.Data[0] != customerID(2) {
		t.Fatalf("data = %v, want only the rapid_movement customer", got.Data)
	}
	if got.Pagination.Total != 1 {
		t.Errorf("total = %d, want the filtered count, not the job total", got.Pagination.Total)
	}
	for _, row := range got.Rows {
		if row.ScenarioID != "rapid_movement" {
			t.Errorf("row = %+v leaked past the scenario filter", row)
		}
	}
}

func TestBacktestAffectedCustomersKeysetPagesEveryCustomerOnce(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), BacktestJobs: jobs})

	ids := make([]string, 0, 25)
	for i := 1; i <= 25; i++ {
		ids = append(ids, customerID(i))
	}
	candidate := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		// Two scenarios over the same population: a customer holds several
		// rows, so a page boundary must fall between customers, not rows.
		{ScenarioID: "structuring", AffectedCustomerIDs: ids},
		{ScenarioID: "rapid_movement", AffectedCustomerIDs: ids},
	}}
	jobID := completedBacktestJob(t, jobs, candidate, &domain.BacktestResult{})

	seen := []string{}
	path := "/api/v1/backtests/" + jobID + "/affected-customers?limit=10"
	for page := 0; page < 5; page++ {
		got := getAffected(t, s, path)
		if got.Pagination.Total != 25 {
			t.Fatalf("page %d total = %d, want 25 distinct customers", page, got.Pagination.Total)
		}
		seen = append(seen, got.Data...)
		if !got.Pagination.HasMore {
			break
		}
		if got.Pagination.NextCursor == "" {
			t.Fatalf("page %d claimed more rows without a cursor", page)
		}
		path = "/api/v1/backtests/" + jobID + "/affected-customers?limit=10&cursor=" + got.Pagination.NextCursor
	}
	if len(seen) != 25 {
		t.Fatalf("paged %d customers, want 25 exactly once: %v", len(seen), seen)
	}
	unique := map[string]bool{}
	for _, id := range seen {
		if unique[id] {
			t.Fatalf("customer %s appeared on two pages", id)
		}
		unique[id] = true
	}
}

// The same job inputs must produce a byte-identical population page. This is
// the guarantee an operator relies on when re-reviewing a comparison months
// after it ran.
func TestBacktestAffectedCustomersAreDeterministic(t *testing.T) {
	candidate := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "rapid_movement", AffectedCustomerIDs: []string{customerID(9), customerID(2), customerID(5)}},
		{ScenarioID: "structuring", AffectedCustomerIDs: []string{customerID(5), customerID(1)}},
	}}
	delta := &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
		{ScenarioID: "structuring", AddedCustomerIDs: []string{customerID(1)}, RemovedCustomerIDs: []string{customerID(7)}},
	}}

	var first string
	for range 5 {
		jobs := store.NewMemoryBacktestJobRepo()
		s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), BacktestJobs: jobs})
		jobID := completedBacktestJob(t, jobs, candidate, delta)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/backtests/"+jobID+"/affected-customers", nil))
		// The job id varies per run, so compare everything except it.
		var body affectedCustomersResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for i := range body.Rows {
			body.Rows[i].JobID = ""
		}
		normalized, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = string(normalized)
			continue
		}
		if string(normalized) != first {
			t.Fatalf("identical inputs produced different pages:\n%s\n%s", first, normalized)
		}
	}
}

// Jobs completed before the durable rows existed must keep answering from
// their stored result rather than reporting an empty population.
func TestBacktestAffectedCustomersFallBackToStoredResult(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), BacktestJobs: jobs})
	ctx := context.Background()
	job := &domain.BacktestJob{
		ID: generateID(), Status: domain.BacktestJobCompleted,
		Candidate: &domain.BacktestResult{ScenarioResults: []domain.BacktestScenarioResult{
			{ScenarioID: "structuring", AffectedCustomerIDs: []string{customerID(4), customerID(8)}},
		}},
	}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}

	got := getAffected(t, s, "/api/v1/backtests/"+job.ID+"/affected-customers")
	if len(got.Data) != 2 || got.Data[0] != customerID(4) {
		t.Fatalf("data = %v, want the legacy job's stored population", got.Data)
	}
}

func TestDiscoverBacktestRulesListsCandidateScenarios(t *testing.T) {
	rules := store.NewMemoryRuleRepo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Rules: rules})
	ctx := context.Background()
	for i := range 3 {
		rule := &domain.RuleDefinition{
			ID: fmt.Sprintf("scenario-%d", i), Name: fmt.Sprintf("Scenario %d", i),
			Type: domain.RuleTypeTMScenario, Version: 1, IsActive: true,
			Definition: json.RawMessage(`{}`), CreatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		}
		if err := rules.Create(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	// A CDD weight set must not be offered as a backtest candidate.
	if err := rules.Create(ctx, &domain.RuleDefinition{ID: "weights", Name: "CDD weights", Type: domain.RuleTypeCDDWeight, Version: 1, IsActive: true, Definition: json.RawMessage(`{}`), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/backtests/rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data       []domain.RuleDefinition `json:"data"`
		Pagination PaginationMeta          `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 3 {
		t.Fatalf("data = %d rules, want the 3 TM scenarios only", len(body.Data))
	}
	for _, rule := range body.Data {
		if rule.Type != domain.RuleTypeTMScenario {
			t.Errorf("rule %s has type %q; only TM scenarios can be compared", rule.ID, rule.Type)
		}
	}

	paged := httptest.NewRecorder()
	s.Handler().ServeHTTP(paged, httptest.NewRequest(http.MethodGet, "/api/v1/backtests/rules?limit=2", nil))
	var firstPage struct {
		Data       []domain.RuleDefinition `json:"data"`
		Pagination PaginationMeta          `json:"pagination"`
	}
	if err := json.Unmarshal(paged.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Data) != 2 || !firstPage.Pagination.HasMore || firstPage.Pagination.NextCursor == "" {
		t.Fatalf("first page = %d rows, has_more=%v", len(firstPage.Data), firstPage.Pagination.HasMore)
	}
}

func TestDiscoverBacktestRulesRequiresRuleRepository(t *testing.T) {
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo()})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/backtests/rules", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
