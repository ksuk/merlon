package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/events"
)

const eventOutboxBatchSize = 100

// RunEventOutboxWorker publishes committed event intents and records the
// delivery result. A transport failure leaves the outbox row pending with a
// retry time; a process restart therefore resumes from PostgreSQL rather than
// reconstructing an event from mutable business state.
func RunEventOutboxWorker(ctx context.Context, repo domain.EventOutboxRepository, bus events.Bus, interval time.Duration) {
	if repo == nil || bus == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	run := func() {
		pending, err := repo.ListPending(ctx, eventOutboxBatchSize)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "event outbox list failed", "error", err)
			}
			return
		}
		for _, durable := range pending {
			event := events.Event{
				ID: durable.ID, Topic: durable.Topic, Payload: durable.Payload,
				SequenceNum: durable.SequenceNum, ChainID: durable.ChainID,
				ChainHopCount: durable.ChainHopCount, CreatedAt: durable.CreatedAt,
			}
			if err := bus.Publish(ctx, event); err != nil {
				next := time.Now().UTC().Add(outboxBackoff(durable.Attempts + 1))
				if recordErr := repo.RecordFailure(ctx, durable.ID, err, next); recordErr != nil {
					slog.ErrorContext(ctx, "event outbox failure recording failed", "event_id", durable.ID, "error", recordErr)
				}
				continue
			}
			if err := repo.MarkPublished(ctx, durable.ID, time.Now().UTC()); err != nil {
				slog.ErrorContext(ctx, "event outbox mark published failed", "event_id", durable.ID, "error", err)
			}
		}
	}

	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func outboxBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}
