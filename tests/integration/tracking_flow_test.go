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
	"github.com/xuri/excelize/v2"
)

const (
	xlsxContentType            = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	stableDateQueryMaxAttempts = 3
)

type acceptancePoint struct {
	ID     uint64 `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	UVMode string `json:"uv_mode"`
}

type savedReport struct {
	ID          uint64            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Points      []acceptancePoint `json:"points"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type dailyItem struct {
	Date      string `json:"date"`
	Complete  bool   `json:"complete"`
	PointID   uint64 `json:"point_id"`
	PointCode string `json:"point_code"`
	PointName string `json:"point_name"`
	PV        uint64 `json:"pv"`
	UV        uint64 `json:"uv"`
	UVMode    string `json:"uv_mode"`
}

type dailyResult struct {
	Timezone   string      `json:"timezone"`
	StartDate  string      `json:"start_date"`
	EndDate    string      `json:"end_date"`
	PointCount int         `json:"point_count"`
	DayCount   int         `json:"day_count"`
	TotalPV    uint64      `json:"total_pv"`
	Items      []dailyItem `json:"items"`
}

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
	viewerUsername := os.Getenv("GO_DOT_INTEGRATION_VIEWER_USERNAME")
	viewerPassword := os.Getenv("GO_DOT_INTEGRATION_VIEWER_PASSWORD")
	require.NotEmpty(t, viewerUsername, "configured integration requires the existing bootstrap Viewer username")
	require.NotEmpty(t, viewerPassword, "configured integration requires the existing bootstrap Viewer password")

	client := &http.Client{Timeout: 15 * time.Second}
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
	postJSON(t, client, base+"/api/v1/projects", session.AccessToken, map[string]any{"code": code, "name": "Integration", "allowed_origins": []string{"http://localhost:5173"}, "timezone": "Asia/Shanghai"}, http.StatusCreated, &creation)
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
	exactPoint := createPoint(t, client, base, session.AccessToken, creation.Project.ID, scene, "product_detail_view", "-商品详情浏览", "EXACT")
	approximatePoint := createPoint(t, client, base, session.AccessToken, creation.Project.ID, scene, "checkout_submit", "@结算提交", "APPROXIMATE")
	if fixtureFile := os.Getenv("GO_DOT_INTEGRATION_FIXTURE_FILE"); fixtureFile != "" {
		fixture := fmt.Sprintf("GO_DOT_FAULT_WRITE_KEY=%q\nGO_DOT_FAULT_PROJECT_ID=%d\nGO_DOT_FAULT_POINT_ID=%d\n", creation.WriteKey, creation.Project.ID, exactPoint.ID)
		require.NoError(t, os.WriteFile(fixtureFile, []byte(fixture), 0o600))
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	reportedAt := time.Now().UTC()
	historicalAt := reportedAt.In(location).AddDate(0, 0, -1).UTC()
	events := []map[string]any{
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0001", "point_code": "product_detail_view", "event_time": reportedAt.UnixMilli(), "anonymous_id": "exact-visitor", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0002", "point_code": "product_detail_view", "event_time": reportedAt.UnixMilli(), "anonymous_id": "exact-visitor", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0003", "point_code": "checkout_submit", "event_time": reportedAt.UnixMilli(), "anonymous_id": "approximate-a", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0004", "point_code": "checkout_submit", "event_time": reportedAt.UnixMilli(), "anonymous_id": "approximate-b", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0005", "point_code": "checkout_submit", "event_time": reportedAt.UnixMilli(), "anonymous_id": "approximate-b", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0006", "point_code": "product_detail_view", "event_time": historicalAt.UnixMilli(), "anonymous_id": "exact-visitor", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0007", "point_code": "product_detail_view", "event_time": historicalAt.UnixMilli(), "anonymous_id": "exact-visitor", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0008", "point_code": "checkout_submit", "event_time": historicalAt.UnixMilli(), "anonymous_id": "approximate-a", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0009", "point_code": "checkout_submit", "event_time": historicalAt.UnixMilli(), "anonymous_id": "approximate-b", "platform": "web", "properties": map[string]any{}},
		{"event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0010", "point_code": "checkout_submit", "event_time": historicalAt.UnixMilli(), "anonymous_id": "approximate-b", "platform": "web", "properties": map[string]any{}},
	}
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("GO_DOT_REDIS_TEST_ADDRESS")})
	defer rdb.Close()
	eventStream := envOr("GO_DOT_INTEGRATION_EVENT_STREAM", "tracking:{queue}:events")
	consumerGroup := envOr("GO_DOT_INTEGRATION_CONSUMER_GROUP", "tracking-workers")
	for i := 0; i < 2; i++ {
		collect(t, client, base, creation.WriteKey, map[string]any{"events": events})
		waitForQueueDrain(t, rdb, eventStream, consumerGroup)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ch, err := sql.Open("clickhouse", os.Getenv("GO_DOT_CLICKHOUSE_TEST_DSN"))
	require.NoError(t, err)
	defer ch.Close()
	aggregateStart := historicalAt.Truncate(time.Hour)
	aggregateEnd := reportedAt.Truncate(time.Hour).Add(time.Hour)
	var lastDiagnostic string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var rows uint64
		err := ch.QueryRowContext(ctx, "SELECT count() FROM tracking_event FINAL WHERE project_id = ?", creation.Project.ID).Scan(&rows)
		require.NoError(c, err)
		require.Equal(c, uint64(10), rows)
		pvKeys, err := scanKeys(ctx, rdb, fmt.Sprintf("rt:pv:hour:{project:%d}:*:%d:*", creation.Project.ID, exactPoint.ID))
		require.NoError(c, err)
		var pv int64
		for _, key := range pvKeys {
			n, e := rdb.Get(ctx, key).Int64()
			require.NoError(c, e)
			pv += n
		}
		require.Equal(c, int64(4), pv)
		uvKeys, err := scanKeys(ctx, rdb, fmt.Sprintf("rt:uv:hour:{project:%d}:*:%d:*:EXACT", creation.Project.ID, exactPoint.ID))
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
		err = ch.QueryRowContext(ctx, `SELECT count() FROM business_stat_hour_generation FINAL WHERE project_id = ? AND hour >= ? AND hour < ?`, creation.Project.ID, aggregateStart, aggregateEnd).Scan(&generations)
		require.NoError(c, err)
		require.GreaterOrEqual(c, generations, uint64(2), "scheduler has not committed generations covering both Shanghai dates")
		var exactPV, exactUV, approximatePV, approximateUV uint64
		err = ch.QueryRowContext(ctx, `
SELECT coalesce(sum(s.pv), 0), uniqExactMerge(s.uv_state)
FROM business_stat_exact_hourly AS s
INNER JOIN business_stat_hour_generation AS g FINAL
  ON s.project_id = g.project_id AND s.hour = g.hour AND s.version = g.generation
WHERE s.project_id = ? AND s.point_id = ? AND s.hour >= ? AND s.hour < ?`, creation.Project.ID, exactPoint.ID, aggregateStart, aggregateEnd).Scan(&exactPV, &exactUV)
		require.NoError(c, err)
		err = ch.QueryRowContext(ctx, `
SELECT coalesce(sum(s.pv), 0), uniqCombined64Merge(s.uv_state)
FROM business_stat_hourly AS s
INNER JOIN business_stat_hour_generation AS g FINAL
  ON s.project_id = g.project_id AND s.hour = g.hour AND s.version = g.generation
WHERE s.project_id = ? AND s.point_id = ? AND s.hour >= ? AND s.hour < ?`, creation.Project.ID, approximatePoint.ID, aggregateStart, aggregateEnd).Scan(&approximatePV, &approximateUV)
		require.NoError(c, err)
		lastDiagnostic = fmt.Sprintf("raw rows=%d redis exact pv=%d uv=%d generations=%d exact pv=%d uv=%d approximate pv=%d uv=%d", rows, pv, len(members), generations, exactPV, exactUV, approximatePV, approximateUV)
		require.Equal(c, uint64(4), exactPV, lastDiagnostic)
		require.Equal(c, uint64(1), exactUV, lastDiagnostic)
		require.Equal(c, uint64(6), approximatePV, lastDiagnostic)
		require.Equal(c, uint64(2), approximateUV, lastDiagnostic)
	}, 90*time.Second, time.Second)
	t.Log("integration convergence: " + lastDiagnostic)

	startDate, endDate := acceptanceDates(reportedAt, location)
	pointIDs := []uint64{exactPoint.ID, approximatePoint.ID}
	var saved savedReport
	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/reports", base, creation.Project.ID), session.AccessToken, map[string]any{
		"name": "=1+1 集成验收日报", "description": "+SUM(A1:A2)", "default_days": 2, "point_ids": pointIDs,
	}, http.StatusCreated, &saved)
	require.Equal(t, "=1+1 集成验收日报", saved.Name)
	var persisted savedReport
	getJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/reports/%d", base, creation.Project.ID, saved.ID), session.AccessToken, &persisted)
	require.Len(t, persisted.Points, 2)
	require.Equal(t, []uint64{exactPoint.ID, approximatePoint.ID}, []uint64{persisted.Points[0].ID, persisted.Points[1].ID})
	require.Equal(t, []string{"EXACT", "APPROXIMATE"}, []string{persisted.Points[0].UVMode, persisted.Points[1].UVMode})
	persistedPointIDs := []uint64{persisted.Points[0].ID, persisted.Points[1].ID}

	query := map[string]any{"project_id": creation.Project.ID, "start_date": startDate, "end_date": endDate, "point_ids": persistedPointIDs}
	result, adminQueryCurrentDate, err := queryDailyOnStableShanghaiDate(location, time.Now, stableDateQueryMaxAttempts, func(result *dailyResult) error {
		postJSON(t, client, base+"/api/v1/statistics/daily-by-point/query", session.AccessToken, query, http.StatusOK, result)
		return nil
	})
	require.NoError(t, err)
	assertDailyMatrix(t, result, startDate, endDate, adminQueryCurrentDate, exactPoint, approximatePoint)

	exportInput := map[string]any{"project_id": creation.Project.ID, "start_date": startDate, "end_date": endDate, "point_ids": persistedPointIDs, "report_name": persisted.Name, "report_description": persisted.Description}
	workbookBytes := request(t, client, http.MethodPost, base+"/api/v1/statistics/daily-by-point/export", session.AccessToken, exportInput, http.StatusOK, xlsxContentType)
	assertWorkbook(t, workbookBytes, result, persisted.Name, persisted.Description, "admin")

	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/members", base, creation.Project.ID), session.AccessToken, map[string]any{"username": viewerUsername, "role": "VIEWER"}, http.StatusCreated, nil)
	var viewerSession struct {
		AccessToken string `json:"access_token"`
	}
	postJSON(t, client, base+"/api/v1/auth/login", "", map[string]any{"username": viewerUsername, "password": viewerPassword}, http.StatusOK, &viewerSession)
	var viewerPersisted savedReport
	getJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/reports/%d", base, creation.Project.ID, saved.ID), viewerSession.AccessToken, &viewerPersisted)
	require.Equal(t, persisted, viewerPersisted)
	viewerResult, viewerQueryCurrentDate, err := queryDailyOnStableShanghaiDate(location, time.Now, stableDateQueryMaxAttempts, func(result *dailyResult) error {
		postJSON(t, client, base+"/api/v1/statistics/daily-by-point/query", viewerSession.AccessToken, query, http.StatusOK, result)
		return nil
	})
	require.NoError(t, err)
	assertDailyMatrix(t, viewerResult, startDate, endDate, viewerQueryCurrentDate, exactPoint, approximatePoint)
	viewerWorkbook := request(t, client, http.MethodPost, base+"/api/v1/statistics/daily-by-point/export", viewerSession.AccessToken, exportInput, http.StatusOK, xlsxContentType)
	assertWorkbook(t, viewerWorkbook, viewerResult, persisted.Name, persisted.Description, viewerUsername)
	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/reports", base, creation.Project.ID), viewerSession.AccessToken, map[string]any{
		"name": "Viewer 不可保存", "description": "forbidden", "default_days": 2, "point_ids": pointIDs,
	}, http.StatusForbidden, nil)
	request(t, client, http.MethodPatch, fmt.Sprintf("%s/api/v1/projects/%d/reports/%d", base, creation.Project.ID, saved.ID), viewerSession.AccessToken, map[string]any{
		"name": persisted.Name, "description": persisted.Description, "default_days": 2, "point_ids": persistedPointIDs, "expected_updated_at": persisted.UpdatedAt,
	}, http.StatusForbidden, "")
	request(t, client, http.MethodDelete, fmt.Sprintf("%s/api/v1/projects/%d/reports/%d", base, creation.Project.ID, saved.ID), viewerSession.AccessToken, nil, http.StatusForbidden, "")
}

