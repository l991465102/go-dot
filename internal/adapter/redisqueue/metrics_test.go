package redisqueue

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestQueueMonitorPublishesPendingAndDeadLetterSnapshot(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(ctx, "tracking:{queue}:events", "workers", "0").Err())
	_, err := client.XAdd(ctx, &redis.XAddArgs{Stream: "tracking:{queue}:events", Values: map[string]any{"batch_id": "b"}}).Result()
	require.NoError(t, err)
	_, err = client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "workers", Consumer: "worker", Streams: []string{"tracking:{queue}:events", ">"}, Count: 1}).Result()
	require.NoError(t, err)
	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: "tracking:{queue}:events", Values: map[string]any{"batch_id": "undelivered"}}).Result()
	require.NoError(t, err)
	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: "tracking:{queue}:dead", Values: map[string]any{"batch_id": "dead"}}).Result()
	require.NoError(t, err)
	sink := &recordingSnapshotSink{}
	monitor := NewQueueMonitor(client, []string{"tracking:{queue}:events"}, "workers", "tracking:{queue}:dead", sink)
	require.NoError(t, monitor.ObserveQueues(ctx))
	require.EqualValues(t, 1, sink.snapshot.Pending)
	// miniredis currently reports XINFO GROUPS lag as the stream length. The
	// important contract here is that lag comes from XINFO and is not copied
	// from XPENDING; live acceptance covers Redis' exact undelivered count.
	require.EqualValues(t, 2, sink.snapshot.Lag)
	require.NotEqual(t, sink.snapshot.Pending, sink.snapshot.Lag)
	require.EqualValues(t, 1, sink.snapshot.DeadLetters)
}

type recordingSnapshotSink struct{ snapshot QueueSnapshot }

func (s *recordingSnapshotSink) SetQueueSnapshot(v QueueSnapshot) { s.snapshot = v }
