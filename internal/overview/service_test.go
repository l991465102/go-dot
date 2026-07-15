package overview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/stretchr/testify/require"
)

func TestServiceReturnsExplicit24HourProjectOverview(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	auth := &fakeAuth{}
	quality := &fakeQuality{invalid: 7, active: map[uint64]bool{11: true}}
	queue := &fakeQueue{snapshot: QueueSnapshot{Pending: 3, OldestPendingAge: 90 * time.Second, DeadLetters: 2}}
	points := &fakePoints{items: []PublishedPoint{{ID: 11, Code: "reported", Name: "Reported"}, {ID: 12, Code: "silent", Name: "Silent"}}}
	svc := NewService(auth, quality, queue, points, func() time.Time { return now })

	got, err := svc.Get(context.Background(), 5, 9)
	require.NoError(t, err)
	require.Equal(t, now.Add(-24*time.Hour), got.Window.Start)
	require.Equal(t, now, got.Window.End)
	require.EqualValues(t, 7, got.InvalidCount)
	require.EqualValues(t, 2, got.DeadLetterCount)
	require.EqualValues(t, 3, got.Queue.Pending)
	require.Equal(t, 90*time.Second, got.Queue.OldestPendingAge)
	require.Equal(t, []PublishedPoint{{ID: 12, Code: "silent", Name: "Silent"}}, got.NoReportPoints)
	require.Equal(t, got.Window, quality.window)
	require.EqualValues(t, 9, quality.projectID)
	require.EqualValues(t, 9, queue.projectID)
	require.Equal(t, []project.Role{project.Admin, project.Viewer}, auth.roles)
}

func TestServiceRejectsCrossProjectBeforeReadingMetrics(t *testing.T) {
	auth := &fakeAuth{err: project.ErrForbidden}
	quality, queue, points := &fakeQuality{}, &fakeQueue{}, &fakePoints{}
	_, err := NewService(auth, quality, queue, points, time.Now).Get(context.Background(), 5, 10)
	require.ErrorIs(t, err, project.ErrForbidden)
	require.Zero(t, quality.calls+queue.calls+points.calls)
}

func TestServicePropagatesInfrastructureFailure(t *testing.T) {
	want := errors.New("redis down")
	_, err := NewService(&fakeAuth{}, &fakeQuality{}, &fakeQueue{err: want}, &fakePoints{}, time.Now).Get(context.Background(), 5, 9)
	require.ErrorIs(t, err, want)
}

type fakeAuth struct {
	err   error
	roles []project.Role
}

func (f *fakeAuth) RequireRole(_ context.Context, _, _ uint64, roles ...project.Role) error {
	f.roles = roles
	return f.err
}

type fakeQuality struct {
	invalid   int64
	active    map[uint64]bool
	window    Window
	projectID uint64
	calls     int
}

func (f *fakeQuality) InvalidCount(_ context.Context, projectID uint64, window Window) (int64, error) {
	f.calls++
	f.projectID = projectID
	f.window = window
	return f.invalid, nil
}
func (f *fakeQuality) ActivePointIDs(_ context.Context, _ uint64, _ Window) (map[uint64]bool, error) {
	return f.active, nil
}

type fakeQueue struct {
	snapshot  QueueSnapshot
	projectID uint64
	calls     int
	err       error
}

func (f *fakeQueue) Snapshot(_ context.Context, projectID uint64) (QueueSnapshot, error) {
	f.calls++
	f.projectID = projectID
	return f.snapshot, f.err
}

type fakePoints struct {
	items []PublishedPoint
	calls int
}

func (f *fakePoints) PublishedPoints(context.Context, uint64) ([]PublishedPoint, error) {
	f.calls++
	return f.items, nil
}
