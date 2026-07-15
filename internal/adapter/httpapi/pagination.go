package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/statistics"
)

func parseStatisticsQuery(r *http.Request) (statistics.Query, error) {
	projectID, err := strconv.ParseUint(r.URL.Query().Get("project_id"), 10, 64)
	if err != nil || projectID == 0 {
		return statistics.Query{}, fmt.Errorf("%w: project_id", statistics.ErrInvalidQuery)
	}
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	if err != nil {
		return statistics.Query{}, fmt.Errorf("%w: start", statistics.ErrInvalidQuery)
	}
	end, err := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
	if err != nil {
		return statistics.Query{}, fmt.Errorf("%w: end", statistics.ErrInvalidQuery)
	}
	return statistics.Query{ProjectID: projectID, Start: start, End: end, Granularity: r.URL.Query().Get("granularity"), Timezone: defaultString(r.URL.Query().Get("timezone"), "UTC"),
		DomainCategoryCode: r.URL.Query().Get("domain_category_code"), ModuleCategoryCode: r.URL.Query().Get("module_category_code"), SceneCategoryCode: r.URL.Query().Get("scene_category_code"),
		PointCodes: queryList(r, "point_code"), Platform: r.URL.Query().Get("platform"), Channel: r.URL.Query().Get("channel")}, nil
}

func parseDiagnosticQuery(r *http.Request) (statistics.DiagnosticQuery, error) {
	base, err := parseStatisticsQuery(r)
	if err != nil {
		return statistics.DiagnosticQuery{}, err
	}
	limit, err := parseLimit(r)
	if err != nil {
		return statistics.DiagnosticQuery{}, err
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 2048 {
		return statistics.DiagnosticQuery{}, fmt.Errorf("%w: cursor", statistics.ErrInvalidQuery)
	}
	return statistics.DiagnosticQuery{ProjectID: base.ProjectID, Start: base.Start, End: base.End, PointCodes: base.PointCodes, Platform: base.Platform, Channel: base.Channel, ErrorCode: r.URL.Query().Get("error_code"), Cursor: cursor, Limit: limit}, nil
}
func parseProjectPage(r *http.Request) (uint64, string, int, error) {
	projectID, err := strconv.ParseUint(r.URL.Query().Get("project_id"), 10, 64)
	if err != nil || projectID == 0 {
		return 0, "", 0, fmt.Errorf("%w: project_id", statistics.ErrInvalidQuery)
	}
	limit, err := parseLimit(r)
	if err != nil {
		return 0, "", 0, err
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 2048 {
		return 0, "", 0, fmt.Errorf("%w: cursor", statistics.ErrInvalidQuery)
	}
	return projectID, cursor, limit, nil
}
func parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 100 {
		return 0, fmt.Errorf("%w: limit", statistics.ErrInvalidQuery)
	}
	return value, nil
}
func queryList(r *http.Request, key string) []string {
	var out []string
	for _, raw := range r.URL.Query()[key] {
		for _, value := range strings.Split(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
