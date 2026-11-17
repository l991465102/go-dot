package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-sql-driver/mysql"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/requirements"
)

type RequirementController struct{ service *requirements.Service }

// NewRequirementController 创建需求管理接口。
func NewRequirementController(s *requirements.Service) RequirementController {
	return RequirementController{service: s}
}

// Register 注册需求与任务接口。
func (c RequirementController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{projectID}/requirements", c.list)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/requirements", c.create)
	mux.HandleFunc("PUT /api/v1/projects/{projectID}/requirements/{id}", c.update)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/requirements/{id}/tasks", c.tasks)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/requirements/{id}/tasks", c.createTask)
	mux.HandleFunc("PUT /api/v1/projects/{projectID}/requirements/{id}/tasks/{taskID}", c.updateTask)
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}/requirements/{id}/tasks/{taskID}", c.deleteTask)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/requirements/{id}/tasks/{taskID}/acceptances", c.acceptance)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/requirements/{id}/tasks/{taskID}/acceptances", c.acceptance)
}

// update 更新需求。
func (c RequirementController) update(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var in requirements.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := c.service.Update(r.Context(), a, p, id, in); err != nil {
		requirementError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func requirementError(w http.ResponseWriter, err error) {
	var me *mysql.MySQLError
	switch {
	case errors.Is(err, project.ErrForbidden):
		writeError(w, 403, "forbidden")
	case errors.Is(err, requirements.ErrInvalid):
		writeError(w, 400, "invalid requirement or task")
	case errors.Is(err, requirements.ErrNotFound):
		writeError(w, 404, "not found")
	case errors.Is(err, requirements.ErrConflict):
		writeError(w, 409, "task is in use or cannot be changed")
	case errors.As(err, &me) && me.Number == 1062:
		writeError(w, 409, "task already exists")
	default:
		writeError(w, 500, "requirement service unavailable")
	}
}

// list 列出项目需求。
func (c RequirementController) list(w http.ResponseWriter, r *http.Request) {
	a, p, ok := principalID(w, r)
	if !ok {
		return
	}
	var before uint64
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		var err error
		before, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || before == 0 {
			writeError(w, 400, "invalid cursor")
			return
		}
	}
	v, e := c.service.List(r.Context(), a, p, before)
	if e != nil {
		requirementError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

// create 创建需求。
func (c RequirementController) create(w http.ResponseWriter, r *http.Request) {
	a, p, ok := principalID(w, r)
	if !ok {
		return
	}
	var in requirements.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	id, e := c.service.Create(r.Context(), a, p, in)
	if e != nil {
		requirementError(w, e)
		return
	}
	writeJSON(w, 201, map[string]uint64{"id": id})
}

// tasks 列出接入任务。
func (c RequirementController) tasks(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	v, e := c.service.Tasks(r.Context(), a, p, id)
	if e != nil {
		requirementError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

// createTask 创建接入任务。
func (c RequirementController) createTask(w http.ResponseWriter, r *http.Request) {
	c.saveTask(w, r, 0)
}

// updateTask 更新接入任务。
func (c RequirementController) updateTask(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseUint(r.PathValue("taskID"), 10, 64)
	if e != nil || id == 0 {
		writeError(w, 400, "invalid task id")
		return
	}
	c.saveTask(w, r, id)
}

// saveTask 解析并保存接入任务。
func (c RequirementController) saveTask(w http.ResponseWriter, r *http.Request, taskID uint64) {
	a, p, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	var in requirements.TaskInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := c.service.SaveTask(r.Context(), a, p, id, taskID, in); e != nil {
		requirementError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// acceptance 读取或添加任务验收记录。
func (c RequirementController) acceptance(w http.ResponseWriter, r *http.Request) {
	actor, p, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	taskID, err := strconv.ParseUint(r.PathValue("taskID"), 10, 64)
	if err != nil || taskID == 0 {
		writeError(w, 400, "invalid task id")
		return
	}
	if r.Method == http.MethodGet {
		rows, e := c.service.ListAcceptances(r.Context(), actor, p, id, taskID)
		if e != nil {
			requirementError(w, e)
			return
		}
		writeJSON(w, 200, rows)
		return
	}
	var in requirements.AcceptanceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := c.service.AddAcceptance(r.Context(), actor, p, id, taskID, in); e != nil {
		requirementError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteTask 删除未验收任务。
func (c RequirementController) deleteTask(w http.ResponseWriter, r *http.Request) {
	actor, p, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	taskID, err := strconv.ParseUint(r.PathValue("taskID"), 10, 64)
	if err != nil || taskID == 0 {
		writeError(w, 400, "invalid task id")
		return
	}
	if err = c.service.DeleteTask(r.Context(), actor, p, id, taskID); err != nil {
		requirementError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
