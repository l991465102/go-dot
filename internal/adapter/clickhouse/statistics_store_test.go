package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
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
	require.Len(t, db.execs, 3)
	approx := strings.Join(strings.Fields(db.execs[0]), " ")
	exact := strings.Join(strings.Fields(db.execs[1]), " ")
	require.Contains(t, approx, "tracking_event FINAL")
	require.Contains(t, approx, "GROUP BY project_id, event_id")
	require.Contains(t, approx, "uniqCombined64StateIf")
	require.Contains(t, exact, "uniqExactStateIf")
	require.Contains(t, exact, "uv_mode = 'EXACT'")
	require.Contains(t, approx, "version")
	require.Equal(t, []any{hour, uint64(123), uint64(9), hour, hour.Add(time.Hour)}, db.args[0])
	require.Contains(t, db.execs[2], "business_stat_hour_generation")
	require.Equal(t, []any{uint64(9), hour, uint64(123)}, db.args[2])
	require.EqualValues(t, 0, statisticsRefreshSettings()["async_insert"])
	require.EqualValues(t, 1, statisticsRefreshSettings()["wait_for_async_insert"])
}

func TestQueryTrendMergesExactStatesAcrossProjectLocalDays(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("stop after recording")}
	store := NewStatisticsStore(db)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err := store.QueryTrend(context.Background(), ports.StatisticsFilter{ProjectID: 9, Start: start, End: start.Add(48 * time.Hour), Granularity: "day", Timezone: "Asia/Shanghai", ExactUV: true})
	require.ErrorContains(t, err, "stop after recording")
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "business_stat_exact_hourly AS s")
	require.Contains(t, query, "uniqExactMerge(uv_state)")
	require.Contains(t, query, "toStartOfDay(hour, ?)")
	require.Contains(t, query, "business_stat_hour_generation FINAL")
	require.Contains(t, query, "s.version = g.generation")
	require.Contains(t, query, "any(s.pv)")
	require.NotContains(t, query, "business_stat_exact_hourly FINAL")
	require.Equal(t, "Asia/Shanghai", db.queryArgs[0][0])
}

func TestRefreshGenerationMarkerFailureLeavesPartialGenerationInvisible(t *testing.T) {
	db := &statisticsRecordingDB{execErrors: []error{nil, nil, errors.New("marker unavailable")}}
	store := NewStatisticsStore(db)
	hour := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	err := store.RefreshProjectHour(context.Background(), 9, hour, 44)
	require.ErrorContains(t, err, "marker unavailable")
	require.Len(t, db.execs, 3)
	db.queryErr = errors.New("recorded")
	_, _ = store.QuerySummary(context.Background(), ports.StatisticsFilter{ProjectID: 9, Start: hour, End: hour.Add(time.Hour)})
	require.Contains(t, db.queries[0], "business_stat_hour_generation FINAL")
}

func TestCommittedGenerationQueryExcludesRemovedDimensionsAndCollapsesSameGenerationRetry(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	store := NewStatisticsStore(db)
	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	_, _ = store.QuerySummary(context.Background(), ports.StatisticsFilter{ProjectID: 9, Start: start, End: start.Add(time.Hour)})
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "INNER JOIN business_stat_hour_generation FINAL")
	require.Contains(t, query, "s.version = g.generation")
	require.Contains(t, query, "any(s.pv) AS pv")
	require.Contains(t, query, "GROUP BY s.hour")
	require.NotContains(t, query, "business_stat_hourly FINAL")
}

func TestEmptyRefreshStillCommitsGeneration(t *testing.T) {
	db := &statisticsRecordingDB{}
	hour := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	require.NoError(t, NewStatisticsStore(db).RefreshProjectHour(context.Background(), 9, hour, 77))
	require.Len(t, db.execs, 3)
	require.Contains(t, db.execs[2], "generation")
}

func TestStatisticsPointCodeListUsesParameterizedIN(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	_, _ = NewStatisticsStore(db).QuerySummary(context.Background(), ports.StatisticsFilter{ProjectID: 9, Start: start, End: start.Add(time.Hour), PointCodes: []string{"a' OR 1=1 --", "safe"}})
	require.Contains(t, db.queries[0], "s.point_code IN (?,?)")
	require.NotContains(t, db.queries[0], "OR 1=1")
	require.Contains(t, db.queryArgs[0], "a' OR 1=1 --")
}

func TestCategoryCodeHistoryUsesDurableAggregateColumnsNotRawTTL(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	start := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	_, _ = NewStatisticsStore(db).QuerySummary(context.Background(), ports.StatisticsFilter{ProjectID: 9, Start: start, End: start.Add(60 * 24 * time.Hour), DomainCategoryCode: "mall"})
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "s.domain_category_code=?")
	require.NotContains(t, query, "SELECT domain_category_id FROM tracking_event")
}

