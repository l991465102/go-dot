package redisqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/redis/go-redis/v9"
)

const overviewReconcileBatchSize = 100

var reconcileStreamCandidateScript = redis.NewScript(`
local function finish(value)
  redis.call('HSET', KEYS[5], ARGV[7], ARGV[8])
  return value
end
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
if not last_delivered then return finish(0) end

local pending = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
local function id_after(left, right)
  local ldash = string.find(left, '-')
  local rdash = string.find(right, '-')
  local lms = tonumber(ldash and string.sub(left, 1, ldash - 1) or left) or 0
  local rms = tonumber(rdash and string.sub(right, 1, rdash - 1) or right) or 0
  if lms ~= rms then return lms > rms end
  local lseq = tonumber(ldash and string.sub(left, ldash + 1) or '0') or 0
  local rseq = tonumber(rdash and string.sub(right, rdash + 1) or '0') or 0
  return lseq > rseq
end
if #pending == 0 and not id_after(ARGV[2], last_delivered) then return finish(0) end
if redis.call('HSETNX', KEYS[2], ARGV[6], ARGV[3] .. '|' .. KEYS[1] .. '|' .. ARGV[4]) == 0 then
  return finish(0)
end
redis.call('HINCRBY', KEYS[3], ARGV[3], 1)
redis.call('ZADD', KEYS[4], ARGV[5], KEYS[1] .. '|' .. ARGV[4])
return finish(1)
`)

var reconcileRetryCandidateScript = redis.NewScript(`
local function finish(value)
  redis.call('ZREM', KEYS[6], ARGV[1])
  return value
end
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return finish(0) end
local raw = redis.call('HGET', KEYS[2], ARGV[1])
if not raw then return finish(0) end
if redis.call('HSETNX', KEYS[3], ARGV[6], ARGV[2] .. '|' .. ARGV[5] .. '|' .. ARGV[3]) == 0 then
  return finish(0)
end
local record = cjson.decode(raw)
record.pending_mapping = ARGV[2] .. '|' .. ARGV[5] .. '|' .. ARGV[3]
record.pending_field = ARGV[6]
redis.call('HSET', KEYS[2], ARGV[1], cjson.encode(record))
redis.call('HINCRBY', KEYS[4], ARGV[2], 1)
redis.call('ZADD', KEYS[5], ARGV[4], ARGV[5] .. '|' .. ARGV[3])
return finish(1)
`)

var fillRetryOverviewBufferScript = redis.NewScript(`
local cursor = redis.call('HGET', KEYS[3], ARGV[1]) or '0'
local scanned = redis.call('ZSCAN', KEYS[1], cursor, 'COUNT', ARGV[3])
local entries = scanned[2]
for i = 1, #entries, 2 do
  local member = entries[i]
  if not redis.call('ZSCORE', KEYS[2], member) then
    local sequence = redis.call('HINCRBY', KEYS[3], ARGV[2], 1)
    redis.call('ZADD', KEYS[2], sequence, member)
  end
end
redis.call('HSET', KEYS[3], ARGV[1], scanned[1])
return #entries / 2
`)

var reconcileDeadCandidateScript = redis.NewScript(`
if ARGV[1] ~= '' and redis.call('HSETNX', KEYS[1], ARGV[2], ARGV[1]) == 1 then
  redis.call('HINCRBY', KEYS[2], ARGV[1], 1)
end
redis.call('HSET', KEYS[3], ARGV[3], ARGV[4])
return 1
`)

type retryOverviewRecord struct {
	BatchID   string          `json:"batch_id"`
	Payload   string          `json:"payload"`
	FirstSeen json.RawMessage `json:"first_seen"`
}

type overviewSource struct {
	kind   string
	stream string
}

// ReconcileOverview discovers outstanding messages that predate or outlive the
// derived project overview index. Eligibility and insertion are checked in one
// Redis script so a concurrent ACK cannot resurrect completed work.
func (q *Queue) ReconcileOverview(ctx context.Context) error {
	if q == nil || q.client == nil || q.overviewGroup == "" || len(q.overviewStreams) == 0 {
		return errors.New("Redis overview queue is not configured")
	}
	sources := q.overviewSources()
	turn, err := q.overviewTurn(ctx, len(sources))
	if err != nil {
		return err
	}
	remaining, visited := q.overviewReconcileLimit, 0
	for remaining > 0 && visited < len(sources) {
		if err := ctx.Err(); err != nil {
			return err
		}
		source := sources[turn]
		var inspected int
		switch source.kind {
		case "pending":
			if err := q.ensureGroup(ctx, source.stream, q.overviewGroup); err != nil {
				return err
			}
			inspected, err = q.reconcilePendingCandidates(ctx, source.stream, remaining)
		case "undelivered":
			if err := q.ensureGroup(ctx, source.stream, q.overviewGroup); err != nil {
				return err
			}
			inspected, err = q.reconcileUndeliveredCandidates(ctx, source.stream, remaining)
		case "retry":
			inspected, err = q.reconcileRetryCandidates(ctx, source.stream, remaining)
		case "dead":
			inspected, err = q.reconcileDeadCandidates(ctx, remaining)
		}
		if err != nil {
			return err
		}
		remaining -= inspected
		visited++
		turn = (turn + 1) % len(sources)
		if err := q.client.HSet(ctx, q.overviewCursorKey(), q.cursorField("turn", ""), turn).Err(); err != nil {
			return fmt.Errorf("persist Redis overview reconciliation turn: %w", err)
		}
	}
	return nil
}

