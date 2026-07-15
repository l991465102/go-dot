package tracking

import (
	"context"
	"errors"
	"strings"

	"github.com/lzw7758/go-dot/internal/project"
)

type Repository interface {
	CreateCategory(context.Context, Category) (Category, error)
	FindCategory(context.Context, uint64) (Category, error)
	FindCategoryInProject(context.Context, uint64, uint64) (Category, error)
	CreateDraft(context.Context, Point) (Point, error)
	FindPoint(context.Context, uint64) (Point, error)
	FindPointInProject(context.Context, uint64, uint64) (Point, error)
	UpdateDraft(context.Context, Point) (Point, error)
	DeleteDraft(context.Context, uint64, uint64, uint64) error
	Publish(context.Context, uint64, uint64, uint64) (Snapshot, error)
	Deprecate(context.Context, uint64, uint64, uint64, string, *uint64) error
	UpdateDevelopmentState(context.Context, uint64, uint64, uint64, DevelopmentState) (Point, error)
}

type CatalogRepository interface {
	ListCategories(context.Context, uint64) ([]Category, error)
	UpdateCategory(context.Context, uint64, uint64, uint64, UpdateCategory) (Category, error)
	ListPoints(context.Context, uint64, PointFilter) (PointList, error)
}

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}

type SnapshotInvalidator interface {
	Invalidate(uint64, string)
}

type Service struct {
	repo        Repository
	auth        Authorizer
	invalidator SnapshotInvalidator
}

func NewService(repo Repository, auth Authorizer, invalidators ...SnapshotInvalidator) *Service {
	service := &Service{repo: repo, auth: auth}
	if len(invalidators) > 0 {
		service.invalidator = invalidators[0]
	}
	return service
}