func TestStatisticsGenerationClickHouseIntegration(t *testing.T) {
	dsn := os.Getenv("GO_DOT_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set GO_DOT_TEST_CLICKHOUSE_DSN to run generation integration")
	}
	db, err := sql.Open("clickhouse", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	suffix := fmt.Sprintf("task9_%d", time.Now().UnixNano())
	raw := "tracking_event_" + suffix
	approx := "business_stat_hourly_" + suffix
	exact := "business_stat_exact_hourly_" + suffix
	generation := "business_stat_generation_" + suffix
	ddls := []string{
		`CREATE TABLE ` + raw + ` (project_id UInt64,event_id String,domain_category_id UInt64,domain_category_code String,module_category_id UInt64,module_category_code String,scene_category_id UInt64,scene_category_code String,point_id UInt64,point_code String,platform String,channel String,visitor_hash FixedString(64),count_pv Bool,count_uv Bool,uv_mode String,statistic_at DateTime64(3,'UTC'),version UInt64) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,event_id)`,
		`CREATE TABLE ` + approx + ` (hour DateTime('UTC'),project_id UInt64,domain_category_id UInt64,domain_category_code String,module_category_id UInt64,module_category_code String,scene_category_id UInt64,scene_category_code String,point_id UInt64,point_code String,platform String,channel String,pv UInt64,uv_state AggregateFunction(uniqCombined64,FixedString(64)),version UInt64) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,hour,domain_category_id,module_category_id,scene_category_id,point_id,platform,channel)`,
		`CREATE TABLE ` + exact + ` (hour DateTime('UTC'),project_id UInt64,domain_category_id UInt64,domain_category_code String,module_category_id UInt64,module_category_code String,scene_category_id UInt64,scene_category_code String,point_id UInt64,point_code String,platform String,channel String,pv UInt64,uv_state AggregateFunction(uniqExact,FixedString(64)),version UInt64) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,hour,domain_category_id,module_category_id,scene_category_id,point_id,platform,channel)`,
		`CREATE TABLE ` + generation + ` (project_id UInt64,hour DateTime('UTC'),generation UInt64,committed_at DateTime64(3,'UTC')) ENGINE=ReplacingMergeTree(generation) ORDER BY (project_id,hour)`,
	}
	for _, ddl := range ddls {
		_, err = db.ExecContext(ctx, ddl)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		for _, table := range []string{raw, approx, exact, generation} {
			_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
		}
	})
	hour := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	insert := `INSERT INTO ` + raw + ` VALUES (9,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	visitorA := strings.Repeat("a", 64)
	visitorB := strings.Repeat("b", 64)
	for _, row := range []struct {
		id      string
		visitor string
		version uint64
	}{{"evt-1", visitorA, 1}, {"evt-1", visitorA, 2}, {"evt-2", visitorB, 1}} {
		_, err = db.ExecContext(ctx, insert, row.id, uint64(1), "mall", uint64(2), "product", uint64(3), "detail", uint64(9), "view", "web", "organic", row.visitor, true, true, "EXACT", hour.Add(time.Minute), row.version)
		require.NoError(t, err)
	}
	store := newStatisticsStoreWithTables(db, raw, approx, exact, generation)
	require.NoError(t, store.RefreshProjectHour(ctx, 9, hour, 10))
	summary, err := store.QuerySummary(ctx, ports.StatisticsFilter{ProjectID: 9, Start: hour, End: hour.Add(time.Hour), ExactUV: true})
	require.NoError(t, err)
	require.EqualValues(t, 2, summary.PV)
	require.EqualValues(t, 2, summary.UV)
	// An uncommitted newer generation must remain invisible.
	_, err = db.ExecContext(ctx, `INSERT INTO `+exact+` SELECT hour,project_id,domain_category_id,domain_category_code,module_category_id,module_category_code,scene_category_id,scene_category_code,point_id,point_code,platform,channel,pv,uv_state,99 FROM `+exact+` WHERE version=10`)
	require.NoError(t, err)
	summary, err = store.QuerySummary(ctx, ports.StatisticsFilter{ProjectID: 9, Start: hour, End: hour.Add(time.Hour), ExactUV: true})
	require.NoError(t, err)
	require.EqualValues(t, 2, summary.PV)
}

type statisticsRecordingDB struct {
	execs      []string
	args       [][]any
	queries    []string
	queryArgs  [][]any
	queryErr   error
	execErrors []error
}

func (d *statisticsRecordingDB) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	d.execs = append(d.execs, q)
	d.args = append(d.args, args)
	if len(d.execErrors) >= len(d.execs) {
		return nil, d.execErrors[len(d.execs)-1]
	}
	return nil, nil
}
func (d *statisticsRecordingDB) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	d.queries = append(d.queries, query)
	d.queryArgs = append(d.queryArgs, args)
	return nil, d.queryErr
}
