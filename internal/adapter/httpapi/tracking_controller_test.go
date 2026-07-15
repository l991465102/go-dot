package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/stretchr/testify/require"
)

func TestTrackingHTTPFullLifecycle(t *testing.T) {
	repo := &memoryTrackingRepo{categories: map[uint64]tracking.Category{}, points: map[uint64]tracking.Point{}}
	svc := tracking.NewService(repo, trackingAllowAll{})
	mux := http.NewServeMux()
	NewTrackingController(svc).Register(mux)

	domain := postCategory(t, mux, tracking.CreateCategory{ProjectID: 9, Code: "commerce", Name: "Commerce", Level: tracking.Domain})
	module := postCategory(t, mux, tracking.CreateCategory{ProjectID: 9, ParentID: &domain.ID, Code: "product", Name: "Product", Level: tracking.Module})
	scene := postCategory(t, mux, tracking.CreateCategory{ProjectID: 9, ParentID: &module.ID, Code: "detail", Name: "Detail", Level: tracking.Scene})
	in := tracking.DraftInput{ProjectID: 999, CategoryID: scene.ID, Code: "product_detail_view", Name: "Product detail", BehaviorType: tracking.PageView, SupportedPlatforms: []string{"web"}, Properties: []tracking.Property{{Code: "product_id", Name: "Product ID", Type: tracking.String, Required: true}}}
	draft := requestJSON[tracking.Point](t, mux, http.MethodPost, "/api/v1/projects/9/points", in, http.StatusCreated)
	require.Equal(t, uint64(9), draft.ProjectID)
	requestJSON[tracking.Snapshot](t, mux, http.MethodPost, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10)+"/publish", nil, http.StatusOK)
	idempotent := requestJSON[tracking.Point](t, mux, http.MethodPatch, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10)+"/development-state", map[string]any{"state": "PENDING"}, http.StatusOK)
	require.Equal(t, tracking.Pending, idempotent.DevelopmentState)
	transitioned := requestJSON[tracking.Point](t, mux, http.MethodPatch, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10)+"/development-state", map[string]any{"state": "ONLINE"}, http.StatusOK)
	require.Equal(t, tracking.Online, transitioned.DevelopmentState)
	requestJSON[map[string]string](t, mux, http.MethodPatch, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10), in, http.StatusConflict)
	requestJSON[map[string]string](t, mux, http.MethodDelete, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10), nil, http.StatusConflict)
	replacement := requestJSON[tracking.Point](t, mux, http.MethodPost, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10)+"/copy", map[string]any{"code": "product_detail_view_v2"}, http.StatusCreated)
	requestJSON[tracking.Snapshot](t, mux, http.MethodPost, "/api/v1/projects/9/points/"+strconv.FormatUint(replacement.ID, 10)+"/publish", nil, http.StatusOK)
	requestJSON[any](t, mux, http.MethodPost, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10)+"/deprecate", map[string]any{"reason": "replaced", "replacement_id": replacement.ID}, http.StatusNoContent)
	original := requestJSON[tracking.Point](t, mux, http.MethodGet, "/api/v1/projects/9/points/"+strconv.FormatUint(draft.ID, 10), nil, http.StatusOK)
	copy := requestJSON[tracking.Point](t, mux, http.MethodGet, "/api/v1/projects/9/points/"+strconv.FormatUint(replacement.ID, 10), nil, http.StatusOK)
	require.Equal(t, tracking.Deprecated, original.State)
	require.Equal(t, tracking.Published, copy.State)
}

func TestViewerCannotDeleteOrDeprecateAndStateIsUnchanged(t *testing.T) {
	repo := &memoryTrackingRepo{categories: map[uint64]tracking.Category{}, points: map[uint64]tracking.Point{7: {ID: 7, ProjectID: 9, State: tracking.Draft}}}
	svc := tracking.NewService(repo, trackingDenyWrites{})
	mux := http.NewServeMux()
	NewTrackingController(svc).Register(mux)
	requestJSON[map[string]string](t, mux, http.MethodDelete, "/api/v1/projects/9/points/7", nil, http.StatusForbidden)
	require.Equal(t, tracking.Draft, repo.points[7].State)
	p := repo.points[7]
	p.State = tracking.Published
	repo.points[7] = p
	requestJSON[map[string]string](t, mux, http.MethodPost, "/api/v1/projects/9/points/7/deprecate", map[string]any{"reason": "old"}, http.StatusForbidden)
	require.Equal(t, tracking.Published, repo.points[7].State)
	requestJSON[map[string]string](t, mux, http.MethodPatch, "/api/v1/projects/9/points/7/development-state", map[string]any{"state": "ONLINE"}, http.StatusForbidden)
	require.NotEqual(t, tracking.Online, repo.points[7].DevelopmentState)
}

