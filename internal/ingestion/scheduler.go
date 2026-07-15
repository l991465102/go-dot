package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
)

type SchedulerJob string

const (
	SchedulerAggregate  SchedulerJob = "aggregate"
	SchedulerReclaim    SchedulerJob = "reclaim"
	SchedulerMetrics    SchedulerJob = "metrics"
	SchedulerExpireKeys SchedulerJob = "expire-keys"
)

type LeaseManager interface {
	WithLease(context.Context, string, time.Duration, func(context.Context) error) error
}
type SchedulerJobs interface {
	RefreshStatistics(context.Context) error
	PromoteDue(context.Context, string, int) (int, error)
	ReclaimStale(context.Context, string, string, string, time.Duration, int) ([]ports.Message, error)
	Retry(context.Context, string, string, ports.Message, error, time.Duration) error
	MoveToDLQ(context.Context, string, string, ports.Message, error) error
	ObserveQueues(context.Context) error
	ExpirePreviousWriteKeys(context.Context, time.Time) (int64, error)
}
type ErrorReporter interface{ Report(error) }
type ErrorReporterFunc func(error)

func (f ErrorReporterFunc) Report(err error) { f(err) }

type StatisticsRefresher interface{ RefreshCurrentAndPrevious(context.Context) error }
type QueueObserver interface{ ObserveQueues(context.Context) error }
type WriteKeyExpirer interface {
	ExpirePreviousWriteKeys(context.Context, time.Time) (int64, error)
}
type SchedulerRuntimeJobs struct {
	Statistics StatisticsRefresher
	Queue      ports.MessageQueue
	Observer   QueueObserver
	Keys       WriteKeyExpirer
}

func (j SchedulerRuntimeJobs) RefreshStatistics(ctx context.Context) error {
	return j.Statistics.RefreshCurrentAndPrevious(ctx)
}
func (j SchedulerRuntimeJobs) PromoteDue(ctx context.Context, stream string, count int) (int, error) {
	return j.Queue.PromoteDue(ctx, stream, count)
}
func (j SchedulerRuntimeJobs) ReclaimStale(ctx context.Context, stream, group, consumer string, idle time.Duration, count int) ([]ports.Message, error) {
	return j.Queue.ReclaimStale(ctx, stream, group, consumer, idle, count)
}
func (j SchedulerRuntimeJobs) Retry(ctx context.Context, stream, group string, msg ports.Message, cause error, delay time.Duration) error {
	return j.Queue.Retry(ctx, stream, group, msg, cause, delay)
}
func (j SchedulerRuntimeJobs) MoveToDLQ(ctx context.Context, stream, group string, msg ports.Message, cause error) error {
	return j.Queue.MoveToDLQ(ctx, stream, group, msg, cause)
}
func (j SchedulerRuntimeJobs) ObserveQueues(ctx context.Context) error {
	if j.Observer == nil {
		return nil
	}
	return j.Observer.ObserveQueues(ctx)
}
func (j SchedulerRuntimeJobs) ExpirePreviousWriteKeys(ctx context.Context, now time.Time) (int64, error) {
	return j.Keys.ExpirePreviousWriteKeys(ctx, now)
}

type SchedulerConfig struct {
	EventStream, InvalidStream, Group, ReclaimConsumer          string
	AggregateEvery, ReclaimEvery, MetricsEvery, ExpireKeysEvery time.Duration
	ReclaimIdle, LeaseTTL                                       time.Duration
	BatchSize                                                   int
	MaxDeliveries                                               int
	Reporter                                                    ErrorReporter
}
type Scheduler struct {
	lease LeaseManager
	jobs  SchedulerJobs
	cfg   SchedulerConfig
}

func NewScheduler(lease LeaseManager, jobs SchedulerJobs, cfg SchedulerConfig) *Scheduler {
	if cfg.EventStream == "" {
		cfg.EventStream = EventStream
	}
	if cfg.InvalidStream == "" {
		cfg.InvalidStream = InvalidEventStream
	}
	if cfg.Group == "" {
		cfg.Group = WorkerGroup
	}
	if cfg.ReclaimConsumer == "" {
		cfg.ReclaimConsumer = "scheduler"
	}
	if cfg.AggregateEvery <= 0 {
		cfg.AggregateEvery = time.Minute
	}
	if cfg.ReclaimEvery <= 0 {
		cfg.ReclaimEvery = 15 * time.Second
	}
	if cfg.MetricsEvery <= 0 {
		cfg.MetricsEvery = time.Minute
	}
	if cfg.ExpireKeysEvery <= 0 {
		cfg.ExpireKeysEvery = time.Hour
	}
	if cfg.ReclaimIdle <= 0 {
		cfg.ReclaimIdle = time.Minute
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 2 * time.Minute
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 100
	}
	if cfg.MaxDeliveries < 1 {
		cfg.MaxDeliveries = 5
	}
	if cfg.Reporter == nil {
		cfg.Reporter = ErrorReporterFunc(func(error) {})
	}
	return &Scheduler{lease: lease, jobs: jobs, cfg: cfg}
}

