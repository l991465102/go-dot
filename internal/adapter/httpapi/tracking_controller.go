package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	v1 "github.com/lzw7758/go-dot/api/tracking/v1"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

type TrackingController struct{ service *tracking.Service }

func NewTrackingController(service *tracking.Service) TrackingController {
	return TrackingController{service: service}
}
func (c TrackingController) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tracking/categories", c.createCategory)
	mux.HandleFunc("POST /api/v1/tracking/points", c.saveDraft)
	mux.HandleFunc("GET /api/v1/tracking/points/{id}", c.getPoint)
	mux.HandleFunc("PATCH /api/v1/tracking/points/{id}", c.updateDraft)
	mux.HandleFunc("DELETE /api/v1/tracking/points/{id}", c.deleteDraft)
	mux.HandleFunc("POST /api/v1/tracking/points/{id}/publish", c.publish)
	mux.HandleFunc("POST /api/v1/tracking/points/{id}/copy", c.copyPoint)
	mux.HandleFunc("POST /api/v1/tracking/points/{id}/deprecate", c.deprecate)
}
func principalAndID(w http.ResponseWriter, r *http.Request) (uint64, uint64, bool) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return 0, 0, false
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid point id")
		return 0, 0, false
	}
	return p.UserID, id, true
}
func principalID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return 0, false
	}
	return p.UserID, true
}
func (c TrackingController) createCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalID(w, r)
	if !ok {
		return
	}
	var req v1.CreateCategoryReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.CreateCategory(r.Context(), actor, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (c TrackingController) saveDraft(w http.ResponseWriter, r *http.Request) {
	actor, ok := principalID(w, r)
	if !ok {
		return
	}
	var req v1.SaveDraftReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.SaveDraft(r.Context(), actor, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (c TrackingController) getPoint(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	result, err := c.service.Get(r.Context(), actor, id)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) updateDraft(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.SaveDraftReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.UpdateDraft(r.Context(), actor, id, req)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) deleteDraft(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	if err := c.service.DeleteDraft(r.Context(), actor, id); err != nil {
		trackingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c TrackingController) publish(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	result, err := c.service.Publish(r.Context(), actor, id)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (c TrackingController) copyPoint(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.CopyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.Copy(r.Context(), actor, id, req.Code)
	if err != nil {
		trackingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
func (c TrackingController) deprecate(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var req v1.DeprecateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := c.service.Deprecate(r.Context(), actor, id, req.Reason, req.ReplacementID); err != nil {
		trackingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func trackingError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, project.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, tracking.ErrPointNotFound), errors.Is(err, tracking.ErrCategoryNotFound):
		status = http.StatusNotFound
	case errors.Is(err, tracking.ErrImmutable), errors.Is(err, tracking.ErrPointNotPublished):
		status = http.StatusConflict
	case strings.Contains(err.Error(), "duplicate"):
		status = http.StatusConflict
	case strings.Contains(err.Error(), "_INVALID"), strings.Contains(err.Error(), "_REQUIRED"):
		status = http.StatusBadRequest
	}
	writeError(w, status, err.Error())
}
