package statistics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestProjectHourRangeHonorsDSTFallback(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	firstOne := time.Date(2026, 11, 1, 1, 30, 0, 0, loc)
	start, end := ProjectHourRange(firstOne, loc)
	require.Equal(t, time.Hour, end.Sub(start))
	require.Equal(t, 1, start.In(loc).Hour())
}

func TestAggregateRefreshesCurrentAndPreviousProjectLocalHours(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 35, 0, 0, time.UTC)
	store := &recordingStatisticsStore{}
	agg := NewAggregator(store, fakeProjects{{ID: 7, Timezone: "Asia/Kathmandu"}}, func() time.Time { return now })

	require.NoError(t, agg.RefreshCurrentAndPrevious(context.Background()))
	require.Len(t, store.hours, 2)
	require.Equal(t, time.Date(2026, 7, 15, 0, 15, 0, 0, time.UTC), store.hours[0].Start)
	require.Equal(t, time.Date(2026, 7, 14, 23, 15, 0, 0, time.UTC), store.hours[1].Start)
	require.Greater(t, store.hours[1].Version, store.hours[0].Version)
}

func TestAggregateRefreshesConfiguredLookbackHoursNewestFirst(t *testing.T) {
	now := time.Date(2026, 7, 20, 16, 35, 0, 0, time.UTC)
	store := &recordingStatisticsStore{}
	agg := NewAggregatorWithLookback(store, fakeProjects{{ID: 7, Timezone: "Asia/Shanghai"}}, func() time.Time { return now }, 26)

	require.NoError(t, agg.RefreshCurrentAndPrevious(context.Background()))
	require.Len(t, store.hours, 26)
	require.Equal(t, time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC), store.hours[0].Start)
	require.Equal(t, time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC), store.hours[25].Start)
}

func TestAggregateContinuesOtherHoursAndProjectsAndReturnsJoinedErrors(t *testing.T) {
	store := &recordingStatisticsStore{fail: map[uint64]error{7: errors.New("project seven unavailable")}}
	agg := NewAggregator(store, fakeProjects{{ID: 7, Timezone: "UTC"}, {ID: 8, Timezone: "UTC"}}, func() time.Time { return time.Date(2026, 7, 15, 1, 30, 0, 0, time.UTC) })
	err := agg.RefreshCurrentAndPrevious(context.Background())
	require.ErrorContains(t, err, "project seven unavailable")
	require.Len(t, store.hours, 4)
	require.Equal(t, uint64(8), store.hours[2].ProjectID)
}

type fakeProjects []ports.StatisticsProject

func (f fakeProjects) ListStatisticsProjects(context.Context) ([]ports.StatisticsProject, error) {
	return f, nil
}

type refreshedHour struct {
	ProjectID uint64
	Start     time.Time
	Version   uint64
}
type recordingStatisticsStore struct {
	hours []refreshedHour
	fail  map[uint64]error
}

func (s *recordingStatisticsStore) RefreshHour(context.Context, time.Time, uint64) error { return nil }
func (s *recordingStatisticsStore) RefreshProjectHour(_ context.Context, projectID uint64, hour time.Time, version uint64) error {
	s.hours = append(s.hours, refreshedHour{projectID, hour, version})
	return s.fail[projectID]
}
func (s *recordingStatisticsStore) QuerySummary(context.Context, ports.StatisticsFilter) (ports.Summary, error) {
	return ports.Summary{}, nil
}
func (s *recordingStatisticsStore) QueryTrend(context.Context, ports.StatisticsFilter) ([]ports.TrendPoint, error) {
	return nil, nil
}