func TestTrackingCatalogRoutesExposeListsAndGovernCategoryUpdates(t *testing.T) {
	repo := &memoryTrackingRepo{categories: map[uint64]tracking.Category{}, points: map[uint64]tracking.Point{}}
	svc := tracking.NewService(repo, trackingAllowAll{})
	mux := http.NewServeMux()
	NewTrackingController(svc).Register(mux)

	domain := postCategory(t, mux, tracking.CreateCategory{ProjectID: 9, Code: "commerce", Name: "Commerce", Level: tracking.Domain})
	requestJSON[[]tracking.Category](t, mux, http.MethodGet, "/api/v1/projects/9/categories", nil, http.StatusOK)
	updated := requestJSON[tracking.Category](t, mux, http.MethodPatch, "/api/v1/projects/9/categories/"+strconv.FormatUint(domain.ID, 10), map[string]any{"name": "Mall", "sort_order": 5, "active": true}, http.StatusOK)
	require.Equal(t, "Mall", updated.Name)
	require.Equal(t, 5, updated.SortOrder)
	points := requestJSON[tracking.PointList](t, mux, http.MethodGet, "/api/v1/projects/9/points?q=detail&definition_state=DRAFT", nil, http.StatusOK)
	require.Empty(t, points.Items)
}

func TestProjectScopedPointLookupHidesForeignPoint(t *testing.T) {
	repo := &memoryTrackingRepo{categories: map[uint64]tracking.Category{}, points: map[uint64]tracking.Point{7: {ID: 7, ProjectID: 9}}}
	svc := tracking.NewService(repo, trackingAllowAll{})
	mux := http.NewServeMux()
	NewTrackingController(svc).Register(mux)
	requestJSON[map[string]string](t, mux, http.MethodGet, "/api/v1/projects/10/points/7", nil, http.StatusNotFound)
}

func TestProjectScopedDraftHidesForeignCategory(t *testing.T) {
	repo := &memoryTrackingRepo{categories: map[uint64]tracking.Category{3: {ID: 3, ProjectID: 9, Level: tracking.Scene, Active: true}}, points: map[uint64]tracking.Point{}}
	svc := tracking.NewService(repo, trackingAllowAll{})
	mux := http.NewServeMux()
	NewTrackingController(svc).Register(mux)
	body := requestJSON[map[string]string](t, mux, http.MethodPost, "/api/v1/projects/10/points", map[string]any{"category_id": 3}, http.StatusNotFound)
	require.Equal(t, "CATEGORY_NOT_FOUND", body["error"])
}

func TestTrackingErrorsAreExplicitAndInfrastructureIsGeneric(t *testing.T) {
	repo := &memoryTrackingRepo{categories: map[uint64]tracking.Category{}, points: map[uint64]tracking.Point{}, findErr: errors.New("secret database host")}
	svc := tracking.NewService(repo, trackingAllowAll{})
	mux := http.NewServeMux()
	NewTrackingController(svc).Register(mux)
	body := requestJSON[map[string]string](t, mux, http.MethodGet, "/api/v1/projects/9/points/7", nil, http.StatusInternalServerError)
	require.Equal(t, "tracking service unavailable", body["error"])
	validation := requestJSON[map[string]string](t, mux, http.MethodPost, "/api/v1/projects/9/categories", map[string]any{}, http.StatusBadRequest)
	require.Equal(t, "CATEGORY_INVALID", validation["error"])
}

func TestStablePropertyErrorsMapToBadRequest(t *testing.T) {
	snapshot := tracking.Snapshot{PointID: 1, Version: 1, SupportedPlatforms: []string{"web"}, Properties: []tracking.Property{{Code: "id", Name: "ID", Type: tracking.String, Required: true}, {Code: "kind", Name: "Kind", Type: tracking.Enum, EnumValues: []string{"a"}}}}
	cases := []error{snapshot.ValidateProperties(nil), snapshot.ValidateProperties(map[string]any{"id": true}), snapshot.ValidateProperties(map[string]any{"id": "1", "kind": "b"}), snapshot.ValidateProperties(map[string]any{"id": "1", "extra": true}), snapshot.Validate("ios", map[string]any{"id": "1"})}
	for _, err := range cases {
		rec := httptest.NewRecorder()
		trackingError(rec, err)
		require.Equal(t, http.StatusBadRequest, rec.Code, err.Error())
		require.NotContains(t, rec.Body.String(), "tracking service unavailable")
	}
}

func postCategory(t *testing.T, h http.Handler, in tracking.CreateCategory) tracking.Category {
	return requestJSON[tracking.Category](t, h, http.MethodPost, "/api/v1/projects/9/categories", in, http.StatusCreated)
}
func requestJSON[T any](t *testing.T, h http.Handler, method, path string, body any, want int) T {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, auth.Principal{UserID: 1}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, want, rec.Code, rec.Body.String())
	var result T
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	}
	return result
}

type trackingAllowAll struct{}

func (trackingAllowAll) RequireRole(context.Context, uint64, uint64, ...project.Role) error {
	return nil
}

type trackingDenyWrites struct{}

func (trackingDenyWrites) RequireRole(_ context.Context, _ uint64, _ uint64, allowed ...project.Role) error {
	if len(allowed) == 1 && allowed[0] == project.Admin {
		return project.ErrForbidden
	}
	return nil
}