func (q *Queue) overviewSources() []overviewSource {
	out := make([]overviewSource, 0, len(q.overviewStreams)*3+1)
	for _, stream := range q.overviewStreams {
		for _, kind := range []string{"pending", "undelivered", "retry"} {
			out = append(out, overviewSource{kind: kind, stream: stream})
		}
	}
	return append(out, overviewSource{kind: "dead", stream: q.deadStream})
}

func (q *Queue) overviewTurn(ctx context.Context, sourceCount int) (int, error) {
	value, err := q.client.HGet(ctx, q.overviewCursorKey(), q.cursorField("turn", "")).Int()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("read Redis overview reconciliation turn: %w", err)
	}
	if value < 0 || value >= sourceCount {
		return 0, nil
	}
	return value, nil
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

func (q *Queue) reconcilePendingCandidates(ctx context.Context, stream string, limit int) (int, error) {
	field := q.cursorField("pending", stream)
	start, err := q.client.HGet(ctx, q.overviewCursorKey(), field).Result()
	if err == redis.Nil || start == "" {
		start = "-"
	} else if err != nil {
		return 0, err
	} else {
		start = nextStreamID(start)
	}
	pending, err := q.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: stream, Group: q.overviewGroup, Start: start, End: "+", Count: int64(min(limit, overviewReconcileBatchSize))}).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("scan Redis Stream %q PEL for overview: %w", stream, err)
	}
	for _, item := range pending {
		messages, readErr := q.client.XRangeN(ctx, stream, item.ID, item.ID, 1).Result()
		if readErr != nil && readErr != redis.Nil {
			return 0, fmt.Errorf("read Redis Stream %q PEL message %q: %w", stream, item.ID, readErr)
		}
		next := item.ID
		if len(messages) == 1 {
			if err := q.reconcileStreamCandidate(ctx, stream, messages[0], field, next); err != nil {
				return 0, err
			}
		} else if err := q.client.HSet(ctx, q.overviewCursorKey(), field, next).Err(); err != nil {
			return 0, err
		}
	}
	return len(pending), nil
}

func nextStreamID(id string) string {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return id
	}
	seq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || seq == ^uint64(0) {
		return id
	}
	return parts[0] + "-" + strconv.FormatUint(seq+1, 10)
}

func (q *Queue) reconcileUndeliveredCandidates(ctx context.Context, stream string, limit int) (int, error) {
	field := q.cursorField("undelivered", stream)
	start, err := q.client.HGet(ctx, q.overviewCursorKey(), field).Result()
	if err == redis.Nil || start == "" {
		last, lastErr := q.groupLastDeliveredID(ctx, stream)
		if lastErr != nil {
			return 0, lastErr
		}
		start = "(" + last
	} else if err != nil {
		return 0, err
	}
	messages, err := q.client.XRangeN(ctx, stream, start, "+", int64(min(limit, overviewReconcileBatchSize))).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("scan Redis Stream %q undelivered messages for overview: %w", stream, err)
	}
	for _, message := range messages {
		next := "(" + message.ID
		if err := q.reconcileStreamCandidate(ctx, stream, message, field, next); err != nil {
			return 0, err
		}
	}
	return len(messages), nil
}

func (q *Queue) reconcileStreamCandidate(ctx context.Context, stream string, message redis.XMessage, cursorField, cursorValue string) error {
	payload := []byte(valueString(message.Values[fieldPayload]))
	projectID := payloadProjectID(payload)
	if projectID == "" {
		return q.client.HSet(ctx, q.overviewCursorKey(), cursorField, cursorValue).Err()
	}
	batchID := valueString(message.Values[fieldBatchID])
	if batchID == "" {
		return q.client.HSet(ctx, q.overviewCursorKey(), cursorField, cursorValue).Err()
	}
	firstSeen, err := strconv.ParseInt(valueString(message.Values[fieldFirstSeen]), 10, 64)
	if err != nil || firstSeen <= 0 {
		return q.client.HSet(ctx, q.overviewCursorKey(), cursorField, cursorValue).Err()
	}
	_, err = reconcileStreamCandidateScript.Run(ctx, q.client,
		[]string{stream, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(projectID), q.overviewCursorKey()},
		q.overviewGroup, message.ID, projectID, batchID, firstSeen, stream+"|"+message.ID, cursorField, cursorValue,
	).Result()
	if err != nil {
		return fmt.Errorf("reconcile Redis Stream %q message %q overview: %w", stream, message.ID, err)
	}
	return nil
}

