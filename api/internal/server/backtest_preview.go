package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

// cohortPageSize matches the worker's paging step so the preview walks the
// customer book the same way the run will.
const cohortPageSize = 500

// cohortSampleSize bounds the ids echoed back for the operator to eyeball.
const cohortSampleSize = 20

type backtestCohort struct {
	CustomerIDs      []string
	TransactionCount int
	// Counted is false when no transaction repository is available, which is
	// what keeps "not counted" distinguishable from "zero".
	Counted bool
}

// resolveBacktestCohort returns the customers a backtest would run over.
//
// It is the single answer used by both the pre-execution preview and the
// cohort recorded on the job, so an operator cannot be shown one population
// and have another one evaluated.
func (s *Server) resolveBacktestCohort(ctx context.Context, customerIDs []string, filter *domain.BacktestCustomerFilter) (backtestCohort, error) {
	cohort := backtestCohort{}
	if len(customerIDs) > 0 {
		cohort.CustomerIDs = append(cohort.CustomerIDs, customerIDs...)
	} else if s.customers != nil {
		var after *domain.Cursor
		for {
			page, err := s.customers.ListByCursor(ctx, cohortPageSize, after)
			if err != nil {
				return backtestCohort{}, err
			}
			if len(page) == 0 {
				break
			}
			for _, c := range page {
				if filter.Matches(c) {
					cohort.CustomerIDs = append(cohort.CustomerIDs, c.ID)
				}
			}
			if len(page) < cohortPageSize {
				break
			}
			last := page[len(page)-1]
			after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
	}

	counter, ok := s.transactions.(domain.CustomerScopedCountRepository)
	if !ok || s.transactions == nil {
		return cohort, nil
	}
	// A count query per customer rather than a page read per customer: the
	// old preview fetched up to 1001 rows each just to take len().
	for _, id := range cohort.CustomerIDs {
		n, err := counter.CountByCustomer(ctx, id)
		if err != nil {
			// One unreadable customer must not turn the whole preview into an
			// error; the cohort is still worth showing, marked as uncounted.
			return backtestCohort{CustomerIDs: cohort.CustomerIDs}, nil
		}
		cohort.TransactionCount += n
	}
	cohort.Counted = true
	return cohort, nil
}

func (c backtestCohort) sample() []string {
	if len(c.CustomerIDs) <= cohortSampleSize {
		return append([]string(nil), c.CustomerIDs...)
	}
	return append([]string(nil), c.CustomerIDs[:cohortSampleSize]...)
}

// isEmpty reports whether the run would have nothing to compare: no customers,
// or customers with no transactions in the book. A transaction count that
// could not be taken is not evidence of emptiness.
func (c backtestCohort) isEmpty() bool {
	if len(c.CustomerIDs) == 0 {
		return true
	}
	return c.Counted && c.TransactionCount == 0
}

func (c backtestCohort) warnings() []string {
	var out []string
	if len(c.CustomerIDs) == 0 {
		out = append(out, "no customers match this cohort; the comparison would evaluate nothing")
	} else if c.Counted && c.TransactionCount == 0 {
		out = append(out, "the selected customers have no transactions; the comparison would produce an empty result")
	}
	if !c.Counted && len(c.CustomerIDs) > 0 {
		out = append(out, "transaction counts are unavailable for this deployment; the cohort size could not be confirmed")
	}
	return out
}

// preview is the shape stored on BacktestMetadata.CohortPreview and returned
// by the preview endpoint, so the record of what ran and what the operator was
// shown are the same numbers.
func (c backtestCohort) preview() map[string]any {
	out := map[string]any{
		"count":               len(c.CustomerIDs),
		"sample_customer_ids": c.sample(),
		"empty":               c.isEmpty(),
	}
	if c.Counted {
		out["transaction_count"] = c.TransactionCount
	}
	return out
}

type backtestPreviewRequest struct {
	CustomerIDs    []string                       `json:"customer_ids,omitempty"`
	CustomerFilter *domain.BacktestCustomerFilter `json:"customer_filter,omitempty"`
}

// handlePreviewBacktestCohort answers "who would this run over, and is there
// anything to compare?" without creating a job.
//
// The previous preview was computed inside POST /backtests, after the job row
// already existed, and only counted transactions when the caller listed
// customer ids explicitly. A filter or all-customer cohort therefore got no
// transaction count at all, which is the one number that distinguishes a
// deliberate empty comparison from a mis-specified one.
func (s *Server) handlePreviewBacktestCohort(w http.ResponseWriter, r *http.Request) {
	if s.customers == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "customer repository not configured")
		return
	}
	var req backtestPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "invalid JSON")
		return
	}
	cohort, err := s.resolveBacktestCohort(r.Context(), req.CustomerIDs, req.CustomerFilter)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	// 200 with an explicit empty flag, not 400: an empty cohort is a fact the
	// operator asked for, and #71 requires the surface to warn or block. The
	// warning belongs where the operator can still change the inputs.
	writeJSON(w, http.StatusOK, map[string]any{
		"customer_count":      len(cohort.CustomerIDs),
		"transaction_count":   cohort.TransactionCount,
		"transaction_counted": cohort.Counted,
		"sample_customer_ids": cohort.sample(),
		"empty":               cohort.isEmpty(),
		"warnings":            cohort.warnings(),
	})
}
