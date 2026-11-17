package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/lzw7758/go-dot/internal/funnel"
	"github.com/stretchr/testify/require"
)

// TestFunnelClickHouse 用真实 ClickHouse 验证步骤顺序、时间窗口、重复事件和项目隔离。
func TestFunnelClickHouse(t *testing.T) {
	dsn := os.Getenv("GO_DOT_FUNNEL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated ClickHouse")
	}
	db, err := sql.Open("clickhouse", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	table := fmt.Sprintf("test_funnel_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, `CREATE TABLE `+table+` (project_id UInt64,event_id String,point_code String,visitor_hash String,statistic_at DateTime64(3,'UTC'),platform String,channel String,version UInt64) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,point_code,statistic_at,event_id)`)
	require.NoError(t, err)
	defer db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	insert := func(project int, id, code, visitor string, minutes int, version int) {
		t.Helper()
		_, e := db.ExecContext(ctx, `INSERT INTO `+table+` (project_id,event_id,point_code,visitor_hash,statistic_at,platform,channel,version) VALUES (?,?,?,?,?,'web','shop',?)`, project, id, code, visitor, start.Add(time.Duration(minutes)*time.Minute), version)
		require.NoError(t, e)
	}
	insert(1, "a1", "view", "u1", 10, 1)
	insert(1, "a2", "order", "u1", 20, 1)
	insert(1, "a3", "pay", "u1", 30, 1)
	insert(1, "b1", "view", "u2", 10, 1)
	insert(1, "b2", "pay", "u2", 15, 1)
	insert(1, "b3", "order", "u2", 20, 1)
	insert(1, "c1", "view", "u3", 10, 1)
	insert(1, "c2", "order", "u3", 10, 1)
	insert(1, "d1", "view", "u4", 10, 1)
	insert(1, "d2", "order", "u4", 140, 1)
	insert(1, "e1", "view", "u5", 80, 1)
	insert(1, "e2", "order", "u5", 90, 1)
	insert(1, "f1", "view", "u6", 1, 1)
	insert(1, "f2", "view", "u6", 50, 1)
	insert(1, "f3", "order", "u6", 65, 1)
	insert(1, "dup", "view", "u7", 5, 1)
	insert(1, "dup", "pay", "u7", 6, 2)
	insert(1, "g2", "order", "u7", 8, 1)
	insert(2, "x1", "view", "other", 10, 1)
	insert(2, "x2", "order", "other", 20, 1)
	store := newStatisticsStoreWithTables(db, table, "a", "b", "c")
	values, err := store.QueryFunnel(ctx, funnel.Query{ProjectID: 1, Start: start, End: start.Add(time.Hour), EvaluatedUntil: start.Add(3 * time.Hour), WindowHours: 1, PointCodes: []string{"view", "order", "pay"}, Platform: "web", Channel: "shop"})
	require.NoError(t, err)
	require.Equal(t, []uint64{5, 3, 1}, values)
}
