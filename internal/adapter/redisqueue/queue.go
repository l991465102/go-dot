package redisqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/redis/go-redis/v9"
)

const (
	fieldBatchID    = "batch_id"
	fieldPayload    = "payload"
	fieldDeliveries = "deliveries"
	fieldFirstSeen  = "first_seen"
	fieldLastError  = "last_error"
)

var moveScript = redis.NewScript(`
local pending = redis.call('XPENDING', KEYS[2], ARGV[1], ARGV[2], ARGV[2], 1)
if #pending == 0 then
  return ''
end
local new_id = redis.call('XADD', KEYS[1], '*',
  'batch_id', ARGV[3],
  'payload', ARGV[4],
  'deliveries', ARGV[5],
  'first_seen', ARGV[6],
  'last_error', ARGV[7])
if ARGV[8] ~= '' then
  redis.call('HINCRBY', KEYS[3], ARGV[8], 1)
  local mapping = redis.call('HGET', KEYS[4], ARGV[9])
  if mapping == ARGV[8] .. '|' .. ARGV[10] then
    redis.call('HDEL', KEYS[4], ARGV[9])
    redis.call('HINCRBY', KEYS[5], ARGV[8], -1)
    redis.call('ZREM', KEYS[6], ARGV[10])
  end
end
redis.call('XACK', KEYS[2], ARGV[1], ARGV[2])
return new_id
`)

var publishScript = redis.NewScript(`
local existing = redis.call('GET', KEYS[2])
if existing then
  return existing
end
local id = redis.call('XADD', KEYS[1], '*',
  'batch_id', ARGV[1],
  'payload', ARGV[2],
  'deliveries', ARGV[3],
  'first_seen', ARGV[4],
  'last_error', ARGV[5])
redis.call('SET', KEYS[2], id, 'EX', ARGV[6])
if ARGV[7] ~= '' then
  redis.call('HSET', KEYS[3], ARGV[9] .. '|' .. id, ARGV[7] .. '|' .. ARGV[9] .. '|' .. ARGV[1])
  redis.call('HINCRBY', KEYS[4], ARGV[7], 1)
  redis.call('ZADD', KEYS[5], ARGV[8], ARGV[9] .. '|' .. ARGV[1])
end
return id
`)

var ackIndexedScript = redis.NewScript(`
local mapping = redis.call('HGET', KEYS[2], ARGV[3])
local acked = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acked > 0 and mapping == ARGV[4] then
  redis.call('HDEL', KEYS[2], ARGV[3])
  redis.call('HINCRBY', KEYS[3], ARGV[5], -1)
  redis.call('ZREM', KEYS[4], ARGV[6])
end
return 1
`)

var scheduleRetryScript = redis.NewScript(`
local pending = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
if #pending == 0 then
  return 0
end
local record = cjson.encode({
  batch_id=ARGV[4], payload=ARGV[5], deliveries=ARGV[6],
  first_seen=ARGV[7], last_error=ARGV[8], pending_mapping=redis.call('HGET', KEYS[4], ARGV[10])
})
redis.call('HDEL', KEYS[4], ARGV[10])
redis.call('HSET', KEYS[3], ARGV[3], record)
redis.call('ZADD', KEYS[2], ARGV[9], ARGV[3])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
return 1
`)

var promoteRetryScript = redis.NewScript(`
local members = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
local promoted = 0
for _, member in ipairs(members) do
  local raw = redis.call('HGET', KEYS[3], member)
  if raw then
    local record = cjson.decode(raw)
    local new_id = redis.call('XADD', KEYS[1], '*',
      'batch_id', record.batch_id, 'payload', record.payload,
      'deliveries', record.deliveries, 'first_seen', record.first_seen,
      'last_error', record.last_error)
    if record.pending_mapping then
      redis.call('HSET', KEYS[4], ARGV[3] .. '|' .. new_id, record.pending_mapping)
    end
    promoted = promoted + 1
  end
  redis.call('HDEL', KEYS[3], member)
  redis.call('ZREM', KEYS[2], member)
end
return promoted
`)

type Queue struct {
	client              redis.UniversalClient
	deadStream          string
	publishDedupTTL     time.Duration
	deadLetterScanLimit int
	overviewStreams     []string
	overviewGroup       string
}

func WithPublishDedupTTL(ttl time.Duration) Option {
	return func(queue *Queue) {
		if ttl > 0 {
			queue.publishDedupTTL = ttl
		}
	}
}

