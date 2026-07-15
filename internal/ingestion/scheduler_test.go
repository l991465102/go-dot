package ingestion

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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
	require.Equal(t, 2, jobs.retryMessage.Deliveries, "durable envelope attempts are independent of XPENDING delivery count")
}

func TestSchedulerRunReturnsOnCancellation(t *testing.T) {
	s := NewScheduler(&fakeLeaseManager{}, &fakeSchedulerJobs{}, SchedulerConfig{AggregateEvery: time.Hour, ReclaimEvery: time.Hour, MetricsEvery: time.Hour, ExpireKeysEvery: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, s.Run(ctx))
}

func TestSchedulerReportsTickErrorsAndContinuesFutureTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reporter := &cancelingReporter{cancel: cancel, after: 2}
	jobs := &fakeSchedulerJobs{aggregateErr: errors.New("clickhouse unavailable")}
	s := NewScheduler(&fakeLeaseManager{}, jobs, SchedulerConfig{AggregateEvery: time.Millisecond, ReclaimEvery: time.Hour, MetricsEvery: time.Hour, ExpireKeysEvery: time.Hour, Reporter: reporter})
	require.NoError(t, s.Run(ctx))
	require.GreaterOrEqual(t, reporter.count.Load(), int32(2))
}

func TestSchedulerIsolatesReclaimFromSlowAggregation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	jobs := &isolatingSchedulerJobs{aggregateStarted: make(chan struct{}), reclaimed: make(chan struct{})}
	s := NewScheduler(&fakeLeaseManager{}, jobs, SchedulerConfig{AggregateEvery: time.Millisecond, ReclaimEvery: 2 * time.Millisecond, MetricsEvery: time.Hour, ExpireKeysEvery: time.Hour})
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-jobs.aggregateStarted:
	case <-time.After(time.Second):
		t.Fatal("aggregation did not start")
	}
	select {
	case <-jobs.reclaimed:
	case <-time.After(time.Second):
		t.Fatal("reclaim was blocked by aggregation")
	}
	cancel()
	require.NoError(t, <-done)
}

func TestSchedulerRepeatedCrashReclaimMovesFifthAttemptToDLQ(t *testing.T) {
	jobs := &fakeSchedulerJobs{}
	s := NewScheduler(&fakeLeaseManager{}, jobs, SchedulerConfig{MaxDeliveries: 5})
	for deliveries := 1; deliveries <= 5; deliveries++ {
		jobs.reclaimed = []ports.Message{{ID: "1-0", Stream: EventStream, BatchID: "b", Payload: []byte(`{}`), Deliveries: deliveries}}
		require.NoError(t, s.RunJob(context.Background(), SchedulerReclaim, time.Unix(int64(deliveries*20), 0)))
	}
	require.Equal(t, 4, jobs.retries)
	require.Equal(t, 1, jobs.dlqs)
	require.Equal(t, 5, jobs.dlqMessage.Deliveries)
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
	mu                             sync.Mutex
	aggregate, retries, promotions int
	dlqs                           int
	reclaimed                      []ports.Message
	retryMessage                   ports.Message
	dlqMessage                     ports.Message
	aggregateErr                   error
}

func (j *fakeSchedulerJobs) RefreshStatistics(context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.aggregate++
	return j.aggregateErr
}
func (j *fakeSchedulerJobs) PromoteDue(context.Context, string, int) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
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
	j.mu.Lock()
	defer j.mu.Unlock()
	j.retries++
	j.retryMessage = msg
	return nil
}
func (j *fakeSchedulerJobs) MoveToDLQ(_ context.Context, _, _ string, msg ports.Message, _ error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.dlqs++
	j.dlqMessage = msg
	return nil
}
func (j *fakeSchedulerJobs) ObserveQueues(context.Context) error { return nil }
func (j *fakeSchedulerJobs) ExpirePreviousWriteKeys(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type cancelingReporter struct {
	count  atomic.Int32
	after  int32
	cancel context.CancelFunc
}

func (r *cancelingReporter) Report(error) {
	if r.count.Add(1) >= r.after {
		r.cancel()
	}
}

type isolatingSchedulerJobs struct {
	aggregateStarted, reclaimed chan struct{}
	aggregateOnce, reclaimOnce  sync.Once
}

func (j *isolatingSchedulerJobs) RefreshStatistics(ctx context.Context) error {
	j.aggregateOnce.Do(func() { close(j.aggregateStarted) })
	<-ctx.Done()
	return ctx.Err()
}
func (j *isolatingSchedulerJobs) PromoteDue(context.Context, string, int) (int, error) {
	j.reclaimOnce.Do(func() { close(j.reclaimed) })
	return 0, nil
}
func (j *isolatingSchedulerJobs) ReclaimStale(context.Context, string, string, string, time.Duration, int) ([]ports.Message, error) {
	return nil, nil
}
func (j *isolatingSchedulerJobs) Retry(context.Context, string, string, ports.Message, error, time.Duration) error {
	return nil
}
func (j *isolatingSchedulerJobs) MoveToDLQ(context.Context, string, string, ports.Message, error) error {
	return nil
}
func (j *isolatingSchedulerJobs) ObserveQueues(context.Context) error { return nil }
func (j *isolatingSchedulerJobs) ExpirePreviousWriteKeys(context.Context, time.Time) (int64, error) {
	return 0, nil
}
