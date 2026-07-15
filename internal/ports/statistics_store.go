package ports

import (
	"context"
	"time"
)

type StatisticsFilter struct {
	ProjectID        uint64
	Start            time.Time
	End              time.Time
	DomainCategoryID uint64
	ModuleCategoryID uint64
	SceneCategoryID  uint64
	PointID          uint64
	PointCode        string
	Platform         string
	Channel          string
	Granularity      string
	Timezone         string
	ExactUV          bool
}

type Summary struct {
	PV          uint64 `json:"pv"`
	UV          uint64 `json:"uv"`
	Approximate bool   `json:"approximate"`
}
type TrendPoint struct {
	Period      time.Time `json:"period"`
	PV          uint64    `json:"pv"`
	UV          uint64    `json:"uv"`
	Approximate bool      `json:"approximate"`
}

type StatisticsStore interface {
	RefreshHour(ctx context.Context, hour time.Time, version uint64) error
	QuerySummary(ctx context.Context, filter StatisticsFilter) (Summary, error)
	QueryTrend(ctx context.Context, filter StatisticsFilter) ([]TrendPoint, error)
}

type ProjectStatisticsStore interface {
	StatisticsStore
	RefreshProjectHour(ctx context.Context, projectID uint64, hour time.Time, version uint64) error
}

type StatisticsProject struct {
	ID       uint64
	Timezone string
}
type StatisticsProjectSource interface {
	ListStatisticsProjects(context.Context) ([]StatisticsProject, error)
}
