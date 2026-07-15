package ingestion

import (
	"context"
	"errors"
	"fmt"
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
	ObserveQueues(context.Context) error
	ExpirePreviousWriteKeys(context.Context, time.Time) (int64, error)
}

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
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticks[0].ticker.C:
			if err := s.RunJob(ctx, SchedulerAggregate, now); err != nil {
				return err
			}
		case now := <-ticks[1].ticker.C:
			if err := s.RunJob(ctx, SchedulerReclaim, now); err != nil {
				return err
			}
		case now := <-ticks[2].ticker.C:
			if err := s.RunJob(ctx, SchedulerMetrics, now); err != nil {
				return err
			}
		case now := <-ticks[3].ticker.C:
			if err := s.RunJob(ctx, SchedulerExpireKeys, now); err != nil {
				return err
			}
		}
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
			// XAUTOCLAIM has already increased Redis' delivery count. Queue.Retry
			// adds one when it creates the durable retry envelope, so offset that
			// claim-side increment to avoid counting maintenance as two attempts.
			if msg.Deliveries > 1 {
				msg.Deliveries--
			}
			if err := s.jobs.Retry(ctx, stream, s.cfg.Group, msg, errors.New("stale pending message reclaimed"), time.Millisecond); err != nil {
				return fmt.Errorf("reschedule reclaimed %s: %w", msg.ID, err)
			}
		}
	}
	return nil
}
