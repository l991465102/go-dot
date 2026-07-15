package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	v1 "github.com/lzw7758/go-dot/api/projects/v1"
	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/lzw7758/go-dot/internal/project"
)

type OverviewController struct{ service *overview.Service }

func NewOverviewController(s *overview.Service) OverviewController { return OverviewController{s} }
func (c OverviewController) Register(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/projects/{id}/overview", c.get)
}
func (c OverviewController) get(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || id == 0 {
		writeError(w, 400, "invalid project id")
		return
	}
	result, err := c.service.Get(r.Context(), p.UserID, id)
	if err != nil {
		if errors.Is(err, project.ErrForbidden) {
			writeError(w, 403, "forbidden")
		} else {
			writeError(w, 500, "project overview unavailable")
		}
		return
	}
	items := make([]v1.OverviewPoint, 0, len(result.NoReportPoints))
	for _, item := range result.NoReportPoints {
		items = append(items, v1.OverviewPoint{ID: item.ID, Code: item.Code, Name: item.Name, PublishedAt: item.PublishedAt})
	}
	writeJSON(w, 200, v1.OverviewRes{Window: v1.OverviewWindow{Start: result.Window.Start, End: result.Window.End}, InvalidCount: result.InvalidCount, DeadLetterCount: result.DeadLetterCount, Queue: v1.OverviewQueue{Pending: result.Queue.Pending, OldestPendingAgeSeconds: int64(result.Queue.OldestPendingAge / time.Second)}, NoReportPoints: items, NoReportThresholdHours: 24})
}
