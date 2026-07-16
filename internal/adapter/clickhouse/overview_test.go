package clickhouse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/stretchr/testify/require"
)

func TestOverviewQualityQueriesAreProjectScopedAndUseExplicitWindow(t *testing.T) {
	db := &statisticsRecordingDB{queryErr: errors.New("recorded")}
	store := NewStatisticsStore(db)
	w := overview.Window{Start: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	_, err := store.ValidCount(context.Background(), 9, w)
	require.ErrorContains(t, err, "recorded")
	_, err = store.InvalidCount(context.Background(), 9, w)
	require.ErrorContains(t, err, "recorded")
	_, err = store.ActivePointIDs(context.Background(), 9, w)
	require.ErrorContains(t, err, "recorded")
	require.Len(t, db.queries, 3)
	for i, q := range db.queries {
		normalized := strings.Join(strings.Fields(q), " ")
		require.Contains(t, normalized, "project_id=?")
		require.Contains(t, normalized, "received_at>=?")
		require.Contains(t, normalized, "received_at<?")
		require.Equal(t, []any{uint64(9), w.Start, w.End}, db.queryArgs[i])
	}
	require.Contains(t, strings.Join(strings.Fields(db.queries[0]), " "), "tracking_event")
}
