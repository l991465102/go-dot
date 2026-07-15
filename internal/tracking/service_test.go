package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/stretchr/testify/require"
)

const publishedPointID uint64 = 10

func TestPublishRequiresSceneAndCompleteSchema(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	point := validDraft()
	point.Category.Level = Module
	repo.points[point.ID] = point
	_, err := svc.Publish(context.Background(), 1, point.ID)
	require.ErrorContains(t, err, "SCENE category")
}

func TestPublishAndDeprecateInvalidateCollectionSnapshot(t *testing.T) {
	repo := &fakeRepository{nextID: 20, points: map[uint64]Point{}, categories: map[uint64]Category{3: {ID: 3, ProjectID: 2, Level: Scene, Active: true}}}
	invalidator := &invalidationRecorder{}
	svc := NewService(repo, allowAll{}, invalidator)
	p := validDraft()
	repo.points[p.ID] = p
	_, err := svc.Publish(context.Background(), 1, p.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"2:product_detail_view"}, invalidator.keys)
	require.NoError(t, svc.Deprecate(context.Background(), 1, p.ID, "old", nil))
	require.Equal(t, []string{"2:product_detail_view", "2:product_detail_view"}, invalidator.keys)
}

func TestPublishedPointCannotBeEditedOrDeleted(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	point := validDraft()
	point.ID = publishedPointID
	point.State = Published
	repo.points[point.ID] = point
	require.ErrorIs(t, func() error {
		_, err := svc.UpdateDraft(context.Background(), 1, publishedPointID, validDraftInput())
		return err
	}(), ErrImmutable)
	require.ErrorIs(t, svc.DeleteDraft(context.Background(), 1, publishedPointID), ErrImmutable)
}

func TestUnknownPropertyIsRejectedBySnapshot(t *testing.T) {
	snapshot := publishedProductViewSnapshot()
	err := snapshot.ValidateProperties(map[string]any{"product_id": "123", "extra": true})
	require.ErrorContains(t, err, "UNKNOWN_PROPERTY")
}

func TestSnapshotValidatesRequiredTypesEnumsDatesAndArrays(t *testing.T) {
	snapshot := Snapshot{Properties: []Property{
		{Code: "id", Type: String, Required: true},
		{Code: "quantity", Type: Integer},
		{Code: "price", Type: Decimal},
		{Code: "active", Type: Boolean},
		{Code: "at", Type: Datetime},
		{Code: "kind", Type: Enum, EnumValues: []string{"A", "B"}},
		{Code: "tags", Type: Array, ArrayItemType: String},
	}}
	require.ErrorContains(t, snapshot.ValidateProperties(nil), "PROPERTY_REQUIRED")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "quantity": 1.5}), "PROPERTY_TYPE_ERROR")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "at": "today"}), "PROPERTY_TYPE_ERROR")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "kind": "C"}), "PROPERTY_ENUM_INVALID")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "tags": []any{"ok", true}}), "PROPERTY_TYPE_ERROR")
	require.NoError(t, snapshot.ValidateProperties(map[string]any{"id": "1", "quantity": float64(2), "price": 1.5, "active": true, "at": "2026-07-15T12:00:00Z", "kind": "A", "tags": []any{"x"}}))
}

func TestSaveAndUpdateDraftRequireActiveScene(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	for _, level := range []CategoryLevel{Domain, Module} {
		repo.categories[3] = Category{ID: 3, ProjectID: 2, Level: level, Active: true}
		_, err := svc.SaveDraft(context.Background(), 1, validDraftInput())
		require.ErrorContains(t, err, "SCENE")
	}
	repo.categories[3] = Category{ID: 3, ProjectID: 2, Level: Scene, Active: true}
	p := validDraft()
	repo.points[p.ID] = p
	repo.categories[3] = Category{ID: 3, ProjectID: 2, Level: Module, Active: true}
	_, err := svc.UpdateDraft(context.Background(), 1, p.ID, validDraftInput())
	require.ErrorContains(t, err, "SCENE")
}