type Option func(*Queue)

func WithDeadStream(stream string) Option {
	return func(queue *Queue) {
		if stream != "" {
			queue.deadStream = stream
		}
	}
}
func WithDeadLetterScanLimit(limit int) Option {
	return func(queue *Queue) {
		if limit > 0 && limit <= 10000 {
			queue.deadLetterScanLimit = limit
		}
	}
}
func WithOverviewStreams(streams []string, group string) Option {
	return func(q *Queue) { q.overviewStreams = append([]string(nil), streams...); q.overviewGroup = group }
}

func New(client redis.UniversalClient, options ...Option) *Queue {
	queue := &Queue{client: client, deadStream: ports.DeadLetterStream, publishDedupTTL: 72 * time.Hour, deadLetterScanLimit: 1000}
	for _, option := range options {
		option(queue)
	}
	return queue
}

func (q *Queue) PublishBatch(ctx context.Context, stream string, batch ports.BatchEnvelope) (string, error) {
	if err := validatePublish(stream, batch); err != nil {
		return "", err
	}
	if _, err := requireRedisHashTag(stream); err != nil {
		return "", err
	}
	createdAt := batch.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	dedupTTLSeconds := int64(q.publishDedupTTL / time.Second)
	if dedupTTLSeconds < 1 {
		dedupTTLSeconds = 1
	}
	values := messageValues(batch.BatchID, batch.Payload, 1, createdAt, "")
	projectID := payloadProjectID(batch.Payload)
	id, err := publishScript.Run(ctx, q.client, []string{stream, publishDedupKey(stream, batch.BatchID), q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(projectID)}, values[fieldBatchID], values[fieldPayload], values[fieldDeliveries], values[fieldFirstSeen], values[fieldLastError], dedupTTLSeconds, projectID, createdAt.UnixMilli(), stream).Text()
	if err != nil {
		return "", fmt.Errorf("publish Redis Stream %q: %w", stream, err)
	}
	return id, nil
}

func publishDedupKey(stream, batchID string) string {
	sum := sha256.Sum256([]byte(stream + "\x00" + batchID))
	tag, _ := redisHashTag(stream)
	return "queue:{" + tag + "}:publish:" + hex.EncodeToString(sum[:])
}

