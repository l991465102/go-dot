package tracking

import (
	"context"
	"fmt"
	"strings"

	"github.com/lzw7758/go-dot/internal/project"
)

type Repository interface {
	CreateCategory(context.Context, Category) (Category, error)
	FindCategory(context.Context, uint64) (Category, error)
	CreateDraft(context.Context, Point) (Point, error)
	FindPoint(context.Context, uint64) (Point, error)
	UpdateDraft(context.Context, Point) (Point, error)
	DeleteDraft(context.Context, uint64) error
	Publish(context.Context, uint64, uint64) (Snapshot, error)
	Deprecate(context.Context, uint64, uint64, string, *uint64) error
}

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}

type Service struct {
	repo Repository
	auth Authorizer
}

func NewService(repo Repository, auth Authorizer) *Service { return &Service{repo: repo, auth: auth} }

func (s *Service) CreateCategory(ctx context.Context, actorID uint64, in CreateCategory) (Category, error) {
	if err := s.auth.RequireRole(ctx, actorID, in.ProjectID, project.Admin); err != nil {
		return Category{}, err
	}
	var parent *Category
	if in.ParentID != nil {
		p, err := s.repo.FindCategory(ctx, *in.ParentID)
		if err != nil {
			return Category{}, err
		}
		parent = &p
	}
	if err := in.validate(parent); err != nil {
		return Category{}, err
	}
	return s.repo.CreateCategory(ctx, Category{ProjectID: in.ProjectID, ParentID: in.ParentID, Code: in.Code, Name: in.Name, Level: in.Level, Description: in.Description, SortOrder: in.SortOrder, Active: true, CreatedBy: actorID})
}

func (s *Service) SaveDraft(ctx context.Context, actorID uint64, in DraftInput) (Point, error) {
	if err := s.auth.RequireRole(ctx, actorID, in.ProjectID, project.Admin); err != nil {
		return Point{}, err
	}
	category, err := s.repo.FindCategory(ctx, in.CategoryID)
	if err != nil {
		return Point{}, err
	}
	if category.ProjectID != in.ProjectID || !category.Active {
		return Point{}, fmt.Errorf("CATEGORY_INVALID: category must be active and in the same project")
	}
	p, err := pointFromInput(in)
	if err != nil {
		return Point{}, err
	}
	p.Category = category
	p.State = Draft
	p.CreatedBy = actorID
	p.UpdatedBy = actorID
	return s.repo.CreateDraft(ctx, p)
}

func (s *Service) UpdateDraft(ctx context.Context, actorID, pointID uint64, in DraftInput) (Point, error) {
	existing, err := s.repo.FindPoint(ctx, pointID)
	if err != nil {
		return Point{}, err
	}
	if err := s.auth.RequireRole(ctx, actorID, existing.ProjectID, project.Admin); err != nil {
		return Point{}, err
	}
	if existing.State != Draft {
		return Point{}, ErrImmutable
	}
	if in.ProjectID != 0 && in.ProjectID != existing.ProjectID {
		return Point{}, fmt.Errorf("POINT_INVALID: project cannot be changed")
	}
	in.ProjectID = existing.ProjectID
	category, err := s.repo.FindCategory(ctx, in.CategoryID)
	if err != nil {
		return Point{}, err
	}
	if category.ProjectID != existing.ProjectID || !category.Active {
		return Point{}, fmt.Errorf("CATEGORY_INVALID: category must be active and in the same project")
	}
	p, err := pointFromInput(in)
	if err != nil {
		return Point{}, err
	}
	p.ID = pointID
	p.Category = category
	p.State = Draft
	p.CreatedBy = existing.CreatedBy
	p.UpdatedBy = actorID
	return s.repo.UpdateDraft(ctx, p)
}

func (s *Service) DeleteDraft(ctx context.Context, actorID, pointID uint64) error {
	p, err := s.repo.FindPoint(ctx, pointID)
	if err != nil {
		return err
	}
	if err := s.auth.RequireRole(ctx, actorID, p.ProjectID, project.Admin); err != nil {
		return err
	}
	if p.State != Draft {
		return ErrImmutable
	}
	return s.repo.DeleteDraft(ctx, pointID)
}

