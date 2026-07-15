package statistics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/stretchr/testify/require"
)

func TestStatisticsRoutesRealtimeOnlyToRedisAndHistoryOnlyToClickHouse(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	realtime := &fakeRealtime{summary: ports.Summary{PV: 7, UV: 3}}
	history := &fakeHistory{summary: ports.Summary{PV: 11, UV: 5}}
	svc := NewService(allowViewer{}, realtime, history, fakePoints{"view": {PointID: 1, PointCode: "view", UVMode: tracking.Approximate}}, nil, nowFn(now))
	q := Query{ProjectID: 9, Start: now.Add(-time.Hour), End: now, PointCodes: []string{"view"}, Granularity: "hour", Timezone: "UTC"}

	got, err := svc.Realtime(context.Background(), 4, q)
	require.NoError(t, err)
	require.EqualValues(t, 7, got.PV)
	require.Equal(t, 1, realtime.calls)
	require.Zero(t, history.summaryCalls+history.trendCalls)

	got, err = svc.Summary(context.Background(), 4, q)
	require.NoError(t, err)
	require.EqualValues(t, 11, got.PV)
	_, err = svc.Trend(context.Background(), 4, q)
	require.NoError(t, err)
	require.Equal(t, 1, history.summaryCalls)
	require.Equal(t, 1, history.trendCalls)
	require.Equal(t, 1, realtime.calls)
}

func TestStatisticsExactPointSelectsExactHistory(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	history := &fakeHistory{}
	points := fakePoints{"exact_view": {PointID: 19, PointCode: "exact_view", UVMode: tracking.Exact}}
	svc := NewService(allowViewer{}, &fakeRealtime{}, history, points, nil, nowFn(now))
	_, err := svc.Summary(context.Background(), 4, Query{ProjectID: 9, Start: now.Add(-time.Hour), End: now, PointCodes: []string{"exact_view"}, Timezone: "UTC"})
	require.NoError(t, err)
	require.True(t, history.last.ExactUV)
	require.EqualValues(t, 19, history.last.PointID)
}

func TestStatisticsResolvesEveryRequestedPointID(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	history := &fakeHistory{}
	points := fakePoints{"first": {PointID: 10, PointCode: "first", UVMode: tracking.Approximate}, "second": {PointID: 20, PointCode: "second", UVMode: tracking.Approximate}}
	svc := NewService(allowViewer{}, &fakeRealtime{}, history, points, nil, nowFn(now))
	_, err := svc.Summary(context.Background(), 4, Query{ProjectID: 9, Start: now.Add(-time.Hour), End: now, PointCodes: []string{"first", "second"}, Timezone: "UTC"})
	require.NoError(t, err)
	require.Equal(t, []uint64{10, 20}, history.last.PointIDs)
}

func TestStatisticsRejectsInvalidAndOversizedRanges(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	svc := NewService(allowViewer{}, &fakeRealtime{}, &fakeHistory{}, nil, nil, nowFn(now))
	for _, q := range []Query{
		{ProjectID: 9, Start: now, End: now},
		{ProjectID: 9, Start: now.Add(-91 * 24 * time.Hour), End: now},
		{ProjectID: 9, Start: now.Add(-time.Hour), End: now, Granularity: "week"},
	} {
		_, err := svc.Trend(context.Background(), 4, q)
		require.ErrorIs(t, err, ErrInvalidQuery)
	}
}

func TestStatisticsAllowsViewerAndDeniesCrossProject(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	auth := selectiveAuth{allowedProject: 9}
	svc := NewService(auth, &fakeRealtime{}, &fakeHistory{}, nil, nil, nowFn(now))
	q := Query{ProjectID: 9, Start: now.Add(-time.Hour), End: now, Timezone: "UTC"}
	_, err := svc.Summary(context.Background(), 4, q)
	require.NoError(t, err)
	q.ProjectID = 10
	_, err = svc.Summary(context.Background(), 4, q)
	require.ErrorIs(t, err, project.ErrForbidden)
}

func TestDiagnosticsLimitRecentQueriesToThirtyDays(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	diagnostics := &fakeDiagnostics{}
	svc := NewService(allowViewer{}, nil, nil, nil, diagnostics, nowFn(now))
	_, err := svc.RecentEvents(context.Background(), 4, DiagnosticQuery{ProjectID: 9, Start: now.Add(-31 * 24 * time.Hour), End: now, Limit: 20})
	require.ErrorIs(t, err, ErrInvalidQuery)
	_, err = svc.RecentEvents(context.Background(), 4, DiagnosticQuery{ProjectID: 9, Start: now.Add(-24 * time.Hour), End: now, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 1, diagnostics.recentCalls)
}

type allowViewer struct{}

func (allowViewer) RequireRole(context.Context, uint64, uint64, ...project.Role) error { return nil }

type selectiveAuth struct{ allowedProject uint64 }

func (a selectiveAuth) RequireRole(_ context.Context, _, projectID uint64, _ ...project.Role) error {
	if projectID != a.allowedProject {
		return project.ErrForbidden
	}
	return nil
}

type fakeRealtime struct {
	calls   int
	summary ports.Summary
}

func (f *fakeRealtime) QueryRealtime(_ context.Context, in ports.StatisticsFilter) (ports.Summary, error) {
	f.calls++
	return f.summary, nil
}

type fakeHistory struct {
	summaryCalls, trendCalls int
	last                     ports.StatisticsFilter
	summary                  ports.Summary
}

func (f *fakeHistory) QuerySummary(_ context.Context, in ports.StatisticsFilter) (ports.Summary, error) {
	f.summaryCalls++
	f.last = in
	return f.summary, nil
}
func (f *fakeHistory) QueryTrend(_ context.Context, in ports.StatisticsFilter) ([]ports.TrendPoint, error) {
	f.trendCalls++
	f.last = in
	return []ports.TrendPoint{}, nil
}

type fakePoints map[string]tracking.Snapshot

func (f fakePoints) Get(_ context.Context, _ uint64, code string) (tracking.Snapshot, error) {
	point, ok := f[code]
	if !ok {
		return tracking.Snapshot{}, tracking.ErrPointNotFound
	}
	return point, nil
}

type fakeDiagnostics struct{ recentCalls int }

func (f *fakeDiagnostics) ListRecent(context.Context, DiagnosticFilter) (EventPage, error) {
	f.recentCalls++
	return EventPage{}, nil
}
func (f *fakeDiagnostics) ListInvalid(context.Context, DiagnosticFilter) (InvalidEventPage, error) {
	return InvalidEventPage{}, nil
}
func (f *fakeDiagnostics) ListDeadLetters(context.Context, DeadLetterFilter) (DeadLetterPage, error) {
	return DeadLetterPage{}, nil
}
func nowFn(now time.Time) func() time.Time { return func() time.Time { return now } }

var _ = errors.New
