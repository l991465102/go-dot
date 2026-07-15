package statistics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
)

type Aggregator struct {
	store       ports.ProjectStatisticsStore
	projects    ports.StatisticsProjectSource
	now         func() time.Time
	mu          sync.Mutex
	lastVersion uint64
}

func NewAggregator(store ports.ProjectStatisticsStore, projects ports.StatisticsProjectSource, now func() time.Time) *Aggregator {
	if now == nil {
		now = time.Now
	}
	return &Aggregator{store: store, projects: projects, now: now}
}

func ProjectHourRange(at time.Time, location *time.Location) (time.Time, time.Time) {
	local := at.In(location)
	start := local.Add(-time.Duration(local.Minute())*time.Minute - time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
	return start.UTC(), start.Add(time.Hour).UTC()
}

func (a *Aggregator) RefreshCurrentAndPrevious(ctx context.Context) error {
	if a == nil || a.store == nil || a.projects == nil {
		return errors.New("statistics store and project source are required")
	}
	projects, err := a.projects.ListStatisticsProjects(ctx)
	if err != nil {
		return fmt.Errorf("list statistics projects: %w", err)
	}
	now := a.now()
	var refreshErrors []error
	for _, project := range projects {
		loc, err := time.LoadLocation(project.Timezone)
		if err != nil {
			refreshErrors = append(refreshErrors, fmt.Errorf("load project %d timezone %q: %w", project.ID, project.Timezone, err))
			continue
		}
		current, _ := ProjectHourRange(now, loc)
		for _, hour := range []time.Time{current, current.Add(-time.Hour)} {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := a.store.RefreshProjectHour(ctx, project.ID, hour, a.nextVersion()); err != nil {
				refreshErrors = append(refreshErrors, fmt.Errorf("refresh project %d hour %s: %w", project.ID, hour.Format(time.RFC3339), err))
			}
		}
	}
	return errors.Join(refreshErrors...)
}

func (a *Aggregator) nextVersion() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := uint64(a.now().UTC().UnixNano())
	if v <= a.lastVersion {
		v = a.lastVersion + 1
	}
	a.lastVersion = v
	return v
}
