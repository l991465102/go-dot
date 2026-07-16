package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetricsEndpointPublishesRequiredFamilies(t *testing.T) {
	m := NewMetrics()
	m.ObserveCollection(2, 1, nil)
	m.ObserveDuplicate(1)
	m.SetQueueSnapshot(QueueSnapshot{Lag: 3, Pending: 2, DeadLetters: 1})
	m.ObserveClickHouseBatch(2, 0, nil)
	m.SetAggregationDelay(4)
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
