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

func TestReconcileOverviewRestoresOnlyOutstandingStreamMessagesByProject(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	restored := New(client, WithOverviewStreams([]string{stream}, group))
	record := func(batch, payload string, firstSeen int64) string {
		id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{
			fieldBatchID: batch, fieldPayload: payload, fieldDeliveries: 1, fieldFirstSeen: firstSeen,
		}}).Result()
		require.NoError(t, err)
		return id
	}
	old := time.Now().Add(-3 * time.Minute).UnixMilli()
	ackedID := record("acked-p9", `{"project_id":9}`, old)
	pendingID := record("pending-p9", `{"project_id":9}`, old+1)
	_ = record("new-p9", `{"project_id":9}`, old+2)
	_ = record("new-p8", `{"project_id":8}`, old+3)
	require.NoError(t, client.XGroupCreate(ctx, stream, group, "0").Err())
	delivered, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: "old-worker", Streams: []string{stream, ">"}, Count: 2,
	}).Result()
	require.NoError(t, err)
	require.Equal(t, ackedID, delivered[0].Messages[0].ID)
	require.Equal(t, pendingID, delivered[0].Messages[1].ID)
	require.NoError(t, client.XAck(ctx, stream, group, ackedID).Err())

	require.NoError(t, restored.ReconcileOverview(ctx))
	p9, err := restored.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 2, p9.Pending, "one PEL entry plus one never-delivered entry")
	require.GreaterOrEqual(t, p9.OldestPendingAge, 3*time.Minute)
	p8, err := restored.Snapshot(ctx, 8)
	require.NoError(t, err)
	require.EqualValues(t, 1, p8.Pending)

	require.NoError(t, restored.ReconcileOverview(ctx))
	p9, err = restored.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 2, p9.Pending, "reconciliation is idempotent")
}

func TestReconcileOverviewRestoresDelayedRetryAndTransfersIndexOnPromotion(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	q := New(client, WithOverviewStreams([]string{stream}, group))
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{
		BatchID: "delayed-p9", Payload: []byte(`{"project_id":9}`), CreatedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "old-worker", 1, 0)
	require.NoError(t, err)
	require.NoError(t, q.Retry(ctx, stream, group, messages[0], context.DeadlineExceeded, time.Millisecond))
	require.NoError(t, client.Del(ctx, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey("9")).Err())

	restored := New(client, WithOverviewStreams([]string{stream}, group))
	require.NoError(t, restored.ReconcileOverview(ctx))
	require.NoError(t, restored.ReconcileOverview(ctx))
	snapshot, err := restored.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Pending)

	time.Sleep(2 * time.Millisecond)
	promoted, err := restored.PromoteDue(ctx, stream, 1)
	require.NoError(t, err)
	require.Equal(t, 1, promoted)
	messages, err = restored.Consume(ctx, stream, group, "new-worker", 1, 0)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.NoError(t, restored.Ack(ctx, stream, group, messages[0].ID))
	snapshot, err = restored.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, snapshot.Pending)
}

func TestReconcileOverviewConcurrentAckCannotRestoreCompletedMessage(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	q := New(client, WithOverviewStreams([]string{stream}, group))
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{
		BatchID: "ack-race-p9", Payload: []byte(`{"project_id":9}`), CreatedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "worker", 1, 0)
	require.NoError(t, err)
	require.NoError(t, client.Del(ctx, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey("9")).Err())

	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- q.ReconcileOverview(ctx)
	}()
	go func() {
		<-start
		errs <- q.Ack(ctx, stream, group, messages[0].ID)
	}()
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, snapshot.Pending)
}

func TestReconcileOverviewConcurrentPromotionConvergesWithoutStaleRetryIndex(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	q := New(client, WithOverviewStreams([]string{stream}, group))
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{
		BatchID: "promote-race-p9", Payload: []byte(`{"project_id":9}`), CreatedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "old-worker", 1, 0)
	require.NoError(t, err)
	require.NoError(t, q.Retry(ctx, stream, group, messages[0], context.DeadlineExceeded, time.Millisecond))
	require.NoError(t, client.Del(ctx, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey("9")).Err())
	time.Sleep(2 * time.Millisecond)

	start := make(chan struct{})
	errCh := make(chan error, 2)
	go func() {
		<-start
		errCh <- q.ReconcileOverview(ctx)
	}()
	go func() {
		<-start
		_, promoteErr := q.PromoteDue(ctx, stream, 1)
		errCh <- promoteErr
	}()
	close(start)
	require.NoError(t, <-errCh)
	require.NoError(t, <-errCh)
	require.NoError(t, q.ReconcileOverview(ctx), "a subsequent pass converges if promotion crossed the scan boundary")
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Pending)

	messages, err = q.Consume(ctx, stream, group, "new-worker", 1, 0)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.NoError(t, q.Ack(ctx, stream, group, messages[0].ID))
	snapshot, err = q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.Zero(t, snapshot.Pending)
	require.Empty(t, client.HKeys(ctx, q.pendingIndexKey()).Val(), "no retry marker survives promotion and ACK")
}
