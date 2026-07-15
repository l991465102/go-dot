package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/stretchr/testify/require"
)

func TestWorkerStoresThenCountsThenAcks(t *testing.T) {
	queue := &workerQueue{}
	log := &callLog{}
	worker := testWorker(queue, &workerStore{log: log}, &workerCounter{log: log})
	queue.log = log

	require.NoError(t, worker.Handle(context.Background(), validWorkerMessage()))
	require.Equal(t, []string{"store-valid", "count:evt-1", "ack"}, log.entries)
}

func TestWorkerRetriesInfrastructureFailureWithoutAck(t *testing.T) {
	queue := &workerQueue{}
	worker := testWorker(queue, &workerStore{err: errors.New("clickhouse unavailable")}, &workerCounter{})

	err := worker.Handle(context.Background(), validWorkerMessage())
	require.ErrorContains(t, err, "clickhouse unavailable")
	require.Equal(t, 1, queue.retries)
	require.Zero(t, queue.acks)
	require.Zero(t, queue.dlqs)
	require.Equal(t, time.Second, queue.retryDelays[0])
}

func TestRetryBackoffIsExponentialAndBounded(t *testing.T) {
	queue := &workerQueue{}
	worker := testWorker(queue, &workerStore{err: errors.New("outage")}, &workerCounter{})
	worker.cfg.RetryBase = time.Second
	worker.cfg.RetryMax = 3 * time.Second
	for _, deliveries := range []int{1, 2, 3, 4} {
		msg := validWorkerMessage()
		msg.Deliveries = deliveries
		require.Error(t, worker.Handle(context.Background(), msg))
	}
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}, queue.retryDelays)
}

func TestWorkerMovesFifthInfrastructureFailureToDLQWithoutDoubleAck(t *testing.T) {
	queue := &workerQueue{}
	worker := testWorker(queue, &workerStore{err: errors.New("clickhouse unavailable")}, &workerCounter{})
	msg := validWorkerMessage()
	msg.Deliveries = 5

	require.NoError(t, worker.Handle(context.Background(), msg))
	require.Equal(t, 1, queue.dlqs)
	require.Zero(t, queue.acks, "MoveToDLQ atomically acknowledges the source")
}

func TestWorkerMalformedPayloadMovesDirectlyToDLQ(t *testing.T) {
	queue := &workerQueue{}
	worker := testWorker(queue, &workerStore{}, &workerCounter{})
	msg := validWorkerMessage()
	msg.Payload = []byte(`{"batch_id":`)

	require.NoError(t, worker.Handle(context.Background(), msg))
	require.Equal(t, 1, queue.dlqs)
	require.Zero(t, queue.retries)
}

func TestWorkerMovesMalformedRedisEnvelopeToDLQPreservingRawData(t *testing.T) {
	queue := &workerQueue{}
	worker := testWorker(queue, &workerStore{}, &workerCounter{})
	raw := []byte(`{"payload":"{\"bad\":true}"}`)
	msg := ports.Message{ID: "poison-1", Stream: EventStream, Deliveries: 1, Malformed: &ports.MalformedMessage{Reason: "batch_id required", RawEnvelope: raw}}

	require.NoError(t, worker.Handle(context.Background(), msg))
	require.Equal(t, 1, queue.dlqs)
	require.Equal(t, raw, queue.lastDLQ.Malformed.RawEnvelope)
}

func TestWorkerRejectsMismatchedBatchAndIncompleteSnapshot(t *testing.T) {
	for _, mutate := range []func(*ports.Message){
		func(msg *ports.Message) { msg.BatchID = "different" },
		func(msg *ports.Message) {
			var payload batchPayload[NormalizedEvent]
			require.NoError(t, json.Unmarshal(msg.Payload, &payload))
			payload.Events[0].SnapshotID = 0
			msg.Payload, _ = json.Marshal(payload)
		},
	} {
		queue := &workerQueue{}
		worker := testWorker(queue, &workerStore{}, &workerCounter{})
		msg := validWorkerMessage()
		mutate(&msg)
		require.NoError(t, worker.Handle(context.Background(), msg))
		require.Equal(t, 1, queue.dlqs)
	}
}

