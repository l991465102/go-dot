package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestRefreshProjectHourUsesLogicalEventDedupAndVersionedAggregateStates(t *testing.T) {
	db := &statisticsRecordingDB{}
	store := NewStatisticsStore(db)
	hour := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)

	require.NoError(t, store.RefreshProjectHour(context.Background(), 9, hour, 123))
	require.Len(t, db.execs, 2)
	approx := strings.Join(strings.Fields(db.execs[0]), " ")
	exact := strings.Join(strings.Fields(db.execs[1]), " ")
	require.Contains(t, approx, "tracking_event FINAL")
	require.Contains(t, approx, "GROUP BY project_id, event_id")
	require.Contains(t, approx, "uniqCombined64StateIf")
	require.Contains(t, exact, "uniqExactStateIf")
	require.Contains(t, exact, "uv_mode = 'EXACT'")
	require.Contains(t, approx, "version")
	require.Equal(t, []any{hour, uint64(123), uint64(9), hour, hour.Add(time.Hour)}, db.args[0])
}

func TestQueryTrendMergesExactStatesAcrossProjectLocalDays(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("stop after recording")}
	store := NewStatisticsStore(db)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err := store.QueryTrend(context.Background(), ports.StatisticsFilter{ProjectID: 9, Start: start, End: start.Add(48 * time.Hour), Granularity: "day", Timezone: "Asia/Shanghai", ExactUV: true})
	require.ErrorContains(t, err, "stop after recording")
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "business_stat_exact_hourly FINAL")
	require.Contains(t, query, "uniqExactMerge(uv_state)")
	require.Contains(t, query, "toStartOfDay(hour, ?)")
	require.Equal(t, "Asia/Shanghai", db.queryArgs[0][0])
}

type statisticsRecordingDB struct {
	execs     []string
	args      [][]any
	queries   []string
	queryArgs [][]any
	queryErr  error
}

func (d *statisticsRecordingDB) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	d.execs = append(d.execs, q)
	d.args = append(d.args, args)
	return nil, nil
}
func (d *statisticsRecordingDB) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	d.queries = append(d.queries, query)
	d.queryArgs = append(d.queryArgs, args)
	return nil, d.queryErr
}
