package ingestion

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/stretchr/testify/require"
)

type memoryTestSink struct {
	project uint64
	records []TestRecord
}

func (s *memoryTestSink) SaveTestRecords(_ context.Context, p uint64, records []TestRecord) error {
	s.project = p
	s.records = records
	return nil
}

// TestTestCollectorIsolation 验证测试事件复用校验且不触发正式队列。
func TestTestCollectorIsolation(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	queue := &recordingQueue{}
	base := NewCollector(snapshotMap{"product_detail_view": productSnapshot()}, queue, Config{Now: func() time.Time { return now }})
	sink := &memoryTestSink{}
	collector := NewTestCollector(base, sink)
	req := CollectRequest{Events: []Event{{EventID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", PointCode: "product_detail_view", EventTime: now.UnixMilli(), AnonymousID: "secret-user", Platform: "web", Properties: map[string]any{"source_page": "search"}}, {EventID: "0190b9a8-7000-7000-8000-000000000001", PointCode: "product_detail_view", EventTime: now.UnixMilli(), UserID: "secret-user", Platform: "web", Properties: map[string]any{"source_page": "secret-property"}}}}
	result, err := collector.Collect(context.Background(), project.Project{ID: 42, Active: true, Timezone: "UTC"}, req)
	require.NoError(t, err)
	require.Equal(t, 1, result.Accepted)
	require.Equal(t, 1, result.Rejected)
	require.Equal(t, "PROPERTY_ENUM_INVALID", result.Results[1].Code)
	require.Empty(t, queue.byStream)
	require.Equal(t, uint64(42), sink.project)
	require.Len(t, sink.records, 2)
	data, err := json.Marshal(sink.records)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(data), "secret-user"))
	require.False(t, strings.Contains(string(data), "secret-property"))
}