func postJSON(t *testing.T, client *http.Client, url, token string, body any, status int, target any) {
	t.Helper()
	payload := request(t, client, http.MethodPost, url, token, body, status, "")
	if target != nil {
		require.NoError(t, json.Unmarshal(payload, target), string(payload))
	}
}

func getJSON(t *testing.T, client *http.Client, url, token string, target any) {
	t.Helper()
	payload := request(t, client, http.MethodGet, url, token, nil, http.StatusOK, "application/json")
	require.NoError(t, json.Unmarshal(payload, target), string(payload))
}

func request(t *testing.T, client *http.Client, method, url, token string, body any, status int, contentType string) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, status, res.StatusCode, string(payload))
	if contentType != "" {
		require.Contains(t, res.Header.Get("Content-Type"), contentType)
	}
	return payload
}

func createPoint(t *testing.T, client *http.Client, base, token string, projectID, categoryID uint64, code, name, uvMode string) acceptancePoint {
	t.Helper()
	var point acceptancePoint
	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/points", base, projectID), token, map[string]any{
		"category_id": categoryID, "code": code, "name": name, "behavior_type": "PAGE_VIEW", "development_state": "ONLINE",
		"trigger_timing": "集成验收动作完成后一次", "trigger_count_rule": "每次完成计数一次", "count_pv": true, "count_uv": true,
		"uv_mode": uvMode, "time_basis": "CLIENT", "supported_platforms": []string{"web"}, "supported_channels": []string{},
		"product_owner": "integration", "properties": []any{},
	}, http.StatusCreated, &point)
	postJSON(t, client, fmt.Sprintf("%s/api/v1/projects/%d/points/%d/publish", base, projectID, point.ID), token, map[string]any{}, http.StatusOK, nil)
	point.Code, point.Name, point.UVMode = code, name, uvMode
	return point
}

