package reporting

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

var errDailyStoreUnavailable = errors.New("reporting: daily store unavailable")

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}

type Service struct {
	auth     Authorizer
	repo     Repository
	daily    DailyStore
	exporter Exporter
	now      func() time.Time
}

func NewService(auth Authorizer, repo Repository, daily DailyStore, exporter Exporter, now func() time.Time) *Service {
	if auth == nil {
		panic("reporting: nil authorizer")
	}
	if repo == nil {
		panic("reporting: nil repository")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{auth: auth, repo: repo, daily: daily, exporter: exporter, now: now}
}

func (s *Service) ListReports(ctx context.Context, actor, projectID uint64) ([]Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, projectID)
}

func (s *Service) GetReport(ctx context.Context, actor, projectID, reportID uint64) (Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return Report{}, err
	}
	return s.repo.Get(ctx, projectID, reportID)
}

func (s *Service) CreateReport(ctx context.Context, actor, projectID uint64, in ReportInput) (Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return Report{}, err
	}
	points, err := s.validateInput(ctx, projectID, &in, false)
	if err != nil {
		return Report{}, err
	}
	report, err := s.repo.Create(ctx, projectID, actor, in)
	if err != nil {
		return Report{}, err
	}
	report.Points = points
	return report, nil
}

func (s *Service) UpdateReport(ctx context.Context, actor, projectID, reportID uint64, in ReportInput) (Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return Report{}, err
	}
	points, err := s.validateInput(ctx, projectID, &in, true)
	if err != nil {
		return Report{}, err
	}
	report, err := s.repo.Update(ctx, projectID, reportID, actor, in)
	if err != nil {
		return Report{}, err
	}
	report.Points = points
	return report, nil
}

func (s *Service) DeleteReport(ctx context.Context, actor, projectID, reportID uint64) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	return s.repo.Delete(ctx, projectID, reportID)
}

func (s *Service) QueryDaily(ctx context.Context, actor uint64, query DailyQuery) (DailyResult, error) {
	if err := s.auth.RequireRole(ctx, actor, query.ProjectID, project.Admin, project.Viewer); err != nil {
		return DailyResult{}, err
	}
	if s.daily == nil {
		return DailyResult{}, errDailyStoreUnavailable
	}
	if query.ProjectID == 0 {
		return DailyResult{}, ErrInvalid
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return DailyResult{}, err
	}
	start, err := time.ParseInLocation(time.DateOnly, query.StartDate, location)
	if err != nil {
		return DailyResult{}, ErrInvalid
	}
	end, err := time.ParseInLocation(time.DateOnly, query.EndDate, location)
	if err != nil {
		return DailyResult{}, ErrInvalid
	}
	now := s.now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	if end.Before(start) || end.After(today) || end.After(start.AddDate(0, 0, 89)) {
		return DailyResult{}, ErrInvalid
	}
	startYear, startMonth, startDay := start.Date()
	endYear, endMonth, endDay := end.Date()
	civilStart := time.Date(startYear, startMonth, startDay, 0, 0, 0, 0, time.UTC)
	civilEnd := time.Date(endYear, endMonth, endDay, 0, 0, 0, 0, time.UTC)
	dayCount := int(civilEnd.Sub(civilStart)/(24*time.Hour)) + 1

	pointIDs := uniquePointIDs(query.PointIDs)
	if len(pointIDs) < 1 || len(pointIDs) > 100 {
		return DailyResult{}, ErrInvalid
	}
	resolved, err := s.repo.ResolvePoints(ctx, query.ProjectID, pointIDs)
	if err != nil {
		return DailyResult{}, err
	}
	byID := make(map[uint64]PointRef, len(resolved))
	for _, point := range resolved {
		if point.DefinitionState != tracking.Published && point.DefinitionState != tracking.Deprecated {
			return DailyResult{}, ErrInvalid
		}
		if point.UVMode != tracking.Approximate && point.UVMode != tracking.Exact {
			return DailyResult{}, ErrInvalid
		}
		if _, duplicate := byID[point.ID]; duplicate {
			return DailyResult{}, ErrInvalid
		}
		byID[point.ID] = point
	}
	if len(byID) != len(pointIDs) {
		return DailyResult{}, ErrInvalid
	}
	points := make([]PointRef, 0, len(pointIDs))
	filter := DailyFilter{ProjectID: query.ProjectID, Start: start, End: end.AddDate(0, 0, 1), Timezone: "Asia/Shanghai"}
	for _, id := range pointIDs {
		point, ok := byID[id]
		if !ok {
			return DailyResult{}, ErrInvalid
		}
		points = append(points, point)
		if point.UVMode == tracking.Exact {
			filter.ExactPointIDs = append(filter.ExactPointIDs, id)
		} else {
			filter.ApproximatePointIDs = append(filter.ApproximatePointIDs, id)
		}
	}

	aggregates, err := s.daily.QueryDaily(ctx, filter)
	if err != nil {
		return DailyResult{}, err
	}
	type dailyKey struct {
		date    string
		pointID uint64
	}
	indexed := make(map[dailyKey]DailyAggregate, len(aggregates))
	for _, aggregate := range aggregates {
		indexed[dailyKey{date: aggregate.Date, pointID: aggregate.PointID}] = aggregate
	}
	result := DailyResult{
		Timezone: "Asia/Shanghai", StartDate: query.StartDate, EndDate: query.EndDate,
		PointCount: len(points), DayCount: dayCount, Items: make([]DailyItem, 0, dayCount*len(points)),
	}
	for date := end; !date.Before(start); date = date.AddDate(0, 0, -1) {
		dateText := date.Format(time.DateOnly)
		complete := date.Before(today)
		for _, point := range points {
			aggregate := indexed[dailyKey{date: dateText, pointID: point.ID}]
			result.Items = append(result.Items, DailyItem{
				Date: dateText, Complete: complete, PointID: point.ID, PointCode: point.Code,
				PointName: point.Name, PV: aggregate.PV, UV: aggregate.UV, UVMode: point.UVMode,
			})
			result.TotalPV += aggregate.PV
		}
	}
	return result, nil
}

func (s *Service) validateInput(ctx context.Context, projectID uint64, in *ReportInput, requireExpectedUpdatedAt bool) ([]PointRef, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Name) == 0 || utf8.RuneCountInString(in.Name) > 128 || utf8.RuneCountInString(in.Description) > 1000 {
		return nil, ErrInvalid
	}
	if in.DefaultDays < 1 || in.DefaultDays > 90 || (requireExpectedUpdatedAt && in.ExpectedUpdatedAt == nil) {
		return nil, ErrInvalid
	}

	in.PointIDs = uniquePointIDs(in.PointIDs)
	if len(in.PointIDs) < 1 || len(in.PointIDs) > 100 {
		return nil, ErrInvalid
	}
	resolved, err := s.repo.ResolvePoints(ctx, projectID, in.PointIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]PointRef, len(resolved))
	for _, point := range resolved {
		if point.DefinitionState != tracking.Published && point.DefinitionState != tracking.Deprecated {
			return nil, ErrInvalid
		}
		if _, duplicate := byID[point.ID]; duplicate {
			return nil, ErrInvalid
		}
		byID[point.ID] = point
	}
	if len(byID) != len(in.PointIDs) {
		return nil, ErrInvalid
	}
	points := make([]PointRef, len(in.PointIDs))
	for i, id := range in.PointIDs {
		point, ok := byID[id]
		if !ok {
			return nil, ErrInvalid
		}
		point.SortNo = i
		points[i] = point
	}
	return points, nil
}

func uniquePointIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	unique := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
