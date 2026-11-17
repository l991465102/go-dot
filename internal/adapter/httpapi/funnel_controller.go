package httpapi

import (
	"errors"
	"net/http"

	"github.com/lzw7758/go-dot/internal/funnel"
	"github.com/lzw7758/go-dot/internal/project"
)

type FunnelController struct{ service *funnel.Service }

// NewFunnelController 创建漏斗分析接口。
func NewFunnelController(s *funnel.Service) FunnelController { return FunnelController{service: s} }

// Register 注册漏斗查询接口。
func (c FunnelController) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/statistics/funnel", c.query)
}

// query 处理漏斗分析请求。
func (c FunnelController) query(w http.ResponseWriter, r *http.Request) {
	actor, ok := statisticsActor(w, r)
	if !ok {
		return
	}
	var q funnel.Query
	if !decodeJSON(w, r, &q) {
		return
	}
	result, err := c.service.Run(r.Context(), actor, q)
	if errors.Is(err, funnel.ErrInvalid) {
		writeAPIError(w, r, 400, "invalid funnel query")
		return
	}
	if errors.Is(err, project.ErrForbidden) {
		writeAPIError(w, r, 403, "forbidden")
		return
	}
	if err != nil {
		writeAPIError(w, r, 500, "funnel service unavailable")
		return
	}
	writeJSON(w, 200, result)
}
