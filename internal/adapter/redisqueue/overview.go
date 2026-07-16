package redisqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/redis/go-redis/v9"
)

const overviewReconcileBatchSize = 100

var reconcileStreamCandidateScript = redis.NewScript(`
local groups = redis.call('XINFO', 'GROUPS', KEYS[1])
local last_delivered = nil
for _, info in ipairs(groups) do
  local name = nil
  local last = nil
  for i = 1, #info, 2 do
    if info[i] == 'name' then name = info[i + 1] end
    if info[i] == 'last-delivered-id' then last = info[i + 1] end
  end
  if name == ARGV[1] then
    last_delivered = last
    break
  end
end
if not last_delivered then return 0 end

local pending = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
local function id_after(left, right)
  local ldash = string.find(left, '-')
  local rdash = string.find(right, '-')
  local lms = tonumber(string.sub(left, 1, ldash - 1))
  local rms = tonumber(string.sub(right, 1, rdash - 1))
  if lms ~= rms then return lms > rms end
  return tonumber(string.sub(left, ldash + 1)) > tonumber(string.sub(right, rdash + 1))
end
if #pending == 0 and not id_after(ARGV[2], last_delivered) then return 0 end
if redis.call('HSETNX', KEYS[2], ARGV[6], ARGV[3] .. '|' .. KEYS[1] .. '|' .. ARGV[4]) == 0 then
  return 0
end
redis.call('HINCRBY', KEYS[3], ARGV[3], 1)
redis.call('ZADD', KEYS[4], ARGV[5], KEYS[1] .. '|' .. ARGV[4])
return 1
`)

var reconcileRetryCandidateScript = redis.NewScript(`
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end
local raw = redis.call('HGET', KEYS[2], ARGV[1])
if not raw then return 0 end
if redis.call('HSETNX', KEYS[3], ARGV[6], ARGV[2] .. '|' .. ARGV[5] .. '|' .. ARGV[3]) == 0 then
  return 0
end
local record = cjson.decode(raw)
record.pending_mapping = ARGV[2] .. '|' .. ARGV[5] .. '|' .. ARGV[3]
record.pending_field = ARGV[6]
redis.call('HSET', KEYS[2], ARGV[1], cjson.encode(record))
redis.call('HINCRBY', KEYS[4], ARGV[2], 1)
redis.call('ZADD', KEYS[5], ARGV[4], ARGV[5] .. '|' .. ARGV[3])
return 1
`)

type retryOverviewRecord struct {
	BatchID   string          `json:"batch_id"`
	Payload   string          `json:"payload"`
	FirstSeen json.RawMessage `json:"first_seen"`
}

// ReconcileOverview discovers outstanding messages that predate or outlive the
// derived project overview index. Eligibility and insertion are checked in one
// Redis script so a concurrent ACK cannot resurrect completed work.
func (q *Queue) ReconcileOverview(ctx context.Context) error {
	if q == nil || q.client == nil || q.overviewGroup == "" || len(q.overviewStreams) == 0 {
		return errors.New("Redis overview queue is not configured")
	}
	for _, stream := range q.overviewStreams {
		if err := q.ensureGroup(ctx, stream, q.overviewGroup); err != nil {
			return err
		}
		lastDelivered, err := q.groupLastDeliveredID(ctx, stream)
		if err != nil {
			return err
		}
		if err := q.reconcilePendingCandidates(ctx, stream); err != nil {
			return err
		}
		if err := q.reconcileUndeliveredCandidates(ctx, stream, lastDelivered); err != nil {
			return err
		}
		if err := q.reconcileRetryCandidates(ctx, stream); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queue) groupLastDeliveredID(ctx context.Context, stream string) (string, error) {
	groups, err := q.client.XInfoGroups(ctx, stream).Result()
	if err != nil {
		return "", fmt.Errorf("read Redis Stream %q groups for overview: %w", stream, err)
	}
	for _, group := range groups {
		if group.Name == q.overviewGroup {
			return group.LastDeliveredID, nil
		}
	}
	return "", fmt.Errorf("Redis Stream %q overview group %q does not exist", stream, q.overviewGroup)
}

func (q *Queue) reconcilePendingCandidates(ctx context.Context, stream string) error {
	start := "-"
	for {
		pending, err := q.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: stream, Group: q.overviewGroup, Start: start, End: "+", Count: overviewReconcileBatchSize,
		}).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("scan Redis Stream %q PEL for overview: %w", stream, err)
		}
		for _, item := range pending {
			messages, err := q.client.XRangeN(ctx, stream, item.ID, item.ID, 1).Result()
			if err != nil && err != redis.Nil {
				return fmt.Errorf("read Redis Stream %q PEL message %q: %w", stream, item.ID, err)
			}
			if len(messages) == 1 {
				if err := q.reconcileStreamCandidate(ctx, stream, messages[0]); err != nil {
					return err
				}
			}
		}
		if len(pending) < overviewReconcileBatchSize {
			return nil
		}
		start = "(" + pending[len(pending)-1].ID
	}
}

