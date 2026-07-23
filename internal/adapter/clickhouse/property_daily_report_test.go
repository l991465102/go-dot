package clickhouse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/stretchr/testify/require"
)

func TestPropertyDailyQueryUsesCommittedGenerationAndTop50OtherForStrings(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	store := NewPropertyStatisticsStore(db)
	filter := reporting.PropertyDailyFilter{
		ProjectID: 9, Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Timezone: "Asia/Shanghai",
	}
	_, err := store.QueryPropertyDaily(context.Background(), filter, []reporting.PropertyRef{{
		PropertySelection: reporting.PropertySelection{PointID: 7, PropertyCode: "source"},
		Type:              tracking.String, Analytics: tracking.PropertyAnalytics{Mode: tracking.AnalyticsDimension},
		UVMode: tracking.Exact,
	}})
	require.ErrorContains(t, err, "recorded")
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "property_dimension_hourly_exact")
	require.Contains(t, query, "property_stat_hour_generation AS g FINAL")
	require.Contains(t, query, "s.version=g.generation")
	require.Contains(t, query, "row_number() OVER (PARTITION BY date")
	require.Contains(t, query, "if(value_rank<=50,property_value,'其他')")
	require.Contains(t, query, "uniqExactMerge")
}

func TestPropertyDailyMetricQueryOnlyRequestsSelectedProperty(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	store := NewPropertyStatisticsStore(db)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, _ = store.QueryPropertyDaily(context.Background(), reporting.PropertyDailyFilter{
		ProjectID: 9, Start: start, End: start.Add(24 * time.Hour), Timezone: "Asia/Shanghai",
	}, []reporting.PropertyRef{{
		PropertySelection: reporting.PropertySelection{PointID: 7, PropertyCode: "amount", Aggregations: []tracking.MetricAggregation{tracking.AggregateMin}},
		Analytics:         tracking.PropertyAnalytics{Mode: tracking.AnalyticsMetric},
	}})
	require.Len(t, db.queries, 1)
	require.Contains(t, db.queries[0], "s.point_id=? AND s.property_code=?")
	require.Contains(t, db.queries[0], "toString(min(value_min))")
	require.NotContains(t, db.queries[0], "sum(value_count)")
	require.NotContains(t, db.queries[0], "sum(value_sum)")
	require.Equal(t, []any{uint64(9), start, start.Add(24 * time.Hour), uint64(7), "amount"}, db.queryArgs[0])
}
