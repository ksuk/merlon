package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/engine"
	"github.com/ksuk/merlon/api/internal/policy"
	"github.com/ksuk/merlon/api/internal/review"
	"github.com/ksuk/merlon/api/internal/store"
)

func TestCustomerReviewQueueAPIAndVersionConflict(t *testing.T) {
	now := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	clockNow := now
	customers := store.NewMemoryCustomerRepo()
	if err := customers.Create(context.Background(), &domain.Customer{ID: "customer-api", ExternalID: "ext-api", CustomerType: domain.CustomerTypeIndividual, CreatedAt: now, UpdatedAt: now, Attributes: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	reviews := store.NewMemoryCustomerReviewRepo()
	p := policy.DefaultCDDReviewPolicy()
	p.Intervals[domain.RiskTierHigh] = 1
	svc := review.NewService(review.Dependencies{Reviews: reviews, Customers: customers, Scoring: &engine.MockScoringEngine{Tier: domain.RiskTierHigh}, Audit: store.NewMemoryAuditRepo(), Outbox: store.NewMemoryEventOutboxRepo(), Policy: p, Clock: func() time.Time { return clockNow }})
	if _, err := svc.Sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	s := New(":0", Deps{Customers: customers, Transactions: store.NewMemoryTransactionRepo(), Alerts: store.NewMemoryAlertRepo(), Cases: store.NewMemoryCaseRepo(), Audit: store.NewMemoryAuditRepo(), EventOutbox: store.NewMemoryEventOutboxRepo(), CustomerReviews: svc})
	item, err := reviews.LatestByCustomer(context.Background(), "customer-api")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		at     time.Time
		status domain.CustomerReviewStatus
	}{
		{name: "immediately before due", at: item.DueAt.Add(-time.Nanosecond), status: domain.CustomerReviewStatusScheduled},
		{name: "exactly due", at: item.DueAt, status: domain.CustomerReviewStatusDue},
		{name: "exactly overdue", at: item.GraceUntil, status: domain.CustomerReviewStatusOverdue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clockNow = tc.at
			request := httptest.NewRequest(http.MethodGet, "/api/v1/customer-reviews?status="+string(tc.status), nil)
			recorder := httptest.NewRecorder()
			s.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("list status = %d: %s", recorder.Code, recorder.Body.String())
			}
			var page paginatedResponse[domain.CustomerReview]
			if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Data) != 1 || page.Data[0].Status != tc.status {
				t.Fatalf("review page = %#v, want one %s review at %s", page, tc.status, tc.at.Format(time.RFC3339Nano))
			}
		})
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/v1/customer-reviews/"+item.ID, strings.NewReader(`{"action":"start","expected_version":999}`))
	patchRecorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(patchRecorder, patch)
	if patchRecorder.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d: %s", patchRecorder.Code, patchRecorder.Body.String())
	}
}