func TestDeleteAndDeprecateReturnAuthorizationFailureWithoutMutation(t *testing.T) {
	repo := &fakeRepository{points: map[uint64]Point{}, categories: map[uint64]Category{}}
	p := validDraft()
	repo.points[p.ID] = p
	denied := errors.New("membership backend unavailable")
	svc := NewService(repo, denyAll{err: denied})
	require.ErrorIs(t, svc.DeleteDraft(context.Background(), 1, p.ID), denied)
	require.Equal(t, Draft, repo.points[p.ID].State)
	p.State = Published
	repo.points[p.ID] = p
	require.ErrorIs(t, svc.Deprecate(context.Background(), 1, p.ID, "old", nil), denied)
	require.Equal(t, Published, repo.points[p.ID].State)
}

func TestPropertySchemaUsesExactGovernedTypes(t *testing.T) {
	require.ErrorContains(t, validatePropertySchema([]Property{{Code: "kind", Name: "Kind", Type: Enum}}), "PROPERTY_SCHEMA_INVALID")
	require.ErrorContains(t, validatePropertySchema([]Property{{Code: "kind", Name: "Kind", Type: Enum, EnumValues: []string{"a", "a"}}}), "PROPERTY_SCHEMA_INVALID")
	require.ErrorContains(t, validatePropertySchema([]Property{{Code: "kind", Name: "Kind", Type: String, EnumValues: []string{"a"}}}), "PROPERTY_SCHEMA_INVALID")
	require.NoError(t, validatePropertySchema([]Property{{Code: "kind", Name: "Kind", Type: Enum, EnumValues: []string{"a", "b"}}, {Code: "amount", Name: "Amount", Type: Decimal}}))
}

func TestIntegerPreservesJSONNumberPrecision(t *testing.T) {
	s := Snapshot{PointID: 1, Version: 1, SupportedPlatforms: []string{"web"}, Properties: []Property{{Code: "id", Name: "ID", Type: Integer}}}
	require.NoError(t, s.Validate("web", map[string]any{"id": json.Number("9007199254740993")}))
	require.ErrorContains(t, s.Validate("web", map[string]any{"id": float64(9007199254740993)}), "PROPERTY_TYPE_ERROR")
}

func TestPointGovernanceValuesAreValidated(t *testing.T) {
	p := validDraft()
	p.Code = "ab"
	require.ErrorContains(t, p.ValidateForPublication(), "POINT_INVALID")
	p = validDraft()
	p.BehaviorType = "VIEW"
	require.ErrorContains(t, p.ValidateForPublication(), "BEHAVIOR_TYPE_INVALID")
	p = validDraft()
	p.DevelopmentState = "DONE"
	require.ErrorContains(t, p.ValidateForPublication(), "DEVELOPMENT_STATE_INVALID")
	p = validDraft()
	p.SupportedPlatforms = []string{"desktop"}
	require.ErrorContains(t, p.ValidateForPublication(), "PLATFORM_INVALID")
}

func TestEarlyStepDraftCanBeSavedButCannotBePublished(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	draft, err := svc.SaveDraft(context.Background(), 1, DraftInput{ProjectID: 2, CategoryID: 3, Code: "product_view", Name: "Product view"})
	require.NoError(t, err)
	require.Equal(t, "product_view", draft.Code)
	_, err = svc.Publish(context.Background(), 1, draft.ID)
	require.ErrorContains(t, err, "POINT_INVALID")
	require.Equal(t, Draft, repo.points[draft.ID].State)
}

func TestDraftRejectsEmptyPointAndPropertyCodesBeforePersistence(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	_, err := svc.SaveDraft(context.Background(), 1, DraftInput{ProjectID: 2, CategoryID: 3, Name: "Name"})
	require.ErrorContains(t, err, "POINT_INVALID")
	require.Empty(t, repo.points)
	base := DraftInput{ProjectID: 2, CategoryID: 3, Code: "product_view", Name: "Name"}
	base.Properties = []PropertyInput{{Name: "Blank"}}
	_, err = svc.SaveDraft(context.Background(), 1, base)
	require.ErrorContains(t, err, "PROPERTY_SCHEMA_INVALID")
	require.Empty(t, repo.points)
	base.Properties = []PropertyInput{{Code: "product_id"}, {Code: "product_id"}}
	_, err = svc.SaveDraft(context.Background(), 1, base)
	require.ErrorContains(t, err, "PROPERTY_SCHEMA_INVALID")
	require.Empty(t, repo.points)
}

