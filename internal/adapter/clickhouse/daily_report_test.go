package clickhouse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/stretchr/testify/require"
)

func TestDailyReportApproximateQueryUsesCommittedGenerationsAndBoundPointIDs(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	start := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	_, err := NewStatisticsStore(db).QueryDaily(context.Background(), reporting.DailyFilter{
		ProjectID: 9, Start: start, End: start.Add(48 * time.Hour), Timezone: "Asia/Shanghai", ApproximatePointIDs: []uint64{3, 2},
	})
	require.ErrorContains(t, err, "recorded")
	require.Len(t, db.queries, 1)
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "business_stat_hourly AS s")
	require.Contains(t, query, "uniqCombined64Merge(uv_state)")
	require.Contains(t, query, "INNER JOIN business_stat_hour_generation AS g FINAL")
	require.Contains(t, query, "s.version = g.generation")
	require.Contains(t, query, "toDate(hour, 'Asia/Shanghai')")
	require.Contains(t, query, "GROUP BY toDate(hour, 'Asia/Shanghai'),point_id,point_code")
	require.Contains(t, query, "s.point_id IN (?,?)")
	require.NotContains(t, query, "point_id IN (3,2)")
	require.Equal(t, []any{uint64(9), start, start.Add(48 * time.Hour), uint64(3), uint64(2)}, db.queryArgs[0])
}

func TestDailyReportExactQueryUsesExactMergeAndCommittedGenerations(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	start := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	_, err := NewStatisticsStore(db).QueryDaily(context.Background(), reporting.DailyFilter{
		ProjectID: 9, Start: start, End: start.Add(24 * time.Hour), Timezone: "Asia/Shanghai", ExactPointIDs: []uint64{7},
	})
	require.ErrorContains(t, err, "recorded")
	require.Len(t, db.queries, 1)
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "business_stat_exact_hourly AS s")
	require.Contains(t, query, "uniqExactMerge(uv_state)")
	require.Contains(t, query, "INNER JOIN business_stat_hour_generation AS g FINAL")
	require.Contains(t, query, "s.version = g.generation")
	require.Contains(t, query, "toDate(hour, 'Asia/Shanghai')")
	require.Contains(t, query, "GROUP BY toDate(hour, 'Asia/Shanghai'),point_id,point_code")
	require.Contains(t, query, "s.point_id IN (?)")
	require.Equal(t, uint64(7), db.queryArgs[0][3])
}
