package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/lzw7758/go-dot/api/projects/v1"
	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/lzw7758/go-dot/internal/tracking"
)

type ProjectController struct{ service *project.Service }

func NewProjectController(service *project.Service) ProjectController {
	return ProjectController{service: service}
}
func (c ProjectController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects", c.list)
	mux.HandleFunc("POST /api/v1/projects", c.create)
	mux.HandleFunc("POST /api/v1/projects/{id}/rotate-write-key", c.rotateWriteKey)
	mux.HandleFunc("PATCH /api/v1/projects/{id}/settings", c.updateSettings)
	mux.HandleFunc("GET /api/v1/projects/{id}/members", c.listMembers)
	mux.HandleFunc("POST /api/v1/projects/{id}/members", c.addMember)
	mux.HandleFunc("DELETE /api/v1/projects/{id}/members/{userID}", c.removeMember)
	mux.HandleFunc("GET /api/v1/projects/{id}/write-key-status", c.writeKeyStatus)
}
func projectID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || id == 0 {
		writeError(w, 400, "invalid project id")
		return 0, false
	}
	return id, true
}
func projectPrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized")
	}
	return p, ok
}
func (c ProjectController) updateSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := projectPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	var req project.SettingsInput
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := c.service.UpdateSettings(r.Context(), p.UserID, id, req)
	if err != nil {
		projectMutationError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (c ProjectController) listMembers(w http.ResponseWriter, r *http.Request) {
	p, ok := projectPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	items, err := c.service.ListMembers(r.Context(), p.UserID, id)
	if err != nil {
		projectMutationError(w, err)
		return
	}
	if items == nil {
		items = []project.Member{}
	}
	writeJSON(w, 200, map[string]any{"members": items})
}
func (c ProjectController) addMember(w http.ResponseWriter, r *http.Request) {
	p, ok := projectPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	var req project.MemberInput
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := c.service.AddMember(r.Context(), p.UserID, id, req); err != nil {
		projectMutationError(w, err)
		return
	}
	w.WriteHeader(201)
}
func (c ProjectController) removeMember(w http.ResponseWriter, r *http.Request) {
	p, ok := projectPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(r.PathValue("userID"), 10, 64)
	if err != nil || userID == 0 {
		writeError(w, 400, "invalid user id")
		return
	}
	if err = c.service.RemoveMember(r.Context(), p.UserID, id, userID); err != nil {
		projectMutationError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (c ProjectController) writeKeyStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := projectPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	result, err := c.service.GetWriteKeyStatus(r.Context(), p.UserID, id)
	if err != nil {
		projectMutationError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func projectMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, project.ErrForbidden):
		writeError(w, 403, "forbidden")
	case errors.Is(err, project.ErrInvalidProject):
		writeError(w, 400, "invalid project settings")
	case errors.Is(err, project.ErrProjectNotFound):
		writeError(w, 404, "resource not found")
	default:
		writeError(w, 500, "project service unavailable")
	}
}
func (c ProjectController) list(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	projects, err := c.service.List(r.Context(), p.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list projects failed")
		return
	}
	if projects == nil {
		projects = []project.Project{}
	}
	writeJSON(w, http.StatusOK, v1.ListRes{Projects: projects})
}
func (c ProjectController) create(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req v1.CreateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	created, err := c.service.Create(r.Context(), p.UserID, req)
	if err != nil {
		if errors.Is(err, project.ErrInvalidProject) {
			writeError(w, http.StatusBadRequest, "invalid project")
		} else {
			writeError(w, http.StatusInternalServerError, "project service unavailable")
		}
		return
	}
	writeJSON(w, http.StatusCreated, created)
}
func (c ProjectController) rotateWriteKey(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	rotation, err := c.service.RotateWriteKey(r.Context(), p.UserID, id)
	if err != nil {
		if errors.Is(err, project.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, project.ErrWriteKeyRotationInProgress) {
			writeError(w, http.StatusConflict, "write key rotation already in progress")
			return
		}
		if errors.Is(err, project.ErrProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "rotate write key failed")
		return
	}
	writeJSON(w, http.StatusOK, rotation)
}

func NewHandler(authService *auth.Service, projectService *project.Service, secureCookie bool, managementServices ...any) http.Handler {
	mux := http.NewServeMux()
	NewAuthController(authService, secureCookie).Register(mux)
	NewProjectController(projectService).Register(mux)
	for _, service := range managementServices {
		switch typed := service.(type) {
		case *tracking.Service:
			if typed != nil {
				NewTrackingController(typed).Register(mux)
			}
		case *statistics.Service:
			if typed != nil {
				NewStatisticsController(typed).Register(mux)
				NewDiagnosticsController(typed).Register(mux)
			}
		case *overview.Service:
			if typed != nil {
				NewOverviewController(typed).Register(mux)
			}
		case *reporting.Service:
			if typed != nil {
				NewReportController(typed).Register(mux)
			}
		}
	}
	return NewAuthMiddleware(authService).Wrap(mux)
}