func (s *Service) Publish(ctx context.Context, actorID, pointID uint64) (Snapshot, error) {
	p, err := s.repo.FindPoint(ctx, pointID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := s.auth.RequireRole(ctx, actorID, p.ProjectID, project.Admin); err != nil {
		return Snapshot{}, err
	}
	if p.State != Draft {
		return Snapshot{}, ErrImmutable
	}
	if err := p.ValidateForPublication(); err != nil {
		return Snapshot{}, err
	}
	return s.repo.Publish(ctx, pointID, actorID)
}

func (s *Service) Deprecate(ctx context.Context, actorID, pointID uint64, reason string, replacementID *uint64) error {
	p, err := s.repo.FindPoint(ctx, pointID)
	if err != nil {
		return err
	}
	if err := s.auth.RequireRole(ctx, actorID, p.ProjectID, project.Admin); err != nil {
		return err
	}
	if p.State != Published {
		return ErrPointNotPublished
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("DEPRECATION_REASON_REQUIRED")
	}
	if replacementID != nil {
		replacement, err := s.repo.FindPoint(ctx, *replacementID)
		if err != nil {
			return err
		}
		if replacement.ProjectID != p.ProjectID || replacement.State != Published {
			return fmt.Errorf("REPLACEMENT_INVALID")
		}
	}
	return s.repo.Deprecate(ctx, pointID, actorID, reason, replacementID)
}

func (s *Service) Copy(ctx context.Context, actorID, pointID uint64, newCode string) (Point, error) {
	p, err := s.repo.FindPoint(ctx, pointID)
	if err != nil {
		return Point{}, err
	}
	if err := s.auth.RequireRole(ctx, actorID, p.ProjectID, project.Admin); err != nil {
		return Point{}, err
	}
	if strings.TrimSpace(newCode) == "" {
		return Point{}, fmt.Errorf("POINT_INVALID: new code is required")
	}
	p.ID = 0
	p.Code = newCode
	p.State = Draft
	p.PublishedVersion = 0
	p.DeprecationReason = ""
	p.ReplacementPointID = nil
	p.CreatedBy = actorID
	p.UpdatedBy = actorID
	return s.repo.CreateDraft(ctx, p)
}

func (s *Service) Get(ctx context.Context, actorID, pointID uint64) (Point, error) {
	p, err := s.repo.FindPoint(ctx, pointID)
	if err != nil {
		return Point{}, err
	}
	if err := s.auth.RequireRole(ctx, actorID, p.ProjectID, project.Admin, project.Viewer); err != nil {
		return Point{}, err
	}
	return p, nil
}

func pointFromInput(in DraftInput) (Point, error) {
	if in.ProjectID == 0 || in.CategoryID == 0 || in.Code == "" || in.Name == "" {
		return Point{}, fmt.Errorf("POINT_INVALID: project_id, category_id, code and name are required")
	}
	if err := validatePropertySchema(in.Properties); err != nil {
		return Point{}, err
	}
	if in.DevelopmentState == "" {
		in.DevelopmentState = "PENDING"
	}
	if in.UVMode == "" {
		in.UVMode = "APPROXIMATE"
	}
	if in.TimeBasis == "" {
		in.TimeBasis = "EVENT_TIME"
	}
	return Point{ProjectID: in.ProjectID, CategoryID: in.CategoryID, Code: in.Code, Name: in.Name, Description: in.Description, BehaviorType: in.BehaviorType, DevelopmentState: in.DevelopmentState, TriggerPage: in.TriggerPage, TriggerTiming: in.TriggerTiming, TriggerCountRule: in.TriggerCountRule, AbnormalTriggerRule: in.AbnormalTriggerRule, CountPV: in.CountPV, CountUV: in.CountUV, UVMode: in.UVMode, TimeBasis: in.TimeBasis, SupportedPlatforms: in.SupportedPlatforms, SupportedChannels: in.SupportedChannels, ProductOwner: in.ProductOwner, DeveloperOwner: in.DeveloperOwner, RequirementURL: in.RequirementURL, Properties: in.Properties}, nil
}
