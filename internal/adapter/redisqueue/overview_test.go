package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type redisCommandHook struct {
	commands atomic.Int64
	failEval atomic.Bool
}

func (h *redisCommandHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *redisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *redisCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.commands.Add(1)
		if h.failEval.Load() && (cmd.Name() == "eval" || cmd.Name() == "evalsha") {
			return errors.New("injected reconciliation interruption")
		}
		return next(ctx, cmd)
	}
}

func TestReconcileOverviewIsBoundedAndPersistentAcrossRestart(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, dead, group := "tracking:{queue}:events", "tracking:{queue}:dead", "workers"
	record := func(stream, batch string, project int) {
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{
			fieldBatchID: batch, fieldPayload: fmt.Sprintf(`{"project_id":%d}`, project),
			fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli(),
		}}).Err())
	}
	for i := 0; i < 7; i++ {
		record(stream, fmt.Sprintf("event-%d", i), 9)
	}
	for i := 0; i < 5; i++ {
		record(dead, fmt.Sprintf("dead-%d", i), 9)
	}
	require.NoError(t, client.XGroupCreate(ctx, stream, group, "0").Err())

	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(2))
	require.NoError(t, q.ReconcileOverview(ctx))
	first, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.LessOrEqual(t, first.Pending+first.DeadLetters, int64(2), "one invocation must share one item budget across scan kinds")
	require.NotEmpty(t, client.HGetAll(ctx, q.overviewCursorKey()).Val(), "progress must live in Redis, not process memory")

	// A new Queue instance resumes the persisted cursor and every bounded pass
	// eventually covers both the source stream and the configured DLQ.
	q = New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(2))
	for i := 0; i < 20; i++ {
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	got, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 7, got.Pending)
	require.EqualValues(t, 5, got.DeadLetters)
}

func TestReconcileOverviewRoundRobinDoesNotStarveDLQDuringStreamGrowth(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, dead, group := "tracking:{queue}:events", "tracking:{queue}:dead", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	for i := 0; i < 4; i++ {
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: dead, Values: map[string]any{
			fieldBatchID: fmt.Sprintf("dead-%d", i), fieldPayload: `{"project_id":8}`,
			fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli(),
		}}).Err())
	}
	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(1))
	for i := 0; i < 16; i++ {
		_, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{
			fieldBatchID: fmt.Sprintf("growing-%d", i), fieldPayload: `{"project_id":9}`,
			fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli(),
		}}).Result()
		require.NoError(t, err)
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	snapshot, err := q.Snapshot(ctx, 8)
	require.NoError(t, err)
	require.EqualValues(t, 4, snapshot.DeadLetters, "round-robin passes must service DLQ despite a continuously growing source")
}

func TestReconcileOverviewRebuildsDLQCountsExactlyOncePerMessageAndProject(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, dead, group := "tracking:{queue}:events", "tracking:{queue}:dead", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	for i, payload := range []string{`{"project_id":7}`, `{"project_id":8}`, `{"project_id":7}`, `not-json`, `{"project_id":0}`} {
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: dead, Values: map[string]any{
			fieldBatchID: fmt.Sprintf("dead-%d", i), fieldPayload: payload,
			fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli(),
		}}).Err())
	}
	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(2))
	for i := 0; i < 12; i++ {
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	seven, err := q.Snapshot(ctx, 7)
	require.NoError(t, err)
	eight, err := q.Snapshot(ctx, 8)
	require.NoError(t, err)
	require.EqualValues(t, 2, seven.DeadLetters)
	require.EqualValues(t, 1, eight.DeadLetters)

	for i := 0; i < 12; i++ {
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	seven, _ = q.Snapshot(ctx, 7)
	eight, _ = q.Snapshot(ctx, 8)
	require.EqualValues(t, 2, seven.DeadLetters, "repeated full scans must be idempotent")
	require.EqualValues(t, 1, eight.DeadLetters)
}

func TestMoveToDLQAndReconcileSharePerMessageIdempotencyMapping(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	stream, dead, group := "tracking:{queue}:events", "tracking:{queue}:dead", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(1))
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "moved", Payload: []byte(`{"project_id":7}`), CreatedAt: time.Now()})
	require.NoError(t, err)
	messages, err := q.Consume(ctx, stream, group, "worker", 1, 0)
	require.NoError(t, err)
	require.NoError(t, q.MoveToDLQ(ctx, stream, group, messages[0], errors.New("poison")))
	for i := 0; i < 8; i++ {
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	snapshot, err := q.Snapshot(ctx, 7)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.DeadLetters, "reconciliation must not recount a DLQ message indexed by MoveToDLQ")

	// Losing the complete derived DLQ overview can be repaired from the stream.
	require.NoError(t, client.Del(ctx, q.deadLetterCountKey(), q.deadLetterIndexKey(), q.overviewCursorKey()).Err())
	for i := 0; i < 8; i++ {
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	snapshot, err = q.Snapshot(ctx, 7)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.DeadLetters)
}

