package reporting

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func TestNewServicePanicsWithoutAuthorizer(t *testing.T) {
	require.PanicsWithValue(t, "reporting: nil authorizer", func() {
		NewService(nil, newFakeRepository(), nil, nil, time.Now)
	})
}

func TestNewServicePanicsWithoutRepository(t *testing.T) {
	require.PanicsWithValue(t, "reporting: nil repository", func() {
		NewService(&fakeAuthorizer{role: project.Admin}, nil, nil, nil, time.Now)
	})
}

func TestViewerCanListButCannotCreateReport(t *testing.T) {
	auth := &fakeAuthorizer{role: project.Viewer}
	repo := &fakeRepository{reports: []Report{{ID: 1, ProjectID: 9, Name: "daily"}}}
	service := NewService(auth, repo, nil, nil, time.Now)

	items, err := service.ListReports(context.Background(), 3, 9)
	require.NoError(t, err)
	require.Len(t, items, 1)
	_, err = service.CreateReport(context.Background(), 3, 9, ReportInput{Name: "new", DefaultDays: 30, PointIDs: []uint64{7}})
	require.ErrorIs(t, err, project.ErrForbidden)
}

func TestViewerCanGetButCannotUpdateOrDeleteReport(t *testing.T) {
	auth := &fakeAuthorizer{role: project.Viewer}
	repo := newFakeRepository()
	repo.reports = []Report{{ID: 7, ProjectID: 9, Name: "daily"}}
	service := NewService(auth, repo, nil, nil, time.Now)

	_, err := service.GetReport(context.Background(), 3, 9, 7)
	require.NoError(t, err)
	expected := time.Now()
	_, err = service.UpdateReport(context.Background(), 3, 9, 7, ReportInput{Name: "new", DefaultDays: 30, PointIDs: []uint64{7}, ExpectedUpdatedAt: &expected})
	require.ErrorIs(t, err, project.ErrForbidden)
	require.ErrorIs(t, service.DeleteReport(context.Background(), 3, 9, 7), project.ErrForbidden)
	require.Zero(t, repo.updateCalls)
	require.Zero(t, repo.deleteCalls)
}

func TestCreateRejectsInvalidInputs(t *testing.T) {
	service := newAdminServiceForTest()
	tooMany := make([]uint64, 101)
	for i := range tooMany {
		tooMany[i] = uint64(i + 1)
	}
	for _, input := range []ReportInput{
		{Name: "", DefaultDays: 30, PointIDs: []uint64{7}},
		{Name: strings.Repeat("n", 129), DefaultDays: 30, PointIDs: []uint64{7}},
		{Name: "daily", Description: strings.Repeat("d", 1001), DefaultDays: 30, PointIDs: []uint64{7}},
		{Name: "daily", DefaultDays: 0, PointIDs: []uint64{7}},
		{Name: "daily", DefaultDays: 91, PointIDs: []uint64{7}},
		{Name: "daily", DefaultDays: 30, PointIDs: nil},
		{Name: "daily", DefaultDays: 30, PointIDs: tooMany},
	} {
		_, err := service.CreateReport(context.Background(), 1, 9, input)
		require.ErrorIs(t, err, ErrInvalid)
	}
}

func TestCreateTrimsNameAndDescription(t *testing.T) {
	service, repo := newAdminServiceWithRepositoryForTest()

	created, err := service.CreateReport(context.Background(), 1, 9, ReportInput{
		Name: "  daily  ", Description: "  summary  ", DefaultDays: 30, PointIDs: []uint64{7},
	})

	require.NoError(t, err)
	require.Equal(t, "daily", repo.lastInput.Name)
	require.Equal(t, "summary", repo.lastInput.Description)
	require.Equal(t, "daily", created.Name)
	require.Equal(t, "summary", created.Description)
}