func acceptanceDates(now time.Time, location *time.Location) (string, string) {
	end := now.In(location)
	return end.AddDate(0, 0, -1).Format(time.DateOnly), end.Format(time.DateOnly)
}

func assertDailyMatrix(t *testing.T, result dailyResult, startDate, endDate, currentDate string, exact, approximate acceptancePoint) {
	t.Helper()
	require.Equal(t, "Asia/Shanghai", result.Timezone)
	require.Equal(t, startDate, result.StartDate)
	require.Equal(t, endDate, result.EndDate)
	require.Equal(t, 2, result.PointCount)
	require.Equal(t, 2, result.DayCount)
	require.Equal(t, uint64(10), result.TotalPV)
	require.Len(t, result.Items, 4)

	items := make(map[string]dailyItem, len(result.Items))
	for _, item := range result.Items {
		items[fmt.Sprintf("%s/%d", item.Date, item.PointID)] = item
	}
	for _, date := range []string{startDate, endDate} {
		for _, point := range []acceptancePoint{exact, approximate} {
			item, ok := items[fmt.Sprintf("%s/%d", date, point.ID)]
			require.True(t, ok, "missing matrix row for %s/%d", date, point.ID)
			require.Equal(t, expectedDateComplete(date, currentDate), item.Complete, "unexpected completeness for %s", date)
			require.Equal(t, point.Code, item.PointCode)
			require.Equal(t, point.Name, item.PointName)
			require.Equal(t, point.UVMode, item.UVMode)
			if point.ID == exact.ID {
				require.Equal(t, uint64(2), item.PV)
				require.Equal(t, uint64(1), item.UV)
			} else {
				require.Equal(t, uint64(3), item.PV)
				require.Equal(t, uint64(2), item.UV)
			}
		}
	}
}