func TestDraftRejectsGovernanceTyposWhenPresent(t *testing.T) {
	svc, _ := newTrackingFixture(t)
	in := DraftInput{ProjectID: 2, CategoryID: 3, Code: "product_view", Name: "Product view", UVMode: "APPROXIMAT", TimeBasis: Client}
	_, err := svc.SaveDraft(context.Background(), 1, in)
	require.ErrorContains(t, err, "UV_MODE_INVALID")
	in.UVMode = Approximate
	in.TimeBasis = "DEVICE"
	_, err = svc.SaveDraft(context.Background(), 1, in)
	require.ErrorContains(t, err, "TIME_BASIS_INVALID")
	in.TimeBasis = Client
	in.SupportedPlatforms = []string{"desktop"}
	_, err = svc.SaveDraft(context.Background(), 1, in)
	require.ErrorContains(t, err, "PLATFORM_INVALID")
	in.SupportedPlatforms = nil
	in.Properties = []PropertyInput{{ArrayItemType: "OBJECT"}}
	_, err = svc.SaveDraft(context.Background(), 1, in)
	require.ErrorContains(t, err, "PROPERTY_SCHEMA_INVALID")
}

func TestForeignCategoryIsIndistinguishableFromMissing(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	repo.categories[4] = Category{ID: 4, ProjectID: 99, Level: Scene, Active: true}
	in := DraftInput{ProjectID: 2, CategoryID: 4}
	_, err := svc.SaveDraft(context.Background(), 1, in)
	require.ErrorIs(t, err, ErrCategoryNotFound)
	parent := uint64(4)
	_, err = svc.CreateCategory(context.Background(), 1, CreateCategory{ProjectID: 2, ParentID: &parent, Code: "child", Name: "Child", Level: Module})
	require.ErrorIs(t, err, ErrCategoryNotFound)
}

func TestDevelopmentStateCanChangeAfterPublicationWithoutChangingContract(t *testing.T) {
	svc, repo := newTrackingFixture(t)
	p := validDraft()
	p.State = Published
	p.DevelopmentState = Testing
	repo.points[p.ID] = p
	before := p.Snapshot(1, 1)
	updated, err := svc.UpdateDevelopmentState(context.Background(), 1, p.ProjectID, p.ID, Online)
	require.NoError(t, err)
	require.Equal(t, Online, updated.DevelopmentState)
	require.Equal(t, before, repo.points[p.ID].Snapshot(1, 1))
	_, err = svc.UpdateDevelopmentState(context.Background(), 1, p.ProjectID, p.ID, "DONE")
	require.ErrorContains(t, err, "DEVELOPMENT_STATE_INVALID")
}

func validDraft() Point {
	in := validDraftInput()
	return Point{ID: 7, ProjectID: in.ProjectID, CategoryID: in.CategoryID, Category: Category{ID: in.CategoryID, ProjectID: in.ProjectID, Level: Scene, Active: true}, Code: in.Code, Name: in.Name, BehaviorType: in.BehaviorType, DevelopmentState: Pending, State: Draft, UVMode: Approximate, TimeBasis: Client, SupportedPlatforms: in.SupportedPlatforms, Properties: in.Properties}
}

func validDraftInput() DraftInput {
	return DraftInput{ProjectID: 2, CategoryID: 3, Code: "product_detail_view", Name: "Product detail view", BehaviorType: "PAGE_VIEW", SupportedPlatforms: []string{"web"}, Properties: []PropertyInput{{Code: "product_id", Name: "Product ID", Type: String, Required: true}}}
}

func publishedProductViewSnapshot() Snapshot {
	return Snapshot{PointID: publishedPointID, PointCode: "product_detail_view", Version: 1, Properties: []Property{{Code: "product_id", Name: "Product ID", Type: String, Required: true}}}
}

type allowAll struct{}

