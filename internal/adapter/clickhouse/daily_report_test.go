package clickhouse

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
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

func TestDailyReportQueriesCommittedBusinessHours(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	start := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	_, err := NewStatisticsStore(db).QueryCommittedHours(context.Background(), reporting.DailyFilter{
		ProjectID: 9, Start: start, End: start.Add(24 * time.Hour), Timezone: "Asia/Shanghai",
	})
	require.ErrorContains(t, err, "recorded")
	require.Len(t, db.queries, 1)
	require.Contains(t, db.queries[0], "SELECT hour FROM business_stat_hour_generation FINAL")
	require.Equal(t, []any{uint64(9), start, start.Add(24 * time.Hour)}, db.queryArgs[0])
}

func TestDailyReportExecutesMixedModeQueriesAndMergesResultSets(t *testing.T) {
	connector := &dailyTestConnector{batches: []dailyRowsBatch{
		{rows: [][]driver.Value{{"2026-07-20", int64(2), "view", int64(3), int64(2)}}},
		{rows: [][]driver.Value{{"2026-07-20", int64(7), "checkout", int64(4), int64(3)}}},
	}}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	start := time.Date(2026, 7, 19, 16, 0, 0, 0, time.UTC)

	items, err := NewStatisticsStore(db).QueryDaily(context.Background(), reporting.DailyFilter{
		ProjectID: 9, Start: start, End: start.Add(24 * time.Hour), Timezone: "Asia/Shanghai",
		ApproximatePointIDs: []uint64{2}, ExactPointIDs: []uint64{7},
	})

	require.NoError(t, err)
	require.Equal(t, 2, connector.calls)
	require.ElementsMatch(t, []reporting.DailyAggregate{
		{Date: "2026-07-20", PointID: 2, PointCode: "view", PV: 3, UV: 2},
		{Date: "2026-07-20", PointID: 7, PointCode: "checkout", PV: 4, UV: 3},
	}, items)
}

func TestDailyReportPropagatesRowScanAndIterationErrors(t *testing.T) {
	start := time.Date(2026, 7, 19, 16, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		batch     dailyRowsBatch
		wantError string
	}{
		{name: "scan", batch: dailyRowsBatch{rows: [][]driver.Value{{"2026-07-20", "not-an-id", "view", int64(3), int64(2)}}}, wantError: "scan daily statistics"},
		{name: "iteration", batch: dailyRowsBatch{rowErr: errors.New("rows interrupted")}, wantError: "rows interrupted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			connector := &dailyTestConnector{batches: []dailyRowsBatch{test.batch}}
			db := sql.OpenDB(connector)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			_, err := NewStatisticsStore(db).QueryDaily(context.Background(), reporting.DailyFilter{
				ProjectID: 9, Start: start, End: start.Add(24 * time.Hour), Timezone: "Asia/Shanghai", ApproximatePointIDs: []uint64{2},
			})
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

type dailyRowsBatch struct {
	rows   [][]driver.Value
	rowErr error
}

type dailyTestConnector struct {
	batches []dailyRowsBatch
	calls   int
}

func (c *dailyTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &dailyTestConn{connector: c}, nil
}

func (c *dailyTestConnector) Driver() driver.Driver { return dailyTestDriver{} }

type dailyTestDriver struct{}

func (dailyTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("daily test driver requires connector")
}

type dailyTestConn struct{ connector *dailyTestConnector }

func (c *dailyTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (c *dailyTestConn) Close() error { return nil }

func (c *dailyTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (c *dailyTestConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.connector.calls >= len(c.connector.batches) {
		return nil, errors.New("unexpected daily query")
	}
	batch := c.connector.batches[c.connector.calls]
	c.connector.calls++
	return &dailyTestRows{batch: batch}, nil
}

type dailyTestRows struct {
	batch dailyRowsBatch
	index int
}

func (r *dailyTestRows) Columns() []string {
	return []string{"date", "point_id", "point_code", "pv", "uv"}
}

func (r *dailyTestRows) Close() error { return nil }

func (r *dailyTestRows) Next(dest []driver.Value) error {
	if r.index < len(r.batch.rows) {
		copy(dest, r.batch.rows[r.index])
		r.index++
		return nil
	}
	if r.batch.rowErr != nil {
		return r.batch.rowErr
	}
	return io.EOF
}
