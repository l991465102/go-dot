package reporting

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

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
