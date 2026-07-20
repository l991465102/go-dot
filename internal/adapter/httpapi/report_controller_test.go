package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func TestReportControllerSavedReportRoutes(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	repo := &reportHTTPRepository{reports: []reporting.Report{{
		ID: 7, ProjectID: 9, Name: "核心日报", DefaultDays: 30,
		UpdatedAt: now, Points: []reporting.PointRef{{ID: 3, Code: "checkout", Name: "下单", DefinitionState: tracking.Published, UVMode: tracking.Exact}},
	}}}

	t.Run("viewer lists and gets reports", func(t *testing.T) {
		controller := newReportControllerForTest(project.Viewer, repo, nil, nil, now)
		list := serveReportRequest(controller, project.Viewer, http.MethodGet, "/api/v1/projects/9/reports", "")
		require.Equal(t, http.StatusOK, list.Code, list.Body.String())
		require.Contains(t, list.Body.String(), `"name":"核心日报"`)
		require.NotContains(t, list.Body.String(), `"points"`)

		get := serveReportRequest(controller, project.Viewer, http.MethodGet, "/api/v1/projects/9/reports/7", "")
		require.Equal(t, http.StatusOK, get.Code, get.Body.String())
		require.Contains(t, get.Body.String(), `"id":7`)
		require.Contains(t, get.Body.String(), `"points"`)
	})

	t.Run("admin creates and viewer cannot create", func(t *testing.T) {
		body := `{"name":"转化日报","description":"核心转化","default_days":30,"point_ids":[3]}`
		admin := serveReportRequest(newReportControllerForTest(project.Admin, repo, nil, nil, now), project.Admin, http.MethodPost, "/api/v1/projects/9/reports", body)
		require.Equal(t, http.StatusCreated, admin.Code, admin.Body.String())
		require.Equal(t, uint64(9), repo.createProjectID)

		viewer := serveReportRequest(newReportControllerForTest(project.Viewer, repo, nil, nil, now), project.Viewer, http.MethodPost, "/api/v1/projects/9/reports", body)
		require.Equal(t, http.StatusForbidden, viewer.Code, viewer.Body.String())
	})

	t.Run("stale update is conflict", func(t *testing.T) {
		repo.updateErr = reporting.ErrConflict
		t.Cleanup(func() { repo.updateErr = nil })
		body := fmt.Sprintf(`{"name":"转化日报","description":"","default_days":30,"point_ids":[3],"expected_updated_at":%q}`, now.Format(time.RFC3339Nano))
		response := serveReportRequest(newReportControllerForTest(project.Admin, repo, nil, nil, now), project.Admin, http.MethodPatch, "/api/v1/projects/9/reports/7", body)
		require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"request_id"`)
	})

	t.Run("admin deletes", func(t *testing.T) {
		response := serveReportRequest(newReportControllerForTest(project.Admin, repo, nil, nil, now), project.Admin, http.MethodDelete, "/api/v1/projects/9/reports/7", "")
		require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
		require.Equal(t, uint64(9), repo.deleteProjectID)
		require.Equal(t, uint64(7), repo.deleteReportID)
	})
}

func TestReportControllerDailyQueryForViewer(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	repo := newReportHTTPRepository()
	daily := &reportHTTPDailyStore{items: []reporting.DailyAggregate{{Date: "2026-07-20", PointID: 3, PointCode: "checkout", PV: 12, UV: 5}}}
	controller := newReportControllerForTest(project.Viewer, repo, daily, nil, now)

	response := serveReportRequest(controller, project.Viewer, http.MethodPost, "/api/v1/statistics/daily-by-point/query", `{"project_id":9,"start_date":"2026-07-20","end_date":"2026-07-20","point_ids":[3]}`)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.JSONEq(t, `{"timezone":"Asia/Shanghai","start_date":"2026-07-20","end_date":"2026-07-20","point_count":1,"day_count":1,"total_pv":12,"items":[{"date":"2026-07-20","complete":false,"point_id":3,"point_code":"checkout","point_name":"下单","pv":12,"uv":5,"uv_mode":"EXACT"}]}`, response.Body.String())
}

func TestReportControllerRejectsInvalidAndNonStrictJSON(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	controller := newReportControllerForTest(project.Viewer, newReportHTTPRepository(), &reportHTTPDailyStore{}, nil, now)
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("%d", i+1)
	}

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed date", body: `{"project_id":9,"start_date":"2026/07/20","end_date":"2026-07-20","point_ids":[3]}`},
		{name: "too many points", body: `{"project_id":9,"start_date":"2026-07-20","end_date":"2026-07-20","point_ids":[` + strings.Join(tooMany, ",") + `]}`},
		{name: "unknown field", body: `{"project_id":9,"start_date":"2026-07-20","end_date":"2026-07-20","point_ids":[3],"surprise":true}`},
		{name: "trailing value", body: `{"project_id":9,"start_date":"2026-07-20","end_date":"2026-07-20","point_ids":[3]} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serveReportRequest(controller, project.Viewer, http.MethodPost, "/api/v1/statistics/daily-by-point/query", test.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

func TestReportControllerMapsCrossProjectAccessWithoutBackendDetails(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	authorizer := &reportHTTPAuthorizer{role: project.Viewer, projectID: 9, foreignErr: fmt.Errorf("mysql secret tenant row: %w", project.ErrForbidden)}
	service := reporting.NewService(authorizer, newReportHTTPRepository(), &reportHTTPDailyStore{}, nil, func() time.Time { return now })
	controller := NewReportController(service)

	response := serveReportRequest(controller, project.Viewer, http.MethodGet, "/api/v1/projects/10/reports", "")

	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "secret")
	require.Contains(t, response.Body.String(), `"request_id"`)
}

func TestReportControllerExportUsesSafeUnicodeFilenameAndUsername(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	exporter := &reportHTTPExporter{contents: []byte("xlsx-content")}
	controller := newReportControllerForTest(project.Viewer, newReportHTTPRepository(), &reportHTTPDailyStore{}, exporter, now)
	body := `{"project_id":9,"start_date":"2026-07-01","end_date":"2026-07-20","point_ids":[3],"report_name":"核心\"日报\r\nX-Evil: yes","report_description":"说明"}`

	response := serveReportRequestWithUsername(controller, http.MethodPost, "/api/v1/statistics/daily-by-point/export", body, "viewer.zhang")

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", response.Header().Get("Content-Type"))
	require.Equal(t, fmt.Sprint(len(exporter.contents)), response.Header().Get("Content-Length"))
	disposition := response.Header().Get("Content-Disposition")
	require.Contains(t, disposition, `attachment; filename="`)
	require.Contains(t, disposition, "20260701_20260720.xlsx")
	require.Contains(t, disposition, "filename*=UTF-8''%E6%A0%B8%E5%BF%83")
	require.NotContains(t, disposition, "\r")
	require.NotContains(t, disposition, "\n")
	require.NotContains(t, disposition, `核心"日报`)
	require.Equal(t, exporter.contents, response.Body.Bytes())
	require.Equal(t, "viewer.zhang", exporter.document.ExportedBy)
}

func TestReportControllerBuildsExportBeforeWritingHeaders(t *testing.T) {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	exporter := &reportHTTPExporter{err: errors.New("excel secret failure")}
	controller := newReportControllerForTest(project.Viewer, newReportHTTPRepository(), &reportHTTPDailyStore{}, exporter, now)
	body := `{"project_id":9,"start_date":"2026-07-20","end_date":"2026-07-20","point_ids":[3]}`

	response := serveReportRequest(controller, project.Viewer, http.MethodPost, "/api/v1/statistics/daily-by-point/export", body)

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
	require.Empty(t, response.Header().Get("Content-Disposition"))
	require.NotContains(t, response.Body.String(), "secret")
}

func newReportControllerForTest(role project.Role, repo *reportHTTPRepository, daily reporting.DailyStore, exporter reporting.Exporter, now time.Time) ReportController {
	service := reporting.NewService(&reportHTTPAuthorizer{role: role, projectID: 9}, repo, daily, exporter, func() time.Time { return now })
	return NewReportController(service)
}

func serveReportRequest(controller ReportController, role project.Role, method, target, body string) *httptest.ResponseRecorder {
	return serveReportRequestWithUsername(controller, method, target, body, strings.ToLower(string(role)))
}

func serveReportRequestWithUsername(controller ReportController, method, target, body, username string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	controller.Register(mux)
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "report-test-request")
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, auth.Principal{UserID: 23, Username: username}))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	return response
}

