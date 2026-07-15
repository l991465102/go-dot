package tracking

import (
	"context"
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
		{Code: "price", Type: Number},
		{Code: "active", Type: Boolean},
		{Code: "at", Type: Datetime},
		{Code: "kind", Type: String, EnumValues: []string{"A", "B"}},
		{Code: "tags", Type: Array, ArrayItemType: String},
	}}
	require.ErrorContains(t, snapshot.ValidateProperties(nil), "PROPERTY_REQUIRED")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "quantity": 1.5}), "PROPERTY_TYPE_ERROR")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "at": "today"}), "PROPERTY_TYPE_ERROR")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "kind": "C"}), "PROPERTY_ENUM_INVALID")
	require.ErrorContains(t, snapshot.ValidateProperties(map[string]any{"id": "1", "tags": []any{"ok", true}}), "PROPERTY_TYPE_ERROR")
	require.NoError(t, snapshot.ValidateProperties(map[string]any{"id": "1", "quantity": float64(2), "price": 1.5, "active": true, "at": "2026-07-15T12:00:00Z", "kind": "A", "tags": []any{"x"}}))
}

func validDraft() Point {
	in := validDraftInput()
	return Point{ID: 7, ProjectID: in.ProjectID, CategoryID: in.CategoryID, Category: Category{ID: in.CategoryID, ProjectID: in.ProjectID, Level: Scene, Active: true}, Code: in.Code, Name: in.Name, BehaviorType: in.BehaviorType, State: Draft, SupportedPlatforms: in.SupportedPlatforms, Properties: in.Properties}
}

func validDraftInput() DraftInput {
	return DraftInput{ProjectID: 2, CategoryID: 3, Code: "product_detail_view", Name: "Product detail view", BehaviorType: "VIEW", SupportedPlatforms: []string{"WEB"}, Properties: []PropertyInput{{Code: "product_id", Name: "Product ID", Type: String, Required: true}}}
}

func publishedProductViewSnapshot() Snapshot {
	return Snapshot{PointID: publishedPointID, PointCode: "product_detail_view", Version: 1, Properties: []Property{{Code: "product_id", Name: "Product ID", Type: String, Required: true}}}
}

type allowAll struct{}

func (allowAll) RequireRole(context.Context, uint64, uint64, ...project.Role) error { return nil }

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
func (r *fakeRepository) DeleteDraft(_ context.Context, id uint64) error {
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
func (r *fakeRepository) Publish(_ context.Context, id, actorID uint64) (Snapshot, error) {
	p := r.points[id]
	if err := p.ValidateForPublication(); err != nil {
		return Snapshot{}, err
	}
	p.State = Published
	p.PublishedVersion = 1
	r.points[id] = p
	return p.Snapshot(actorID, 1), nil
}
func (r *fakeRepository) Deprecate(_ context.Context, id, actorID uint64, reason string, replacementID *uint64) error {
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