func expectedDateComplete(date, currentDate string) bool { return date != currentDate }

func queryDailyOnStableShanghaiDate(location *time.Location, now func() time.Time, maxAttempts int, query func(*dailyResult) error) (dailyResult, string, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		before := now().In(location).Format(time.DateOnly)
		var result dailyResult
		if err := query(&result); err != nil {
			return dailyResult{}, "", err
		}
		after := now().In(location).Format(time.DateOnly)
		if before == after {
			return result, after, nil
		}
	}
	return dailyResult{}, "", fmt.Errorf("query crossed the Asia/Shanghai date boundary %d times", maxAttempts)
}

func assertWorkbook(t *testing.T, contents []byte, result dailyResult, reportName, reportDescription, exportedBy string) {
	t.Helper()
	book, err := excelize.OpenReader(bytes.NewReader(contents))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, book.Close()) })
	require.Equal(t, []string{"数据明细", "报表说明"}, book.GetSheetList())
	rows, err := book.GetRows("数据明细")
	require.NoError(t, err)
	require.Len(t, rows, len(result.Items)+1)
	require.Equal(t, []string{"日期", "数据状态", "点位编码", "点位名称", "PV", "UV", "UV口径"}, rows[0])
	for index, item := range result.Items {
		row := rows[index+1]
		require.GreaterOrEqual(t, len(row), 7)
		require.Equal(t, item.Date, row[0])
		expectedStatus := "未完成"
		if item.Complete {
			expectedStatus = "完整"
		}
		require.Equal(t, expectedStatus, row[1])
		require.Equal(t, spreadsheetText(item.PointCode), row[2])
		require.Equal(t, spreadsheetText(item.PointName), row[3])
		require.Equal(t, strconv.FormatUint(item.PV, 10), row[4])
		require.Equal(t, strconv.FormatUint(item.UV, 10), row[5])
		expectedMode := "近似"
		if item.UVMode == "EXACT" {
			expectedMode = "精确"
		}
		require.Equal(t, expectedMode, row[6])
	}
	explanationRows, err := book.GetRows("报表说明")
	require.NoError(t, err)
	explanation := make(map[string]string, len(explanationRows))
	for _, row := range explanationRows {
		if len(row) >= 2 {
			explanation[row[0]] = row[1]
		}
	}
	require.Equal(t, spreadsheetText(reportName), explanation["报表名称"])
	require.Equal(t, spreadsheetText(reportDescription), explanation["报表说明"])
	require.Equal(t, result.StartDate+" 至 "+result.EndDate, explanation["查询范围"])
	require.Equal(t, "Asia/Shanghai", explanation["统计时区"])
	require.Equal(t, exportedBy, explanation["导出用户"])
	for _, target := range []struct{ sheet, cell string }{
		{sheet: "报表说明", cell: "B2"}, {sheet: "报表说明", cell: "B3"},
	} {
		assertTextCellHasNoFormula(t, book, target.sheet, target.cell)
	}
	for row := 2; row <= len(result.Items)+1; row++ {
		assertTextCellHasNoFormula(t, book, "数据明细", fmt.Sprintf("D%d", row))
	}
}