func (allowAll) RequireRole(context.Context, uint64, uint64, ...project.Role) error { return nil }

type denyAll struct{ err error }

func (d denyAll) RequireRole(context.Context, uint64, uint64, ...project.Role) error { return d.err }

type invalidationRecorder struct{ keys []string }

func (r *invalidationRecorder) Invalidate(projectID uint64, code string) {
	r.keys = append(r.keys, fmt.Sprintf("%d:%s", projectID, code))
}

type fakeRepository struct {
	nextID     uint64
	points     map[uint64]Point
	categories map[uint64]Category
}

func newTrackingFixture(t *testing.T) (*Service, *fakeRepository) {
	t.Helper()
	repo := &fakeRepository{nextID: 20, points: map[uint64]Point{}, categories: map[uint64]Category{3: {ID: 3, ProjectID: 2, Level: Scene, Active: true}}}
	return NewService(repo, allowAll{}), repo
}

func (r *fakeRepository) CreateCategory(_ context.Context, c Category) (Category, error) {
	r.nextID++
	c.ID = r.nextID
	r.categories[c.ID] = c
	return c, nil
}
func (r *fakeRepository) FindCategory(_ context.Context, id uint64) (Category, error) {
	c, ok := r.categories[id]
	if !ok {
		return Category{}, ErrCategoryNotFound
	}
	return c, nil
}
func (r *fakeRepository) FindCategoryInProject(ctx context.Context, projectID, id uint64) (Category, error) {
	c, err := r.FindCategory(ctx, id)
	if err != nil || c.ProjectID != projectID {
		return Category{}, ErrCategoryNotFound
	}
	return c, nil
}
func (r *fakeRepository) CreateDraft(_ context.Context, p Point) (Point, error) {
	r.nextID++
	p.ID = r.nextID
	r.points[p.ID] = p
	return p, nil
}
func (r *fakeRepository) FindPoint(_ context.Context, id uint64) (Point, error) {
	p, ok := r.points[id]
	if !ok {
		return Point{}, ErrPointNotFound
	}
	return p, nil
}
func (r *fakeRepository) FindPointInProject(ctx context.Context, projectID, id uint64) (Point, error) {
	p, err := r.FindPoint(ctx, id)
	if err != nil {
		return Point{}, err
	}
	if p.ProjectID != projectID {
		return Point{}, ErrPointNotFound
	}
	return p, nil
}
func (r *fakeRepository) UpdateDraft(_ context.Context, p Point) (Point, error) {
	old, ok := r.points[p.ID]
	if !ok {
		return Point{}, ErrPointNotFound
	}
	if old.State != Draft {
		return Point{}, ErrImmutable
	}
	r.points[p.ID] = p
	return p, nil
}
func (r *fakeRepository) DeleteDraft(_ context.Context, projectID, id, actor uint64) error {
	p, ok := r.points[id]
	if !ok {
		return ErrPointNotFound
	}
	if p.State != Draft {
		return ErrImmutable
	}
	delete(r.points, id)
	return nil
}
func (r *fakeRepository) Publish(_ context.Context, projectID, id, actorID uint64) (Snapshot, error) {
	p := r.points[id]
	if err := p.ValidateForPublication(); err != nil {
		return Snapshot{}, err
	}
	p.State = Published
	p.PublishedVersion = 1
	r.points[id] = p
	return p.Snapshot(actorID, 1), nil
}
func (r *fakeRepository) Deprecate(_ context.Context, projectID, id, actorID uint64, reason string, replacementID *uint64) error {
	p := r.points[id]
	if p.State != Published {
		return ErrPointNotPublished
	}
	p.State = Deprecated
	p.DeprecationReason = reason
	p.ReplacementPointID = replacementID
	r.points[id] = p
	return nil
}
func (r *fakeRepository) UpdateDevelopmentState(_ context.Context, projectID, id, actor uint64, state DevelopmentState) (Point, error) {
	p, ok := r.points[id]
	if !ok || p.ProjectID != projectID {
		return Point{}, ErrPointNotFound
	}
	p.DevelopmentState = state
	r.points[id] = p
	return p, nil
}