func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	type tick struct {
		job    SchedulerJob
		ticker *time.Ticker
	}
	ticks := []tick{{SchedulerAggregate, time.NewTicker(s.cfg.AggregateEvery)}, {SchedulerReclaim, time.NewTicker(s.cfg.ReclaimEvery)}, {SchedulerMetrics, time.NewTicker(s.cfg.MetricsEvery)}, {SchedulerExpireKeys, time.NewTicker(s.cfg.ExpireKeysEvery)}}
	defer func() {
		for _, t := range ticks {
			t.ticker.Stop()
		}
	}()
	var running sync.WaitGroup
	launch := func(job SchedulerJob, now time.Time) {
		running.Add(1)
		go func() { defer running.Done(); s.runAndReport(ctx, job, now) }()
	}
	for {
		select {
		case <-ctx.Done():
			running.Wait()
			return nil
		case now := <-ticks[0].ticker.C:
			launch(SchedulerAggregate, now)
		case now := <-ticks[1].ticker.C:
			launch(SchedulerReclaim, now)
		case now := <-ticks[2].ticker.C:
			launch(SchedulerMetrics, now)
		case now := <-ticks[3].ticker.C:
			launch(SchedulerExpireKeys, now)
		}
	}
}
func (s *Scheduler) runAndReport(ctx context.Context, job SchedulerJob, now time.Time) {
	if err := s.RunJob(ctx, job, now); err != nil && ctx.Err() == nil {
		s.cfg.Reporter.Report(fmt.Errorf("scheduler %s: %w", job, err))
	}
}
func (s *Scheduler) validate() error {
	if s == nil || s.lease == nil || s.jobs == nil {
		return errors.New("scheduler lease manager and jobs are required")
	}
	return nil
}

func (s *Scheduler) RunJob(ctx context.Context, job SchedulerJob, now time.Time) error {
	if err := s.validate(); err != nil {
		return err
	}
	interval := s.interval(job)
	if interval <= 0 {
		return fmt.Errorf("unknown scheduler job %q", job)
	}
	bucket := now.UTC().Truncate(interval).UnixNano()
	key := fmt.Sprintf("scheduler:{scheduler}:%s:%d", job, bucket)
	return s.lease.WithLease(ctx, key, s.cfg.LeaseTTL, func(leaseCtx context.Context) error {
		switch job {
		case SchedulerAggregate:
			return s.jobs.RefreshStatistics(leaseCtx)
		case SchedulerReclaim:
			return s.reclaim(leaseCtx)
		case SchedulerMetrics:
			return s.jobs.ObserveQueues(leaseCtx)
		case SchedulerExpireKeys:
			_, err := s.jobs.ExpirePreviousWriteKeys(leaseCtx, now.UTC())
			return err
		default:
			return fmt.Errorf("unknown scheduler job %q", job)
		}
	})
}
func (s *Scheduler) interval(job SchedulerJob) time.Duration {
	switch job {
	case SchedulerAggregate:
		return s.cfg.AggregateEvery
	case SchedulerReclaim:
		return s.cfg.ReclaimEvery
	case SchedulerMetrics:
		return s.cfg.MetricsEvery
	case SchedulerExpireKeys:
		return s.cfg.ExpireKeysEvery
	}
	return 0
}
func (s *Scheduler) reclaim(ctx context.Context) error {
	for _, stream := range []string{s.cfg.EventStream, s.cfg.InvalidStream} {
		if _, err := s.jobs.PromoteDue(ctx, stream, s.cfg.BatchSize); err != nil {
			return fmt.Errorf("promote %s: %w", stream, err)
		}
		messages, err := s.jobs.ReclaimStale(ctx, stream, s.cfg.Group, s.cfg.ReclaimConsumer, s.cfg.ReclaimIdle, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("reclaim %s: %w", stream, err)
		}
		for _, msg := range messages {
			cause := errors.New("stale pending message reclaimed")
			if msg.Deliveries >= s.cfg.MaxDeliveries {
				if err := s.jobs.MoveToDLQ(ctx, stream, s.cfg.Group, msg, cause); err != nil {
					return fmt.Errorf("DLQ reclaimed %s: %w", msg.ID, err)
				}
				continue
			}
			if err := s.jobs.Retry(ctx, stream, s.cfg.Group, msg, cause, time.Millisecond); err != nil {
				return fmt.Errorf("reschedule reclaimed %s: %w", msg.ID, err)
			}
		}
	}
	return nil
}