func spreadsheetText(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func assertTextCellHasNoFormula(t *testing.T, book *excelize.File, sheet, cell string) {
	t.Helper()
	formula, err := book.GetCellFormula(sheet, cell)
	require.NoError(t, err)
	require.Empty(t, formula)
	cellType, err := book.GetCellType(sheet, cell)
	require.NoError(t, err)
	require.Contains(t, []excelize.CellType{excelize.CellTypeInlineString, excelize.CellTypeSharedString}, cellType)
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

func waitForQueueDrain(t *testing.T, client *redis.Client, stream, group string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		groups, err := client.XInfoGroups(ctx, stream).Result()
		require.NoError(c, err)
		for _, current := range groups {
			if current.Name == group {
				require.Zero(c, current.Pending, "consumer group still has pending messages")
				require.Zero(c, current.Lag, "consumer group still has undelivered messages")
				return
			}
		}
		require.Fail(c, "consumer group not found", "%s on %s", group, stream)
	}, 30*time.Second, 200*time.Millisecond)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func TestAcceptanceDatesUsesTwoShanghaiCivilDays(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start, end := acceptanceDates(time.Date(2026, 7, 20, 16, 30, 0, 0, time.UTC), location)
	require.Equal(t, "2026-07-20", start)
	require.Equal(t, "2026-07-21", end)
}

func TestExpectedDateCompleteUsesShanghaiDateCapturedAtQueryResponse(t *testing.T) {
	tests := []struct {
		name        string
		date        string
		currentDate string
		complete    bool
	}{
		{name: "current date remains incomplete", date: "2026-07-21", currentDate: "2026-07-21", complete: false},
		{name: "previous date is complete", date: "2026-07-20", currentDate: "2026-07-21", complete: true},
		{name: "both requested dates may be historical after midnight", date: "2026-07-19", currentDate: "2026-07-21", complete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.complete, expectedDateComplete(tt.date, tt.currentDate))
		})
	}
}

func TestQueryDailyOnStableShanghaiDateRetriesOneMidnightCrossing(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	times := []time.Time{
		time.Date(2026, 7, 20, 15, 59, 59, 0, time.UTC),
		time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 20, 16, 0, 1, 0, time.UTC),
		time.Date(2026, 7, 20, 16, 0, 2, 0, time.UTC),
	}
	clockCalls := 0
	requestCalls := 0

	result, currentDate, err := queryDailyOnStableShanghaiDate(location, func() time.Time {
		current := times[clockCalls]
		clockCalls++
		return current
	}, 3, func(result *dailyResult) error {
		requestCalls++
		result.TotalPV = uint64(requestCalls)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 2, requestCalls)
	require.Equal(t, uint64(2), result.TotalPV, "must return the stable retry response")
	require.Equal(t, "2026-07-21", currentDate)
}
