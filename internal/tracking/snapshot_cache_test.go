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

func TestSnapshotCacheInvalidationPreventsStaleLoadRepopulation(t *testing.T) {
	old := Snapshot{ID: 1, ProjectID: 4, PointCode: "product_view", Version: 1}
	fresh := Snapshot{ID: 2, ProjectID: 4, PointCode: "product_view", Version: 2}
	store := &blockingSnapshotStore{started: make(chan struct{}), release: make(chan struct{}), first: old, next: fresh}
	cache := NewSnapshotCache(store, time.Minute, time.Now)
	result := make(chan Snapshot, 1)
	errs := make(chan error, 1)
	go func() {
		snapshot, err := cache.Get(context.Background(), 4, "product_view")
		result <- snapshot
		errs <- err
	}()
	<-store.started
	cache.Invalidate(4, "product_view")
	close(store.release)
	require.NoError(t, <-errs)
	require.Equal(t, uint64(2), (<-result).ID)

	cached, err := cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	require.Equal(t, uint64(2), cached.ID)
	require.Equal(t, 2, store.calls)
}

func TestSnapshotCacheOwnsDeepCopiesOnStoreAndReturn(t *testing.T) {
	source := Snapshot{ID: 7, ProjectID: 4, PointCode: "product_view", CategoryPath: []CategoryRef{{Code: "mall"}}, SupportedPlatforms: []string{"web"}, SupportedChannels: []string{"direct"}, Properties: []Property{{Code: "source", EnumValues: []string{"home"}, ExampleValue: map[string]any{"nested": []any{"value"}}}}}
	store := &snapshotStoreFake{snapshot: source}
	cache := NewSnapshotCache(store, time.Minute, time.Now)
	first, err := cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)

	store.snapshot.CategoryPath[0].Code = "store-mutated"
	store.snapshot.SupportedPlatforms[0] = "ios"
	store.snapshot.Properties[0].EnumValues[0] = "store-mutated"
	first.CategoryPath[0].Code = "caller-mutated"
	first.SupportedChannels[0] = "caller-mutated"
	first.Properties[0].EnumValues[0] = "caller-mutated"
	first.Properties[0].ExampleValue.(map[string]any)["nested"].([]any)[0] = "caller-mutated"

	second, err := cache.Get(context.Background(), 4, "product_view")
	require.NoError(t, err)
	require.Equal(t, "mall", second.CategoryPath[0].Code)
	require.Equal(t, "web", second.SupportedPlatforms[0])
	require.Equal(t, "direct", second.SupportedChannels[0])
	require.Equal(t, "home", second.Properties[0].EnumValues[0])
	require.Equal(t, "value", second.Properties[0].ExampleValue.(map[string]any)["nested"].([]any)[0])
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

type blockingSnapshotStore struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	first   Snapshot
	next    Snapshot
	calls   int
}

func (s *blockingSnapshotStore) FindPublishedSnapshot(context.Context, uint64, string) (Snapshot, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.started)
		<-s.release
		return s.first, nil
	}
	return s.next, nil
}
