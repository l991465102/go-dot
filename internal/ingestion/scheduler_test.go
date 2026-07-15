package ingestion

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestSchedulerLeaseContentionRunsBucketOnce(t *testing.T) {
	lease := &fakeLeaseManager{}
	jobs := &fakeSchedulerJobs{}
	s := NewScheduler(lease, jobs, SchedulerConfig{LeaseTTL: time.Minute})
	now := time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC)

	require.NoError(t, s.RunJob(context.Background(), SchedulerAggregate, now))
	require.NoError(t, s.RunJob(context.Background(), SchedulerAggregate, now))
	require.Equal(t, 1, jobs.aggregate)
}

func TestSchedulerReclaimsAndPromotesWithoutWorkerOwnership(t *testing.T) {
	lease := &fakeLeaseManager{}
	jobs := &fakeSchedulerJobs{reclaimed: []ports.Message{{ID: "1-0", Stream: EventStream, BatchID: "b", Payload: []byte(`{}`), Deliveries: 2}}}
	s := NewScheduler(lease, jobs, SchedulerConfig{EventStream: EventStream, InvalidStream: InvalidEventStream, Group: WorkerGroup, ReclaimConsumer: "scheduler", ReclaimIdle: time.Minute, LeaseTTL: time.Minute})

	require.NoError(t, s.RunJob(context.Background(), SchedulerReclaim, time.Now()))
	require.Equal(t, 1, jobs.retries)
	require.Equal(t, 2, jobs.promotions)
	require.Equal(t, 1, jobs.retryMessage.Deliveries, "offset XAUTOCLAIM's delivery increment before Queue.Retry adds one")
}

func TestSchedulerRunReturnsOnCancellation(t *testing.T) {
	s := NewScheduler(&fakeLeaseManager{}, &fakeSchedulerJobs{}, SchedulerConfig{AggregateEvery: time.Hour, ReclaimEvery: time.Hour, MetricsEvery: time.Hour, ExpireKeysEvery: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, s.Run(ctx))
}

type fakeLeaseManager struct {
	mu   sync.Mutex
	held map[string]bool
}

func (l *fakeLeaseManager) WithLease(ctx context.Context, key string, _ time.Duration, fn func(context.Context) error) error {
	l.mu.Lock()
	if l.held == nil {
		l.held = map[string]bool{}
	}
	if l.held[key] {
		l.mu.Unlock()
		return nil
	}
	l.held[key] = true
	l.mu.Unlock()
	return fn(ctx)
}

type fakeSchedulerJobs struct {
	aggregate, retries, promotions int
	reclaimed                      []ports.Message
	retryMessage                   ports.Message
}

func (j *fakeSchedulerJobs) RefreshStatistics(context.Context) error { j.aggregate++; return nil }
func (j *fakeSchedulerJobs) PromoteDue(context.Context, string, int) (int, error) {
	j.promotions++
	return 0, nil
}
func (j *fakeSchedulerJobs) ReclaimStale(_ context.Context, stream, _, _ string, _ time.Duration, _ int) ([]ports.Message, error) {
	if len(j.reclaimed) > 0 && j.reclaimed[0].Stream == stream {
		return j.reclaimed, nil
	}
	return nil, nil
}
func (j *fakeSchedulerJobs) Retry(_ context.Context, _, _ string, msg ports.Message, _ error, _ time.Duration) error {
	j.retries++
	j.retryMessage = msg
	return nil
}
func (j *fakeSchedulerJobs) ObserveQueues(context.Context) error { return nil }
func (j *fakeSchedulerJobs) ExpirePreviousWriteKeys(context.Context, time.Time) (int64, error) {
	return 0, nil
}
