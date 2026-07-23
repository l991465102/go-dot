package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	reportsv1 "github.com/lzw7758/go-dot/api/reports/v1"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/reporting"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type ReportController struct{ service *reporting.Service }

func NewReportController(service *reporting.Service) ReportController {
	return ReportController{service: service}
}

func (c ReportController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{projectID}/reports", c.list)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/reports", c.create)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/reports/{reportID}", c.get)
	mux.HandleFunc("PATCH /api/v1/projects/{projectID}/reports/{reportID}", c.update)
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}/reports/{reportID}", c.delete)
	mux.HandleFunc("POST /api/v1/statistics/daily-by-point/query", c.queryDaily)
	mux.HandleFunc("POST /api/v1/statistics/daily-by-point/export", c.exportDaily)
}

func (c ReportController) list(w http.ResponseWriter, r *http.Request) {
	principal, projectID, ok := reportPrincipalAndProject(w, r)
	if !ok {
		return
	}
	items, err := c.service.ListReports(r.Context(), principal.UserID, projectID)
	if err != nil {
		reportError(w, r, err)
		return
	}
	if items == nil {
		items = []reporting.Report{}
	}
	writeJSON(w, http.StatusOK, reportsv1.ReportSummaries(items))
}

func (c ReportController) get(w http.ResponseWriter, r *http.Request) {
	principal, projectID, reportID, ok := reportPrincipalProjectAndID(w, r)
	if !ok {
		return
	}
	item, err := c.service.GetReport(r.Context(), principal.UserID, projectID, reportID)
	if err != nil {
		reportError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reportsv1.ReportRes(item))
}

func (c ReportController) create(w http.ResponseWriter, r *http.Request) {
	principal, projectID, ok := reportPrincipalAndProject(w, r)
	if !ok {
		return
	}
	var request reportsv1.CreateReq
	if !decodeJSON(w, r, &request) {
		return
	}
	item, err := c.service.CreateReport(r.Context(), principal.UserID, projectID, request.ReportInput())
	if err != nil {
		reportError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, reportsv1.ReportRes(item))
}

func (c ReportController) update(w http.ResponseWriter, r *http.Request) {
	principal, projectID, reportID, ok := reportPrincipalProjectAndID(w, r)
	if !ok {
		return
	}
	var request reportsv1.UpdateReq
	if !decodeJSON(w, r, &request) {
		return
	}
	item, err := c.service.UpdateReport(r.Context(), principal.UserID, projectID, reportID, request.ReportInput())
	if err != nil {
		reportError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reportsv1.ReportRes(item))
}

func (c ReportController) delete(w http.ResponseWriter, r *http.Request) {
	principal, projectID, reportID, ok := reportPrincipalProjectAndID(w, r)
	if !ok {
		return
	}
	if err := c.service.DeleteReport(r.Context(), principal.UserID, projectID, reportID); err != nil {
		reportError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c ReportController) queryDaily(w http.ResponseWriter, r *http.Request) {
	principal, ok := reportPrincipal(w, r)
	if !ok {
		return
	}
	var request reportsv1.DailyQueryReq
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := c.service.QueryDaily(r.Context(), principal.UserID, request)
	if err != nil {
		reportError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reportsv1.DailyQueryRes(result))
}

func (c ReportController) exportDaily(w http.ResponseWriter, r *http.Request) {
	principal, ok := reportPrincipal(w, r)
	if !ok {
		return
	}
	var request reportsv1.DailyExportReq
	if !decodeJSON(w, r, &request) {
		return
	}
	file, err := c.service.ExportDaily(r.Context(), principal.UserID, principal.Username, request)
	if err != nil {
		reportError(w, r, err)
		return
	}

	// Build the complete workbook before committing download headers. Export errors
	// can therefore still be returned as ordinary JSON API errors.
	contents := file.Contents
	w.Header().Set("Content-Type", xlsxContentType)
	w.Header().Set("Content-Disposition", contentDisposition(file.Filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(contents)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(contents)
}

func reportPrincipal(w http.ResponseWriter, r *http.Request) (principal authPrincipal, ok bool) {
	value, found := PrincipalFromContext(r.Context())
	if !found {
		writeAPIError(w, r, http.StatusUnauthorized, "unauthorized")
		return authPrincipal{}, false
	}
	return authPrincipal{UserID: value.UserID, Username: value.Username}, true
}

// authPrincipal keeps controller helpers limited to the identity fields the
// reporting API consumes.
type authPrincipal struct {
	UserID   uint64
	Username string
}

func reportPrincipalAndProject(w http.ResponseWriter, r *http.Request) (authPrincipal, uint64, bool) {
	principal, ok := reportPrincipal(w, r)
	if !ok {
		return authPrincipal{}, 0, false
	}
	projectID, err := parsePositivePathID(r.PathValue("projectID"))
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid project id")
		return authPrincipal{}, 0, false
	}
	return principal, projectID, true
}

func reportPrincipalProjectAndID(w http.ResponseWriter, r *http.Request) (authPrincipal, uint64, uint64, bool) {
	principal, projectID, ok := reportPrincipalAndProject(w, r)
	if !ok {
		return authPrincipal{}, 0, 0, false
	}
	reportID, err := parsePositivePathID(r.PathValue("reportID"))
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid report id")
		return authPrincipal{}, 0, 0, false
	}
	return principal, projectID, reportID, true
}

func parsePositivePathID(value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return 0, reporting.ErrInvalid
	}
	return id, nil
}

func reportError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusServiceUnavailable, "reporting service unavailable"
	switch {
	case errors.Is(err, reporting.ErrInvalid):
		status, message = http.StatusBadRequest, "invalid report request"
	case errors.Is(err, project.ErrForbidden):
		status, message = http.StatusForbidden, "forbidden"
	case errors.Is(err, reporting.ErrNotFound), errors.Is(err, project.ErrProjectNotFound):
		status, message = http.StatusNotFound, "resource not found"
	case errors.Is(err, reporting.ErrConflict):
		status, message = http.StatusConflict, "conflict"
	}
	writeAPIError(w, r, status, message)
}

func contentDisposition(filename string) string {
	display := sanitizeDispositionFilename(filename)
	if display == "" {
		display = "custom-report.xlsx"
	}
	return `attachment; filename="` + asciiFilenameFallback(display) + `"; filename*=UTF-8''` + encodeRFC5987(display)
}

func sanitizeDispositionFilename(filename string) string {
	filename = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case r == '/' || r == '\\' || r == '"' || r == '\'' || r == ';':
			return -1
		default:
			return r
		}
	}, filename)
	return strings.TrimSpace(filename)
}

func asciiFilenameFallback(filename string) string {
	var result strings.Builder
	separator := false
	for _, r := range filename {
		if r > 0x7f {
			separator = true
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			if separator && result.Len() > 0 {
				result.WriteByte('_')
			}
			separator = false
			result.WriteRune(r)
			continue
		}
		separator = true
	}
	fallback := strings.Trim(result.String(), "-_.")
	if fallback == "" {
		return "custom-report.xlsx"
	}
	if fallback[0] >= '0' && fallback[0] <= '9' {
		fallback = "custom-report_" + fallback
	}
	return fallback
}

func encodeRFC5987(filename string) string {
	const hex = "0123456789ABCDEF"
	var encoded strings.Builder
	for i := 0; i < len(filename); i++ {
		current := filename[i]
		if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') || (current >= '0' && current <= '9') || current == '-' || current == '_' || current == '.' {
			encoded.WriteByte(current)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hex[current>>4])
		encoded.WriteByte(hex[current&0x0f])
	}
	return encoded.String()
}