func TestReconcileOverviewFairlyRotatesAcrossConfiguredStreams(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	events, invalid, dead, group := "tracking:{queue}:events", "tracking:{queue}:invalid", "tracking:{queue}:dead", "workers"
	for _, stream := range []string{events, invalid} {
		require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	}
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: dead, Values: map[string]any{fieldBatchID: "d", fieldPayload: `{"project_id":7}`, fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli()}}).Err())
	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{events, invalid}, group), WithOverviewReconcileLimit(1))
	for i := 0; i < 24; i++ {
		for _, stream := range []string{events, invalid} {
			require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{fieldBatchID: fmt.Sprintf("%s-%d", stream, i), fieldPayload: `{"project_id":9}`, fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli()}}).Err())
		}
		require.NoError(t, q.ReconcileOverview(ctx))
	}
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.GreaterOrEqual(t, snapshot.Pending, int64(6), "both growing streams must receive repeated turns")
	deadSnapshot, err := q.Snapshot(ctx, 7)
	require.NoError(t, err)
	require.EqualValues(t, 1, deadSnapshot.DeadLetters)
}

func TestReconcileOverviewDoesNotAdvanceCandidateCursorWhenAtomicProcessingIsInterrupted(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	hook := &redisCommandHook{}
	client.AddHook(hook)
	ctx := context.Background()
	stream, group := "tracking:{queue}:events", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	q := New(client, WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(1))
	_, err := q.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "crash", Payload: []byte(`{"project_id":9}`), CreatedAt: time.Now()})
	require.NoError(t, err)
	_, err = q.Consume(ctx, stream, group, "worker", 1, 0)
	require.NoError(t, err)
	require.NoError(t, client.Del(ctx, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey("9")).Err())

	hook.failEval.Store(true)
	require.ErrorContains(t, q.ReconcileOverview(ctx), "injected reconciliation interruption")
	require.Empty(t, client.HGet(ctx, q.overviewCursorKey(), q.cursorField("pending", stream)).Val(), "failed candidate processing must not skip the candidate")
	hook.failEval.Store(false)
	require.NoError(t, q.ReconcileOverview(ctx))
	snapshot, err := q.Snapshot(ctx, 9)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Pending)
}

func TestReconcileOverviewHasBoundedCommandCountAndKeepsExhaustedDLQCursor(t *testing.T) {
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	hook := &redisCommandHook{}
	client.AddHook(hook)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	stream, dead, group := "tracking:{queue}:events", "tracking:{queue}:dead", "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: dead, Values: map[string]any{fieldBatchID: "one", fieldPayload: `{"project_id":7}`, fieldDeliveries: 1, fieldFirstSeen: time.Now().UnixMilli()}}).Result()
	require.NoError(t, err)
	q := New(client, WithDeadStream(dead), WithOverviewStreams([]string{stream}, group), WithOverviewReconcileLimit(1))
	for i := 0; i < 12; i++ {
		before := hook.commands.Load()
		require.NoError(t, q.ReconcileOverview(ctx))
		require.LessOrEqual(t, hook.commands.Load()-before, int64(20), "each pass must have a bounded Redis command count")
	}
	require.NoError(t, ctx.Err())
	require.Equal(t, id, client.HGet(ctx, q.overviewCursorKey(), q.cursorField("dead", dead)).Val(), "an exhausted forward cursor must not rewind and rescan history")
}

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