func (q *Queue) reconcileRetryCandidates(ctx context.Context, stream string, limit int) (int, error) {
	limit = min(limit, overviewReconcileBatchSize)
	processed, err := q.processRetryOverviewBuffer(ctx, stream, limit)
	if err != nil || processed == limit {
		return processed, err
	}
	if _, err := q.fillRetryOverviewBuffer(ctx, stream, limit-processed); err != nil {
		return processed, err
	}
	more, err := q.processRetryOverviewBuffer(ctx, stream, limit-processed)
	return processed + more, err
}

func (q *Queue) fillRetryOverviewBuffer(ctx context.Context, stream string, count int) (int, error) {
	if count < 1 {
		return 0, nil
	}
	result, err := fillRetryOverviewBufferScript.Run(ctx, q.client,
		[]string{retryScheduleKey(stream), q.retryOverviewBufferKey(stream), q.overviewCursorKey()},
		q.cursorField("retry", stream), q.cursorField("retry-sequence", stream), count,
	).Int()
	if err != nil {
		return 0, fmt.Errorf("persist Redis retry scan for %q: %w", stream, err)
	}
	return result, nil
}

func (q *Queue) processRetryOverviewBuffer(ctx context.Context, stream string, limit int) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	items, err := q.client.ZRange(ctx, q.retryOverviewBufferKey(stream), 0, int64(limit-1)).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("read Redis retry overview buffer for %q: %w", stream, err)
	}
	for _, member := range items {
		if err := q.processRetryOverviewMember(ctx, stream, member); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func (q *Queue) processRetryOverviewMember(ctx context.Context, stream, member string) error {
	remove := func() error { return q.client.ZRem(ctx, q.retryOverviewBufferKey(stream), member).Err() }
	raw, err := q.client.HGet(ctx, retryDataKey(stream), member).Bytes()
	if err == redis.Nil {
		return remove()
	}
	if err != nil {
		return fmt.Errorf("read Redis retry %q overview record: %w", member, err)
	}
	var record retryOverviewRecord
	if json.Unmarshal(raw, &record) != nil || record.BatchID == "" {
		return remove()
	}
	firstSeen, err := strconv.ParseInt(strings.Trim(string(record.FirstSeen), `"`), 10, 64)
	if err != nil || firstSeen <= 0 {
		return remove()
	}
	projectID := payloadProjectID([]byte(record.Payload))
	if projectID == "" {
		return remove()
	}
	_, err = reconcileRetryCandidateScript.Run(ctx, q.client,
		[]string{retryScheduleKey(stream), retryDataKey(stream), q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(projectID), q.retryOverviewBufferKey(stream)},
		member, projectID, record.BatchID, firstSeen, stream, retryIndexField(stream, member),
	).Result()
	if err != nil {
		return fmt.Errorf("reconcile Redis retry %q overview: %w", member, err)
	}
	return nil
}

func (q *Queue) retryOverviewBufferKey(stream string) string {
	tag, _ := redisHashTag(stream)
	sum := sha256.Sum256([]byte(q.overviewGroup + "\x00" + strings.Join(q.overviewStreams, "\x00") + "\x00" + q.deadStream + "\x00" + stream))
	return "queue:{" + tag + "}:overview:retry-buffer:" + hex.EncodeToString(sum[:8])
}

func (q *Queue) reconcileDeadCandidates(ctx context.Context, limit int) (int, error) {
	field := q.cursorField("dead", q.deadStream)
	start, err := q.client.HGet(ctx, q.overviewCursorKey(), field).Result()
	if err == redis.Nil || start == "" || start == "-" {
		start = "-"
	} else if err != nil {
		return 0, err
	} else {
		start = "(" + start
	}
	messages, err := q.client.XRangeN(ctx, q.deadStream, start, "+", int64(min(limit, overviewReconcileBatchSize))).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("scan Redis DLQ %q for overview: %w", q.deadStream, err)
	}
	for _, message := range messages {
		payload := []byte(valueString(message.Values[fieldPayload]))
		candidate := ports.Message{Payload: payload}
		if valueString(message.Values[fieldMalformed]) == "1" || isMalformedRawEnvelope(payload) {
			candidate.Malformed = &ports.MalformedMessage{RawEnvelope: payload}
		}
		projectID := messageProjectID(candidate)
		_, err := reconcileDeadCandidateScript.Run(ctx, q.client, []string{q.deadLetterIndexKey(), q.deadLetterCountKey(), q.overviewCursorKey()}, projectID, q.deadStream+"|"+message.ID, field, message.ID).Result()
		if err != nil {
			return 0, fmt.Errorf("reconcile Redis DLQ message %q overview: %w", message.ID, err)
		}
	}
	return len(messages), nil
}

func (q *Queue) overviewCursorKey() string {
	tag, _ := redisHashTag(q.deadStream)
	return "queue:{" + tag + "}:overview:cursors"
}

func (q *Queue) cursorField(kind, stream string) string {
	sum := sha256.Sum256([]byte(q.overviewGroup + "\x00" + strings.Join(q.overviewStreams, "\x00") + "\x00" + q.deadStream))
	return hex.EncodeToString(sum[:8]) + "|" + kind + "|" + stream
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
