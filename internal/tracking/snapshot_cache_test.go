package tracking

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotCacheCachesForTTLAndSupportsInvalidation(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	store := &snapshotStoreFake{snapshot: Snapshot{ID: 7, ProjectID: 4, PointCode: "product_view"}}
	cache := NewSnapshotCache(store, time.Minute, func() time.Time { return now })

	first, err := cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	second, err := cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, store.calls)

	cache.Invalidate(4, "product_view")
	_, err = cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	require.Equal(t, 2, store.calls)
}

func TestSnapshotCacheExpiresAndDoesNotCacheFailures(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	store := &snapshotStoreFake{err: ErrPointNotPublished}
	cache := NewSnapshotCache(store, time.Minute, func() time.Time { return now })
	_, err := cache.Get(context.Background(), 4, "product_view")
	require.ErrorIs(t, err, ErrPointNotPublished)
	store.err = nil
	store.snapshot = Snapshot{ID: 8, ProjectID: 4, PointCode: "product_view"}
	_, err = cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	require.Equal(t, 2, store.calls)

	now = now.Add(time.Minute)
	_, err = cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	require.Equal(t, 3, store.calls)
}

type snapshotStoreFake struct {
	mu       sync.Mutex
	snapshot Snapshot
	err      error
	calls    int
}

func (s *snapshotStoreFake) FindPublishedSnapshot(context.Context, uint64, string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.snapshot, s.err
}