func (s *Service) CreateCategory(ctx context.Context, actorID uint64, in CreateCategory) (Category, error) {
	if err := s.auth.RequireRole(ctx, actorID, in.ProjectID, project.Admin); err != nil {
		return Category{}, err
	}
	var parent *Category
	if in.ParentID != nil {
		p, err := s.repo.FindCategoryInProject(ctx, in.ProjectID, *in.ParentID)
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

func (s *Service) ListCategories(ctx context.Context, actorID, projectID uint64) ([]Category, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(CatalogRepository)
	if !ok {
		return nil, errors.New("tracking catalog unavailable")
	}
	return repo.ListCategories(ctx, projectID)
}

func (s *Service) UpdateCategory(ctx context.Context, actorID, projectID, id uint64, in UpdateCategory) (Category, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return Category{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return Category{}, coded("CATEGORY_INVALID", "name is required")
	}
	if _, err := s.repo.FindCategoryInProject(ctx, projectID, id); err != nil {
		return Category{}, err
	}
	repo, ok := s.repo.(CatalogRepository)
	if !ok {
		return Category{}, errors.New("tracking catalog unavailable")
	}
	return repo.UpdateCategory(ctx, projectID, id, actorID, in)
}

func (s *Service) ListPoints(ctx context.Context, actorID, projectID uint64, filter PointFilter) (PointList, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin, project.Viewer); err != nil {
		return PointList{}, err
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	repo, ok := s.repo.(CatalogRepository)
	if !ok {
		return PointList{}, errors.New("tracking catalog unavailable")
	}
	return repo.ListPoints(ctx, projectID, filter)
}

func (s *Service) SaveDraft(ctx context.Context, actorID uint64, in DraftInput) (Point, error) {
	if err := s.auth.RequireRole(ctx, actorID, in.ProjectID, project.Admin); err != nil {
		return Point{}, err
	}
	category, err := s.repo.FindCategoryInProject(ctx, in.ProjectID, in.CategoryID)
	if err != nil {
		return Point{}, err
	}
	if category.ProjectID != in.ProjectID || !category.Active || category.Level != Scene {
		return Point{}, coded("CATEGORY_INVALID", "category must be an active SCENE in the same project")
	}
	p, err := pointFromInput(in)
	if err != nil {
		return Point{}, err
	}
	p.Category = category
	p.State = Draft
	p.CreatedBy = actorID
	p.UpdatedBy = actorID
	p.AuditAction = "CREATE_DRAFT"
	if err := p.ValidateDraft(); err != nil {
		return Point{}, err
	}
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
	return s.updateDraftAuthorized(ctx, actorID, existing, in)
}

func (s *Service) updateDraftAuthorized(ctx context.Context, actorID uint64, existing Point, in DraftInput) (Point, error) {
	if existing.State != Draft {
		return Point{}, ErrImmutable
	}
	if in.ProjectID != 0 && in.ProjectID != existing.ProjectID {
		return Point{}, coded("POINT_INVALID", "project cannot be changed")
	}
	in.ProjectID = existing.ProjectID
	category, err := s.repo.FindCategoryInProject(ctx, existing.ProjectID, in.CategoryID)
	if err != nil {
		return Point{}, err
	}
	if category.ProjectID != existing.ProjectID || !category.Active || category.Level != Scene {
		return Point{}, coded("CATEGORY_INVALID", "category must be an active SCENE in the same project")
	}
	p, err := pointFromInput(in)
	if err != nil {
		return Point{}, err
	}
	p.ID = existing.ID
	p.Category = category
	p.State = Draft
	p.CreatedBy = existing.CreatedBy
	p.UpdatedBy = actorID
	p.AuditAction = "UPDATE_DRAFT"
	if err := p.ValidateDraft(); err != nil {
		return Point{}, err
	}
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
	return s.repo.DeleteDraft(ctx, p.ProjectID, pointID, actorID)
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
	snapshot, err := s.repo.Publish(ctx, p.ProjectID, pointID, actorID)
	if err == nil && s.invalidator != nil {
		s.invalidator.Invalidate(p.ProjectID, p.Code)
	}
	return snapshot, err
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
		return coded("DEPRECATION_REASON_REQUIRED", "")
	}
	if replacementID != nil {
		replacement, err := s.repo.FindPoint(ctx, *replacementID)
		if err != nil {
			return err
		}
		if replacement.ProjectID != p.ProjectID || replacement.State != Published {
			return coded("REPLACEMENT_INVALID", "")
		}
	}
	if err := s.repo.Deprecate(ctx, p.ProjectID, pointID, actorID, reason, replacementID); err != nil {
		return err
	}
	if s.invalidator != nil {
		s.invalidator.Invalidate(p.ProjectID, p.Code)
	}
	return nil
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
		return Point{}, coded("POINT_INVALID", "new code is required")
	}
	p.ID = 0
	p.Code = newCode
	p.State = Draft
	p.PublishedVersion = 0
	p.DeprecationReason = ""
	p.ReplacementPointID = nil
	p.CreatedBy = actorID
	p.UpdatedBy = actorID
	p.AuditAction = "COPY"
	return s.repo.CreateDraft(ctx, p)
}

