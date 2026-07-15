package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/stretchr/testify/require"
)

func TestOverviewControllerReturnsProjectScopedOperationalDTO(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	svc := overview.NewService(httpOverviewAuth{project: 9}, httpOverviewQuality{}, httpOverviewQueue{}, httpOverviewPoints{}, func() time.Time { return now })
	mux := http.NewServeMux()
	NewOverviewController(svc).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/9/overview", nil).WithContext(context.WithValue(context.Background(), principalKey{}, auth.Principal{UserID: 3}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	require.Equal(t, 200, res.Code, res.Body.String())
	require.JSONEq(t, `{"window":{"start":"2026-07-14T12:00:00Z","end":"2026-07-15T12:00:00Z"},"invalid_count":4,"dead_letter_count":2,"queue":{"pending":3,"oldest_pending_age_seconds":90},"no_report_points":[{"id":12,"code":"silent","name":"Silent","published_at":"0001-01-01T00:00:00Z"}],"no_report_threshold_hours":24}`, res.Body.String())
}

func TestOverviewControllerForbidsCrossProject(t *testing.T) {
	svc := overview.NewService(httpOverviewAuth{project: 9}, httpOverviewQuality{}, httpOverviewQueue{}, httpOverviewPoints{}, time.Now)
	mux := http.NewServeMux()
	NewOverviewController(svc).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/10/overview", nil).WithContext(context.WithValue(context.Background(), principalKey{}, auth.Principal{UserID: 3}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	require.Equal(t, 403, res.Code)
}

type httpOverviewAuth struct{ project uint64 }

func (a httpOverviewAuth) RequireRole(_ context.Context, _, p uint64, _ ...project.Role) error {
	if p != a.project {
		return project.ErrForbidden
	}
	return nil
}

type httpOverviewQuality struct{}

func (httpOverviewQuality) InvalidCount(context.Context, uint64, overview.Window) (int64, error) {
	return 4, nil
}
func (httpOverviewQuality) ActivePointIDs(context.Context, uint64, overview.Window) (map[uint64]bool, error) {
	return map[uint64]bool{}, nil
}

type httpOverviewQueue struct{}

func (httpOverviewQueue) Snapshot(context.Context, uint64) (overview.QueueSnapshot, error) {
	return overview.QueueSnapshot{Pending: 3, OldestPendingAge: 90 * time.Second, DeadLetters: 2}, nil
}

type httpOverviewPoints struct{}

func (httpOverviewPoints) PublishedPoints(context.Context, uint64) ([]overview.PublishedPoint, error) {
	return []overview.PublishedPoint{{ID: 12, Code: "silent", Name: "Silent"}}, nil
}
