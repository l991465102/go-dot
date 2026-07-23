package clickhouse

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPropertyStatisticsRefreshBuildsMetricDimensionAndGeneration(t *testing.T) {
	db := &statisticsRecordingDB{}
	store := NewPropertyStatisticsStore(db)
	hour := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)

	require.NoError(t, store.RefreshProjectHour(context.Background(), 9, hour, 77))
	require.Len(t, db.execs, 4)
	metric := strings.Join(strings.Fields(db.execs[0]), " ")
	approximate := strings.Join(strings.Fields(db.execs[1]), " ")
	exact := strings.Join(strings.Fields(db.execs[2]), " ")
	require.Contains(t, metric, "tracking_property_event FINAL")
	require.Contains(t, metric, "GROUP BY project_id, event_id, property_code")
	require.Contains(t, metric, "sum(value_decimal)")
	require.Contains(t, approximate, "uniqCombined64State(visitor_hash)")
	require.Contains(t, exact, "uniqExactState(visitor_hash)")
	require.Contains(t, exact, "uv_mode = 'EXACT'")
	require.Contains(t, db.execs[3], "property_stat_hour_generation")
	require.Equal(t, []any{uint64(9), hour, uint64(77)}, db.args[3])
}

func TestPropertyStatisticsConfigureRetention(t *testing.T) {
	db := &statisticsRecordingDB{}
	store := NewPropertyStatisticsStore(db)
	require.NoError(t, store.ConfigureRetention(context.Background(), 730))
	require.Len(t, db.execs, 4)
	require.Contains(t, db.execs[0], "ALTER TABLE property_metric_hourly MODIFY TTL hour + INTERVAL 730 DAY DELETE")
	require.Contains(t, db.execs[1], "ALTER TABLE property_dimension_hourly_approximate MODIFY TTL hour + INTERVAL 730 DAY DELETE")
	require.Contains(t, db.execs[2], "ALTER TABLE property_dimension_hourly_exact MODIFY TTL hour + INTERVAL 730 DAY DELETE")
	require.Contains(t, db.execs[3], "ALTER TABLE property_stat_hour_generation MODIFY TTL hour + INTERVAL 730 DAY DELETE")

	require.ErrorContains(t, store.ConfigureRetention(context.Background(), 89), "at least 90")
}
