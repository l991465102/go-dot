package observability

import (
	"context"
	"time"

	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
)

type CollectService interface {
	Collect(context.Context, project.Project, ingestion.CollectRequest) (ingestion.CollectResult, error)
}
type ObservedCollectService struct {
	Next    CollectService
	Metrics *Metrics
	Logger  *JSONLogger
}

func (s ObservedCollectService) Collect(ctx context.Context, p project.Project, req ingestion.CollectRequest) (ingestion.CollectResult, error) {
	result, err := s.Next.Collect(ctx, p, req)
	if s.Metrics != nil {
		s.Metrics.ObserveCollection(result.Accepted, result.Rejected, err)
	}
	if s.Logger != nil {
		eventIDs := make([]string, 0, len(result.Results))
		for _, item := range result.Results {
			if item.EventID != "" {
				eventIDs = append(eventIDs, item.EventID)
			}
		}
		fields := map[string]any{"project_id": p.ID, "batch_id": result.BatchID, "event_ids": eventIDs, "accepted": result.Accepted, "rejected": result.Rejected}
		if requestID, ok := ctx.Value(RequestIDContextKey{}).(string); ok {
			fields["request_id"] = requestID
		}
		level, message := "info", "collection completed"
		if err != nil {
			level, message = "error", "collection failed"
		}
		_ = s.Logger.Log(level, message, fields)
	}
	return result, err
}

// RequestIDContextKey is supported for non-HTTP adapters that need correlation.
// HTTP code should normally use httpapi.RequestIDFromContext.
type RequestIDContextKey struct{}

type ObservedEventStore struct {
	Next    ports.EventStore
	Metrics *Metrics
	Logger  *JSONLogger
}

func (s ObservedEventStore) SaveValid(ctx context.Context, token string, events []ports.StoredEvent) error {
	start := time.Now()
	err := s.Next.SaveValid(ctx, token, events)
	if s.Metrics != nil {
		s.Metrics.ObserveClickHouseBatch(len(events), time.Since(start), err)
		s.Metrics.ObserveWorker(time.Since(start), false)
	}
	s.logStore("valid event batch", token, validEventFields(events), err)
	return err
}
func (s ObservedEventStore) SaveInvalid(ctx context.Context, token string, events []ports.InvalidEvent) error {
	start := time.Now()
	err := s.Next.SaveInvalid(ctx, token, events)
	if s.Metrics != nil {
		s.Metrics.ObserveClickHouseBatch(len(events), time.Since(start), err)
		s.Metrics.ObserveWorker(time.Since(start), false)
	}
	s.logStore("invalid event batch", token, invalidEventFields(events), err)
	return err
}

func (s ObservedEventStore) logStore(message, batchID string, fields map[string]any, err error) {
	if s.Logger == nil {
		return
	}
	fields["batch_id"] = batchID
	level := "info"
	if err != nil {
		level = "error"
	}
	_ = s.Logger.Log(level, message, fields)
}
func validEventFields(events []ports.StoredEvent) map[string]any {
	ids := make([]string, 0, len(events))
	var projectID uint64
	for _, event := range events {
		ids = append(ids, event.EventID)
		projectID = event.ProjectID
	}
	return map[string]any{"project_id": projectID, "event_ids": ids, "count": len(events)}
}
func invalidEventFields(events []ports.InvalidEvent) map[string]any {
	ids := make([]string, 0, len(events))
	var projectID uint64
	for _, event := range events {
		ids = append(ids, event.EventID)
		projectID = event.ProjectID
	}
	return map[string]any{"project_id": projectID, "event_ids": ids, "count": len(events)}
}

type ObservedCounter struct {
	Next    ports.RealtimeCounter
	Metrics *Metrics
}

func (c ObservedCounter) Apply(ctx context.Context, event ports.CountEvent) (bool, error) {
	start := time.Now()
	applied, err := c.Next.Apply(ctx, event)
	if c.Metrics != nil {
		c.Metrics.ObserveRedis(time.Since(start))
		if err == nil && !applied {
			c.Metrics.ObserveDuplicate(1)
		}
		if err == nil {
			delay := time.Since(event.StatTime).Seconds()
			if delay < 0 {
				delay = 0
			}
			c.Metrics.SetRealtimeDelay(delay)
		}
	}
	return applied, err
}

type ObservedQueue struct {
	Next    ports.MessageQueue
	Metrics *Metrics
}

func (q ObservedQueue) observe(start time.Time) {
	if q.Metrics != nil {
		q.Metrics.ObserveRedis(time.Since(start))
	}
}
func (q ObservedQueue) PublishBatch(ctx context.Context, stream string, batch ports.BatchEnvelope) (string, error) {
	start := time.Now()
	id, err := q.Next.PublishBatch(ctx, stream, batch)
	q.observe(start)
	return id, err
}
func (q ObservedQueue) Consume(ctx context.Context, stream, group, consumer string, count int, block time.Duration) ([]ports.Message, error) {
	start := time.Now()
	items, err := q.Next.Consume(ctx, stream, group, consumer, count, block)
	q.observe(start)
	return items, err
}
func (q ObservedQueue) Ack(ctx context.Context, stream, group string, ids ...string) error {
	start := time.Now()
	err := q.Next.Ack(ctx, stream, group, ids...)
	q.observe(start)
	return err
}
func (q ObservedQueue) Retry(ctx context.Context, stream, group string, msg ports.Message, cause error, delay time.Duration) error {
	start := time.Now()
	err := q.Next.Retry(ctx, stream, group, msg, cause, delay)
	q.observe(start)
	if q.Metrics != nil {
		q.Metrics.ObserveWorker(time.Since(start), true)
	}
	return err
}
func (q ObservedQueue) PromoteDue(ctx context.Context, stream string, count int) (int, error) {
	start := time.Now()
	n, err := q.Next.PromoteDue(ctx, stream, count)
	q.observe(start)
	return n, err
}
func (q ObservedQueue) MoveToDLQ(ctx context.Context, stream, group string, msg ports.Message, cause error) error {
	start := time.Now()
	err := q.Next.MoveToDLQ(ctx, stream, group, msg, cause)
	q.observe(start)
	return err
}
func (q ObservedQueue) ReclaimStale(ctx context.Context, stream, group, consumer string, idle time.Duration, count int) ([]ports.Message, error) {
	start := time.Now()
	items, err := q.Next.ReclaimStale(ctx, stream, group, consumer, idle, count)
	q.observe(start)
	return items, err
}

type QueueSnapshotSink struct{ Metrics *Metrics }

func (s QueueSnapshotSink) SetQueueSnapshot(snapshot redisqueue.QueueSnapshot) {
	if s.Metrics != nil {
		s.Metrics.SetQueueSnapshot(QueueSnapshot{Lag: snapshot.Pending, Pending: snapshot.Pending, DeadLetters: snapshot.DeadLetters, OldestPendingAge: snapshot.OldestPendingAge})
	}
}

type ErrorLogger struct{ Logger *JSONLogger }

func (l ErrorLogger) Report(err error) {
	if l.Logger != nil && err != nil {
		_ = l.Logger.Log("error", "scheduler job failed", nil)
	}
}