func TestWorkerRetriesPartialCounterFailureIdempotently(t *testing.T) {
	queue := &workerQueue{}
	store := &workerStore{}
	counter := &workerCounter{failEvent: "evt-2", failOnce: true, applied: map[string]bool{}}
	worker := testWorker(queue, store, counter)
	msg := validWorkerMessage()
	var payload batchPayload[NormalizedEvent]
	require.NoError(t, json.Unmarshal(msg.Payload, &payload))
	second := payload.Events[0]
	second.EventID = "evt-2"
	payload.Events = append(payload.Events, second)
	msg.Payload, _ = json.Marshal(payload)

	require.Error(t, worker.Handle(context.Background(), msg))
	require.Equal(t, 1, queue.retries)
	msg.Deliveries = 2
	require.NoError(t, worker.Handle(context.Background(), msg))
	require.Equal(t, 2, store.validCalls, "ClickHouse retry uses the same dedup token")
	require.Equal(t, []string{"evt-1", "evt-2", "evt-1", "evt-2"}, counter.calls)
	require.Equal(t, 1, queue.acks)
}

func TestWorkerStoresInvalidStreamThenAcks(t *testing.T) {
	queue := &workerQueue{}
	store := &workerStore{}
	worker := testWorker(queue, store, &workerCounter{})
	original := json.RawMessage(`{"event_id":"evt-bad","point_code":"view","platform":"web","channel":"organic"}`)
	payload, err := json.Marshal(batchPayload[invalidEvent]{BatchID: "batch-invalid", ProjectID: 7, ReceivedAt: time.Unix(2, 0).UTC(), Events: []invalidEvent{{ProjectID: 7, BatchID: "batch-invalid", ReceivedAt: time.Unix(2, 0).UTC(), ErrorCode: "PROPERTY_REQUIRED", ErrorMessage: "missing", Original: original}}})
	require.NoError(t, err)

	require.NoError(t, worker.Handle(context.Background(), ports.Message{ID: "2-0", Stream: InvalidEventStream, BatchID: "batch-invalid", Payload: payload, Deliveries: 1}))
	require.Equal(t, 1, store.invalidCalls)
	require.Equal(t, "evt-bad", store.invalid[0].EventID)
	require.Equal(t, 1, queue.acks)
}

func TestWorkerRunUsesBoundedReadAndLeavesMaintenanceToScheduler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &workerQueue{consumeFn: func(_ context.Context, stream, group, consumer string, count int, block time.Duration) ([]ports.Message, error) {
		require.Equal(t, 16, count)
		require.Equal(t, 2*time.Second, block)
		cancel()
		return nil, nil
	}}
	worker := testWorker(queue, &workerStore{}, &workerCounter{})
	worker.cfg.ReclaimIdle = time.Millisecond

	require.NoError(t, worker.Run(ctx))
	require.Zero(t, queue.reclaims)
}

func TestWorkerRunNeverDuplicatesSchedulerReclaimOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	consumes := 0
	queue := &workerQueue{consumeFn: func(_ context.Context, _, _, _ string, _ int, _ time.Duration) ([]ports.Message, error) {
		consumes++
		if consumes == 3 {
			cancel()
		}
		return nil, nil
	}}
	worker := testWorker(queue, &workerStore{}, &workerCounter{})
	worker.cfg.ReclaimIdle = time.Nanosecond

	require.NoError(t, worker.Run(ctx))
	require.Zero(t, queue.reclaims, "Scheduler exclusively owns pending recovery")
}

