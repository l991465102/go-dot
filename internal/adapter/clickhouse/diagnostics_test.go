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

	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/stretchr/testify/require"
)

func TestRecentEventQueryIsProjectScopedParameterizedAndStable(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	store := NewStatisticsStore(db)
	start := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	_, err := store.ListRecent(context.Background(), statistics.DiagnosticFilter{ProjectID: 9, Start: start, End: start.Add(24 * time.Hour), PointCodes: []string{"x' OR 1=1 --"}, Limit: 20})
	require.ErrorContains(t, err, "recorded")
	query := strings.Join(strings.Fields(db.queries[0]), " ")
	require.Contains(t, query, "project_id=?")
	require.Contains(t, query, "point_code IN (?)")
	require.Contains(t, query, "ORDER BY received_at DESC,event_id DESC")
	require.NotContains(t, query, "OR 1=1")
	require.Contains(t, db.queryArgs[0], "x' OR 1=1 --")
}

func TestDiagnosticsRejectMalformedCursorBeforeClickHouse(t *testing.T) {
	db := &statisticsRecordingDB{}
	_, err := NewStatisticsStore(db).ListInvalid(context.Background(), statistics.DiagnosticFilter{ProjectID: 9, Start: time.Now().Add(-time.Hour), End: time.Now(), Limit: 20, Cursor: "not-base64"})
	require.ErrorIs(t, err, statistics.ErrInvalidQuery)
	require.Empty(t, db.queries)
}

func TestSanitizePropertiesRemovesIdentityAndSecretKeysRecursively(t *testing.T) {
	got := sanitizeProperties(map[string]any{"product_id": "p", "user_id": "u", "nested": map[string]any{"anonymous_id": "a", "token": "secret", "ok": true}})
	require.Equal(t, map[string]any{"product_id": "p", "nested": map[string]any{"ok": true}}, got)
}

func TestSanitizePropertiesHandlesArbitraryContainersAndCanonicalSensitiveKeys(t *testing.T) {
	input := map[string]any{"safe": []map[string]any{{"userId": "u", "nested": []any{map[string]string{"Anonymous-ID": "a", "ok": "yes"}}}}, "SESSION.id": "s", "access_Token": "t"}
	got := sanitizeProperties(input)
	require.Equal(t, map[string]any{"safe": []any{map[string]any{"nested": []any{map[string]any{"ok": "yes"}}}}}, got)
}

func TestDiagnosticsClickHouseIntegration(t *testing.T) {
	dsn := os.Getenv("GO_DOT_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set GO_DOT_TEST_CLICKHOUSE_DSN to run diagnostics integration")
	}
	db, err := sql.Open("clickhouse", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	suffix := fmt.Sprintf("task10_%d", time.Now().UnixNano())
	raw, invalid := "tracking_event_"+suffix, "tracking_invalid_event_"+suffix
	_, err = db.ExecContext(ctx, `CREATE TABLE `+raw+` (event_id String,project_id UInt64,point_code String,platform String,channel String,app_version String,client_event_at DateTime64(3,'UTC'),received_at DateTime64(3,'UTC'),statistic_at DateTime64(3,'UTC'),properties_json String,version UInt64) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,received_at,event_id)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE `+invalid+` (event_id String,project_id UInt64,point_code String,platform String,channel String,error_code String,error_message String,received_at DateTime64(3,'UTC'),version UInt64) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,received_at,event_id,error_code)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+raw)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+invalid)
	})
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = db.ExecContext(ctx, `INSERT INTO `+raw+` VALUES (?,?,?,?,?,?,?,?,?,?,?)`, "evt-1", uint64(7), "view", "web", "organic", "1", now, now, now, `{"user_id":"secret","product_id":"p"}`, uint64(1))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO `+invalid+` VALUES (?,?,?,?,?,?,?,?,?)`, "bad-1", uint64(7), "view", "web", "organic", "BAD", "invalid", now, uint64(1))
	require.NoError(t, err)
	store := newStatisticsStoreWithAllTables(db, raw, invalid, "unused_approx", "unused_exact", "unused_generation")
	filter := statistics.DiagnosticFilter{ProjectID: 7, Start: now.Add(-time.Minute), End: now.Add(time.Minute), Limit: 10}
	recent, err := store.ListRecent(ctx, filter)
	require.NoError(t, err)
	require.Len(t, recent.Items, 1)
	require.NotContains(t, recent.Items[0].Properties, "user_id")
	invalidPage, err := store.ListInvalid(ctx, filter)
	require.NoError(t, err)
	require.Len(t, invalidPage.Items, 1)
}
