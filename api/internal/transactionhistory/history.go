// Package transactionhistory centralizes deterministic transaction snapshot
// loading for realtime monitoring, scheduled monitoring, and backtests.
package transactionhistory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
)

const defaultPageSize = 1000

// Query defines a half-open event-time window and an inclusive ingestion-time
// snapshot. Transactions satisfy From <= ExecutedAt < To and
// CreatedAt <= CreatedThrough.
type Query struct {
	From           time.Time
	To             time.Time
	CreatedThrough time.Time
	// CreatedBeforeExclusive changes the ingestion cutoff from
	// CreatedAt <= CreatedThrough to CreatedAt < CreatedThrough. Scheduled
	// batches use this so transactions accepted at the batch boundary are
	// left for the next run.
	CreatedBeforeExclusive bool
	PageSize               int
}

// ListCustomerTransactionsAsOf returns a stable event-time ordered snapshot.
// Repositories with the keyset history capability use it directly; legacy
// repositories are exhaustively paged and filtered to identical semantics.
func ListCustomerTransactionsAsOf(ctx context.Context, repo domain.TransactionRepository, customerID string, query Query) ([]domain.Transaction, error) {
	if repo == nil {
		return nil, fmt.Errorf("transaction repository is required")
	}
	if !query.To.After(query.From) {
		return nil, fmt.Errorf("transaction event window end must be after start")
	}
	if query.CreatedThrough.IsZero() {
		return nil, fmt.Errorf("transaction snapshot cutoff is required")
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	if history, ok := repo.(domain.TransactionHistoryRepository); ok {
		createdThrough := query.CreatedThrough
		if query.CreatedBeforeExclusive {
			createdThrough = createdThrough.Add(-time.Nanosecond)
		}
		var out []domain.Transaction
		var after *domain.TransactionEventCursor
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			page, err := history.ListByCustomerEventRange(ctx, customerID, query.From, query.To, createdThrough, pageSize, after)
			if err != nil {
				return nil, err
			}
			for _, txn := range page {
				if matchesQuery(txn, query) {
					out = append(out, txn)
				}
			}
			if len(page) < pageSize {
				return out, nil
			}
			last := page[len(page)-1]
			after = &domain.TransactionEventCursor{ExecutedAt: last.ExecutedAt, ID: last.ID}
		}
	}

	var out []domain.Transaction
	for offset := 0; ; offset += pageSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := repo.ListByCustomer(ctx, customerID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, txn := range page {
			if matchesQuery(txn, query) {
				out = append(out, txn)
			}
		}
		if len(page) < pageSize {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ExecutedAt.Equal(out[j].ExecutedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].ExecutedAt.Before(out[j].ExecutedAt)
	})
	return out, nil
}

func matchesQuery(txn domain.Transaction, query Query) bool {
	createdInSnapshot := !txn.CreatedAt.After(query.CreatedThrough)
	if query.CreatedBeforeExclusive {
		createdInSnapshot = txn.CreatedAt.Before(query.CreatedThrough)
	}
	return !txn.ExecutedAt.Before(query.From) && txn.ExecutedAt.Before(query.To) && createdInSnapshot
}