func TestWorkerRunPropagatesQueueFailure(t *testing.T) {
	queue := &workerQueue{consumeErr: errors.New("redis unavailable")}
	worker := testWorker(queue, &workerStore{}, &workerCounter{})
	err := worker.Run(context.Background())
	require.ErrorContains(t, err, "redis unavailable")
}

func TestWorkerRunCancellationDrainsHandlerAndLeavesUnfinishedPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	store := &workerStore{blockUntilCanceled: true, started: started, done: done}
	delivered := false
	queue := &workerQueue{respectCanceledRetry: true, consumeFn: func(ctx context.Context, stream, _, _ string, _ int, _ time.Duration) ([]ports.Message, error) {
		if stream == EventStream && !delivered {
			delivered = true
			return []ports.Message{validWorkerMessage()}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	worker := testWorker(queue, store, &workerCounter{})
	go func() { <-started; cancel() }()

	require.NoError(t, worker.Run(ctx))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active handler was not drained")
	}
	require.Zero(t, queue.acks)
	require.Zero(t, queue.dlqs)
}

func TestWorkerRunNeverLaunchesMessagesAfterCancellation(t *testing.T) {
	var total int
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		store := &workerStore{}
		queue := &workerQueue{reclaimMessages: []ports.Message{validWorkerMessage()}}
		worker := testWorker(queue, store, &workerCounter{})
		require.NoError(t, worker.Run(ctx))
		total += store.validCalls
	}
	require.Zero(t, total)
}