func (q *Queue) reconcileUndeliveredCandidates(ctx context.Context, stream, lastDelivered string) error {
	start := "(" + lastDelivered
	for {
		messages, err := q.client.XRangeN(ctx, stream, start, "+", overviewReconcileBatchSize).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("scan Redis Stream %q undelivered messages for overview: %w", stream, err)
		}
		for _, message := range messages {
			if err := q.reconcileStreamCandidate(ctx, stream, message); err != nil {
				return err
			}
		}
		if len(messages) < overviewReconcileBatchSize {
			return nil
		}
		start = "(" + messages[len(messages)-1].ID
	}
}

func (q *Queue) reconcileStreamCandidate(ctx context.Context, stream string, message redis.XMessage) error {
	payload := []byte(valueString(message.Values[fieldPayload]))
	projectID := payloadProjectID(payload)
	if projectID == "" {
		return nil
	}
	batchID := valueString(message.Values[fieldBatchID])
	if batchID == "" {
		return nil
	}
	firstSeen, err := strconv.ParseInt(valueString(message.Values[fieldFirstSeen]), 10, 64)
	if err != nil || firstSeen <= 0 {
		return nil
	}
	_, err = reconcileStreamCandidateScript.Run(ctx, q.client,
		[]string{stream, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(projectID)},
		q.overviewGroup, message.ID, projectID, batchID, firstSeen, stream+"|"+message.ID,
	).Result()
	if err != nil {
		return fmt.Errorf("reconcile Redis Stream %q message %q overview: %w", stream, message.ID, err)
	}
	return nil
}

func (q *Queue) reconcileRetryCandidates(ctx context.Context, stream string) error {
	var cursor uint64
	for {
		items, next, err := q.client.ZScan(ctx, retryScheduleKey(stream), cursor, "*", overviewReconcileBatchSize).Result()
		if err != nil && err != redis.Nil {
			return fmt.Errorf("scan Redis retry schedule %q for overview: %w", stream, err)
		}
		for i := 0; i+1 < len(items); i += 2 {
			member := items[i]
			raw, err := q.client.HGet(ctx, retryDataKey(stream), member).Bytes()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				return fmt.Errorf("read Redis retry %q overview record: %w", member, err)
			}
			var record retryOverviewRecord
			if json.Unmarshal(raw, &record) != nil || record.BatchID == "" {
				continue
			}
			firstSeen, err := strconv.ParseInt(strings.Trim(string(record.FirstSeen), `"`), 10, 64)
			if err != nil || firstSeen <= 0 {
				continue
			}
			projectID := payloadProjectID([]byte(record.Payload))
			if projectID == "" {
				continue
			}
			_, err = reconcileRetryCandidateScript.Run(ctx, q.client,
				[]string{retryScheduleKey(stream), retryDataKey(stream), q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(projectID)},
				member, projectID, record.BatchID, firstSeen, stream, retryIndexField(stream, member),
			).Result()
			if err != nil {
				return fmt.Errorf("reconcile Redis retry %q overview: %w", member, err)
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

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
