package redisqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type QueueSnapshot struct {
	Lag              int64
	Pending          int64
	OldestPendingAge time.Duration
	DeadLetters      int64
}
type QueueSnapshotSink interface{ SetQueueSnapshot(QueueSnapshot) }
type QueueMonitor struct {
	client      redis.UniversalClient
	streams     []string
	group, dead string
	sink        QueueSnapshotSink
}

func NewQueueMonitor(client redis.UniversalClient, streams []string, group, dead string, sink QueueSnapshotSink) *QueueMonitor {
	return &QueueMonitor{client: client, streams: streams, group: group, dead: dead, sink: sink}
}
func (m *QueueMonitor) ObserveQueues(ctx context.Context) error {
	var snapshot QueueSnapshot
	for _, stream := range m.streams {
		groups, err := m.client.XInfoGroups(ctx, stream).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("read consumer groups %s: %w", stream, err)
		}
		for _, group := range groups {
			if group.Name == m.group && group.Lag > 0 {
				snapshot.Lag += group.Lag
				break
			}
		}
		pending, err := m.client.XPending(ctx, stream, m.group).Result()
		if err == nil {
			snapshot.Pending += pending.Count
		}
		if err != nil && err != redis.Nil {
			return fmt.Errorf("read pending %s: %w", stream, err)
		}
		items, err := m.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: stream, Group: m.group, Start: "-", End: "+", Count: 1}).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("read oldest pending %s: %w", stream, err)
		}
		if len(items) > 0 && items[0].Idle > snapshot.OldestPendingAge {
			snapshot.OldestPendingAge = items[0].Idle
		}
	}
	dead, err := m.client.XLen(ctx, m.dead).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("read dead letters: %w", err)
	}
	snapshot.DeadLetters = dead
	if m.sink != nil {
		m.sink.SetQueueSnapshot(snapshot)
	}
	return nil
}

type NoopQueueSnapshotSink struct{}

func (NoopQueueSnapshotSink) SetQueueSnapshot(QueueSnapshot) {}
