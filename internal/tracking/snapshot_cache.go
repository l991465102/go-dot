package tracking

import (
	"context"
	"sync"
	"time"
)

type SnapshotStore interface {
	FindPublishedSnapshot(context.Context, uint64, string) (Snapshot, error)
}

type snapshotCacheKey struct {
	projectID uint64
	pointCode string
}
type snapshotCacheEntry struct {
	snapshot  Snapshot
	expiresAt time.Time
}

type SnapshotCache struct {
	store   SnapshotStore
	ttl     time.Duration
	now     func() time.Time
	mu      sync.RWMutex
	entries map[snapshotCacheKey]snapshotCacheEntry
}

func NewSnapshotCache(store SnapshotStore, ttl time.Duration, now func() time.Time) *SnapshotCache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &SnapshotCache{store: store, ttl: ttl, now: now, entries: make(map[snapshotCacheKey]snapshotCacheEntry)}
}

func (c *SnapshotCache) Get(ctx context.Context, projectID uint64, pointCode string) (Snapshot, error) {
	key := snapshotCacheKey{projectID: projectID, pointCode: pointCode}
	now := c.now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.snapshot, nil
	}
	snapshot, err := c.store.FindPublishedSnapshot(ctx, projectID, pointCode)
	if err != nil {
		return Snapshot{}, err
	}
	c.mu.Lock()
	c.entries[key] = snapshotCacheEntry{snapshot: snapshot, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return snapshot, nil
}

func (c *SnapshotCache) Invalidate(projectID uint64, pointCode string) {
	c.mu.Lock()
	delete(c.entries, snapshotCacheKey{projectID: projectID, pointCode: pointCode})
	c.mu.Unlock()
}

func (c *SnapshotCache) InvalidateProject(projectID uint64) {
	c.mu.Lock()
	for key := range c.entries {
		if key.projectID == projectID {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}
