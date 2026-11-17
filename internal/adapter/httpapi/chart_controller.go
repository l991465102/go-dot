package httpapi

import (
	"errors"
	"net/http"

	"github.com/lzw7758/go-dot/internal/charts"
	"github.com/lzw7758/go-dot/internal/project"
)

type ChartController struct{ service *charts.Service }

// NewChartController 创建已保存图表接口。
func NewChartController(s *charts.Service) ChartController { return ChartController{service: s} }

// Register 注册图表列表、创建和删除接口。
func (c ChartController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{projectID}/charts", c.list)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/charts", c.create)
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}/charts/{id}", c.delete)
}
func chartError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, project.ErrForbidden):
		writeError(w, 403, "forbidden")
	case errors.Is(err, charts.ErrInvalid):
		writeError(w, 400, "invalid chart")
	case errors.Is(err, charts.ErrNotFound):
		writeError(w, 404, "not found")
	default:
		writeError(w, 500, "chart service unavailable")
	}
}

// list 列出已保存图表。
func (c ChartController) list(w http.ResponseWriter, r *http.Request) {
	a, p, ok := principalID(w, r)
	if !ok {
		return
	}
	v, err := c.service.List(r.Context(), a, p, r.URL.Query().Get("kind"))
	if err != nil {
		chartError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// create 保存图表配置。
func (c ChartController) create(w http.ResponseWriter, r *http.Request) {
	a, p, ok := principalID(w, r)
	if !ok {
		return
	}
	var in charts.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	id, err := c.service.Create(r.Context(), a, p, in)
	if err != nil {
		chartError(w, err)
		return
	}
	writeJSON(w, 201, map[string]uint64{"id": id})
}

// delete 删除图表配置。
func (c ChartController) delete(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := principalAndID(w, r)
	if !ok {
		return
	}
	if err := c.service.Delete(r.Context(), a, p, id); err != nil {
		chartError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