func TestCreateAppliesUnicodeRuneLengthBoundaries(t *testing.T) {
	for _, test := range []struct {
		name        string
		reportName  string
		description string
		wantErr     error
	}{
		{name: "128 rune name", reportName: strings.Repeat("界", 128)},
		{name: "129 rune name", reportName: strings.Repeat("界", 129), wantErr: ErrInvalid},
		{name: "1000 rune description", reportName: "daily", description: strings.Repeat("界", 1000)},
		{name: "1001 rune description", reportName: "daily", description: strings.Repeat("界", 1001), wantErr: ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newAdminServiceForTest()
			_, err := service.CreateReport(context.Background(), 1, 9, ReportInput{
				Name: test.reportName, Description: test.description, DefaultDays: 30, PointIDs: []uint64{7},
			})
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUpdateRequiresExpectedUpdatedAt(t *testing.T) {
	service := newAdminServiceForTest()
	_, err := service.UpdateReport(context.Background(), 1, 9, 7, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{3}})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestPointOrderKeepsFirstOccurrence(t *testing.T) {
	service, repo := newAdminServiceWithRepositoryForTest()
	created, err := service.CreateReport(context.Background(), 1, 9, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{3, 2, 3}})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 2}, repo.lastPointIDs)
	require.Equal(t, []uint64{3, 2}, []uint64{created.Points[0].ID, created.Points[1].ID})
	require.Equal(t, []int{0, 1}, []int{created.Points[0].SortNo, created.Points[1].SortNo})
}

func TestCreateRejectsPointThatCannotBeResolvedBeforeMutation(t *testing.T) {
	service, repo := newAdminServiceWithRepositoryForTest()

	_, err := service.CreateReport(context.Background(), 1, 9, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{2, 99}})

	require.ErrorIs(t, err, ErrInvalid)
	require.Zero(t, repo.createCalls)
}

func TestCreateResolvesPointsInRequestedProject(t *testing.T) {
	service, repo := newAdminServiceWithRepositoryForTest()

	_, err := service.CreateReport(context.Background(), 1, 42, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{7}})

	require.NoError(t, err)
	require.Equal(t, uint64(42), repo.resolvedProjectID)
}

func TestServicePropagatesAuthorizationAndRepositoryErrors(t *testing.T) {
	authErr := errors.New("authorization unavailable")
	service := NewService(&fakeAuthorizer{err: authErr}, newFakeRepository(), nil, nil, time.Now)
	_, err := service.ListReports(context.Background(), 1, 9)
	require.ErrorIs(t, err, authErr)

	repoErr := errors.New("point resolver unavailable")
	repo := newFakeRepository()
	repo.resolveErr = repoErr
	service = NewService(&fakeAuthorizer{role: project.Admin}, repo, nil, nil, time.Now)
	_, err = service.CreateReport(context.Background(), 1, 9, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{7}})
	require.ErrorIs(t, err, repoErr)
	require.Zero(t, repo.createCalls)
}

func TestUpdateValidatesAndResolvesBeforeMutation(t *testing.T) {
	service, repo := newAdminServiceWithRepositoryForTest()
	expected := time.Now()

	updated, err := service.UpdateReport(context.Background(), 1, 9, 7, ReportInput{
		Name: " daily ", Description: " notes ", DefaultDays: 14,
		PointIDs: []uint64{7, 2, 7}, ExpectedUpdatedAt: &expected,
	})

	require.NoError(t, err)
	require.Equal(t, []uint64{7, 2}, repo.lastPointIDs)
	require.Equal(t, []uint64{7, 2}, []uint64{updated.Points[0].ID, updated.Points[1].ID})
	require.Equal(t, "daily", repo.lastInput.Name)
	require.Equal(t, "notes", repo.lastInput.Description)
	require.Equal(t, &expected, repo.lastInput.ExpectedUpdatedAt)
	require.Equal(t, 1, repo.updateCalls)
}

func TestAdminCanDeleteReport(t *testing.T) {
	service, repo := newAdminServiceWithRepositoryForTest()

	require.NoError(t, service.DeleteReport(context.Background(), 1, 9, 7))
	require.Equal(t, 1, repo.deleteCalls)
}

