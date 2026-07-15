package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/redis/go-redis/v9"
)

func (q *Queue) Snapshot(ctx context.Context, projectID uint64) (overview.QueueSnapshot, error) {
	if q == nil || q.client == nil || projectID == 0 || q.overviewGroup == "" || len(q.overviewStreams) == 0 {
		return overview.QueueSnapshot{}, errors.New("Redis overview queue is not configured")
	}
	var out overview.QueueSnapshot
	projectKey := strconv.FormatUint(projectID, 10)
	pending, err := q.client.HGet(ctx, q.pendingCountKey(), projectKey).Int64()
	if err != nil && err != redis.Nil {
		return out, fmt.Errorf("read project pending count: %w", err)
	}
	if err == nil {
		out.Pending = max(pending, 0)
	}
	oldest, err := q.client.ZRangeWithScores(ctx, q.pendingAgeKey(projectKey), 0, 0).Result()
	if err != nil && err != redis.Nil {
		return out, fmt.Errorf("read oldest project pending: %w", err)
	}
	if len(oldest) > 0 {
		created := time.UnixMilli(int64(oldest[0].Score))
		if age := time.Since(created); age > 0 {
			out.OldestPendingAge = age
		}
	}
	count, err := q.client.HGet(ctx, q.deadLetterCountKey(), projectKey).Int64()
	if err != nil && err != redis.Nil {
		return out, fmt.Errorf("read project dead-letter count: %w", err)
	}
	if err == nil {
		out.DeadLetters = count
	}
	// Redis idle is millisecond based; normalize for a stable API contract.
	out.OldestPendingAge = out.OldestPendingAge.Truncate(time.Millisecond)
	return out, nil
}