type memoryTrackingRepo struct {
	next       uint64
	categories map[uint64]tracking.Category
	points     map[uint64]tracking.Point
	findErr    error
}

func (r *memoryTrackingRepo) CreateCategory(_ context.Context, c tracking.Category) (tracking.Category, error) {
	r.next++
	c.ID = r.next
	r.categories[c.ID] = c
	return c, nil
}
func (r *memoryTrackingRepo) FindCategory(_ context.Context, id uint64) (tracking.Category, error) {
	c, ok := r.categories[id]
	if !ok {
		return c, tracking.ErrCategoryNotFound
	}
	return c, nil
}
func (r *memoryTrackingRepo) FindCategoryInProject(ctx context.Context, projectID, id uint64) (tracking.Category, error) {
	c, err := r.FindCategory(ctx, id)
	if err != nil || c.ProjectID != projectID {
		return tracking.Category{}, tracking.ErrCategoryNotFound
	}
	return c, nil
}
func (r *memoryTrackingRepo) ListCategories(_ context.Context, projectID uint64) ([]tracking.Category, error) {
	items := []tracking.Category{}
	for _, item := range r.categories {
		if item.ProjectID == projectID {
			items = append(items, item)
		}
	}
	return items, nil
}
func (r *memoryTrackingRepo) UpdateCategory(_ context.Context, projectID, id, _ uint64, in tracking.UpdateCategory) (tracking.Category, error) {
	item, ok := r.categories[id]
	if !ok || item.ProjectID != projectID {
		return tracking.Category{}, tracking.ErrCategoryNotFound
	}
	item.Name, item.Description, item.SortOrder, item.Active = in.Name, in.Description, in.SortOrder, in.Active
	r.categories[id] = item
	return item, nil
}
func (r *memoryTrackingRepo) ListPoints(_ context.Context, projectID uint64, _ tracking.PointFilter) (tracking.PointList, error) {
	items := []tracking.Point{}
	for _, item := range r.points {
		if item.ProjectID == projectID {
			items = append(items, item)
		}
	}
	return tracking.PointList{Items: items, Total: len(items)}, nil
}
func (r *memoryTrackingRepo) CreateDraft(_ context.Context, p tracking.Point) (tracking.Point, error) {
	r.next++
	p.ID = r.next
	r.points[p.ID] = p
	return p, nil
}
func (r *memoryTrackingRepo) FindPoint(_ context.Context, id uint64) (tracking.Point, error) {
	if r.findErr != nil {
		return tracking.Point{}, r.findErr
	}
	p, ok := r.points[id]
	if !ok {
		return p, tracking.ErrPointNotFound
	}
	return p, nil
}
func (r *memoryTrackingRepo) FindPointInProject(ctx context.Context, projectID, id uint64) (tracking.Point, error) {
	p, err := r.FindPoint(ctx, id)
	if err != nil {
		return tracking.Point{}, err
	}
	if p.ProjectID != projectID {
		return tracking.Point{}, tracking.ErrPointNotFound
	}
	return p, nil
}
func (r *memoryTrackingRepo) UpdateDraft(_ context.Context, p tracking.Point) (tracking.Point, error) {
	old := r.points[p.ID]
	if old.State != tracking.Draft {
		return p, tracking.ErrImmutable
	}
	r.points[p.ID] = p
	return p, nil
}
func (r *memoryTrackingRepo) DeleteDraft(_ context.Context, projectID, id, actor uint64) error {
	if r.points[id].State != tracking.Draft {
		return tracking.ErrImmutable
	}
	delete(r.points, id)
	return nil
}
func (r *memoryTrackingRepo) Publish(_ context.Context, projectID, id, actor uint64) (tracking.Snapshot, error) {
	p := r.points[id]
	if p.State != tracking.Draft {
		return tracking.Snapshot{}, tracking.ErrImmutable
	}
	if err := p.ValidateForPublication(); err != nil {
		return tracking.Snapshot{}, err
	}
	p.State = tracking.Published
	p.PublishedVersion++
	r.points[id] = p
	return p.Snapshot(actor, p.PublishedVersion), nil
}
func (r *memoryTrackingRepo) Deprecate(_ context.Context, projectID, id, actor uint64, reason string, replacement *uint64) error {
	p := r.points[id]
	if p.State != tracking.Published {
		return tracking.ErrPointNotPublished
	}
	p.State = tracking.Deprecated
	p.DeprecationReason = reason
	p.ReplacementPointID = replacement
	r.points[id] = p
	return nil
}
func (r *memoryTrackingRepo) UpdateDevelopmentState(_ context.Context, projectID, id, actor uint64, state tracking.DevelopmentState) (tracking.Point, error) {
	p, ok := r.points[id]
	if !ok || p.ProjectID != projectID {
		return tracking.Point{}, tracking.ErrPointNotFound
	}
	p.DevelopmentState = state
	r.points[id] = p
	return p, nil
}
