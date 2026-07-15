package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/stretchr/testify/require"
)

func TestStatisticsControllerParsesRFC3339AndAllowsViewer(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	store := &httpHistory{summary: ports.Summary{PV: 4, UV: 2}}
	svc := statistics.NewService(httpAuth{project: 9}, nil, store, nil, nil, func() time.Time { return now })
	mux := http.NewServeMux()
	NewStatisticsController(svc).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statistics/summary?project_id=9&start=2026-07-15T10:00:00Z&end=2026-07-15T11:00:00Z&granularity=hour", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, auth.Principal{UserID: 3}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.JSONEq(t, `{"pv":4,"uv":2,"approximate":false}`, res.Body.String())
	require.Equal(t, "hour", store.last.Granularity)
}

func TestStatisticsControllerMapsValidationAndForbidden(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	svc := statistics.NewService(httpAuth{project: 9}, nil, &httpHistory{}, nil, nil, func() time.Time { return now })
	mux := http.NewServeMux()
	NewStatisticsController(svc).Register(mux)
	for _, tc := range []struct {
		url    string
		status int
	}{
		{"/api/v1/statistics/summary?project_id=9&start=bad&end=2026-07-15T11:00:00Z", 400},
		{"/api/v1/statistics/summary?project_id=10&start=2026-07-15T10:00:00Z&end=2026-07-15T11:00:00Z", 403},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil).WithContext(context.WithValue(context.Background(), principalKey{}, auth.Principal{UserID: 3}))
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		require.Equal(t, tc.status, res.Code, res.Body.String())
	}
}

func TestStatisticsControllerMapsOversizedFilterTo413(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	svc := statistics.NewService(httpAuth{project: 9}, nil, &httpHistory{}, nil, nil, func() time.Time { return now })
	mux := http.NewServeMux()
	NewStatisticsController(svc).Register(mux)
	url := "/api/v1/statistics/summary?project_id=9&start=2026-07-15T10:00:00Z&end=2026-07-15T11:00:00Z&point_code=" + strings.Repeat("x", 129)
	req := httptest.NewRequest(http.MethodGet, url, nil).WithContext(context.WithValue(context.Background(), principalKey{}, auth.Principal{UserID: 3}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, res.Code, res.Body.String())
}

func TestStatisticsControllerUnexpectedFailureHasRequestIDAndNoBackendDetail(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	svc := statistics.NewService(httpAuth{project: 9}, nil, &httpHistory{err: errors.New("clickhouse secret host")}, nil, nil, func() time.Time { return now })
	mux := http.NewServeMux()
	NewStatisticsController(svc).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statistics/summary?project_id=9&start=2026-07-15T10:00:00Z&end=2026-07-15T11:00:00Z", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, auth.Principal{UserID: 3}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	require.Equal(t, 500, res.Code)
	require.Contains(t, res.Body.String(), "req-123")
	require.NotContains(t, res.Body.String(), "secret")
}

func TestDiagnosticsControllerIsReadOnlyAndReturnsSanitizedShape(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	diagnostics := &httpDiagnostics{recent: statistics.EventPage{Items: []statistics.Event{{EventID: "evt", PointCode: "view", Properties: map[string]any{"product_id": "p"}}}}}
	svc := statistics.NewService(httpAuth{project: 9}, nil, nil, nil, diagnostics, func() time.Time { return now })
	mux := http.NewServeMux()
	NewDiagnosticsController(svc).Register(mux)
	url := "/api/v1/diagnostics/recent-events?project_id=9&start=2026-07-15T10:00:00Z&end=2026-07-15T11:00:00Z"
	req := httptest.NewRequest(http.MethodGet, url, nil).WithContext(context.WithValue(context.Background(), principalKey{}, auth.Principal{UserID: 3}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	require.Equal(t, 200, res.Code, res.Body.String())
	require.NotContains(t, res.Body.String(), "visitor_hash")
	require.Contains(t, res.Body.String(), "product_id")

	post := httptest.NewRequest(http.MethodPost, "/api/v1/diagnostics/dead-letters/replay", nil).WithContext(req.Context())
	postRes := httptest.NewRecorder()
	mux.ServeHTTP(postRes, post)
	require.Equal(t, http.StatusNotFound, postRes.Code)
}

type httpAuth struct{ project uint64 }

func (a httpAuth) RequireRole(_ context.Context, _, projectID uint64, roles ...project.Role) error {
	if projectID != a.project {
		return project.ErrForbidden
	}
	return nil
}
func (a httpAuth) ProjectTimezone(context.Context, uint64, uint64) (string, error) { return "UTC", nil }

type httpHistory struct {
	last    ports.StatisticsFilter
	summary ports.Summary
	err     error
}

type httpDiagnostics struct{ recent statistics.EventPage }

func (h *httpDiagnostics) ListRecent(context.Context, statistics.DiagnosticFilter) (statistics.EventPage, error) {
	return h.recent, nil
}
func (h *httpDiagnostics) ListInvalid(context.Context, statistics.DiagnosticFilter) (statistics.InvalidEventPage, error) {
	return statistics.InvalidEventPage{}, nil
}
func (h *httpDiagnostics) ListDeadLetters(context.Context, statistics.DeadLetterFilter) (statistics.DeadLetterPage, error) {
	return statistics.DeadLetterPage{}, nil
}

func (h *httpHistory) QuerySummary(_ context.Context, f ports.StatisticsFilter) (ports.Summary, error) {
	h.last = f
	return h.summary, h.err
}
func (h *httpHistory) QueryTrend(context.Context, ports.StatisticsFilter) ([]ports.TrendPoint, error) {
	return []ports.TrendPoint{}, h.err
}