func TestQueryDailyFillsMissingPointDaysAndMarksTodayIncomplete(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, location)
	daily := &fakeDailyStore{items: []DailyAggregate{{Date: "2026-07-19", PointID: 3, PointCode: "checkout", PV: 4, UV: 2}}}
	repo := &fakeRepository{resolved: []PointRef{
		{ID: 3, Code: "checkout", Name: "Checkout", UVMode: tracking.Exact},
		{ID: 2, Code: "view", Name: "View", UVMode: tracking.Approximate},
	}}
	service := NewService(&fakeAuthorizer{role: project.Viewer}, repo, daily, nil, func() time.Time { return now })

	result, err := service.QueryDaily(context.Background(), 7, DailyQuery{ProjectID: 9, StartDate: "2026-07-19", EndDate: "2026-07-20", PointIDs: []uint64{3, 2}})

	require.NoError(t, err)
	require.Equal(t, "Asia/Shanghai", result.Timezone)
	require.Equal(t, 2, result.DayCount)
	require.Equal(t, 2, result.PointCount)
	require.Len(t, result.Items, 4)
	require.Equal(t, []string{"2026-07-20", "2026-07-20", "2026-07-19", "2026-07-19"}, []string{result.Items[0].Date, result.Items[1].Date, result.Items[2].Date, result.Items[3].Date})
	require.Equal(t, []uint64{3, 2, 3, 2}, []uint64{result.Items[0].PointID, result.Items[1].PointID, result.Items[2].PointID, result.Items[3].PointID})
	require.False(t, result.Items[0].Complete)
	require.False(t, result.Items[1].Complete)
	require.True(t, result.Items[2].Complete)
	require.Equal(t, uint64(4), result.TotalPV)
	require.Equal(t, uint64(2), result.Items[2].UV)
	require.Equal(t, "Checkout", result.Items[2].PointName)
	require.Equal(t, tracking.Exact, result.Items[2].UVMode)
	require.Equal(t, 1, daily.calls)
	require.Equal(t, uint64(9), daily.filter.ProjectID)
	require.Equal(t, []uint64{2}, daily.filter.ApproximatePointIDs)
	require.Equal(t, []uint64{3}, daily.filter.ExactPointIDs)
	require.Equal(t, "Asia/Shanghai", daily.filter.Timezone)
	require.Equal(t, "2026-07-19T00:00:00+08:00", daily.filter.Start.Format(time.RFC3339))
	require.Equal(t, "2026-07-21T00:00:00+08:00", daily.filter.End.Format(time.RFC3339))
	require.Equal(t, uint64(9), repo.resolvedProjectID)
}

func TestQueryDailyRejectsInvalidQueries(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, location)
	tooMany := make([]uint64, 101)
	for i := range tooMany {
		tooMany[i] = uint64(i + 1)
	}
	tests := []struct {
		name  string
		query DailyQuery
	}{
		{name: "malformed start", query: DailyQuery{ProjectID: 9, StartDate: "2026/07/19", EndDate: "2026-07-20", PointIDs: []uint64{3}}},
		{name: "malformed end", query: DailyQuery{ProjectID: 9, StartDate: "2026-07-19", EndDate: "July 20", PointIDs: []uint64{3}}},
		{name: "future end", query: DailyQuery{ProjectID: 9, StartDate: "2026-07-20", EndDate: "2026-07-21", PointIDs: []uint64{3}}},
		{name: "start after end", query: DailyQuery{ProjectID: 9, StartDate: "2026-07-20", EndDate: "2026-07-19", PointIDs: []uint64{3}}},
		{name: "zero points", query: DailyQuery{ProjectID: 9, StartDate: "2026-07-19", EndDate: "2026-07-20"}},
		{name: "101 unique points", query: DailyQuery{ProjectID: 9, StartDate: "2026-07-19", EndDate: "2026-07-20", PointIDs: tooMany}},
		{name: "91 inclusive days", query: DailyQuery{ProjectID: 9, StartDate: "2026-04-21", EndDate: "2026-07-20", PointIDs: []uint64{3}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			daily := &fakeDailyStore{}
			service := NewService(&fakeAuthorizer{role: project.Viewer}, newFakeRepository(), daily, nil, func() time.Time { return now })
			_, err := service.QueryDaily(context.Background(), 7, test.query)
			require.ErrorIs(t, err, ErrInvalid)
			require.Zero(t, daily.calls)
		})
	}
}

func TestQueryDailyDeduplicatesPointsAndRejectsUnauthorizedResolution(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, location)

	repo := &fakeRepository{resolved: []PointRef{{ID: 3, Code: "checkout", Name: "Checkout", UVMode: tracking.Exact}}}
	daily := &fakeDailyStore{}
	service := NewService(&fakeAuthorizer{role: project.Viewer}, repo, daily, nil, func() time.Time { return now })
	result, err := service.QueryDaily(context.Background(), 7, DailyQuery{ProjectID: 9, StartDate: "2026-07-20", EndDate: "2026-07-20", PointIDs: []uint64{3, 3}})
	require.NoError(t, err)
	require.Equal(t, 1, result.PointCount)
	require.Equal(t, []uint64{3}, repo.lastPointIDs)

	repo.resolved = []PointRef{{ID: 3, UVMode: tracking.Exact}}
	_, err = service.QueryDaily(context.Background(), 7, DailyQuery{ProjectID: 9, StartDate: "2026-07-20", EndDate: "2026-07-20", PointIDs: []uint64{3, 2}})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestQueryDailyHasNoTotalUVJSONField(t *testing.T) {
	typ := reflect.TypeOf(DailyResult{})
	for i := 0; i < typ.NumField(); i++ {
		require.NotEqual(t, "total_uv", typ.Field(i).Tag.Get("json"))
	}
}

