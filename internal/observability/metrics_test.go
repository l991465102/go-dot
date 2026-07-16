package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMetricsEndpointPublishesRequiredFamilies(t *testing.T) {
	m := NewMetrics()
	m.ObserveCollection(2, 1, nil)
	m.ObserveDuplicate(1)
	m.SetQueueSnapshot(QueueSnapshot{Lag: 3, Pending: 2, DeadLetters: 1})
	m.ObserveClickHouseBatch(2, 0, nil)
	m.MarkAggregationSuccess(time.Now().Add(-4 * time.Second))
	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	for _, name := range []string{
		"go_dot_collection_events_total", "go_dot_collection_rejected_total", "go_dot_collection_duplicate_total",
		"go_dot_queue_lag", "go_dot_queue_pending", "go_dot_queue_dlq", "go_dot_clickhouse_batch_size",
		"go_dot_clickhouse_batch_duration_seconds", "go_dot_aggregation_delay_seconds",
	} {
		require.Contains(t, recorder.Body.String(), name)
	}
}

func TestAggregationDelayReportsStalenessSinceLastSuccessfulRefresh(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	m := newMetrics(func() time.Time { return now })
	m.MarkAggregationSuccess(now.Add(-7 * time.Second))
	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(t, recorder.Body.String(), "go_dot_aggregation_delay_seconds 7")

	now = now.Add(5 * time.Second)
	recorder = httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(t, recorder.Body.String(), "go_dot_aggregation_delay_seconds 12")
}
