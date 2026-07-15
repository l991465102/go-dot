package clickhouse

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestSaveValidUsesReliableDeduplicatedInsertAndCanonicalJSON(t *testing.T) {
	db := &recordingExecutor{}
	store := New(db)
	event := validEvent()
	event.Properties = map[string]any{"z": 1, "a": map[string]any{"y": 2, "x": 1}}

	require.NoError(t, store.SaveValid(context.Background(), "batch-1", []ports.StoredEvent{event}))
	require.Len(t, db.calls, 1)
	require.Contains(t, db.calls[0].query, "INSERT INTO tracking_event")
	require.Equal(t, `{"a":{"x":1,"y":2},"z":1}`, db.calls[0].args[22])
	settings := insertSettings("batch-1")
	require.EqualValues(t, 1, settings["async_insert"])
	require.EqualValues(t, 1, settings["wait_for_async_insert"])
	require.EqualValues(t, 1, settings["async_insert_deduplicate"])
	require.Equal(t, "batch-1", settings["insert_deduplication_token"])
}

func TestSaveInvalidPreservesErrorAndCanonicalOriginalPayload(t *testing.T) {
	db := &recordingExecutor{}
	store := New(db)
	original := json.RawMessage(`{"z":2,"event_id":"evt-1","point_code":"p"}`)

	require.NoError(t, store.SaveInvalid(context.Background(), "batch-2", []ports.InvalidEvent{{
		ProjectID: 7, EventID: "evt-1", PointCode: "p", ErrorCode: "PROPERTY_REQUIRED",
		ErrorMessage: "product_id required", Original: original, ReceivedAt: time.Unix(1, 0).UTC(),
	}}))
	require.Len(t, db.calls, 1)
	require.Contains(t, db.calls[0].query, "INSERT INTO tracking_invalid_event")
	require.Equal(t, "PROPERTY_REQUIRED", db.calls[0].args[6])
	require.Equal(t, `{"event_id":"evt-1","point_code":"p","z":2}`, db.calls[0].args[8])
	require.Equal(t, "batch-2", insertSettings("batch-2")["insert_deduplication_token"])
}

func TestSaveRejectsEmptyTokenBeforeWriting(t *testing.T) {
	db := &recordingExecutor{}
	store := New(db)
	require.ErrorContains(t, store.SaveValid(context.Background(), "", []ports.StoredEvent{validEvent()}), "token")
	require.Empty(t, db.calls)
}

func TestStoreCanTargetIsolatedIntegrationTables(t *testing.T) {
	db := &recordingExecutor{}
	store := newWithTables(db, "tracking_event_task7", "tracking_invalid_event_task7")

	require.NoError(t, store.SaveValid(context.Background(), "batch-1", []ports.StoredEvent{validEvent()}))
	require.Contains(t, db.calls[0].query, "INSERT INTO tracking_event_task7")
}

func TestSavePropagatesInsertFailure(t *testing.T) {
	db := &recordingExecutor{err: errors.New("clickhouse unavailable")}
	err := New(db).SaveValid(context.Background(), "batch-1", []ports.StoredEvent{validEvent()})
	require.ErrorContains(t, err, "clickhouse unavailable")
}

func TestSaveInvalidRejectsTrailingJSONBeforeWriting(t *testing.T) {
	db := &recordingExecutor{}
	event := ports.InvalidEvent{ProjectID: 7, Original: json.RawMessage(`{"ok":true} {"extra":true}`), ReceivedAt: time.Unix(1, 0)}

	err := New(db).SaveInvalid(context.Background(), "batch-1", []ports.InvalidEvent{event})
	require.ErrorContains(t, err, "multiple JSON values")
	require.Empty(t, db.calls)
}

func TestEventStoreClickHouseIntegration(t *testing.T) {
	dsn := os.Getenv("GO_DOT_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set GO_DOT_TEST_CLICKHOUSE_DSN to run the ClickHouse integration test")
	}
	db, err := sql.Open("clickhouse", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))

	suffix := fmt.Sprintf("task7_%d", time.Now().UnixNano())
	validTable := "tracking_event_" + suffix
	invalidTable := "tracking_invalid_event_" + suffix
	_, err = db.ExecContext(ctx, `CREATE TABLE `+validTable+` (
event_id String,batch_id String,project_id UInt64,point_id UInt64,point_code String,point_version UInt32,
domain_category_id UInt64,domain_category_code String,module_category_id UInt64,module_category_code String,
scene_category_id UInt64,scene_category_code String,visitor_hash FixedString(64),platform String,channel String,
app_version String,session_hash String,client_event_at DateTime64(3,'UTC'),received_at DateTime64(3,'UTC'),
statistic_at DateTime64(3,'UTC'),statistic_date Date,time_corrected Bool,properties_json String,version UInt64
) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,event_id)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE `+invalidTable+` (
project_id UInt64,batch_id String,event_id String,point_code String,platform String,channel String,error_code String,
error_message String,raw_json String,received_at DateTime64(3,'UTC'),version UInt64
) ENGINE=ReplacingMergeTree(version) ORDER BY (project_id,event_id,error_code)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+validTable)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+invalidTable)
	})

	store := newWithTables(db, validTable, invalidTable)
	event := validEvent()
	require.NoError(t, store.SaveValid(ctx, "batch-integration", []ports.StoredEvent{event}))
	require.NoError(t, store.SaveValid(ctx, "batch-integration", []ports.StoredEvent{event}))
	var validCount uint64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count() FROM "+validTable+" FINAL WHERE event_id = ?", event.EventID).Scan(&validCount))
	require.EqualValues(t, 1, validCount)

	invalid := ports.InvalidEvent{ProjectID: 7, EventID: "bad-1", PointCode: "view", ErrorCode: "PROPERTY_REQUIRED", ErrorMessage: "required", Original: json.RawMessage(`{"z":2,"a":1}`), ReceivedAt: time.Now().UTC()}
	require.NoError(t, store.SaveInvalid(ctx, "invalid-integration", []ports.InvalidEvent{invalid}))
	var errorCode, raw string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT error_code,raw_json FROM "+invalidTable+" FINAL WHERE event_id = ?", invalid.EventID).Scan(&errorCode, &raw))
	require.Equal(t, "PROPERTY_REQUIRED", errorCode)
	require.Equal(t, `{"a":1,"z":2}`, raw)
}

func validEvent() ports.StoredEvent {
	return ports.StoredEvent{EventID: "evt-1", ProjectID: 7, PointID: 9, PointCode: "view", PointVersion: 1,
		DomainCategoryID: 1, DomainCategoryCode: "mall", ModuleCategoryID: 2, ModuleCategoryCode: "product", SceneCategoryID: 3, SceneCategoryCode: "detail",
		VisitorHash: 42, Platform: "web", Channel: "organic", AppVersion: "1.0", SessionHash: "s", ClientEventAt: time.Unix(1, 0).UTC(),
		ReceivedAt: time.Unix(2, 0).UTC(), StatisticAt: time.Unix(1, 0).UTC(), TimeCorrected: false, Properties: map[string]any{"a": 1}, Version: 1}
}

type execCall struct {
	ctx   context.Context
	query string
	args  []any
}

type recordingExecutor struct {
	calls []execCall
	err   error
}

func (r *recordingExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	r.calls = append(r.calls, execCall{ctx: ctx, query: strings.Join(strings.Fields(query), " "), args: args})
	return nil, r.err
}
