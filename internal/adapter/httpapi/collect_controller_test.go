package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	collectv1 "github.com/lzw7758/go-dot/api/collect/v1"
	"github.com/lzw7758/go-dot/internal/adapter/httpapi"
	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/project"
)

func TestCollectHTTPValidAndPartialBatchReturnAccepted(t *testing.T) {
	collector := &collectServiceFake{result: ingestion.CollectResult{BatchID: "b_1", Accepted: 1, Rejected: 1, Results: []ingestion.EventResult{{Index: 0, EventID: "a", Status: "accepted"}, {Index: 1, EventID: "b", Status: "rejected", Code: "PROPERTY_ENUM_INVALID"}}}}
	handler := collectHandler(collector, &limitFake{allowed: true})
	response := collectRequest(t, handler, []byte(`{"project_id":999,"events":[{"event_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","point_code":"product_view","event_time":1784073600123,"anonymous_id":"d","platform":"web"}]}`), "write-key", "https://app.example")
	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, uint64(42), collector.project.ID)
	require.Equal(t, uint64(999), collector.request.ProjectID)
	var result collectv1.EventsRes
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, 1, result.Accepted)
	require.Equal(t, 1, result.Rejected)
}

func TestCollectHTTPRejectsInvalidKeyAndOrigin(t *testing.T) {
	collector := &collectServiceFake{}
	keyRepo := &keyRepoFake{err: project.ErrProjectNotFound}
	handler := httpapi.NewCollectHandler(httpapi.NewCollectController(collector, 256<<10), httpapi.NewWriteKeyMiddleware(keyRepo, time.Now), httpapi.NewRateLimitMiddleware(&limitFake{allowed: true}))
	response := collectRequest(t, handler, []byte(`{"events":[{}]}`), "bad", "")
	require.Equal(t, http.StatusUnauthorized, response.Code)

	handler = collectHandler(collector, &limitFake{allowed: true})
	response = collectRequest(t, handler, []byte(`{"events":[{}]}`), "write-key", "https://evil.example")
	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestCollectHTTPAllowsPreflightButEnforcesOriginOnPost(t *testing.T) {
	handler := collectHandler(&collectServiceFake{}, &limitFake{allowed: true})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/collect/events", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, "https://app.example", response.Header().Get("Access-Control-Allow-Origin"))
}

func TestCollectHTTPRejectsOversizedBodyBeforeDecode(t *testing.T) {
	handler := collectHandler(&collectServiceFake{}, &limitFake{allowed: true})
	response := collectRequest(t, handler, bytes.Repeat([]byte("x"), (257<<10)), "write-key", "")
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestCollectHTTPReturnsRetryAfterWhenRateLimited(t *testing.T) {
	handler := collectHandler(&collectServiceFake{}, &limitFake{allowed: false, retry: 3 * time.Second})
	response := collectRequest(t, handler, []byte(`{"events":[{}]}`), "write-key", "")
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Equal(t, "3", response.Header().Get("Retry-After"))
}

func TestCollectHTTPDoesNotLeakInfrastructureErrors(t *testing.T) {
	collector := &collectServiceFake{err: errors.New("redis secret-host connection refused")}
	handler := collectHandler(collector, &limitFake{allowed: true})
	response := collectRequest(t, handler, []byte(`{"events":[{}]}`), "write-key", "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), "secret-host")
}

func TestRedisTokenBucketExhaustsAtomically(t *testing.T) {
	client := &scriptedRateRedis{results: [][]any{{int64(1), "0"}, {int64(0), "3.5"}}}
	bucket := httpapi.NewRedisTokenBucket(client, 1, 0.01)
	allowed, _, err := bucket.Allow(context.Background(), "collect:rate:1:ip:device")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, retry, err := bucket.Allow(context.Background(), "collect:rate:1:ip:device")
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, 3500*time.Millisecond, retry)
	require.Contains(t, client.script, "redis.call('TIME')")
	require.Contains(t, client.script, "HMGET")
}

func TestRedisTokenBucketAgainstInMemoryRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	bucket := httpapi.NewRedisTokenBucket(client, 1, 0.01)
	allowed, _, err := bucket.Allow(context.Background(), "collect:rate:1:ip:device")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, retry, err := bucket.Allow(context.Background(), "collect:rate:1:ip:device")
	require.NoError(t, err)
	require.False(t, allowed)
	require.Greater(t, retry, time.Second)
}

func collectHandler(collector *collectServiceFake, limiter *limitFake) http.Handler {
	key := httpapi.HashWriteKey("write-key")
	repo := &keyRepoFake{expectedHash: key, project: project.Project{ID: 42, Active: true, AllowedOrigins: []string{"https://app.example"}, Timezone: "Asia/Shanghai"}}
	return httpapi.NewCollectHandler(httpapi.NewCollectController(collector, 256<<10), httpapi.NewWriteKeyMiddleware(repo, time.Now), httpapi.NewRateLimitMiddleware(limiter))
}

func collectRequest(t *testing.T, handler http.Handler, body []byte, key, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/collect/events", bytes.NewReader(body))
	req.Header.Set("X-Write-Key", key)
	req.Header.Set("X-Device-ID", "device-1")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

type collectServiceFake struct {
	result  ingestion.CollectResult
	err     error
	project project.Project
	request ingestion.CollectRequest
}

func (f *collectServiceFake) Collect(_ context.Context, p project.Project, request ingestion.CollectRequest) (ingestion.CollectResult, error) {
	f.project, f.request = p, request
	return f.result, f.err
}

type keyRepoFake struct {
	expectedHash string
	project      project.Project
	err          error
}

func (f *keyRepoFake) FindProjectByWriteKeyHash(_ context.Context, hash string, _ time.Time) (project.Project, error) {
	if f.err != nil {
		return project.Project{}, f.err
	}
	if f.expectedHash != "" && hash != f.expectedHash {
		return project.Project{}, project.ErrProjectNotFound
	}
	return f.project, nil
}

type limitFake struct {
	allowed bool
	retry   time.Duration
	err     error
	key     string
}

func (f *limitFake) Allow(_ context.Context, key string) (bool, time.Duration, error) {
	f.key = key
	return f.allowed, f.retry, f.err
}

type scriptedRateRedis struct {
	results [][]any
	script  string
}

func (f *scriptedRateRedis) Eval(ctx context.Context, script string, _ []string, _ ...interface{}) *redis.Cmd {
	f.script = script
	cmd := redis.NewCmd(ctx)
	cmd.SetVal(f.results[0])
	f.results = f.results[1:]
	return cmd
}
func (f *scriptedRateRedis) EvalSha(ctx context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
	cmd := redis.NewCmd(ctx)
	cmd.SetErr(redis.ErrNoScript)
	return cmd
}
func (f *scriptedRateRedis) EvalRO(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	return f.Eval(ctx, script, keys, args...)
}
func (f *scriptedRateRedis) EvalShaRO(ctx context.Context, sha string, keys []string, args ...interface{}) *redis.Cmd {
	return f.EvalSha(ctx, sha, keys, args...)
}
func (f *scriptedRateRedis) ScriptExists(ctx context.Context, _ ...string) *redis.BoolSliceCmd {
	cmd := redis.NewBoolSliceCmd(ctx)
	cmd.SetVal([]bool{false})
	return cmd
}
func (f *scriptedRateRedis) ScriptLoad(ctx context.Context, _ string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx)
	cmd.SetVal("sha")
	return cmd
}