func (s *Service) GetInProject(ctx context.Context, actorID, projectID, pointID uint64) (Point, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin, project.Viewer); err != nil {
		return Point{}, err
	}
	return s.repo.FindPointInProject(ctx, projectID, pointID)
}
func (s *Service) UpdateDraftInProject(ctx context.Context, actorID, projectID, pointID uint64, in DraftInput) (Point, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return Point{}, err
	}
	p, err := s.repo.FindPointInProject(ctx, projectID, pointID)
	if err != nil {
		return Point{}, err
	}
	in.ProjectID = projectID
	return s.updateDraftAuthorized(ctx, actorID, p, in)
}
func (s *Service) DeleteDraftInProject(ctx context.Context, actorID, projectID, pointID uint64) error {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return err
	}
	p, err := s.repo.FindPointInProject(ctx, projectID, pointID)
	if err != nil {
		return err
	}
	if p.State != Draft {
		return ErrImmutable
	}
	return s.repo.DeleteDraft(ctx, projectID, pointID, actorID)
}
func (s *Service) PublishInProject(ctx context.Context, actorID, projectID, pointID uint64) (Snapshot, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return Snapshot{}, err
	}
	p, err := s.repo.FindPointInProject(ctx, projectID, pointID)
	if err != nil {
		return Snapshot{}, err
	}
	if p.State != Draft {
		return Snapshot{}, ErrImmutable
	}
	if err := p.ValidateForPublication(); err != nil {
		return Snapshot{}, err
	}
	snapshot, err := s.repo.Publish(ctx, projectID, pointID, actorID)
	if err == nil && s.invalidator != nil {
		s.invalidator.Invalidate(projectID, p.Code)
	}
	return snapshot, err
}
func (s *Service) CopyInProject(ctx context.Context, actorID, projectID, pointID uint64, newCode string) (Point, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return Point{}, err
	}
	p, err := s.repo.FindPointInProject(ctx, projectID, pointID)
	if err != nil {
		return Point{}, err
	}
	if !pointCodePattern.MatchString(newCode) {
		return Point{}, coded("POINT_INVALID", "invalid new code")
	}
	p.ID = 0
	p.Code = newCode
	p.State = Draft
	p.PublishedVersion = 0
	p.DeprecationReason = ""
	p.ReplacementPointID = nil
	p.CreatedBy = actorID
	p.UpdatedBy = actorID
	p.AuditAction = "COPY"
	return s.repo.CreateDraft(ctx, p)
}
func (s *Service) DeprecateInProject(ctx context.Context, actorID, projectID, pointID uint64, reason string, replacementID *uint64) error {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return err
	}
	p, err := s.repo.FindPointInProject(ctx, projectID, pointID)
	if err != nil {
		return err
	}
	if p.State != Published {
		return ErrPointNotPublished
	}
	if strings.TrimSpace(reason) == "" {
		return coded("DEPRECATION_REASON_REQUIRED", "")
	}
	if err := s.repo.Deprecate(ctx, projectID, pointID, actorID, reason, replacementID); err != nil {
		return err
	}
	if s.invalidator != nil {
		s.invalidator.Invalidate(projectID, p.Code)
	}
	return nil
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

func (s *Service) UpdateDevelopmentState(ctx context.Context, actorID, projectID, pointID uint64, state DevelopmentState) (Point, error) {
	if err := s.auth.RequireRole(ctx, actorID, projectID, project.Admin); err != nil {
		return Point{}, err
	}
	if !validDevelopmentState(state) {
		return Point{}, coded("DEVELOPMENT_STATE_INVALID", "unsupported development state")
	}
	p, err := s.repo.FindPointInProject(ctx, projectID, pointID)
	if err != nil {
		return Point{}, err
	}
	if p.State != Draft && p.State != Published {
		return Point{}, coded("POINT_STATE_INVALID", "development state can only change for draft or published points")
	}
	return s.repo.UpdateDevelopmentState(ctx, projectID, pointID, actorID, state)
}

func pointFromInput(in DraftInput) (Point, error) {
	if in.ProjectID == 0 || in.CategoryID == 0 {
		return Point{}, coded("POINT_INVALID", "project_id and category_id are required")
	}
	if in.DevelopmentState == "" {
		in.DevelopmentState = Pending
	}
	if in.UVMode == "" {
		in.UVMode = Approximate
	}
	if in.TimeBasis == "" {
		in.TimeBasis = Client
	}
	return Point{ProjectID: in.ProjectID, CategoryID: in.CategoryID, Code: in.Code, Name: in.Name, Description: in.Description, BehaviorType: in.BehaviorType, DevelopmentState: in.DevelopmentState, TriggerPage: in.TriggerPage, TriggerTiming: in.TriggerTiming, TriggerCountRule: in.TriggerCountRule, AbnormalTriggerRule: in.AbnormalTriggerRule, CountPV: in.CountPV, CountUV: in.CountUV, UVMode: in.UVMode, TimeBasis: in.TimeBasis, SupportedPlatforms: in.SupportedPlatforms, SupportedChannels: in.SupportedChannels, ProductOwner: in.ProductOwner, DeveloperOwner: in.DeveloperOwner, RequirementURL: in.RequirementURL, Properties: in.Properties}, nil
}
