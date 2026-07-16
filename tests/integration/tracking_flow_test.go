//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrackingFlow is intentionally opt-in. With GO_DOT_INTEGRATION_BASE_URL set,
// an unavailable or dirty stack is a failure rather than a skip. Run it against
// empty Compose volumes so the initial admin password is deterministic.
func TestTrackingFlow(t *testing.T) {
	base := strings.TrimRight(os.Getenv("GO_DOT_INTEGRATION_BASE_URL"), "/")
	if base == "" {
		t.Skip("GO_DOT_INTEGRATION_BASE_URL is not set")
	}
	require.NotEmpty(t, os.Getenv("GO_DOT_REDIS_TEST_ADDRESS"), "configured integration requires Redis")
	require.NotEmpty(t, os.Getenv("GO_DOT_CLICKHOUSE_TEST_DSN"), "configured integration requires ClickHouse")

	client := &http.Client{Timeout: 5 * time.Second}
	var session struct {
		AccessToken string `json:"access_token"`
	}
	postJSON(t, client, base+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin"}, http.StatusOK, &session)
	postJSON(t, client, base+"/api/v1/auth/change-password", session.AccessToken, map[string]any{"current_password": "admin", "new_password": "Admin-Changed-2026!"}, http.StatusNoContent, nil)

	code := "integration_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var creation struct {
		Project struct {
			ID uint64 `json:"id"`
		} `json:"project"`
		WriteKey string `json:"write_key"`
	}
	postJSON(t, client, base+"/api/v1/projects", session.AccessToken, map[string]any{"code": code, "name": "Integration", "allowed_origins": []string{"http://localhost:5173"}, "timezone": "UTC"}, http.StatusCreated, &creation)
	require.NotEmpty(t, creation.WriteKey)

	category := func(level string, parent any) uint64 {
		body := map[string]any{"code": strings.ToLower(level) + "_" + code, "name": level, "level": level, "sort_order": 1}
		if parent != nil {
			body["parent_id"] = parent
		}
		var result struct {
			ID uint64 `json:"id"`
		}
		postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/categories", base, creation.Project.ID), session.AccessToken, body, http.StatusCreated, &result)
		return result.ID
	}
	domain := category("DOMAIN", nil)
	module := category("MODULE", domain)
	scene := category("SCENE", module)
	var point struct {
		ID uint64 `json:"id"`
	}
	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/points", base, creation.Project.ID), session.AccessToken, map[string]any{
		"category_id": scene, "code": "product_detail_view", "name": "商品详情浏览", "behavior_type": "PAGE_VIEW", "development_state": "ONLINE",
		"trigger_timing": "详情主体加载完成后一次", "trigger_count_rule": "页面生命周期一次", "count_pv": true, "count_uv": true,
		"uv_mode": "EXACT", "time_basis": "SERVER", "supported_platforms": []string{"web"}, "supported_channels": []string{},
		"product_owner": "integration", "properties": []any{},
	}, http.StatusCreated, &point)
	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/points/%d/publish", base, creation.Project.ID, point.ID), session.AccessToken, map[string]any{}, http.StatusOK, nil)
	if fixtureFile := os.Getenv("GO_DOT_INTEGRATION_FIXTURE_FILE"); fixtureFile != "" {
		fixture := fmt.Sprintf("GO_DOT_FAULT_WRITE_KEY=%q\nGO_DOT_FAULT_PROJECT_ID=%d\nGO_DOT_FAULT_POINT_ID=%d\n", creation.WriteKey, creation.Project.ID, point.ID)
		require.NoError(t, os.WriteFile(fixtureFile, []byte(fixture), 0o600))
	}

	events := []map[string]any{
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0001", "point_code": "product_detail_view", "event_time": time.Now().UnixMilli(), "anonymous_id": "integration-visitor", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0002", "point_code": "product_detail_view", "event_time": time.Now().UnixMilli(), "anonymous_id": "integration-visitor", "platform": "web", "properties": map[string]any{}},
	}
	for i := 0; i < 2; i++ {
		collect(t, client, base, creation.WriteKey, map[string]any{"events": events})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("GO_DOT_REDIS_TEST_ADDRESS")})
	defer rdb.Close()
	ch, err := sql.Open("clickhouse", os.Getenv("GO_DOT_CLICKHOUSE_TEST_DSN"))
	require.NoError(t, err)
	defer ch.Close()
	var lastDiagnostic string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var rows, uv uint64
		err := ch.QueryRowContext(ctx, "SELECT count(), uniqExact(visitor_hash) FROM tracking_event FINAL WHERE project_id = ?", creation.Project.ID).Scan(&rows, &uv)
		require.NoError(c, err)
		require.Equal(c, uint64(2), rows)
		require.Equal(c, uint64(1), uv)
		pvKeys, err := scanKeys(ctx, rdb, fmt.Sprintf("rt:pv:hour:{project:%d}:*:%d:*", creation.Project.ID, point.ID))
		require.NoError(c, err)
		var pv int64
		for _, key := range pvKeys {
			n, e := rdb.Get(ctx, key).Int64()
			require.NoError(c, e)
			pv += n
		}
		require.Equal(c, int64(2), pv)
		uvKeys, err := scanKeys(ctx, rdb, fmt.Sprintf("rt:uv:hour:{project:%d}:*:%d:*:EXACT", creation.Project.ID, point.ID))
		require.NoError(c, err)
		members := map[string]struct{}{}
		for _, key := range uvKeys {
			values, e := rdb.SMembers(ctx, key).Result()
			require.NoError(c, e)
			for _, value := range values {
				members[value] = struct{}{}
			}
		}
		require.Len(c, members, 1)

		var generations uint64
		err = ch.QueryRowContext(ctx, `SELECT count() FROM business_stat_hour_generation FINAL WHERE project_id = ? AND hour >= now() - INTERVAL 2 HOUR`, creation.Project.ID).Scan(&generations)
		require.NoError(c, err)
		require.Greater(c, generations, uint64(0), "scheduler has not committed an hourly generation")
		var aggregatePV, aggregateUV uint64
		err = ch.QueryRowContext(ctx, `
SELECT coalesce(sum(s.pv), 0), uniqExactMerge(s.uv_state)
FROM business_stat_exact_hourly AS s
INNER JOIN business_stat_hour_generation AS g FINAL
  ON s.project_id = g.project_id AND s.hour = g.hour AND s.version = g.generation
WHERE s.project_id = ? AND s.point_id = ? AND s.hour >= now() - INTERVAL 2 HOUR`, creation.Project.ID, point.ID).Scan(&aggregatePV, &aggregateUV)
		require.NoError(c, err)
		lastDiagnostic = fmt.Sprintf("raw rows=%d uv=%d redis pv=%d uv=%d generations=%d aggregate pv=%d uv=%d", rows, uv, pv, len(members), generations, aggregatePV, aggregateUV)
		require.Equal(c, uint64(2), aggregatePV, lastDiagnostic)
		require.Equal(c, uint64(1), aggregateUV, lastDiagnostic)
	}, 90*time.Second, time.Second)
	t.Log("integration convergence: " + lastDiagnostic)
}

func postJSON(t *testing.T, client *http.Client, url, token string, body any, status int, target any) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	payload, _ := io.ReadAll(res.Body)
	require.Equal(t, status, res.StatusCode, string(payload))
	if target != nil {
		require.NoError(t, json.Unmarshal(payload, target), string(payload))
	}
}
func collect(t *testing.T, client *http.Client, base, key string, body any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/collect/events", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Write-Key", key)
	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	payload, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusAccepted, res.StatusCode, string(payload))
}
func scanKeys(ctx context.Context, client *redis.Client, pattern string) ([]string, error) {
	var cursor uint64
	var out []string
	for {
		keys, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		out = append(out, keys...)
		cursor = next
		if cursor == 0 {
			return out, nil
		}
	}
}
