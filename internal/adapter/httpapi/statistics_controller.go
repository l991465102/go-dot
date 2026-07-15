package httpapi

import (
	"context"
	"errors"
	"net/http"

	statisticsv1 "github.com/lzw7758/go-dot/api/statistics/v1"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/lzw7758/go-dot/internal/tracking"
)

type StatisticsController struct{ service *statistics.Service }

func NewStatisticsController(service *statistics.Service) StatisticsController {
	return StatisticsController{service: service}
}
func (c StatisticsController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/statistics/realtime", c.realtime)
	mux.HandleFunc("GET /api/v1/statistics/summary", c.summary)
	mux.HandleFunc("GET /api/v1/statistics/trend", c.trend)
}
func (c StatisticsController) realtime(w http.ResponseWriter, r *http.Request) {
	c.summaryLike(w, r, c.service.Realtime)
}
func (c StatisticsController) summary(w http.ResponseWriter, r *http.Request) {
	c.summaryLike(w, r, c.service.Summary)
}
func (c StatisticsController) summaryLike(w http.ResponseWriter, r *http.Request, run func(context.Context, uint64, statistics.Query) (ports.Summary, error)) {
	actor, ok := statisticsActor(w, r)
	if !ok {
		return
	}
	query, err := parseStatisticsQuery(r)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	result, err := run(r.Context(), actor, query)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, statisticsv1.SummaryRes(result))
}
func (c StatisticsController) trend(w http.ResponseWriter, r *http.Request) {
	actor, ok := statisticsActor(w, r)
	if !ok {
		return
	}
	query, err := parseStatisticsQuery(r)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	result, err := c.service.Trend(r.Context(), actor, query)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	if result == nil {
		result = []ports.TrendPoint{}
	}
	writeJSON(w, http.StatusOK, statisticsv1.TrendRes{Items: result})
}
func statisticsActor(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeAPIError(w, r, 401, "unauthorized")
		return 0, false
	}
	return principal.UserID, true
}
func statisticsError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "statistics service unavailable"
	switch {
	case errors.Is(err, statistics.ErrInvalidQuery):
		status, message = 400, "invalid query"
	case errors.Is(err, project.ErrForbidden):
		status, message = 403, "forbidden"
	case errors.Is(err, tracking.ErrPointNotFound), errors.Is(err, tracking.ErrPointNotPublished), errors.Is(err, project.ErrProjectNotFound):
		status, message = 404, "resource not found"
	case errors.Is(err, tracking.ErrImmutable), errors.Is(err, tracking.ErrConflict):
		status, message = 409, "conflict"
	case errors.Is(err, statistics.ErrInputTooLarge):
		status, message = 413, "input too large"
	case errors.Is(err, statistics.ErrRateLimited):
		status, message = 429, "rate limit exceeded"
	}
	writeAPIError(w, r, status, message)
}
