package reporting

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

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

type fakeAuthorizer struct {
	role  project.Role
	calls [][]project.Role
}

func (a *fakeAuthorizer) RequireRole(_ context.Context, _, _ uint64, allowed ...project.Role) error {
	a.calls = append(a.calls, append([]project.Role(nil), allowed...))
	for _, role := range allowed {
		if role == a.role {
			return nil
		}
	}
	return project.ErrForbidden
}

type fakeRepository struct {
	reports      []Report
	points       map[uint64]PointRef
	lastInput    ReportInput
	lastPointIDs []uint64
	createCalls  int
	updateCalls  int
	deleteCalls  int
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

func (r *fakeRepository) ResolvePoints(_ context.Context, _ uint64, ids []uint64) ([]PointRef, error) {
	resolved := make([]PointRef, 0, len(ids))
	for _, id := range ids {
		if point, ok := r.points[id]; ok {
			point.SortNo = len(resolved)
			resolved = append(resolved, point)
		}
	}
	return resolved, nil
}

func newAdminServiceForTest() *Service {
	service, _ := newAdminServiceWithRepositoryForTest()
	return service
}

func newAdminServiceWithRepositoryForTest() (*Service, *fakeRepository) {
	repo := newFakeRepository()
	return NewService(&fakeAuthorizer{role: project.Admin}, repo, nil, nil, time.Now), repo
}
