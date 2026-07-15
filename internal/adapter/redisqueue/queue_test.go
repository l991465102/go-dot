package redisqueue_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/ports/contracttest"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestMessageQueueContract(t *testing.T) {
	client := inMemoryRedisClient(t)
	runContract(t, client)
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
	stream := "contract:atomic-dlq"
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
	_, err := queue.PublishBatch(ctx, "tracking:events", ports.BatchEnvelope{
		BatchID: "batch-1", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, "tracking:events", "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.NoError(t, queue.MoveToDLQ(ctx, "tracking:events", "workers", messages[0], context.DeadlineExceeded))
	require.NoError(t, queue.MoveToDLQ(ctx, "tracking:events", "workers", messages[0], context.DeadlineExceeded))

	dlq, err := client.XLen(ctx, ports.DeadLetterStream).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), dlq)
}

func TestMoveToDLQUsesConfiguredStream(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client, redisqueue.WithDeadStream("custom:dead"))
	ctx := context.Background()
	_, err := queue.PublishBatch(ctx, "tracking:events", ports.BatchEnvelope{
		BatchID: "batch-1", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	messages, err := queue.Consume(ctx, "tracking:events", "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, queue.MoveToDLQ(ctx, "tracking:events", "workers", messages[0], context.DeadlineExceeded))

	require.Equal(t, int64(1), client.XLen(ctx, "custom:dead").Val())
	require.Equal(t, int64(0), client.XLen(ctx, ports.DeadLetterStream).Val())
}

func TestRejectsPositiveSubMillisecondDurations(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	ctx := context.Background()
	_, err := queue.Consume(ctx, "tracking:events", "workers", "consumer", 1, 500*time.Microsecond)
	require.ErrorContains(t, err, "block duration")
	_, err = queue.ReclaimStale(ctx, "tracking:events", "workers", "consumer", 500*time.Microsecond, 1)
	require.ErrorContains(t, err, "idle duration")
}

func TestPublishStoresRequiredEnvelopeFields(t *testing.T) {
	client := inMemoryRedisClient(t)
	queue := redisqueue.New(client)
	createdAt := time.Unix(1_700_000_000, 123_000_000).UTC()
	id, err := queue.PublishBatch(context.Background(), "tracking:events", ports.BatchEnvelope{
		BatchID: "batch-1", Payload: []byte(`{"ok":true}`), CreatedAt: createdAt,
	})
	require.NoError(t, err)

	messages, err := client.XRangeN(context.Background(), "tracking:events", id, id, 1).Result()
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
	_, err := queue.PublishBatch(context.Background(), "tracking:events", ports.BatchEnvelope{BatchID: "batch-ttl", Payload: []byte(`{"ok":true}`)})
	require.NoError(t, err)
	keys, err := client.Keys(context.Background(), "queue:publish:*").Result()
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
	messages, err := queue.Consume(context.Background(), "tracking:events", "workers", "consumer", 1, time.Millisecond)
	require.NoError(t, err)
	require.Empty(t, messages)

	groups, err := client.XInfoGroups(context.Background(), "tracking:events").Result()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "workers", groups[0].Name)
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
