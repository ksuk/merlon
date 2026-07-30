package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestSetup_FirstCallSucceeds(t *testing.T) {
	users := store.NewMemoryUserRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     users,
	})

	body := `{"email":"admin@example.com","password":"correct-horse-battery-staple"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp meResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Role != domain.RoleAdmin {
		t.Errorf("role = %s, want %s", resp.Role, domain.RoleAdmin)
	}

	count, err := users.Count(req.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}
}

func TestSetup_SecondCallRejected(t *testing.T) {
	users := store.NewMemoryUserRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     users,
	})

	body := `{"email":"admin@example.com","password":"correct-horse-battery-staple"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first setup status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	body = `{"email":"second-admin@example.com","password":"correct-horse-battery-staple"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("second setup status = %d, want %d", rec.Code, http.StatusConflict)
	}
	assertErrorCode(t, rec, "conflict")

	var resp errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error != "initial setup has already been completed" {
		t.Errorf("error = %q, want exact setup conflict message", resp.Error)
	}
}

type setupCountBarrierRepo struct {
	domain.UserRepository

	mu      sync.Mutex
	arrived int
	release chan struct{}
}

func newSetupCountBarrierRepo(repo domain.UserRepository) *setupCountBarrierRepo {
	return &setupCountBarrierRepo{
		UserRepository: repo,
		release:        make(chan struct{}),
	}
}

// Count captures the empty-table result before releasing either request. This
// deterministically reproduces two setup handlers passing the early Count
// optimization concurrently.
func (r *setupCountBarrierRepo) Count(ctx context.Context) (int, error) {
	count, err := r.UserRepository.Count(ctx)
	if err != nil {
		return 0, err
	}

	r.mu.Lock()
	r.arrived++
	if r.arrived == 2 {
		close(r.release)
	}
	release := r.release
	r.mu.Unlock()

	select {
	case <-release:
		return count, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestSetup_ConcurrentCallsCreateExactlyOneAdministrator(t *testing.T) {
	memoryUsers := store.NewMemoryUserRepo()
	users := newSetupCountBarrierRepo(memoryUsers)
	audit := store.NewMemoryAuditRepo()
	s := New(":0", Deps{
		Customers: store.NewMemoryCustomerRepo(),
		Users:     users,
		Audit:     audit,
	})
	handler := s.Handler()

	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for _, email := range []string{"first@example.com", "second@example.com"} {
		email := email
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body := `{"email":"` + email + `","password":"correct-horse-battery-staple"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			responses <- rec
		}()
	}
	close(start)
	wg.Wait()
	close(responses)

	statusCounts := map[int]int{}
	for rec := range responses {
		statusCounts[rec.Code]++
		if rec.Code == http.StatusConflict {
			assertErrorCode(t, rec, "conflict")
		}
	}
	if statusCounts[http.StatusCreated] != 1 || statusCounts[http.StatusConflict] != 1 {
		t.Errorf("status counts = %v, want one 201 and one 409", statusCounts)
	}

	count, err := memoryUsers.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("user count = %d, want 1", count)
	}

	entries, err := audit.List(context.Background(), domain.AuditListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List audit entries: %v", err)
	}
	initialSetupCount := 0
	for _, entry := range entries {
		if entry.Action == "initial_setup" {
			initialSetupCount++
		}
	}
	if initialSetupCount != 1 {
		t.Errorf("initial_setup audit count = %d, want 1", initialSetupCount)
	}
}