type reportHTTPAuthorizer struct {
	role       project.Role
	projectID  uint64
	foreignErr error
}

func (a *reportHTTPAuthorizer) RequireRole(_ context.Context, _, projectID uint64, roles ...project.Role) error {
	if projectID != a.projectID {
		if a.foreignErr != nil {
			return a.foreignErr
		}
		return project.ErrForbidden
	}
	for _, role := range roles {
		if role == a.role {
			return nil
		}
	}
	return project.ErrForbidden
}

type reportHTTPRepository struct {
	reports         []reporting.Report
	createProjectID uint64
	deleteProjectID uint64
	deleteReportID  uint64
	updateErr       error
}

func newReportHTTPRepository() *reportHTTPRepository {
	return &reportHTTPRepository{reports: []reporting.Report{{ID: 7, ProjectID: 9, Name: "核心日报", DefaultDays: 30}}}
}

func (r *reportHTTPRepository) List(context.Context, uint64) ([]reporting.Report, error) {
	return r.reports, nil
}
func (r *reportHTTPRepository) Get(_ context.Context, projectID, reportID uint64) (reporting.Report, error) {
	for _, report := range r.reports {
		if report.ProjectID == projectID && report.ID == reportID {
			return report, nil
		}
	}
	return reporting.Report{}, reporting.ErrNotFound
}
func (r *reportHTTPRepository) Create(_ context.Context, projectID, actor uint64, input reporting.ReportInput) (reporting.Report, error) {
	r.createProjectID = projectID
	return reporting.Report{ID: 8, ProjectID: projectID, Name: input.Name, Description: input.Description, DefaultDays: input.DefaultDays, CreatedBy: actor, UpdatedBy: actor}, nil
}
func (r *reportHTTPRepository) Update(_ context.Context, projectID, reportID, actor uint64, input reporting.ReportInput) (reporting.Report, error) {
	if r.updateErr != nil {
		return reporting.Report{}, r.updateErr
	}
	return reporting.Report{ID: reportID, ProjectID: projectID, Name: input.Name, UpdatedBy: actor}, nil
}
func (r *reportHTTPRepository) Delete(_ context.Context, projectID, reportID uint64) error {
	r.deleteProjectID, r.deleteReportID = projectID, reportID
	return nil
}
func (r *reportHTTPRepository) ResolvePoints(_ context.Context, projectID uint64, ids []uint64) ([]reporting.PointRef, error) {
	if projectID != 9 {
		return nil, reporting.ErrInvalid
	}
	points := make([]reporting.PointRef, 0, len(ids))
	for _, id := range ids {
		points = append(points, reporting.PointRef{ID: id, Code: "checkout", Name: "下单", DefinitionState: tracking.Published, UVMode: tracking.Exact})
	}
	return points, nil
}

type reportHTTPDailyStore struct{ items []reporting.DailyAggregate }

func (s *reportHTTPDailyStore) QueryDaily(context.Context, reporting.DailyFilter) ([]reporting.DailyAggregate, error) {
	return s.items, nil
}

type reportHTTPExporter struct {
	contents []byte
	err      error
	document reporting.ExportDocument
}

func (e *reportHTTPExporter) Build(document reporting.ExportDocument) ([]byte, error) {
	e.document = document
	return e.contents, e.err
}