func (q *Queue) Consume(ctx context.Context, stream, group, consumer string, count int, block time.Duration) ([]ports.Message, error) {
	if err := validateConsumer(stream, group, consumer, count); err != nil {
		return nil, err
	}
	if err := validateRedisDuration("block", block); err != nil {
		return nil, err
	}
	if err := q.ensureGroup(ctx, stream, group); err != nil {
		return nil, err
	}
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: consumer, Streams: []string{stream, ">"}, Count: int64(count), Block: block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return []ports.Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Redis Stream %q: %w", stream, err)
	}
	messages, err := decodeStreams(streams)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (q *Queue) Ack(ctx context.Context, stream, group string, ids ...string) error {
	if stream == "" || group == "" {
		return errors.New("stream and group are required")
	}
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		field := stream + "|" + id
		mapping, err := q.client.HGet(ctx, q.pendingIndexKey(), field).Result()
		if err == redis.Nil {
			if err := q.client.XAck(ctx, stream, group, id).Err(); err != nil {
				return fmt.Errorf("ack Redis Stream %q: %w", stream, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		parts := strings.SplitN(mapping, "|", 2)
		if len(parts) != 2 {
			return errors.New("invalid Redis pending index")
		}
		if _, err := ackIndexedScript.Run(ctx, q.client, []string{stream, q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(parts[0])}, group, id, field, mapping, parts[0], parts[1]).Result(); err != nil {
			return fmt.Errorf("ack indexed Redis Stream %q: %w", stream, err)
		}
	}
	return nil
}

func (q *Queue) Retry(ctx context.Context, stream, group string, msg ports.Message, cause error, delay time.Duration) error {
	if err := validateMove(stream, group, msg); err != nil {
		return err
	}
	if delay <= 0 {
		return errors.New("retry delay must be positive")
	}
	if err := validateRedisDuration("retry delay", delay); err != nil {
		return err
	}
	if _, err := requireRedisHashTag(stream); err != nil {
		return err
	}
	firstSeen, err := q.messageFirstSeen(ctx, stream, msg.ID, false)
	if err != nil {
		return err
	}
	batchID, payload := messageBody(msg)
	member := group + ":" + msg.ID
	due := time.Now().Add(delay).UnixMilli()
	_, err = scheduleRetryScript.Run(ctx, q.client, []string{stream, retryScheduleKey(stream), retryDataKey(stream), q.pendingIndexKey()},
		group, msg.ID, member, batchID, string(payload), max(msg.Deliveries+1, 2), firstSeen, errorText(cause), due, stream+"|"+msg.ID).Result()
	if err != nil {
		return fmt.Errorf("schedule Redis Stream message %q retry: %w", msg.ID, err)
	}
	return nil
}

func (q *Queue) PromoteDue(ctx context.Context, stream string, count int) (int, error) {
	if stream == "" || count < 1 {
		return 0, errors.New("stream is required and count must be positive")
	}
	if _, err := requireRedisHashTag(stream); err != nil {
		return 0, err
	}
	result, err := promoteRetryScript.Run(ctx, q.client, []string{stream, retryScheduleKey(stream), retryDataKey(stream), q.pendingIndexKey()}, time.Now().UnixMilli(), count, stream).Int()
	if err != nil {
		return 0, fmt.Errorf("promote due Redis Stream retries for %q: %w", stream, err)
	}
	return result, nil
}

func retryScheduleKey(stream string) string { return stream + ":retry:schedule" }
func retryDataKey(stream string) string     { return stream + ":retry:data" }

func (q *Queue) MoveToDLQ(ctx context.Context, sourceStream, group string, msg ports.Message, cause error) error {
	if err := validateMove(sourceStream, group, msg); err != nil {
		return err
	}
	if err := validateSameRedisHashTag(sourceStream, q.deadStream); err != nil {
		return err
	}
	firstSeen, err := q.messageFirstSeen(ctx, sourceStream, msg.ID, msg.Malformed != nil)
	if err != nil {
		return err
	}
	batchID, payload := messageBody(msg)
	projectID := payloadProjectID(payload)
	_, err = moveScript.Run(ctx, q.client, []string{q.deadStream, sourceStream, q.deadLetterCountKey(), q.pendingIndexKey(), q.pendingCountKey(), q.pendingAgeKey(projectID)},
		group, msg.ID, batchID, string(payload), max(msg.Deliveries, 1), firstSeen, errorText(cause), projectID, sourceStream+"|"+msg.ID, sourceStream+"|"+batchID,
	).Result()
	if err != nil {
		return fmt.Errorf("move Redis Stream message %q to DLQ: %w", msg.ID, err)
	}
	return nil
}

func payloadProjectID(payload []byte) string {
	var envelope struct {
		ProjectID uint64 `json:"project_id"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.ProjectID == 0 {
		return ""
	}
	return strconv.FormatUint(envelope.ProjectID, 10)
}
func (q *Queue) deadLetterCountKey() string {
	tag, _ := redisHashTag(q.deadStream)
	return "queue:{" + tag + "}:dead:project-counts"
}
func (q *Queue) pendingIndexKey() string {
	tag, _ := redisHashTag(q.deadStream)
	return "queue:{" + tag + "}:pending:index"
}
func (q *Queue) pendingCountKey() string {
	tag, _ := redisHashTag(q.deadStream)
	return "queue:{" + tag + "}:pending:counts"
}
func (q *Queue) pendingAgeKey(projectID string) string {
	tag, _ := redisHashTag(q.deadStream)
	return "queue:{" + tag + "}:pending:age:" + projectID
}

var redisStreamIDPattern = regexp.MustCompile(`^[0-9]+-[0-9]+$`)

// ListDeadLetters exposes operational metadata only. The queued payload is
// decoded solely to enforce project isolation and is never returned.
func (q *Queue) ListDeadLetters(ctx context.Context, f statistics.DeadLetterFilter) (statistics.DeadLetterPage, error) {
	if q == nil || q.client == nil {
		return statistics.DeadLetterPage{}, errors.New("Redis queue client is required")
	}
	if f.ProjectID == 0 || f.Limit < 1 || f.Limit > 100 || (f.Cursor != "" && !redisStreamIDPattern.MatchString(f.Cursor)) {
		return statistics.DeadLetterPage{}, fmt.Errorf("%w: dead-letter cursor or limit", statistics.ErrInvalidQuery)
	}
	end := "+"
	if f.Cursor != "" {
		end = "(" + f.Cursor
	}
	items := make([]statistics.DeadLetter, 0, f.Limit)
	scanned, stopped, lastScanned := 0, false, ""
	for len(items) < f.Limit && scanned < q.deadLetterScanLimit {
		batchSize := min(100, q.deadLetterScanLimit-scanned)
		messages, err := q.client.XRevRangeN(ctx, q.deadStream, end, "-", int64(batchSize)).Result()
		if err != nil && err != redis.Nil {
			return statistics.DeadLetterPage{}, fmt.Errorf("read dead letters: %w", err)
		}
		if len(messages) == 0 {
			break
		}
		for _, message := range messages {
			scanned++
			lastScanned = message.ID
			end = "(" + message.ID
			payload := valueString(message.Values[fieldPayload])
			var envelope struct {
				ProjectID uint64 `json:"project_id"`
			}
			if json.Unmarshal([]byte(payload), &envelope) != nil || envelope.ProjectID != f.ProjectID {
				if scanned == q.deadLetterScanLimit {
					stopped = true
					break
				}
				continue
			}
			deliveries, _ := strconv.Atoi(valueString(message.Values[fieldDeliveries]))
			firstSeenMillis, _ := strconv.ParseInt(valueString(message.Values[fieldFirstSeen]), 10, 64)
			items = append(items, statistics.DeadLetter{ID: message.ID, BatchID: valueString(message.Values[fieldBatchID]), Deliveries: deliveries, FirstSeen: time.UnixMilli(firstSeenMillis).UTC(), LastError: sanitizeQueueError(valueString(message.Values[fieldLastError]))})
			if len(items) == f.Limit || scanned == q.deadLetterScanLimit {
				stopped = true
				break
			}
		}
		if len(messages) < batchSize {
			break
		}
	}
	page := statistics.DeadLetterPage{Items: items}
	if stopped && lastScanned != "" {
		page.NextCursor = lastScanned
	}
	return page, nil
}

func sanitizeQueueError(value string) string {
	if value == "" {
		return ""
	}
	return "processing failed"
}

func (q *Queue) ReclaimStale(ctx context.Context, stream, group, consumer string, idle time.Duration, count int) ([]ports.Message, error) {
	if err := validateConsumer(stream, group, consumer, count); err != nil {
		return nil, err
	}
	if err := validateRedisDuration("idle", idle); err != nil {
		return nil, err
	}
	if err := q.ensureGroup(ctx, stream, group); err != nil {
		return nil, err
	}
	messages, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: stream, Group: group, Consumer: consumer, MinIdle: idle, Start: "0-0", Count: int64(count),
	}).Result()
	if errors.Is(err, redis.Nil) {
		return []ports.Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reclaim Redis Stream %q: %w", stream, err)
	}
	decoded, err := decodeMessages(stream, messages)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func (q *Queue) ensureGroup(ctx context.Context, stream, group string) error {
	err := q.client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create Redis Stream group %q on %q: %w", group, stream, err)
	}
	return nil
}

func (q *Queue) pendingDeliveries(ctx context.Context, stream, group string, messages []ports.Message) ([]ports.Message, error) {
	for i := range messages {
		pending, err := q.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: stream, Group: group, Start: messages[i].ID, End: messages[i].ID, Count: 1,
		}).Result()
		if err != nil {
			return nil, fmt.Errorf("read pending metadata for %q: %w", messages[i].ID, err)
		}
		if len(pending) == 1 && pending[0].ID == messages[i].ID {
			messages[i].Deliveries = max(messages[i].Deliveries, int(pending[0].RetryCount))
		}
	}
	return messages, nil
}

func (q *Queue) messageFirstSeen(ctx context.Context, stream, id string, allowFallback bool) (int64, error) {
	messages, err := q.client.XRangeN(ctx, stream, id, id, 1).Result()
	if err != nil {
		return 0, fmt.Errorf("read first-seen metadata for %q: %w", id, err)
	}
	if len(messages) != 1 {
		return 0, fmt.Errorf("read first-seen metadata for %q: message not found", id)
	}
	value := valueString(messages[0].Values[fieldFirstSeen])
	firstSeen, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		if allowFallback {
			return time.Now().UTC().UnixMilli(), nil
		}
		return 0, fmt.Errorf("read first-seen metadata for %q: invalid value", id)
	}
	return firstSeen, nil
}

func decodeStreams(streams []redis.XStream) ([]ports.Message, error) {
	var result []ports.Message
	for _, stream := range streams {
		messages, err := decodeMessages(stream.Stream, stream.Messages)
		if err != nil {
			return nil, err
		}
		result = append(result, messages...)
	}
	return result, nil
}

func decodeMessages(stream string, messages []redis.XMessage) ([]ports.Message, error) {
	result := make([]ports.Message, 0, len(messages))
	for _, raw := range messages {
		message := decodeMessage(stream, raw)
		result = append(result, message)
	}
	return result, nil
}

func decodeMessage(stream string, raw redis.XMessage) ports.Message {
	batchID := valueString(raw.Values[fieldBatchID])
	payload := valueString(raw.Values[fieldPayload])
	issues := make([]string, 0, 3)
	if batchID == "" {
		issues = append(issues, `field "batch_id" is required`)
	}
	if payload == "" {
		issues = append(issues, `field "payload" is required`)
	}
	deliveries := 1
	if value := valueString(raw.Values[fieldDeliveries]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			issues = append(issues, "invalid deliveries")
		} else {
			deliveries = parsed
		}
	}
	message := ports.Message{
		ID: raw.ID, Stream: stream, BatchID: batchID, Payload: []byte(payload), Deliveries: deliveries,
		LastError: valueString(raw.Values[fieldLastError]),
	}
	if len(issues) > 0 {
		rawEnvelope, _ := json.Marshal(raw.Values)
		message.Malformed = &ports.MalformedMessage{Reason: strings.Join(issues, "; "), RawEnvelope: rawEnvelope}
	}
	return message
}

func messageValues(batchID string, payload []byte, deliveries int, firstSeen time.Time, lastError string) map[string]any {
	return map[string]any{
		fieldBatchID: batchID, fieldPayload: string(payload), fieldDeliveries: deliveries,
		fieldFirstSeen: firstSeen.UTC().UnixMilli(), fieldLastError: lastError,
	}
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func validatePublish(stream string, batch ports.BatchEnvelope) error {
	if stream == "" {
		return errors.New("stream is required")
	}
	if batch.BatchID == "" {
		return errors.New("batch ID is required")
	}
	if len(batch.Payload) == 0 {
		return errors.New("payload is required")
	}
	return nil
}

func validateConsumer(stream, group, consumer string, count int) error {
	if stream == "" || group == "" || consumer == "" {
		return errors.New("stream, group, and consumer are required")
	}
	if count <= 0 {
		return errors.New("count must be positive")
	}
	return nil
}

func validateMove(stream, group string, msg ports.Message) error {
	if stream == "" || group == "" || msg.ID == "" || (msg.Malformed == nil && (msg.BatchID == "" || len(msg.Payload) == 0)) {
		return errors.New("stream, group, message ID, batch ID, and payload are required")
	}
	return nil
}

func messageBody(msg ports.Message) (string, []byte) {
	if msg.Malformed == nil {
		return msg.BatchID, msg.Payload
	}
	batchID := msg.BatchID
	if batchID == "" {
		batchID = "malformed:" + msg.ID
	}
	payload := msg.Payload
	if len(msg.Malformed.RawEnvelope) > 0 {
		payload = msg.Malformed.RawEnvelope
	}
	return batchID, payload
}

func redisHashTag(key string) (string, bool) {
	start := strings.IndexByte(key, '{')
	if start < 0 {
		return "", false
	}
	end := strings.IndexByte(key[start+1:], '}')
	if end <= 0 {
		return "", false
	}
	return key[start+1 : start+1+end], true
}

func requireRedisHashTag(key string) (string, error) {
	tag, ok := redisHashTag(key)
	if !ok {
		return "", fmt.Errorf("Redis key %q must contain a nonempty Redis hash tag", key)
	}
	return tag, nil
}

func validateSameRedisHashTag(keys ...string) error {
	var common string
	for _, key := range keys {
		tag, err := requireRedisHashTag(key)
		if err != nil {
			return err
		}
		if common == "" {
			common = tag
		} else if tag != common {
			return fmt.Errorf("Redis keys must use the same Redis hash tag: %q does not match", key)
		}
	}
	return nil
}

func validateRedisDuration(name string, duration time.Duration) error {
	if duration < 0 {
		return fmt.Errorf("%s duration must not be negative", name)
	}
	if duration > 0 && duration < time.Millisecond {
		return fmt.Errorf("%s duration must be zero or at least 1ms", name)
	}
	return nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
