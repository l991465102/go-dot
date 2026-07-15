package tracking

import (
	"context"
	"errors"
	"sync"
	"time"
)

type SnapshotStore interface {
	FindPublishedSnapshot(context.Context, uint64, string) (Snapshot, error)
}
type CategoryIDStore interface {
	ResolveCategoryIDs(context.Context, uint64, string, string, string) (uint64, uint64, uint64, error)
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
	store              SnapshotStore
	ttl                time.Duration
	now                func() time.Time
	mu                 sync.RWMutex
	entries            map[snapshotCacheKey]snapshotCacheEntry
	keyGenerations     map[snapshotCacheKey]uint64
	projectGenerations map[uint64]uint64
}

func NewSnapshotCache(store SnapshotStore, ttl time.Duration, now func() time.Time) *SnapshotCache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &SnapshotCache{store: store, ttl: ttl, now: now, entries: make(map[snapshotCacheKey]snapshotCacheEntry), keyGenerations: make(map[snapshotCacheKey]uint64), projectGenerations: make(map[uint64]uint64)}
}

func (c *SnapshotCache) Get(ctx context.Context, projectID uint64, pointCode string) (Snapshot, error) {
	key := snapshotCacheKey{projectID: projectID, pointCode: pointCode}
	for {
		now := c.now()
		c.mu.RLock()
		entry, ok := c.entries[key]
		keyGeneration := c.keyGenerations[key]
		projectGeneration := c.projectGenerations[projectID]
		c.mu.RUnlock()
		if ok && now.Before(entry.expiresAt) {
			return cloneSnapshot(entry.snapshot), nil
		}
		snapshot, err := c.store.FindPublishedSnapshot(ctx, projectID, pointCode)
		if err != nil {
			return Snapshot{}, err
		}
		owned := cloneSnapshot(snapshot)
		c.mu.Lock()
		if c.keyGenerations[key] != keyGeneration || c.projectGenerations[projectID] != projectGeneration {
			c.mu.Unlock()
			continue
		}
		c.entries[key] = snapshotCacheEntry{snapshot: owned, expiresAt: now.Add(c.ttl)}
		c.mu.Unlock()
		return cloneSnapshot(owned), nil
	}
}

func (c *SnapshotCache) ResolveCategoryIDs(ctx context.Context, projectID uint64, domain, module, scene string) (uint64, uint64, uint64, error) {
	store, ok := c.store.(CategoryIDStore)
	if !ok {
		return 0, 0, 0, errors.New("category ID store unavailable")
	}
	return store.ResolveCategoryIDs(ctx, projectID, domain, module, scene)
}

func (c *SnapshotCache) Invalidate(projectID uint64, pointCode string) {
	c.mu.Lock()
	key := snapshotCacheKey{projectID: projectID, pointCode: pointCode}
	delete(c.entries, key)
	c.keyGenerations[key]++
	c.mu.Unlock()
}

func (c *SnapshotCache) InvalidateProject(projectID uint64) {
	c.mu.Lock()
	c.projectGenerations[projectID]++
	for key := range c.entries {
		if key.projectID == projectID {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	clone := snapshot
	clone.CategoryPath = append([]CategoryRef(nil), snapshot.CategoryPath...)
	clone.SupportedPlatforms = append([]string(nil), snapshot.SupportedPlatforms...)
	clone.SupportedChannels = append([]string(nil), snapshot.SupportedChannels...)
	clone.Properties = make([]Property, len(snapshot.Properties))
	for i, property := range snapshot.Properties {
		clone.Properties[i] = property
		clone.Properties[i].EnumValues = append([]string(nil), property.EnumValues...)
		clone.Properties[i].ExampleValue = cloneSnapshotValue(property.ExampleValue)
	}
	return clone
}

func cloneSnapshotValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, item := range typed {
			clone[key] = cloneSnapshotValue(item)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneSnapshotValue(item)
		}
		return clone
	case []string:
		return append([]string(nil), typed...)
	case []byte:
		return append([]byte(nil), typed...)
	default:
		return value
	}
}
