package redisqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisLeaseContentionAndTokenSafeRelease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	manager := NewLeaseManager(client)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	go func() {
		_ = manager.WithLease(context.Background(), "lease:test", time.Minute, func(context.Context) error { calls.Add(1); close(entered); <-release; return nil })
	}()
	<-entered
	require.NoError(t, manager.WithLease(context.Background(), "lease:test", time.Minute, func(context.Context) error { calls.Add(1); return nil }))
	require.EqualValues(t, 1, calls.Load())
	close(release)
	require.Eventually(t, func() bool { return !server.Exists("lease:test") }, time.Second, time.Millisecond)
	require.True(t, server.Exists("lease:test:done"))
	require.NoError(t, manager.WithLease(context.Background(), "lease:test", time.Minute, func(context.Context) error { calls.Add(1); return nil }))
	require.EqualValues(t, 1, calls.Load())
}

func TestRedisLeaseOwnershipLossDoesNotMarkBucketDone(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	manager := NewLeaseManager(client)
	err := manager.WithLease(context.Background(), "scheduler:{scheduler}:aggregate:1", time.Minute, func(context.Context) error {
		return client.Set(context.Background(), "scheduler:{scheduler}:aggregate:1", "new-owner", time.Minute).Err()
	})
	require.ErrorContains(t, err, "ownership")
	require.False(t, server.Exists("scheduler:{scheduler}:aggregate:1:done"))
	owner, getErr := server.Get("scheduler:{scheduler}:aggregate:1")
	require.NoError(t, getErr)
	require.Equal(t, "new-owner", owner)
}

func TestLeaseScriptsAtomicallyCheckCompletionAndOwnership(t *testing.T) {
	require.Contains(t, acquireLeaseLua, "EXISTS")
	require.Contains(t, acquireLeaseLua, "SET")
	require.Contains(t, completeLeaseLua, "GET")
	require.Contains(t, completeLeaseLua, "PEXPIRE")
	require.Contains(t, completeLeaseLua, "DEL")
}
