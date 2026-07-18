package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/store"
)

type errorBacktestJobRepository struct {
	domain.BacktestJobRepository
	getErr    error
	cancelErr error
}

func (r *errorBacktestJobRepository) Get(context.Context, string) (*domain.BacktestJob, error) {
	return nil, r.getErr
}

func (r *errorBacktestJobRepository) Cancel(context.Context, string) error {
	return r.cancelErr
}

func TestCreateBacktestJobRequiresUTCWindowAndSelector(t *testing.T) {
	jobs := store.NewMemoryBacktestJobRepo()
	rules := store.NewMemoryRuleRepo()
	if err := rules.Create(context.Background(), &domain.RuleDefinition{Name: "v2", Type: domain.RuleTypeTMScenario, Definition: json.RawMessage(`{"scenario_id":"tm_structuring_basic"}`), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{BacktestJobs: jobs, Rules: rules, ConfigDigests: map[string]string{"tm": "abc"}})
	bad := `{"from":"2026-01-01T00:00:00+09:00","to":"2026-01-02T00:00:00Z","customer_ids":["c1"],"candidate_rule_set_id":"v2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(bad))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	good := `{"from":"2026-01-01T00:00:00Z","to":"2026-01-02T00:00:00Z","customer_ids":["c1"],"candidate_rule_set_id":"v2"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(good))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job domain.BacktestJob
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.BacktestJobQueued || job.ConfigDigests["tm"] != "abc" {
		t.Fatalf("job=%+v", job)
	}
	stored, err := jobs.Get(context.Background(), job.ID)
	if err != nil || stored.CandidateRuleVersion != 1 || len(stored.CandidateRuleDefinition) == 0 {
		t.Fatalf("stored rule snapshot=%+v err=%v", stored, err)
	}
}

func TestBacktestJobHandlersMapOnlyNotFoundErrorsTo404(t *testing.T) {
	for _, endpoint := range []string{
		"/api/v1/backtests/job-1",
		"/api/v1/backtests/job-1/affected-customers",
	} {
		endpoint := endpoint
		t.Run(endpoint, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				err  error
				want int
			}{
				{name: "not found", err: &domain.ErrNotFound{Entity: "backtest_job", ID: "job-1"}, want: http.StatusNotFound},
				{name: "repository failure", err: errors.New("database unavailable"), want: http.StatusInternalServerError},
			} {
				t.Run(tc.name, func(t *testing.T) {
					repo := &errorBacktestJobRepository{BacktestJobRepository: store.NewMemoryBacktestJobRepo(), getErr: tc.err}
					s := New(":0", Deps{BacktestJobs: repo})
					req := httptest.NewRequest(http.MethodGet, endpoint, nil)
					rec := httptest.NewRecorder()
					s.Handler().ServeHTTP(rec, req)
					if rec.Code != tc.want {
						t.Fatalf("status=%d, want=%d body=%s", rec.Code, tc.want, rec.Body.String())
					}
				})
			}
		})
	}
}

func TestCancelBacktestJobMapsRepositoryErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: &domain.ErrNotFound{Entity: "backtest_job", ID: "job-1"}, want: http.StatusNotFound},
		{name: "repository failure", err: errors.New("database unavailable"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &errorBacktestJobRepository{BacktestJobRepository: store.NewMemoryBacktestJobRepo(), cancelErr: tc.err}
			s := New(":0", Deps{BacktestJobs: repo})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests/job-1/cancel", nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d, want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
