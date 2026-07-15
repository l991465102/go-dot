package httpapi

import (
	"net/http"
	"strconv"

	v1 "github.com/lzw7758/go-dot/api/projects/v1"
	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/project"
)

type ProjectController struct{ service *project.Service }

func NewProjectController(service *project.Service) ProjectController {
	return ProjectController{service: service}
}
func (c ProjectController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects", c.list)
	mux.HandleFunc("POST /api/v1/projects", c.create)
	mux.HandleFunc("POST /api/v1/projects/{id}/rotate-write-key", c.rotateWriteKey)
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
		writeError(w, http.StatusBadRequest, err.Error())
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
		if err == project.ErrForbidden {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "rotate write key failed")
		return
	}
	writeJSON(w, http.StatusOK, rotation)
}

func NewHandler(authService *auth.Service, projectService *project.Service, secureCookie bool) http.Handler {
	mux := http.NewServeMux()
	NewAuthController(authService, secureCookie).Register(mux)
	NewProjectController(projectService).Register(mux)
	return NewAuthMiddleware(authService).Wrap(mux)
}
