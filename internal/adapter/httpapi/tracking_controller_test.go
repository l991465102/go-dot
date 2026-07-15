package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
	in := tracking.DraftInput{ProjectID: 9, CategoryID: scene.ID, Code: "product_detail_view", Name: "Product detail", BehaviorType: "VIEW", SupportedPlatforms: []string{"WEB"}, Properties: []tracking.Property{{Code: "product_id", Name: "Product ID", Type: tracking.String, Required: true}}}
	draft := requestJSON[tracking.Point](t, mux, http.MethodPost, "/api/v1/tracking/points", in, http.StatusCreated)
	requestJSON[tracking.Snapshot](t, mux, http.MethodPost, "/api/v1/tracking/points/"+strconv.FormatUint(draft.ID, 10)+"/publish", nil, http.StatusOK)
	requestJSON[map[string]string](t, mux, http.MethodPatch, "/api/v1/tracking/points/"+strconv.FormatUint(draft.ID, 10), in, http.StatusConflict)
	requestJSON[map[string]string](t, mux, http.MethodDelete, "/api/v1/tracking/points/"+strconv.FormatUint(draft.ID, 10), nil, http.StatusConflict)
	replacement := requestJSON[tracking.Point](t, mux, http.MethodPost, "/api/v1/tracking/points/"+strconv.FormatUint(draft.ID, 10)+"/copy", map[string]any{"code": "product_detail_view_v2"}, http.StatusCreated)
	requestJSON[tracking.Snapshot](t, mux, http.MethodPost, "/api/v1/tracking/points/"+strconv.FormatUint(replacement.ID, 10)+"/publish", nil, http.StatusOK)
	requestJSON[any](t, mux, http.MethodPost, "/api/v1/tracking/points/"+strconv.FormatUint(draft.ID, 10)+"/deprecate", map[string]any{"reason": "replaced", "replacement_id": replacement.ID}, http.StatusNoContent)
	original := requestJSON[tracking.Point](t, mux, http.MethodGet, "/api/v1/tracking/points/"+strconv.FormatUint(draft.ID, 10), nil, http.StatusOK)
	copy := requestJSON[tracking.Point](t, mux, http.MethodGet, "/api/v1/tracking/points/"+strconv.FormatUint(replacement.ID, 10), nil, http.StatusOK)
	require.Equal(t, tracking.Deprecated, original.State)
	require.Equal(t, tracking.Published, copy.State)
}

func postCategory(t *testing.T, h http.Handler, in tracking.CreateCategory) tracking.Category {
	return requestJSON[tracking.Category](t, h, http.MethodPost, "/api/v1/tracking/categories", in, http.StatusCreated)
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

type memoryTrackingRepo struct {
	next       uint64
	categories map[uint64]tracking.Category
	points     map[uint64]tracking.Point
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
func (r *memoryTrackingRepo) CreateDraft(_ context.Context, p tracking.Point) (tracking.Point, error) {
	r.next++
	p.ID = r.next
	r.points[p.ID] = p
	return p, nil
}
func (r *memoryTrackingRepo) FindPoint(_ context.Context, id uint64) (tracking.Point, error) {
	p, ok := r.points[id]
	if !ok {
		return p, tracking.ErrPointNotFound
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
func (r *memoryTrackingRepo) DeleteDraft(_ context.Context, id uint64) error {
	if r.points[id].State != tracking.Draft {
		return tracking.ErrImmutable
	}
	delete(r.points, id)
	return nil
}
func (r *memoryTrackingRepo) Publish(_ context.Context, id, actor uint64) (tracking.Snapshot, error) {
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
func (r *memoryTrackingRepo) Deprecate(_ context.Context, id, actor uint64, reason string, replacement *uint64) error {
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
