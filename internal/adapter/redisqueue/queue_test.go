package redisqueue_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/ports/contracttest"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestMessageQueueContract(t *testing.T) {
	client := inMemoryRedisClient(t)
	runContract(t, client)
}

func TestPublishRejectsMissingHashTagBeforeMutation(t *testing.T) {
	client := inMemoryRedisClient(t)
	_, err := redisqueue.New(client).PublishBatch(context.Background(), "tracking:events", ports.BatchEnvelope{BatchID: "bad", Payload: []byte(`{}`)})
	require.ErrorContains(t, err, "Redis hash tag")
	require.Equal(t, int64(0), client.DBSize(context.Background()).Val())
}

func TestMoveToDLQRejectsMismatchedHashTagsBeforeMutation(t *testing.T) {
	client := inMemoryRedisClient(t)
	ctx := context.Background()
	queue := redisqueue.New(client, redisqueue.WithDeadStream("tracking:{dead}:dead"))
	_, err := queue.PublishBatch(ctx, "tracking:{source}:events", ports.BatchEnvelope{BatchID: "b", Payload: []byte(`{}`)})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, "tracking:{source}:events", "workers", "c", 1, time.Millisecond)
	require.NoError(t, err)
	err = queue.MoveToDLQ(ctx, "tracking:{source}:events", "workers", messages[0], errors.New("bad"))
	require.ErrorContains(t, err, "same Redis hash tag")
	pending, pendingErr := client.XPending(ctx, "tracking:{source}:events", "workers").Result()
	require.NoError(t, pendingErr)
	require.Equal(t, int64(1), pending.Count)
}

func TestMessageQueueContractAgainstComposeRedis(t *testing.T) {
	address := os.Getenv("GO_DOT_REDIS_TEST_ADDRESS")
	if address == "" {
		t.Skip("set GO_DOT_REDIS_TEST_ADDRESS to run the contract against Compose Redis")
	}
	client := liveRedisClient(t, address)
	runContract(t, client)
}

func runContract(t *testing.T, client *redis.Client) {
	t.Helper()
	contracttest.RunMessageQueueContract(t, func() contracttest.QueueFixture {
		return contracttest.QueueFixture{
			Queue:      redisqueue.New(client),
			DeadStream: ports.DeadLetterStream,
			Reset: func(ctx context.Context) error {
				return client.FlushDB(ctx).Err()
			},
			Pending: func(ctx context.Context, stream, group string) (int64, error) {
				pending, err := client.XPending(ctx, stream, group).Result()
				return pending.Count, err
			},
		}
	})
}

