package config

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadRejectsMissingSecrets(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "")
	_, err := Load(context.Background())
	require.ErrorContains(t, err, "GO_DOT_JWT_SECRET")
}

func TestLoadProvidesQueueDefaults(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_QUEUE_EVENT_STREAM", "")
	t.Setenv("GO_DOT_QUEUE_INVALID_STREAM", "")
	t.Setenv("GO_DOT_QUEUE_DEAD_STREAM", "")
	t.Setenv("GO_DOT_QUEUE_CONSUMER_GROUP", "")
	t.Setenv("GO_DOT_QUEUE_PUBLISH_DEDUP_TTL", "")

	cfg, err := Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tracking:{queue}:events", cfg.Queue.EventStream)
	require.Equal(t, "tracking:{queue}:invalid", cfg.Queue.InvalidStream)
	require.Equal(t, "tracking:{queue}:dead", cfg.Queue.DeadStream)
	require.Equal(t, "tracking-workers", cfg.Queue.ConsumerGroup)
	require.Equal(t, 72*time.Hour, cfg.Queue.PublishDedupTTL)
	require.Equal(t, 8, cfg.Worker.Concurrency)
	require.Equal(t, 5, cfg.Worker.MaxDeliveries)
	require.Equal(t, 30*time.Second, cfg.Worker.ReclaimIdle)
	require.Equal(t, 30*time.Second, cfg.Worker.DrainTimeout)
	require.Equal(t, time.Second, cfg.Worker.RetryBase)
	require.Equal(t, time.Minute, cfg.Worker.RetryMax)
	require.Equal(t, time.Minute, cfg.Scheduler.AggregateEvery)
	require.Equal(t, 2, cfg.Scheduler.AggregateLookbackHours)
	require.Equal(t, 15*time.Second, cfg.Scheduler.ReclaimEvery)
	require.Equal(t, time.Minute, cfg.Scheduler.ReclaimIdle)
	require.Equal(t, 2*time.Minute, cfg.Scheduler.LeaseTTL)
	require.Equal(t, 100, cfg.Scheduler.BatchSize)
}

func TestLoadValidatesSchedulerAggregateLookbackHours(t *testing.T) {
	for _, value := range []string{"0", "169", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
			t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
			t.Setenv("GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS", value)
			_, err := Load(context.Background())
			require.ErrorContains(t, err, "GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS")
		})
	}

	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS", "26")
	cfg, err := Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, 26, cfg.Scheduler.AggregateLookbackHours)
}

func TestLoadRejectsQueueStreamsWithoutOneCommonRedisHashTag(t *testing.T) {
	for name, values := range map[string][3]string{
		"missing":  {"tracking:events", "tracking:{queue}:invalid", "tracking:{queue}:dead"},
		"mismatch": {"tracking:{events}:events", "tracking:{invalid}:invalid", "tracking:{events}:dead"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
			t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
			t.Setenv("GO_DOT_QUEUE_EVENT_STREAM", values[0])
			t.Setenv("GO_DOT_QUEUE_INVALID_STREAM", values[1])
			t.Setenv("GO_DOT_QUEUE_DEAD_STREAM", values[2])
			_, err := Load(context.Background())
			require.ErrorContains(t, err, "Redis hash tag")
		})
	}
}

func TestLoadRejectsInvalidWorkerConcurrency(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_WORKER_CONCURRENCY", "0")
	_, err := Load(context.Background())
	require.ErrorContains(t, err, "GO_DOT_WORKER_CONCURRENCY")
}

func TestLoadRejectsWorkerRetryMaximumBelowBase(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_WORKER_RETRY_BASE", "10s")
	t.Setenv("GO_DOT_WORKER_RETRY_MAX", "5s")
	_, err := Load(context.Background())
	require.ErrorContains(t, err, "GO_DOT_WORKER_RETRY_MAX")
}

func TestLoadRejectsWorkerRetryBoundsThatCanChurnOrOverflow(t *testing.T) {
	for name, value := range map[string]string{"GO_DOT_WORKER_RETRY_BASE": "1ms", "GO_DOT_WORKER_RETRY_MAX": "25h"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
			t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
			t.Setenv(name, value)
			_, err := Load(context.Background())
			require.ErrorContains(t, err, name)
		})
	}
}

func TestLoadRejectsUnsafeQueuePublishDedupTTL(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_QUEUE_PUBLISH_DEDUP_TTL", "71h")
	_, err := Load(context.Background())
	require.ErrorContains(t, err, "GO_DOT_QUEUE_PUBLISH_DEDUP_TTL")
}

func TestLoadAcceptsMinimumQueuePublishDedupTTL(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_QUEUE_PUBLISH_DEDUP_TTL", "72h")
	cfg, err := Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, 72*time.Hour, cfg.Queue.PublishDedupTTL)
}

func TestLoadSecureCookiesFromEnvironment(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_SECURE_COOKIES", "true")

	cfg, err := Load(context.Background())
	require.NoError(t, err)
	require.True(t, cfg.Auth.SecureCookies)
}

func TestLoadRejectsInvalidSecureCookiesValue(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_SECURE_COOKIES", "sometimes")

	_, err := Load(context.Background())
	require.ErrorContains(t, err, "GO_DOT_SECURE_COOKIES")
}
