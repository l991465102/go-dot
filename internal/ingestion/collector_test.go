package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func TestCollectSeparatesValidAndInvalidEvents(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	queue := &recordingQueue{}
	collector := NewCollector(snapshotMap{"product_detail_view": productSnapshot()}, queue, Config{Now: func() time.Time { return now }})
	req := CollectRequest{ProjectID: 999, Events: []Event{
		{EventID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", PointCode: "product_detail_view", EventTime: now.UnixMilli(), AnonymousID: "device-1", Platform: "web", Properties: map[string]any{"source_page": "search"}},
		{EventID: "0190b9a8-7000-7000-8000-000000000001", PointCode: "missing_point", EventTime: now.UnixMilli(), AnonymousID: "device-1", Platform: "web"},
		{EventID: "0190b9a8-7000-7000-8000-000000000002", PointCode: "product_detail_view", EventTime: now.UnixMilli(), UserID: "7", Platform: "web", Properties: map[string]any{"source_page": "invalid"}},
	}}

	result, err := collector.Collect(context.Background(), project.Project{ID: 42, Active: true, Timezone: "Asia/Shanghai"}, req)
	require.NoError(t, err)
	require.Equal(t, 1, result.Accepted)
	require.Equal(t, 2, result.Rejected)
	require.Len(t, queue.byStream[EventStream], 1)
	require.Len(t, queue.byStream[InvalidEventStream], 1)
	require.Equal(t, queue.byStream[EventStream][0].BatchID, queue.byStream[InvalidEventStream][0].BatchID)
	require.NotEmpty(t, result.BatchID)
	require.Equal(t, result.BatchID, queue.byStream[EventStream][0].BatchID)

	var accepted struct {
		ProjectID uint64            `json:"project_id"`
		Events    []NormalizedEvent `json:"events"`
	}
	require.NoError(t, json.Unmarshal(queue.byStream[EventStream][0].Payload, &accepted))
	require.Equal(t, uint64(42), accepted.ProjectID)
	require.Equal(t, uint64(42), accepted.Events[0].ProjectID)
	require.Equal(t, uint64(123), accepted.Events[0].SnapshotID)
	require.Equal(t, tracking.Approximate, accepted.Events[0].UVMode)
}

func TestCollectUsesStableBatchIDAndCorrectsOutOfRangeTime(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	queue := &recordingQueue{}
	collector := NewCollector(snapshotMap{"product_detail_view": productSnapshot()}, queue, Config{Now: func() time.Time { return now }})
	req := CollectRequest{Events: []Event{{EventID: "0190b9a8-7000-7000-8000-000000000001", PointCode: "product_detail_view", EventTime: now.Add(-8 * 24 * time.Hour).UnixMilli(), AnonymousID: "device-1", Platform: "web", Properties: map[string]any{"source_page": "search"}}}}

	first, err := collector.Collect(context.Background(), project.Project{ID: 42, Active: true, Timezone: "Asia/Shanghai"}, req)
	require.NoError(t, err)
	second, err := collector.Collect(context.Background(), project.Project{ID: 42, Active: true, Timezone: "Asia/Shanghai"}, req)
	require.NoError(t, err)
	require.Equal(t, first.BatchID, second.BatchID)

	var payload struct {
		Events []NormalizedEvent `json:"events"`
	}
	require.NoError(t, json.Unmarshal(queue.byStream[EventStream][0].Payload, &payload))
	require.True(t, payload.Events[0].TimeCorrected)
	require.Equal(t, now, payload.Events[0].StatTime)
}

func TestCollectRejectsInvalidBatchSizeWithoutPublishing(t *testing.T) {
	queue := &recordingQueue{}
	collector := NewCollector(snapshotMap{}, queue, Config{})
	_, err := collector.Collect(context.Background(), project.Project{ID: 42, Active: true, Timezone: "UTC"}, CollectRequest{})
	require.ErrorIs(t, err, ErrInvalidBatch)
	require.Empty(t, queue.byStream)
}

func TestCollectReturnsSnapshotInfrastructureFailureWithoutInvalidPublishing(t *testing.T) {
	queue := &recordingQueue{}
	collector := NewCollector(failingSnapshots{}, queue, Config{})
	req := CollectRequest{Events: []Event{{EventID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", PointCode: "product_view", EventTime: time.Now().UnixMilli(), AnonymousID: "device", Platform: "web"}}}
	_, err := collector.Collect(context.Background(), project.Project{ID: 42, Active: true, Timezone: "UTC"}, req)
	require.ErrorContains(t, err, "load point snapshot")
	require.Empty(t, queue.byStream)
}

func TestCollectRetryAfterSecondStreamFailureHasOneEffectiveBatchPerStream(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	queue := &partialFailureIdempotentQueue{recordingQueue: recordingQueue{}}
	collector := NewCollector(snapshotMap{"product_detail_view": productSnapshot()}, queue, Config{Now: func() time.Time { return now }})
	req := CollectRequest{Events: []Event{
		{EventID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", PointCode: "product_detail_view", EventTime: now.UnixMilli(), AnonymousID: "device-1", Platform: "web", Properties: map[string]any{"source_page": "search"}},
		{EventID: "0190b9a8-7000-7000-8000-000000000001", PointCode: "missing", EventTime: now.UnixMilli(), AnonymousID: "device-1", Platform: "web"},
	}}
	p := project.Project{ID: 42, Active: true, Timezone: "UTC"}
	_, err := collector.Collect(context.Background(), p, req)
	require.ErrorContains(t, err, "publish invalid batch")
	result, err := collector.Collect(context.Background(), p, req)
	require.NoError(t, err)
	require.Equal(t, 1, result.Accepted)
	require.Equal(t, 1, result.Rejected)
	require.Len(t, queue.byStream[EventStream], 1)
	require.Len(t, queue.byStream[InvalidEventStream], 1)
}

func productSnapshot() tracking.Snapshot {
	return tracking.Snapshot{ID: 123, ProjectID: 42, PointID: 10, CategoryID: 3, CategoryCode: "product_detail", CategoryPath: []tracking.CategoryRef{{ID: 1, Code: "mall", Level: tracking.Domain}, {ID: 2, Code: "product", Level: tracking.Module}, {ID: 3, Code: "product_detail", Level: tracking.Scene}}, PointCode: "product_detail_view", Version: 1, CountPV: true, CountUV: true, UVMode: tracking.Approximate, TimeBasis: tracking.Client, SupportedPlatforms: []string{"web"}, Properties: []tracking.Property{{Code: "source_page", Name: "source", Type: tracking.Enum, Required: true, EnumValues: []string{"search", "home"}}}}
}

type snapshotMap map[string]tracking.Snapshot

func (m snapshotMap) Get(_ context.Context, projectID uint64, pointCode string) (tracking.Snapshot, error) {
	s, ok := m[pointCode]
	if !ok || s.ProjectID != projectID {
		return tracking.Snapshot{}, tracking.ErrPointNotPublished
	}
	return s, nil
}

type failingSnapshots struct{}

func (failingSnapshots) Get(context.Context, uint64, string) (tracking.Snapshot, error) {
	return tracking.Snapshot{}, errors.New("mysql unavailable")
}

type recordingQueue struct {
	byStream map[string][]ports.BatchEnvelope
}

func (q *recordingQueue) PublishBatch(_ context.Context, stream string, batch ports.BatchEnvelope) (string, error) {
	if q.byStream == nil {
		q.byStream = map[string][]ports.BatchEnvelope{}
	}
	q.byStream[stream] = append(q.byStream[stream], batch)
	return "1-0", nil
}
func (*recordingQueue) Consume(context.Context, string, string, string, int, time.Duration) ([]ports.Message, error) {
	panic("unused")
}
func (*recordingQueue) Ack(context.Context, string, string, ...string) error { panic("unused") }
func (*recordingQueue) Retry(context.Context, string, string, ports.Message, error) error {
	panic("unused")
}
func (*recordingQueue) MoveToDLQ(context.Context, string, string, ports.Message, error) error {
	panic("unused")
}
func (*recordingQueue) ReclaimStale(context.Context, string, string, string, time.Duration, int) ([]ports.Message, error) {
	panic("unused")
}

type partialFailureIdempotentQueue struct {
	recordingQueue
	failed bool
	seen   map[string]string
}

func (q *partialFailureIdempotentQueue) PublishBatch(ctx context.Context, stream string, batch ports.BatchEnvelope) (string, error) {
	if q.seen == nil {
		q.seen = map[string]string{}
	}
	key := stream + ":" + batch.BatchID
	if id, ok := q.seen[key]; ok {
		return id, nil
	}
	if stream == InvalidEventStream && !q.failed {
		q.failed = true
		return "", errors.New("redis unavailable")
	}
	id, err := q.recordingQueue.PublishBatch(ctx, stream, batch)
	if err == nil {
		q.seen[key] = id
	}
	return id, err
}
