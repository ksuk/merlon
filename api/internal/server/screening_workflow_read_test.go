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

// The durable screening read surface -- runs, results, and result history --
// shipped without a single Go test. These cover the filters and the cursor
// contract each route advertises, because an ignored filter here silently
// widens or narrows what a reviewer believes they are looking at.

type screeningReadFixture struct {
	server    *Server
	wave3     *store.MemoryWave3Repo
	customerA string
	customerB string
	// resultIDs and runIDs are in creation order, oldest first.
	resultIDs []string
	runIDs    []string
}

func newScreeningReadFixture(t *testing.T) *screeningReadFixture {
	t.Helper()
	ctx := context.Background()
	customers := store.NewMemoryCustomerRepo()
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{Customers: customers, Wave3: wave3, Audit: store.NewMemoryAuditRepo()})

	fixture := &screeningReadFixture{
		server:    s,
		wave3:     wave3,
		customerA: "00000000000000000000000000000a01",
		customerB: "00000000000000000000000000000b01",
	}
	for _, id := range []string{fixture.customerA, fixture.customerB} {
		if err := customers.Create(ctx, &domain.Customer{ID: id, ExternalID: "screening-read-" + id, CustomerType: domain.CustomerTypeIndividual, CountryCode: "JP", Status: domain.CustomerStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}

	// Five runs, each with one result, spaced a minute apart so the keyset
	// order is unambiguous. Customer A owns runs 0,2,4; customer B owns 1,3.
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		customerID := fixture.customerA
		if i%2 == 1 {
			customerID = fixture.customerB
		}
		at := base.Add(time.Duration(i) * time.Minute)
		runID := fmt.Sprintf("000000000000000000000000000run%02d", i)
		resultID := fmt.Sprintf("000000000000000000000000000res%02d", i)
		status := domain.ScreeningResultStatusNew
		if i == 3 {
			status = domain.ScreeningResultStatusReviewing
		}
		listID := "ofac_sdn"
		if i >= 3 {
			listID = "un_sc"
		}
		run := &domain.ScreeningRun{ID: runID, CustomerID: customerID, Status: domain.ScreeningRunCompleted, ListIDs: []string{listID}, StartedAt: at, CreatedAt: at}
		result := domain.ScreeningResultRecord{
			ID: resultID, CustomerID: customerID, ListID: listID, ListType: "sanctions",
			EntryID: fmt.Sprintf("entry-%d", i), MatchedName: "Example Person", Status: status,
			Suppressed: i == 4, ScreenedAt: at, CreatedAt: at,
		}
		if i == 4 {
			result.SuppressionReason = "prior_false_positive:000000000000000000000000000res00"
		}
		if err := wave3.PersistScreeningRun(ctx, run, []domain.ScreeningResultRecord{result}); err != nil {
			t.Fatal(err)
		}
		fixture.runIDs = append(fixture.runIDs, runID)
		fixture.resultIDs = append(fixture.resultIDs, resultID)
	}
	return fixture
}

func (f *screeningReadFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

type paginatedResults struct {
	Data       []domain.ScreeningResultRecord `json:"data"`
	Pagination PaginationMeta                 `json:"pagination"`
}

type paginatedRuns struct {
	Data       []domain.ScreeningRun `json:"data"`
	Pagination PaginationMeta        `json:"pagination"`
}

func (f *screeningReadFixture) listResults(t *testing.T, query string) paginatedResults {
	t.Helper()
	rec := f.get(t, "/api/v1/screening/results"+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET results%s = %d, body=%s", query, rec.Code, rec.Body.String())
	}
	var out paginatedResults
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode results%s: %v", query, err)
	}
	return out
}

