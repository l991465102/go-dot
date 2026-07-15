package mysql

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPublishedPointsIsProjectScopedAndReturnsDurablePublicationTime(t *testing.T) {
	published := time.Date(2026, 7, 1, 3, 0, 0, 0, time.UTC)
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{{uint64(11), "view", "View", published}}
	items, err := NewTrackingRepository(db).PublishedPoints(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, published, items[0].PublishedAt)
	require.Contains(t, script.queries[0], "project_id=?")
	require.Contains(t, script.queries[0], "definition_state='PUBLISHED'")
}
