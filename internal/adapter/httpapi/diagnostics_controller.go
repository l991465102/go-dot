package httpapi

import (
	"net/http"

	diagnosticsv1 "github.com/lzw7758/go-dot/api/diagnostics/v1"
	"github.com/lzw7758/go-dot/internal/statistics"
)

type DiagnosticsController struct{ service *statistics.Service }

func NewDiagnosticsController(service *statistics.Service) DiagnosticsController {
	return DiagnosticsController{service: service}
}
func (c DiagnosticsController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/diagnostics/recent-events", c.recent)
	mux.HandleFunc("GET /api/v1/diagnostics/invalid-events", c.invalid)
	mux.HandleFunc("GET /api/v1/diagnostics/dead-letters", c.deadLetters)
}
func (c DiagnosticsController) recent(w http.ResponseWriter, r *http.Request) {
	actor, ok := statisticsActor(w, r)
	if !ok {
		return
	}
	query, err := parseDiagnosticQuery(r)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	result, err := c.service.RecentEvents(r.Context(), actor, query)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	if result.Items == nil {
		result.Items = []statistics.Event{}
	}
	writeJSON(w, 200, diagnosticsv1.RecentEventsRes(result))
}
func (c DiagnosticsController) invalid(w http.ResponseWriter, r *http.Request) {
	actor, ok := statisticsActor(w, r)
	if !ok {
		return
	}
	query, err := parseDiagnosticQuery(r)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	result, err := c.service.InvalidEvents(r.Context(), actor, query)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	if result.Items == nil {
		result.Items = []statistics.InvalidEvent{}
	}
	writeJSON(w, 200, diagnosticsv1.InvalidEventsRes(result))
}
func (c DiagnosticsController) deadLetters(w http.ResponseWriter, r *http.Request) {
	actor, ok := statisticsActor(w, r)
	if !ok {
		return
	}
	projectID, cursor, limit, err := parseProjectPage(r)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	result, err := c.service.DeadLetters(r.Context(), actor, projectID, cursor, limit)
	if err != nil {
		statisticsError(w, r, err)
		return
	}
	if result.Items == nil {
		result.Items = []statistics.DeadLetter{}
	}
	writeJSON(w, 200, diagnosticsv1.DeadLettersRes(result))
}
