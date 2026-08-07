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

	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

func seedTargetCustomer(t *testing.T, customers *store.MemoryCustomerRepo, id, country string, createdAt time.Time) {
	t.Helper()
	err := customers.Create(context.Background(), &domain.Customer{
		ID: id, ExternalID: "target-" + id, CustomerType: domain.CustomerTypeIndividual,
		CountryCode: country, Status: domain.CustomerStatusActive,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func previewManifest(t *testing.T, s *Server, body string) *domain.TargetManifest {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/preview", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("preview = %d, body=%s", rec.Code, rec.Body.String())
	}
	var manifest domain.TargetManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	return &manifest
}

// The defect this pins: the resolver read one page of 10,001 customers and
// filtered that, so a match beyond the first page was dropped without any
// error. Twenty-five customers with a page size of ten reproduce it exactly:
// only the 23rd matches, so a single-page resolver returns an empty manifest.
func TestTargetPreviewFindsMatchesBeyondTheFirstPage(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	s := New(":0", Deps{Customers: customers, Wave3: store.NewMemoryWave3Repo(), Audit: store.NewMemoryAuditRepo()})
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	var wanted string
	for i := 1; i <= 25; i++ {
		id := fmt.Sprintf("000000000000000000000000000t%03d", i)
		country := "JP"
		if i == 23 {
			country = "KP"
			wanted = id
		}
		seedTargetCustomer(t, customers, id, country, base.Add(time.Duration(i)*time.Minute))
	}

	manifest := previewManifest(t, s, `{"operation":"batch_score","target_mode":"filter","filter":{"country_code":"KP"},"rationale":"sanctioned jurisdiction review"}`)
	if manifest.TargetCount != 1 {
		t.Fatalf("target_count = %d, want 1", manifest.TargetCount)
	}
	if len(manifest.CustomerIDs) != 1 || manifest.CustomerIDs[0] != wanted {
		t.Fatalf("customer_ids = %v, want [%s]", manifest.CustomerIDs, wanted)
	}
}

func TestTargetPreviewCoversTheWholeBookForModeAll(t *testing.T) {
	customers := store.NewMemoryCustomerRepo()
	s := New(":0", Deps{Customers: customers, Wave3: store.NewMemoryWave3Repo(), Audit: store.NewMemoryAuditRepo()})
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	// More than one resolution page, so a single-page resolver would under-count.
	const total = targetResolutionPageSize + 37
	for i := range total {
		seedTargetCustomer(t, customers, fmt.Sprintf("0000000000000000000000000a%06d", i), "JP", base.Add(time.Duration(i)*time.Second))
	}

	manifest := previewManifest(t, s, `{"operation":"batch_score","target_mode":"all","rationale":"annual rescore"}`)
	if manifest.TargetCount != total {
		t.Fatalf("target_count = %d, want the whole book of %d", manifest.TargetCount, total)
	}
	seen := map[string]bool{}
	for _, id := range manifest.CustomerIDs {
		if seen[id] {
			t.Fatalf("customer %s resolved twice; the keyset walk overlapped a page", id)
		}
		seen[id] = true
	}
}

func TestTargetPreviewRejectsAnOversizedMatchSet(t *testing.T) {
	customers := &countingCustomerRepo{MemoryCustomerRepo: store.NewMemoryCustomerRepo(), synthetic: maxTargetMatches + 5}
	s := New(":0", Deps{Customers: customers, Wave3: store.NewMemoryWave3Repo(), Audit: store.NewMemoryAuditRepo()})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/preview", strings.NewReader(`{"operation":"batch_score","target_mode":"all","rationale":"too many"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a match set over the cap", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "10000") {
		t.Errorf("body = %s, want the cap named", rec.Body.String())
	}
}

func TestTargetPreviewRejectsAnUnscannablePopulation(t *testing.T) {
	// Every customer is scanned but none match, which is what an over-narrow
	// filter over an enormous book looks like.
	customers := &countingCustomerRepo{MemoryCustomerRepo: store.NewMemoryCustomerRepo(), synthetic: maxTargetScan + targetResolutionPageSize, country: "JP"}
	s := New(":0", Deps{Customers: customers, Wave3: store.NewMemoryWave3Repo(), Audit: store.NewMemoryAuditRepo()})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/preview", strings.NewReader(`{"operation":"batch_score","target_mode":"filter","filter":{"country_code":"KP"},"rationale":"needle in a haystack"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want an explicit 400 rather than a silently partial manifest", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "narrow the filter") {
		t.Errorf("body = %s, want an actionable message", rec.Body.String())
	}
}

// countingCustomerRepo serves a synthetic book without materialising it, so a
// 200,000-row scan does not need 200,000 stored customers.
type countingCustomerRepo struct {
	*store.MemoryCustomerRepo
	synthetic int
	country   string
	served    int
	mu        sync.Mutex
}