func TestMoveToDLQIsAtomic(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	ctx := context.Background()
	stream := "contract:{queue}:atomic-dlq"
	_, err := queue.PublishBatch(ctx, stream, ports.BatchEnvelope{
		BatchID: "batch", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, stream, "workers", "consumer", 1, 10*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	// Force the DLQ XADD to fail. If the adapter used separate commands, the
	// source ACK could still succeed and lose the event.
	require.NoError(t, client.Set(ctx, ports.DeadLetterStream, "wrong-type", 0).Err())
	err = queue.MoveToDLQ(ctx, stream, "workers", messages[0], context.DeadlineExceeded)
	require.Error(t, err)
	pending, pendingErr := client.XPending(ctx, stream, "workers").Result()
	require.NoError(t, pendingErr)
	require.Equal(t, int64(1), pending.Count)
}

func TestMoveToDLQIsIdempotentAfterLostResponse(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	ctx := context.Background()
	_, err := queue.PublishBatch(ctx, "tracking:{queue}:events", ports.BatchEnvelope{
		BatchID: "batch-1", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, "tracking:{queue}:events", "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.NoError(t, queue.MoveToDLQ(ctx, "tracking:{queue}:events", "workers", messages[0], context.DeadlineExceeded))
	require.NoError(t, queue.MoveToDLQ(ctx, "tracking:{queue}:events", "workers", messages[0], context.DeadlineExceeded))

	dlq, err := client.XLen(ctx, ports.DeadLetterStream).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), dlq)
}

func TestMoveToDLQUsesConfiguredStream(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client, redisqueue.WithDeadStream("custom:{queue}:dead"))
	ctx := context.Background()
	_, err := queue.PublishBatch(ctx, "tracking:{queue}:events", ports.BatchEnvelope{
		BatchID: "batch-1", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, "tracking:{queue}:events", "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, queue.MoveToDLQ(ctx, "tracking:{queue}:events", "workers", messages[0], context.DeadlineExceeded))

	require.Equal(t, int64(1), client.XLen(ctx, "custom:{queue}:dead").Val())
	require.Equal(t, int64(0), client.XLen(ctx, ports.DeadLetterStream).Val())
}

func TestListDeadLettersReturnsOnlyProjectMetadataWithoutPayload(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	queue := redisqueue.New(client, redisqueue.WithDeadStream("tracking:{queue}:dead"))
	ctx := context.Background()
	for _, values := range []map[string]any{
		{"batch_id": "other", "payload": `{"project_id":8,"secret":"x"}`, "deliveries": "2", "first_seen": "1000", "last_error": "outage"},
		{"batch_id": "mine", "payload": `{"project_id":7,"events":[]}`, "deliveries": "3", "first_seen": "2000", "last_error": "bad"},
	} {
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: "tracking:{queue}:dead", Values: values}).Err())
	}

	page, err := queue.ListDeadLetters(ctx, statistics.DeadLetterFilter{ProjectID: 7, Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "mine", page.Items[0].BatchID)
	require.Equal(t, 3, page.Items[0].Deliveries)
	require.NotContains(t, fmt.Sprintf("%+v", page.Items[0]), "secret")
}

func TestListDeadLettersBoundsSparseScanAndContinuesFromLastScanned(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	queue := redisqueue.New(client, redisqueue.WithDeadStream("tracking:{queue}:dead"), redisqueue.WithDeadLetterScanLimit(3))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: "tracking:{queue}:dead", Values: map[string]any{"batch_id": fmt.Sprintf("other-%d", i), "payload": `{"project_id":8}`, "deliveries": "1", "first_seen": "1000"}}).Err())
	}
	page, err := queue.ListDeadLetters(ctx, statistics.DeadLetterFilter{ProjectID: 7, Limit: 2})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.NotEmpty(t, page.NextCursor)
	next, err := queue.ListDeadLetters(ctx, statistics.DeadLetterFilter{ProjectID: 7, Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Empty(t, next.Items)
	require.Empty(t, next.NextCursor)
}

func TestRejectsPositiveSubMillisecondDurations(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	ctx := context.Background()
	_, err := queue.Consume(ctx, "tracking:{queue}:events", "workers", "consumer", 1, 500*time.Microsecond)
	require.ErrorContains(t, err, "block duration")
	_, err = queue.ReclaimStale(ctx, "tracking:{queue}:events", "workers", "consumer", 500*time.Microsecond, 1)
	require.ErrorContains(t, err, "idle duration")
	err = queue.Retry(ctx, "tracking:{queue}:events", "workers", ports.Message{ID: "1-0", BatchID: "b", Payload: []byte(`{}`)}, errors.New("temporary"), time.Microsecond)
	require.ErrorContains(t, err, "retry delay")
}

func TestPublishStoresRequiredEnvelopeFields(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	createdAt := time.Unix(1_700_000_000, 123_000_000).UTC()
	id, err := queue.PublishBatch(context.Background(), "tracking:{queue}:events", ports.BatchEnvelope{
		BatchID: "batch-1", Payload: []byte(`{"ok":true}`), CreatedAt: createdAt,
	})
	require.NoError(t, err)

	messages, err := client.XRangeN(context.Background(), "tracking:{queue}:events", id, id, 1).Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "batch-1", messages[0].Values["batch_id"])
	require.Equal(t, `{"ok":true}`, messages[0].Values["payload"])
	require.Equal(t, "1", messages[0].Values["deliveries"])
	require.Equal(t, "1700000000123", messages[0].Values["first_seen"])
	require.Equal(t, "", messages[0].Values["last_error"])
}

func TestPublishDeduplicationTTLIsConfigurable(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client, redisqueue.WithPublishDedupTTL(5*time.Minute))
	_, err := queue.PublishBatch(context.Background(), "tracking:{queue}:events", ports.BatchEnvelope{BatchID: "batch-ttl", Payload: []byte(`{"ok":true}`)})
	require.NoError(t, err)
	keys, err := client.Keys(context.Background(), "queue:{queue}:publish:*").Result()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	ttl, err := client.TTL(context.Background(), keys[0]).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 4*time.Minute)
	require.LessOrEqual(t, ttl, 5*time.Minute)
}

func TestConsumeCreatesGroupAndStreamWhenAbsent(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	messages, err := queue.Consume(context.Background(), "tracking:{queue}:events", "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.Empty(t, messages)

	groups, err := client.XInfoGroups(context.Background(), "tracking:{queue}:events").Result()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "workers", groups[0].Name)
}