func TestWorkerRunDLQsPoisonAndContinuesValidSibling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	poison := ports.Message{ID: "bad-1", Stream: EventStream, Deliveries: 1, Malformed: &ports.MalformedMessage{Reason: "payload required", RawEnvelope: []byte(`{"batch_id":"bad"}`)}}
	delivered := false
	queue := &workerQueue{
		cancelAfterOutcomes: cancel,
		consumeFn: func(ctx context.Context, stream, _, _ string, _ int, _ time.Duration) ([]ports.Message, error) {
			if stream == EventStream && !delivered {
				delivered = true
				return []ports.Message{poison, validWorkerMessage()}, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	store := &workerStore{}
	worker := testWorker(queue, store, &workerCounter{})

	require.NoError(t, worker.Run(ctx))
	require.Equal(t, 1, queue.dlqs)
	require.Equal(t, 1, queue.acks)
	require.Equal(t, 1, store.validCalls)
}

func testWorker(queue *workerQueue, store ports.EventStore, counter ports.RealtimeCounter) *Worker {
	return NewWorker(queue, store, counter, WorkerConfig{EventStream: EventStream, InvalidStream: InvalidEventStream, Group: WorkerGroup, Consumer: "test", VisitorMasterKey: []byte("master-key"), Concurrency: 2, MaxDeliveries: 5, RetryBase: time.Second, RetryMax: time.Minute, DrainTimeout: 50 * time.Millisecond})
}

func validWorkerMessage() ports.Message {
	event := NormalizedEvent{Event: Event{EventID: "evt-1", PointCode: "view", EventTime: 1000, UserID: "42", SessionID: "session", Platform: "web", Channel: "organic", Properties: map[string]any{"a": 1}}, ProjectID: 7, SnapshotID: 8, PointID: 9, PointVersion: 1, CategoryID: 3, CategoryCode: "detail", CategoryPath: []tracking.CategoryRef{{ID: 1, Code: "mall", Level: tracking.Domain}, {ID: 2, Code: "product", Level: tracking.Module}, {ID: 3, Code: "detail", Level: tracking.Scene}}, UVMode: tracking.Approximate, CountPV: true, CountUV: true, ProjectTimezone: "UTC", ReceivedAt: time.Unix(2, 0).UTC(), ClientTime: time.Unix(1, 0).UTC(), StatTime: time.Unix(1, 0).UTC()}
	payload, _ := json.Marshal(batchPayload[NormalizedEvent]{BatchID: "batch-1", ProjectID: 7, ReceivedAt: time.Unix(2, 0).UTC(), Events: []NormalizedEvent{event}})
	return ports.Message{ID: "1-0", Stream: EventStream, BatchID: "batch-1", Payload: payload, Deliveries: 1}
}

type callLog struct{ entries []string }

func (l *callLog) add(entry string) { l.entries = append(l.entries, entry) }

type workerStore struct {
	log                *callLog
	err                error
	validCalls         int
	invalidCalls       int
	invalid            []ports.InvalidEvent
	blockUntilCanceled bool
	started            chan struct{}
	done               chan struct{}
}

func (s *workerStore) SaveValid(ctx context.Context, _ string, events []ports.StoredEvent) error {
	s.validCalls++
	if s.log != nil {
		s.log.add("store-valid")
	}
	if s.blockUntilCanceled {
		close(s.started)
		defer close(s.done)
		<-ctx.Done()
		return ctx.Err()
	}
	return s.err
}
func (s *workerStore) SaveInvalid(_ context.Context, _ string, events []ports.InvalidEvent) error {
	s.invalidCalls++
	s.invalid = events
	if s.log != nil {
		s.log.add("store-invalid")
	}
	return s.err
}

type workerCounter struct {
	log       *callLog
	failEvent string
	failOnce  bool
	applied   map[string]bool
	calls     []string
}

func (c *workerCounter) Apply(_ context.Context, event ports.CountEvent) (bool, error) {
	c.calls = append(c.calls, event.EventID)
	if c.log != nil {
		c.log.add("count:" + event.EventID)
	}
	if event.EventID == c.failEvent && c.failOnce {
		c.failOnce = false
		return false, errors.New("redis unavailable")
	}
	if c.applied != nil && c.applied[event.EventID] {
		return false, nil
	}
	if c.applied != nil {
		c.applied[event.EventID] = true
	}
	return true, nil
}

type workerQueue struct {
	mu                            sync.Mutex
	log                           *callLog
	acks, retries, dlqs, reclaims int
	consumeErr                    error
	consumeFn                     func(context.Context, string, string, string, int, time.Duration) ([]ports.Message, error)
	respectCanceledRetry          bool
	retryDelays                   []time.Duration
	lastDLQ                       ports.Message
	reclaimMessages               []ports.Message
	cancelAfterOutcomes           context.CancelFunc
}

func (q *workerQueue) PublishBatch(context.Context, string, ports.BatchEnvelope) (string, error) {
	return "", nil
}
func (q *workerQueue) Consume(ctx context.Context, stream, group, consumer string, count int, block time.Duration) ([]ports.Message, error) {
	if q.consumeFn != nil {
		return q.consumeFn(ctx, stream, group, consumer, count, block)
	}
	return nil, q.consumeErr
}
func (q *workerQueue) Ack(context.Context, string, string, ...string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acks++
	if q.cancelAfterOutcomes != nil && q.acks+q.dlqs >= 2 {
		q.cancelAfterOutcomes()
	}
	if q.log != nil {
		q.log.add("ack")
	}
	return nil
}
func (q *workerQueue) Retry(ctx context.Context, _ string, _ string, _ ports.Message, _ error, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retries++
	q.retryDelays = append(q.retryDelays, delay)
	if q.respectCanceledRetry {
		return ctx.Err()
	}
	return nil
}
func (q *workerQueue) MoveToDLQ(_ context.Context, _ string, _ string, msg ports.Message, _ error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dlqs++
	q.lastDLQ = msg
	if q.cancelAfterOutcomes != nil && q.acks+q.dlqs >= 2 {
		q.cancelAfterOutcomes()
	}
	return nil
}
func (q *workerQueue) PromoteDue(context.Context, string, int) (int, error) { return 0, nil }
func (q *workerQueue) ReclaimStale(context.Context, string, string, string, time.Duration, int) ([]ports.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.reclaims++
	messages := q.reclaimMessages
	q.reclaimMessages = nil
	return messages, nil
}
