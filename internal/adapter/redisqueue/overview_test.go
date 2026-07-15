package redisqueue

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOverviewSnapshotUsesProjectPendingIndexAndProjectDLQCount(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, invalid, dead, group := "tracking:{queue}:events", "tracking:{queue}:invalid", "tracking:{queue}:dead", "workers"
	for _, s := range []string{stream, invalid} {
		require.NoError(t, client.XGroupCreateMkStream(ctx, s, group, "0").Err())
	}
	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream, invalid}, group))
	created := time.Now().Add(-2 * time.Minute)
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "p9", Payload: []byte(`{"project_id":9}`), CreatedAt: created})
	require.NoError(t, err)
	_, err = q.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "p8", Payload: []byte(`{"project_id":8}`), CreatedAt: created})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "c", 10, 0)
	require.NoError(t, err)
	var msg ports.Message
	for _, item := range messages {
		if string(item.Payload) == `{"project_id":9}` {
			msg = item
		}
	}
	require.NoError(t, q.MoveToDLQ(ctx, stream, group, msg, context.DeadlineExceeded))
	p9, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, p9.Pending)
	require.EqualValues(t, 1, p9.DeadLetters)
	p8, err := q.Snapshot(ctx, 8)
	require.NoError(t, err)
	require.EqualValues(t, 1, p8.Pending)
	require.GreaterOrEqual(t, p8.OldestPendingAge, 2*time.Minute)
	require.Zero(t, p8.DeadLetters)
}

func TestOverviewPendingIndexDecrementsOnlyOnceOnRepeatedAck(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	q := New(client, WithOverviewStreams([]string{stream}, group))
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "p9", Payload: []byte(`{"project_id":9}`), CreatedAt: time.Now()})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "c", 1, 0)
	require.NoError(t, err)
	require.NoError(t, q.Ack(ctx, stream, group, messages[0].ID))
	require.NoError(t, q.Ack(ctx, stream, group, messages[0].ID))
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, snapshot.Pending)
}

func TestOverviewPendingIndexSurvivesRetryPromotion(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	q := New(client, WithOverviewStreams([]string{stream}, group))
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "retry-p9", Payload: []byte(`{"project_id":9}`), CreatedAt: time.Now()})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "c", 1, 0)
	require.NoError(t, err)
	require.NoError(t, q.Retry(ctx, stream, group, messages[0], context.DeadlineExceeded, time.Millisecond))
	time.Sleep(2 * time.Millisecond)
	promoted, err := q.PromoteDue(ctx, stream, 1)
	require.NoError(t, err)
	require.Equal(t, 1, promoted)
	messages, err = q.Consume(ctx, stream, group, "c", 1, 0)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.NoError(t, q.Ack(ctx, stream, group, messages[0].ID))
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, snapshot.Pending)
}

func TestOverviewOldestIndexDoesNotCollideForSameBatchAcrossStreams(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	events, invalid, group := "tracking:{queue}:events", "tracking:{queue}:invalid", "workers"
	for _, stream := range []string{events, invalid} {
		require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	}
	q := New(client, WithOverviewStreams([]string{events, invalid}, group))
	payload := []byte(`{"project_id":9}`)
	created := time.Now().Add(-time.Minute)
	_, err := q.PublishBatch(ctx, events, ports.BatchEnvelope{BatchID: "same", Payload: payload, CreatedAt: created})
	require.NoError(t, err)
	_, err = q.PublishBatch(ctx, invalid, ports.BatchEnvelope{BatchID: "same", Payload: payload, CreatedAt: created})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, events, group, "c", 1, 0)
	require.NoError(t, err)
	require.NoError(t, q.Ack(ctx, events, group, messages[0].ID))
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Pending)
	require.GreaterOrEqual(t, snapshot.OldestPendingAge, time.Minute)
}