func TestConsumeReturnsMalformedEntryBesideValidSibling(t *testing.T) {
	client := inMemoryRedisClient(t)
	ctx := context.Background()
	stream := "tracking:{queue}:events"
	_, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"payload": `{"missing_batch":true}`, "deliveries": "1"}}).Result()
	require.NoError(t, err)
	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"batch_id": "missing-payload", "deliveries": "1"}}).Result()
	require.NoError(t, err)
	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"batch_id": "bad-deliveries", "payload": `{"bad":true}`, "deliveries": "oops"}}).Result()
	require.NoError(t, err)
	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"batch_id": "good", "payload": `{"ok":true}`, "deliveries": "1", "first_seen": "1", "last_error": ""}}).Result()
	require.NoError(t, err)

	messages, err := redisqueue.New(client).Consume(ctx, stream, "workers", "consumer", 10, time.Millisecond)
	require.NoError(t, err)
	require.Len(t, messages, 4)
	require.NotNil(t, messages[0].Malformed)
	require.Contains(t, messages[0].Malformed.Reason, "batch_id")
	require.NotNil(t, messages[1].Malformed)
	require.Contains(t, messages[1].Malformed.Reason, "payload")
	require.NotNil(t, messages[2].Malformed)
	require.Contains(t, messages[2].Malformed.Reason, "deliveries")
	require.JSONEq(t, `{"batch_id":"bad-deliveries","deliveries":"oops","payload":"{\"bad\":true}"}`, string(messages[2].Malformed.RawEnvelope))
	require.Nil(t, messages[3].Malformed)
	require.Equal(t, "good", messages[3].BatchID)
}

func TestReclaimReturnsMalformedPendingEntry(t *testing.T) {
	client := inMemoryRedisClient(t)
	ctx := context.Background()
	stream := "tracking:{queue}:events"
	_, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"batch_id": "bad", "payload": `{"bad":true}`, "deliveries": "zero"}}).Result()
	require.NoError(t, err)
	queue := redisqueue.New(client)
	messages, err := queue.Consume(ctx, stream, "workers", "first", 1, time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, messages[0].Malformed)
	time.Sleep(2 * time.Millisecond)

	reclaimed, err := queue.ReclaimStale(ctx, stream, "workers", "second", time.Millisecond, 1)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.NotNil(t, reclaimed[0].Malformed)
	require.Contains(t, reclaimed[0].Malformed.Reason, "deliveries")
}

func TestReclaimPreservesDurableEnvelopeAttemptsInsteadOfXPendingCount(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	stream := "tracking:{queue}:events"
	group := "workers"
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	_, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"batch_id": "b", "payload": "{}", "deliveries": "2", "first_seen": time.Now().UnixMilli()}}).Result()
	require.NoError(t, err)
	_, err = client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: "dead", Streams: []string{stream, ">"}, Count: 1}).Result()
	require.NoError(t, err)
	for range 3 {
		_, _, err = client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: stream, Group: group, Consumer: "other", MinIdle: 0, Start: "0-0", Count: 1}).Result()
		require.NoError(t, err)
	}
	time.Sleep(2 * time.Millisecond)
	messages, err := redisqueue.New(client).ReclaimStale(ctx, stream, group, "scheduler", time.Millisecond, 1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, 2, messages[0].Deliveries)
}

func TestDelayedRetrySurvivesQueueRestartAndIncrementsDelivery(t *testing.T) {
	client := inMemoryRedisClient(t)
	ctx := context.Background()
	stream := "tracking:{queue}:events"
	first := redisqueue.New(client)
	_, err := first.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "restart", Payload: []byte(`{"ok":true}`)})
	require.NoError(t, err)
	messages, err := first.Consume(ctx, stream, "workers", "before-restart", 1, time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, first.Retry(ctx, stream, "workers", messages[0], context.DeadlineExceeded, time.Millisecond))
	time.Sleep(2 * time.Millisecond)

	afterRestart := redisqueue.New(client)
	promoted, err := afterRestart.PromoteDue(ctx, stream, 10)
	require.NoError(t, err)
	require.Equal(t, 1, promoted)
	retried, err := afterRestart.Consume(ctx, stream, "workers", "after-restart", 1, time.Millisecond)
	require.NoError(t, err)
	require.Len(t, retried, 1)
	require.Equal(t, 2, retried[0].Deliveries)
}

func TestScheduleRetryIsAtomicWhenDurableScheduleWriteFails(t *testing.T) {
	client := inMemoryRedisClient(t)
	ctx := context.Background()
	stream := "tracking:{queue}:events"
	queue := redisqueue.New(client)
	_, err := queue.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "atomic", Payload: []byte(`{"ok":true}`)})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, stream, "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, stream+":retry:schedule", "wrong-type", 0).Err())

	err = queue.Retry(ctx, stream, "workers", messages[0], context.DeadlineExceeded, time.Second)
	require.Error(t, err)
	pending, pendingErr := client.XPending(ctx, stream, "workers").Result()
	require.NoError(t, pendingErr)
	require.Equal(t, int64(1), pending.Count, "failed schedule must not ACK the source")
}

func inMemoryRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func liveRedisClient(t *testing.T, address string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("GO_DOT_REDIS_TEST_ADDRESS is set but Redis ping failed: %v", err)
	}
	return client
}
