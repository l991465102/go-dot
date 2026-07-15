package overview

import (
	"context"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
)

const WindowDuration = 24 * time.Hour

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}
type QualityStore interface {
	InvalidCount(context.Context, uint64, Window) (int64, error)
	ActivePointIDs(context.Context, uint64, Window) (map[uint64]bool, error)
}
type QueueStore interface {
	Snapshot(context.Context, uint64) (QueueSnapshot, error)
}
type PointStore interface {
	PublishedPoints(context.Context, uint64) ([]PublishedPoint, error)
}

type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type QueueSnapshot struct {
	Pending          int64         `json:"pending"`
	OldestPendingAge time.Duration `json:"-"`
	DeadLetters      int64         `json:"-"`
}
type PublishedPoint struct {
	ID          uint64    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
}
type Result struct {
	Window          Window
	InvalidCount    int64
	DeadLetterCount int64
	Queue           QueueSnapshot
	NoReportPoints  []PublishedPoint
}

type Service struct {
	auth    Authorizer
	quality QualityStore
	queue   QueueStore
	points  PointStore
	now     func() time.Time
}

func NewService(a Authorizer, q QualityStore, queue QueueStore, p PointStore, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{a, q, queue, p, now}
}
func (s *Service) Get(ctx context.Context, actor, projectID uint64) (Result, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return Result{}, err
	}
	end := s.now().UTC()
	window := Window{Start: end.Add(-WindowDuration), End: end}
	invalid, err := s.quality.InvalidCount(ctx, projectID, window)
	if err != nil {
		return Result{}, err
	}
	queue, err := s.queue.Snapshot(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	points, err := s.points.PublishedPoints(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	active, err := s.quality.ActivePointIDs(ctx, projectID, window)
	if err != nil {
		return Result{}, err
	}
	silent := make([]PublishedPoint, 0)
	for _, point := range points {
		if !active[point.ID] {
			silent = append(silent, point)
		}
	}
	return Result{Window: window, InvalidCount: invalid, DeadLetterCount: queue.DeadLetters, Queue: queue, NoReportPoints: silent}, nil
}