func resultIDsOf(items []domain.ScreeningResultRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func TestListScreeningResultsFilters(t *testing.T) {
	f := newScreeningReadFixture(t)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		// Newest first throughout, matching the created_at DESC, id DESC keyset.
		{"unfiltered returns every result", "", []string{"res04", "res03", "res02", "res01", "res00"}},
		{"customer_id", "?customer_id=" + f.customerB, []string{"res03", "res01"}},
		{"status", "?status=REVIEWING", []string{"res03"}},
		{"list_id", "?list_id=un_sc", []string{"res04", "res03"}},
		{"suppressed=true", "?suppressed=true", []string{"res04"}},
		{"suppressed=false", "?suppressed=false", []string{"res03", "res02", "res01", "res00"}},
		{"from", "?from=2026-08-01T12:03:00Z", []string{"res04", "res03"}},
		{"to", "?to=2026-08-01T12:01:00Z", []string{"res01", "res00"}},
		{"combined customer and list", "?customer_id=" + f.customerA + "&list_id=ofac_sdn", []string{"res02", "res00"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resultIDsOf(f.listResults(t, tc.query).Data)
			want := make([]string, 0, len(tc.want))
			for _, suffix := range tc.want {
				want = append(want, "000000000000000000000000000"+suffix)
			}
			if len(got) != len(want) {
				t.Fatalf("ids = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("ids = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestListScreeningResultsRejectsInvalidFilters(t *testing.T) {
	f := newScreeningReadFixture(t)
	for _, query := range []string{"?suppressed=maybe", "?from=yesterday", "?to=2026-13-45", "?cursor=not-base64"} {
		rec := f.get(t, "/api/v1/screening/results"+query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET results%s = %d, want 400", query, rec.Code)
		}
	}
}

func TestListScreeningResultsCursorPaginationCoversEveryRowOnce(t *testing.T) {
	f := newScreeningReadFixture(t)

	seen := []string{}
	query := "?limit=2"
	for page := 0; page < 5; page++ {
		out := f.listResults(t, query)
		if len(out.Data) == 0 {
			t.Fatalf("page %d returned no rows", page)
		}
		seen = append(seen, resultIDsOf(out.Data)...)
		if !out.Pagination.HasMore {
			break
		}
		if out.Pagination.NextCursor == "" {
			t.Fatalf("page %d has_more with no next_cursor", page)
		}
		query = "?limit=2&cursor=" + out.Pagination.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("paged ids = %v, want all 5 results exactly once", seen)
	}
	unique := map[string]bool{}
	for _, id := range seen {
		if unique[id] {
			t.Fatalf("id %s returned on two pages: %v", id, seen)
		}
		unique[id] = true
	}
}

func TestGetScreeningResult(t *testing.T) {
	f := newScreeningReadFixture(t)

	rec := f.get(t, "/api/v1/screening/results/"+f.resultIDs[4])
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got domain.ScreeningResultRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != f.resultIDs[4] || !got.Suppressed || got.SuppressionReason == "" {
		t.Fatalf("result = %+v, want the suppressed row with its reason", got)
	}

	missing := f.get(t, "/api/v1/screening/results/00000000000000000000000000000zzz")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown result status = %d, want 404", missing.Code)
	}
}

func TestListScreeningResultHistoryIsOldestFirst(t *testing.T) {
	f := newScreeningReadFixture(t)
	ctx := context.Background()
	id := f.resultIDs[0]

	if _, err := f.wave3.ReviewScreeningResult(ctx, id, domain.ScreeningResultStatusReviewing, "under review", "analyst", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.wave3.ReviewScreeningResult(ctx, id, domain.ScreeningResultStatusFalsePositive, "different date of birth", "analyst", 2); err != nil {
		t.Fatal(err)
	}

	rec := f.get(t, "/api/v1/screening/results/"+id+"/history")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var history []domain.ScreeningResultHistoryEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history = %d entries, want 2", len(history))
	}
	if history[0].ToStatus != domain.ScreeningResultStatusReviewing || history[1].ToStatus != domain.ScreeningResultStatusFalsePositive {
		t.Fatalf("history order = %q then %q, want oldest transition first", history[0].ToStatus, history[1].ToStatus)
	}
	if history[0].Version != 2 || history[1].Version != 3 {
		t.Fatalf("history versions = %d,%d, want the post-transition versions 2,3", history[0].Version, history[1].Version)
	}

	// A result with no transitions must return an empty array, never null:
	// the UI iterates the response directly.
	empty := f.get(t, "/api/v1/screening/results/"+f.resultIDs[1]+"/history")
	if empty.Code != http.StatusOK || empty.Body.String() != "[]\n" {
		t.Fatalf("empty history = %d %q, want 200 []", empty.Code, empty.Body.String())
	}
}

func TestListScreeningRunsFilterAndPagination(t *testing.T) {
	f := newScreeningReadFixture(t)

	all := f.get(t, "/api/v1/screening/runs")
	if all.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", all.Code, all.Body.String())
	}
	var runs paginatedRuns
	if err := json.Unmarshal(all.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs.Data) != 5 {
		t.Fatalf("runs = %d, want 5", len(runs.Data))
	}
	if runs.Data[0].ID != f.runIDs[4] {
		t.Fatalf("first run = %s, want the newest %s", runs.Data[0].ID, f.runIDs[4])
	}

	filtered := f.get(t, "/api/v1/screening/runs?customer_id="+f.customerB)
	var byCustomer paginatedRuns
	if err := json.Unmarshal(filtered.Body.Bytes(), &byCustomer); err != nil {
		t.Fatal(err)
	}
	if len(byCustomer.Data) != 2 {
		t.Fatalf("customer B runs = %d, want 2", len(byCustomer.Data))
	}
	for _, run := range byCustomer.Data {
		if run.CustomerID != f.customerB {
			t.Fatalf("customer_id filter leaked run for %s", run.CustomerID)
		}
	}

	page := f.get(t, "/api/v1/screening/runs?limit=3")
	var firstPage paginatedRuns
	if err := json.Unmarshal(page.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Data) != 3 || !firstPage.Pagination.HasMore {
		t.Fatalf("first page = %d rows has_more=%v, want 3 and true", len(firstPage.Data), firstPage.Pagination.HasMore)
	}
	next := f.get(t, "/api/v1/screening/runs?limit=3&cursor="+firstPage.Pagination.NextCursor)
	var secondPage paginatedRuns
	if err := json.Unmarshal(next.Body.Bytes(), &secondPage); err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Data) != 2 || secondPage.Pagination.HasMore {
		t.Fatalf("second page = %d rows has_more=%v, want 2 and false", len(secondPage.Data), secondPage.Pagination.HasMore)
	}
	if secondPage.Data[0].ID == firstPage.Data[2].ID {
		t.Fatal("second page repeated the last row of the first page")
	}
}

func TestGetScreeningRun(t *testing.T) {
	f := newScreeningReadFixture(t)

	rec := f.get(t, "/api/v1/screening/runs/"+f.runIDs[0])
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var run domain.ScreeningRun
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.ID != f.runIDs[0] || run.ResultCount != 1 {
		t.Fatalf("run = %+v, want run %s with one result", run, f.runIDs[0])
	}

	missing := f.get(t, "/api/v1/screening/runs/00000000000000000000000000000zzz")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown run status = %d, want 404", missing.Code)
	}
}

func TestScreeningReadRoutesReportUnconfiguredWorkflow(t *testing.T) {
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo()})
	for _, path := range []string{
		"/api/v1/screening/runs",
		"/api/v1/screening/runs/x",
		"/api/v1/screening/results",
		"/api/v1/screening/results/x",
		"/api/v1/screening/results/x/history",
	} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d, want 503 without a durable workflow store", path, rec.Code)
		}
	}
}