func (r *countingCustomerRepo) ListByCursor(_ context.Context, limit int, _ *domain.Cursor) ([]domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.served >= r.synthetic {
		return nil, nil
	}
	country := r.country
	if country == "" {
		country = "JP"
	}
	out := make([]domain.Customer, 0, limit)
	for range limit {
		if r.served >= r.synthetic {
			break
		}
		r.served++
		out = append(out, domain.Customer{
			ID: fmt.Sprintf("%032d", r.served), ExternalID: fmt.Sprintf("synthetic-%d", r.served),
			CustomerType: domain.CustomerTypeIndividual, CountryCode: country,
			Status: domain.CustomerStatusActive, CreatedAt: time.Unix(int64(r.served), 0).UTC(),
		})
	}
	return out, nil
}

// largeManifest stores a confirmed-ready manifest above the dual-control
// threshold without paying to resolve one.
func largeManifest(t *testing.T, wave3 *store.MemoryWave3Repo, createdBy string) *domain.TargetManifest {
	t.Helper()
	manifest := &domain.TargetManifest{
		ID: generateID(), Operation: "batch_score", TargetMode: domain.TargetModeAll,
		TargetCount: largeBatchThreshold + 1, Token: "large-token", Status: "preview", Version: 1,
		ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedBy: createdBy, CreatedAt: time.Now().UTC(),
	}
	if err := wave3.CreateTargetManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func confirmRequest(t *testing.T, s *Server, manifest *domain.TargetManifest, role domain.Role, actor string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"token":%q,"rationale":"reviewed the population","expected_version":%d}`, manifest.Token, manifest.Version)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/"+manifest.ID+"/confirm", strings.NewReader(body))
	if role != "" {
		req = req.WithContext(auth.WithRole(req.Context(), role))
	}
	if actor != "" {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyPrincipal, Principal{UserID: actor}))
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLargeTargetConfirmationRequiresThePermission(t *testing.T) {
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: wave3, Audit: store.NewMemoryAuditRepo()})
	manifest := largeManifest(t, wave3, "analyst-1")

	denied := confirmRequest(t, s, manifest, domain.RoleAnalyst, "analyst-2")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("analyst confirming a large batch = %d, want 403", denied.Code)
	}

	allowed := confirmRequest(t, s, manifest, domain.RoleAdmin, "admin-1")
	if allowed.Code != http.StatusOK {
		t.Fatalf("admin confirming a large batch = %d, body=%s", allowed.Code, allowed.Body.String())
	}
}

func TestLargeTargetConfirmationRejectsTheOperatorWhoPreviewedIt(t *testing.T) {
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: wave3, Audit: store.NewMemoryAuditRepo()})
	manifest := largeManifest(t, wave3, "admin-1")

	self := confirmRequest(t, s, manifest, domain.RoleAdmin, "admin-1")
	if self.Code != http.StatusForbidden {
		t.Fatalf("self-confirmation = %d, want 403", self.Code)
	}
	other := confirmRequest(t, s, manifest, domain.RoleAdmin, "admin-2")
	if other.Code != http.StatusOK {
		t.Fatalf("second operator confirming = %d, body=%s", other.Code, other.Body.String())
	}
}

// A small manifest keeps the everyday single-operator workflow.
func TestSmallTargetConfirmationIsUnaffectedByDualControl(t *testing.T) {
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: wave3, Audit: store.NewMemoryAuditRepo()})
	manifest := &domain.TargetManifest{
		ID: generateID(), Operation: "batch_score", TargetMode: domain.TargetModeSelected,
		CustomerIDs: []string{"c1"}, TargetCount: 1, Token: "small-token", Status: "preview", Version: 1,
		ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedBy: "analyst-1", CreatedAt: time.Now().UTC(),
	}
	if err := wave3.CreateTargetManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	rec := confirmRequest(t, s, manifest, domain.RoleAnalyst, "analyst-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("small self-confirmation = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestTargetConfirmationRejectsExpiredAndStaleTokens(t *testing.T) {
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: wave3, Audit: store.NewMemoryAuditRepo()})
	ctx := context.Background()

	expired := &domain.TargetManifest{ID: generateID(), Operation: "batch_score", TargetMode: domain.TargetModeAll, TargetCount: 2, Token: "expired-token", Status: "preview", Version: 1, ExpiresAt: time.Now().UTC().Add(-time.Minute), CreatedBy: "analyst-1"}
	if err := wave3.CreateTargetManifest(ctx, expired); err != nil {
		t.Fatal(err)
	}
	rec := confirmRequest(t, s, expired, domain.RoleAdmin, "admin-1")
	if rec.Code == http.StatusOK {
		t.Fatalf("expired manifest confirmed with %d", rec.Code)
	}

	fresh := &domain.TargetManifest{ID: generateID(), Operation: "batch_score", TargetMode: domain.TargetModeAll, TargetCount: 2, Token: "right-token", Status: "preview", Version: 1, ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedBy: "analyst-1"}
	if err := wave3.CreateTargetManifest(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	wrong := *fresh
	wrong.Token = "wrong-token"
	if rec := confirmRequest(t, s, &wrong, domain.RoleAdmin, "admin-1"); rec.Code == http.StatusOK {
		t.Fatal("a manifest was confirmed with the wrong token")
	}
	stale := *fresh
	stale.Version = 99
	if rec := confirmRequest(t, s, &stale, domain.RoleAdmin, "admin-1"); rec.Code != http.StatusConflict {
		t.Fatalf("stale expected_version = %d, want 409", rec.Code)
	}
}

// Two operators confirming at once: exactly one wins, and the manifest is
// consumed once.
func TestConcurrentTargetConfirmationYieldsOneWinner(t *testing.T) {
	wave3 := store.NewMemoryWave3Repo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: wave3, Audit: store.NewMemoryAuditRepo()})
	manifest := &domain.TargetManifest{ID: generateID(), Operation: "batch_score", TargetMode: domain.TargetModeAll, TargetCount: 5, Token: "race-token", Status: "preview", Version: 1, ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedBy: "analyst-1"}
	if err := wave3.CreateTargetManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes <- confirmRequest(t, s, manifest, domain.RoleAdmin, fmt.Sprintf("admin-%d", i)).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent confirmations = %d ok, %d conflict; want exactly one winner", ok, conflict)
	}
}

// An idempotent retry must not append a second audit record: the operation
// happened once, so the evidence says once.
func TestIdempotentTargetConfirmationWritesOneAuditRecord(t *testing.T) {
	wave3 := store.NewMemoryWave3Repo()
	audit := store.NewMemoryAuditRepo()
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: wave3, Audit: audit})
	manifest := &domain.TargetManifest{ID: generateID(), Operation: "batch_score", TargetMode: domain.TargetModeAll, TargetCount: 5, Token: "idem-token", IdempotencyKey: "retry-key", Status: "preview", Version: 1, ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedBy: "analyst-1"}
	if err := wave3.CreateTargetManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	body := `{"token":"idem-token","rationale":"reviewed the population","expected_version":1,"idempotency_key":"retry-key"}`
	for attempt := range 3 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/"+manifest.ID+"/confirm", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyPrincipal, Principal{UserID: "admin-1"}))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d = %d, body=%s", attempt, rec.Code, rec.Body.String())
		}
	}

	entries, err := audit.List(context.Background(), domain.AuditListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	confirmations := 0
	for _, entry := range entries {
		if entry.Action == "target_manifest_confirmed" {
			confirmations++
		}
	}
	if confirmations != 1 {
		t.Fatalf("audit records for one confirmation = %d, want 1", confirmations)
	}
}

// Where authentication is in force, a request that somehow arrives without a
// role must be refused, not waved through. The router's own middleware answers
// such a request with 401 before the handler runs, so the gate is exercised
// directly: it is the last line, not the first.
func TestLargeTargetConfirmationFailsClosedWithoutARole(t *testing.T) {
	authenticated := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: store.NewMemoryWave3Repo(), APIKeys: store.NewMemoryAPIKeyRepo()})
	unauthenticated := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: store.NewMemoryWave3Repo()})
	manifest := &domain.TargetManifest{TargetCount: largeBatchThreshold + 1, CreatedBy: "analyst-1"}
	roleless := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/x/confirm", nil)

	if status, _ := authenticated.checkLargeBatchAuthorization(roleless, manifest); status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: a deployment with authentication must not accept a roleless confirmation", status)
	}
	// A deployment that runs without authentication has no roles to check;
	// separation of duties still applies there.
	if status, _ := unauthenticated.checkLargeBatchAuthorization(roleless, manifest); status != 0 {
		t.Fatalf("status = %d, want 0 where no authentication is configured", status)
	}
}

// A manifest with no recorded author cannot be checked for separation of
// duties, so an authenticated deployment refuses it rather than assuming.
func TestLargeTargetConfirmationRequiresAKnownAuthor(t *testing.T) {
	s := New(":0", Deps{Customers: store.NewMemoryCustomerRepo(), Wave3: store.NewMemoryWave3Repo(), APIKeys: store.NewMemoryAPIKeyRepo()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/targets/x/confirm", nil)
	req = req.WithContext(auth.WithRole(req.Context(), domain.RoleAdmin))

	authorless := &domain.TargetManifest{TargetCount: largeBatchThreshold + 1}
	if status, _ := s.checkLargeBatchAuthorization(req, authorless); status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a large manifest with no recorded author", status)
	}
	// The same manifest below the threshold is untouched by any of this.
	small := &domain.TargetManifest{TargetCount: largeBatchThreshold}
	if status, _ := s.checkLargeBatchAuthorization(req, small); status != 0 {
		t.Fatalf("status = %d, want 0 below the threshold", status)
	}
}