type fakeAuthorizer struct {
	role  project.Role
	err   error
	calls [][]project.Role
}

func (a *fakeAuthorizer) RequireRole(_ context.Context, _, _ uint64, allowed ...project.Role) error {
	a.calls = append(a.calls, append([]project.Role(nil), allowed...))
	if a.err != nil {
		return a.err
	}
	for _, role := range allowed {
		if role == a.role {
			return nil
		}
	}
	return project.ErrForbidden
}

type fakeRepository struct {
	reports           []Report
	points            map[uint64]PointRef
	lastInput         ReportInput
	lastPointIDs      []uint64
	createCalls       int
	updateCalls       int
	deleteCalls       int
	resolveErr        error
	resolvedProjectID uint64
	resolved          []PointRef
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{points: map[uint64]PointRef{
		2: {ID: 2, Code: "view", Name: "View", DefinitionState: tracking.Published},
		3: {ID: 3, Code: "click", Name: "Click", DefinitionState: tracking.Published},
		7: {ID: 7, Code: "checkout", Name: "Checkout", DefinitionState: tracking.Published},
	}}
}

func (r *fakeRepository) List(_ context.Context, projectID uint64) ([]Report, error) {
	items := make([]Report, 0, len(r.reports))
	for _, report := range r.reports {
		if report.ProjectID == projectID {
			items = append(items, report)
		}
	}
	return items, nil
}

func (r *fakeRepository) Get(_ context.Context, projectID, reportID uint64) (Report, error) {
	for _, report := range r.reports {
		if report.ProjectID == projectID && report.ID == reportID {
			return report, nil
		}
	}
	return Report{}, ErrNotFound
}

func (r *fakeRepository) Create(_ context.Context, projectID, actor uint64, in ReportInput) (Report, error) {
	r.createCalls++
	r.lastInput = in
	r.lastPointIDs = append([]uint64(nil), in.PointIDs...)
	return Report{ID: 10, ProjectID: projectID, Name: in.Name, Description: in.Description, DefaultDays: in.DefaultDays, CreatedBy: actor, UpdatedBy: actor}, nil
}

func (r *fakeRepository) Update(_ context.Context, projectID, reportID, actor uint64, in ReportInput) (Report, error) {
	r.updateCalls++
	r.lastInput = in
	r.lastPointIDs = append([]uint64(nil), in.PointIDs...)
	return Report{ID: reportID, ProjectID: projectID, Name: in.Name, Description: in.Description, DefaultDays: in.DefaultDays, UpdatedBy: actor}, nil
}

func (r *fakeRepository) Delete(_ context.Context, _, _ uint64) error {
	r.deleteCalls++
	return nil
}

func (r *fakeRepository) ResolvePoints(_ context.Context, projectID uint64, ids []uint64) ([]PointRef, error) {
	r.resolvedProjectID = projectID
	r.lastPointIDs = append([]uint64(nil), ids...)
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	if r.resolved != nil {
		return append([]PointRef(nil), r.resolved...), nil
	}
	resolved := make([]PointRef, 0, len(ids))
	for _, id := range ids {
		if point, ok := r.points[id]; ok {
			point.SortNo = len(resolved)
			resolved = append(resolved, point)
		}
	}
	return resolved, nil
}

type fakeDailyStore struct {
	items  []DailyAggregate
	err    error
	calls  int
	filter DailyFilter
}

func (s *fakeDailyStore) QueryDaily(_ context.Context, filter DailyFilter) ([]DailyAggregate, error) {
	s.calls++
	s.filter = filter
	return append([]DailyAggregate(nil), s.items...), s.err
}

func newAdminServiceForTest() *Service {
	service, _ := newAdminServiceWithRepositoryForTest()
	return service
}

func newAdminServiceWithRepositoryForTest() (*Service, *fakeRepository) {
	repo := newFakeRepository()
	return NewService(&fakeAuthorizer{role: project.Admin}, repo, nil, nil, time.Now), repo
}
