package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/lzw7758/go-dot/api/tracking/v1"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

type TrackingController struct{ service *tracking.Service }

func NewTrackingController(service *tracking.Service) TrackingController {
	return TrackingController{service: service}
}
func (c TrackingController) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/projects/{projectID}/categories", c.createCategory)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/categories", c.listCategories)
	mux.HandleFunc("PATCH /api/v1/projects/{projectID}/categories/{id}", c.updateCategory)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/points", c.saveDraft)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/points", c.listPoints)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/points/{id}", c.getPoint)
	mux.HandleFunc("PATCH /api/v1/projects/{projectID}/points/{id}", c.updateDraft)
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}/points/{id}", c.deleteDraft)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/points/{id}/publish", c.publish)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/points/{id}/copy", c.copyPoint)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/points/{id}/deprecate", c.deprecate)
	mux.HandleFunc("PATCH /api/v1/projects/{projectID}/points/{id}/development-state", c.updateDevelopmentState)
}
func (c TrackingController) listCategories(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := principalID(w, r)
	if !ok {
		return
	}
	result, err := c.service.ListCategories(r.Context(), actor, projectID)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) updateCategory(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.UpdateCategoryReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.UpdateCategory(r.Context(), actor, projectID, id, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) listPoints(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := principalID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := tracking.PointFilter{Query: q.Get("q"), DefinitionState: tracking.DefinitionState(q.Get("definition_state")), DevelopmentState: tracking.DevelopmentState(q.Get("development_state")), BehaviorType: tracking.BehaviorType(q.Get("behavior_type")), Platform: q.Get("platform"), Owner: q.Get("owner"), CreatedFrom: q.Get("created_from"), CreatedTo: q.Get("created_to")}
	filter.CategoryID, _ = strconv.ParseUint(q.Get("category_id"), 10, 64)
	filter.Page, _ = strconv.Atoi(q.Get("page"))
	filter.PageSize, _ = strconv.Atoi(q.Get("page_size"))
	result, err := c.service.ListPoints(r.Context(), actor, projectID, filter)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func principalAndID(w http.ResponseWriter, r *http.Request) (uint64, uint64, uint64, bool) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return 0, 0, 0, false
	}
	projectID, err := strconv.ParseUint(r.PathValue("projectID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return 0, 0, 0, false
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid point id")
		return 0, 0, 0, false
	}
	return p.UserID, projectID, id, true
}
func principalID(w http.ResponseWriter, r *http.Request) (uint64, uint64, bool) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return 0, 0, false
	}
	projectID, err := strconv.ParseUint(r.PathValue("projectID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return 0, 0, false
	}
	return p.UserID, projectID, true
}
func (c TrackingController) createCategory(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := principalID(w, r)
	if !ok {
		return
	}
	var req v1.CreateCategoryReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.ProjectID = projectID
	result, err := c.service.CreateCategory(r.Context(), actor, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (c TrackingController) saveDraft(w http.ResponseWriter, r *http.Request) {
	actor, projectID, ok := principalID(w, r)
	if !ok {
		return
	}
	var req v1.SaveDraftReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.ProjectID = projectID
	result, err := c.service.SaveDraft(r.Context(), actor, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (c TrackingController) getPoint(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	result, err := c.service.GetInProject(r.Context(), actor, projectID, id)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) updateDraft(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.SaveDraftReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.UpdateDraftInProject(r.Context(), actor, projectID, id, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) deleteDraft(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	if err := c.service.DeleteDraftInProject(r.Context(), actor, projectID, id); err != nil {
		trackingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c TrackingController) publish(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	result, err := c.service.PublishInProject(r.Context(), actor, projectID, id)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) copyPoint(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.CopyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.CopyInProject(r.Context(), actor, projectID, id, req.Code)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (c TrackingController) deprecate(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.DeprecateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := c.service.DeprecateInProject(r.Context(), actor, projectID, id, req.Reason, req.ReplacementID); err != nil {
		trackingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c TrackingController) updateDevelopmentState(w http.ResponseWriter, r *http.Request) {
	actor, projectID, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.DevelopmentStateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := c.service.UpdateDevelopmentState(r.Context(), actor, projectID, id, req.State)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
func trackingError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "tracking service unavailable"
	switch {
	case errors.Is(err, project.ErrForbidden):
		status = http.StatusForbidden
		message = "forbidden"
	case errors.Is(err, tracking.ErrPointNotFound):
		status = http.StatusNotFound
		message = "POINT_NOT_FOUND"
	case errors.Is(err, tracking.ErrCategoryNotFound):
		status = http.StatusNotFound
		message = "CATEGORY_NOT_FOUND"
	case errors.Is(err, tracking.ErrImmutable), errors.Is(err, tracking.ErrPointNotPublished), errors.Is(err, tracking.ErrConflict):
		status = http.StatusConflict
		message = err.Error()
	default:
		if code, ok := tracking.ErrorCode(err); ok {
			if code == "CATEGORY_IN_USE" {
				status = http.StatusConflict
			} else {
				status = http.StatusBadRequest
			}
			message = code
		}
	}
	writeError(w, status, message)
}
