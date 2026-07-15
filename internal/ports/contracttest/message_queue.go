package contracttest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/stretchr/testify/require"
)

type QueueFixture struct {
	Queue   ports.MessageQueue
	Reset   func(context.Context) error
	Pending func(context.Context, string, string) (int64, error)
}

type Factory func() QueueFixture

func RunMessageQueueContract(t *testing.T, factory Factory) {
	t.Helper()

	t.Run("publication order and blocking consume", func(t *testing.T) {
		fixture := resetFixture(t, factory)
		ctx := context.Background()
		stream := unique("contract:order")
		first := ports.BatchEnvelope{BatchID: "batch-1", Payload: []byte(`{"n":1}`), CreatedAt: time.Unix(10, 0).UTC()}
		second := ports.BatchEnvelope{BatchID: "batch-2", Payload: []byte(`{"n":2}`), CreatedAt: time.Unix(20, 0).UTC()}
		_, err := fixture.Queue.PublishBatch(ctx, stream, first)
		require.NoError(t, err)
		_, err = fixture.Queue.PublishBatch(ctx, stream, second)
		require.NoError(t, err)

		messages, err := fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 2, 50*time.Millisecond)
		require.NoError(t, err)
		require.Len(t, messages, 2)
		require.Equal(t, []string{"batch-1", "batch-2"}, []string{messages[0].BatchID, messages[1].BatchID})

		started := time.Now()
		messages, err = fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 1, 40*time.Millisecond)
		require.NoError(t, err)
		require.Empty(t, messages)
		require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond)
	})

	t.Run("ack removes pending message", func(t *testing.T) {
		fixture := resetFixture(t, factory)
		ctx := context.Background()
		stream := unique("contract:ack")
		_, err := fixture.Queue.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "batch", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now()})
		require.NoError(t, err)
		messages, err := fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 1, 10*time.Millisecond)
		require.NoError(t, err)
		require.Len(t, messages, 1)
		require.NoError(t, fixture.Queue.Ack(ctx, stream, "workers", messages[0].ID))
		pending, err := fixture.Pending(ctx, stream, "workers")
		require.NoError(t, err)
		require.Zero(t, pending)
	})

	t.Run("stale reclaim increases delivery count", func(t *testing.T) {
		fixture := resetFixture(t, factory)
		ctx := context.Background()
		stream := unique("contract:reclaim")
		_, err := fixture.Queue.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "batch", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now()})
		require.NoError(t, err)
		messages, err := fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 1, 10*time.Millisecond)
		require.NoError(t, err)
		require.Equal(t, 1, messages[0].Deliveries)
		time.Sleep(5 * time.Millisecond)
		reclaimed, err := fixture.Queue.ReclaimStale(ctx, stream, "workers", "consumer-b", time.Millisecond, 10)
		require.NoError(t, err)
		require.Len(t, reclaimed, 1)
		require.Equal(t, messages[0].Payload, reclaimed[0].Payload)
		require.GreaterOrEqual(t, reclaimed[0].Deliveries, 2)
	})

	t.Run("retry republishes with error and incremented deliveries", func(t *testing.T) {
		fixture := resetFixture(t, factory)
		ctx := context.Background()
		stream := unique("contract:retry")
		_, err := fixture.Queue.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "batch", Payload: []byte(`{"ok":true}`), CreatedAt: time.Now()})
		require.NoError(t, err)
		messages, err := fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 1, 10*time.Millisecond)
		require.NoError(t, err)
		require.NoError(t, fixture.Queue.Retry(ctx, stream, "workers", messages[0], fmt.Errorf("temporary")))
		retried, err := fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 1, 10*time.Millisecond)
		require.NoError(t, err)
		require.Len(t, retried, 1)
		require.Equal(t, 2, retried[0].Deliveries)
		require.Equal(t, "temporary", retried[0].LastError)
	})

	t.Run("dlq preserves payload and acknowledges source", func(t *testing.T) {
		fixture := resetFixture(t, factory)
		ctx := context.Background()
		stream := unique("contract:dlq")
		payload := []byte(`{"nested":{"value":"unchanged"}}`)
		_, err := fixture.Queue.PublishBatch(ctx, stream, ports.BatchEnvelope{BatchID: "batch", Payload: payload, CreatedAt: time.Now()})
		require.NoError(t, err)
		messages, err := fixture.Queue.Consume(ctx, stream, "workers", "consumer-a", 1, 10*time.Millisecond)
		require.NoError(t, err)
		require.NoError(t, fixture.Queue.MoveToDLQ(ctx, stream, "workers", messages[0], fmt.Errorf("permanent")))
		require.Equal(t, int64(0), mustPending(t, fixture.Pending, stream, "workers"))

		dlqMessages, err := fixture.Queue.Consume(ctx, ports.DLQStream(stream), "dlq-readers", "reader", 1, 10*time.Millisecond)
		require.NoError(t, err)
		require.Len(t, dlqMessages, 1)
		require.Equal(t, payload, dlqMessages[0].Payload)
		require.Equal(t, "permanent", dlqMessages[0].LastError)
	})
}

func resetFixture(t *testing.T, factory Factory) QueueFixture {
	t.Helper()
	fixture := factory()
	require.NotNil(t, fixture.Queue)
	require.NotNil(t, fixture.Pending)
	require.NoError(t, fixture.Reset(context.Background()))
	return fixture
}

func mustPending(t *testing.T, pending func(context.Context, string, string) (int64, error), stream, group string) int64 {
	t.Helper()
	count, err := pending(context.Background(), stream, group)
	require.NoError(t, err)
	return count
}

func unique(prefix string) string {
	return fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano())
}
