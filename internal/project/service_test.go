package project_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/project"
)

func TestRequireRoleRejectsViewerWrite(t *testing.T) {
	svc := newProjectFixture(t, project.Member{UserID: 7, ProjectID: 9, Role: project.Viewer})
	err := svc.RequireRole(context.Background(), 7, 9, project.Admin)
	require.ErrorIs(t, err, project.ErrForbidden)
}

func TestProjectCreatorBecomesAdminAndOnlyHashIsStored(t *testing.T) {
	svc, repo := newProjectFixtureWithRepo(t)
	created, err := svc.Create(context.Background(), 7, project.CreateInput{Code: "checkout", Name: "Checkout"})
	require.NoError(t, err)
	require.Len(t, created.WriteKey, 43)
	require.NotEqual(t, created.WriteKey, repo.projects[created.Project.ID].WriteKeyHash)
	require.NoError(t, svc.RequireRole(context.Background(), 7, created.Project.ID, project.Admin))
}

func TestRotateWriteKeyKeepsPreviousHashFor24Hours(t *testing.T) {
	svc, repo := newProjectFixtureWithRepo(t)
	created, err := svc.Create(context.Background(), 7, project.CreateInput{Code: "checkout", Name: "Checkout"})
	require.NoError(t, err)
	oldHash := repo.projects[created.Project.ID].WriteKeyHash

	rotation, err := svc.RotateWriteKey(context.Background(), 7, created.Project.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rotation.WriteKey)
	stored := repo.projects[created.Project.ID]
	require.Equal(t, oldHash, stored.PreviousWriteKeyHash)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), *stored.PreviousWriteKeyExpiresAt, 2*time.Second)
}

func TestRotateWriteKeyRejectsSecondRotationDuringGrace(t *testing.T) {
	svc, _ := newProjectFixtureWithRepo(t)
	created, err := svc.Create(context.Background(), 7, project.CreateInput{Code: "checkout", Name: "Checkout"})
	require.NoError(t, err)
	_, err = svc.RotateWriteKey(context.Background(), 7, created.Project.ID)
	require.NoError(t, err)

	_, err = svc.RotateWriteKey(context.Background(), 7, created.Project.ID)
	require.ErrorIs(t, err, project.ErrWriteKeyRotationInProgress)
}

func newProjectFixture(t *testing.T, members ...project.Member) *project.Service {
	t.Helper()
	repo := newProjectMemoryRepository()
	for _, m := range members {
		repo.members[[2]uint64{m.UserID, m.ProjectID}] = m
	}
	return project.NewService(repo)
}
func newProjectFixtureWithRepo(t *testing.T) (*project.Service, *projectMemoryRepository) {
	t.Helper()
	repo := newProjectMemoryRepository()
	return project.NewService(repo), repo
}

type projectMemoryRepository struct {
	mu       sync.Mutex
	nextID   uint64
	projects map[uint64]project.Project
	members  map[[2]uint64]project.Member
}

func newProjectMemoryRepository() *projectMemoryRepository {
	return &projectMemoryRepository{projects: map[uint64]project.Project{}, members: map[[2]uint64]project.Member{}}
}
func (r *projectMemoryRepository) CreateProject(_ context.Context, p project.Project, member project.Member) (project.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	p.ID = r.nextID
	member.ProjectID = p.ID
	r.projects[p.ID] = p
	r.members[[2]uint64{member.UserID, p.ID}] = member
	return p, nil
}
func (r *projectMemoryRepository) FindMember(_ context.Context, userID, projectID uint64) (project.Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[[2]uint64{userID, projectID}]
	if !ok {
		return project.Member{}, project.ErrForbidden
	}
	return m, nil
}
func (r *projectMemoryRepository) RotateWriteKey(_ context.Context, projectID uint64, hash string, now, expires time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.projects[projectID]
	if p.PreviousWriteKeyExpiresAt != nil && p.PreviousWriteKeyExpiresAt.After(now) {
		return project.ErrWriteKeyRotationInProgress
	}
	p.PreviousWriteKeyHash, p.PreviousWriteKeyExpiresAt, p.WriteKeyHash = p.WriteKeyHash, &expires, hash
	r.projects[projectID] = p
	return nil
}
func (r *projectMemoryRepository) ListProjects(_ context.Context, userID uint64) ([]project.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []project.Project
	for key := range r.members {
		if key[0] == userID {
			p := r.projects[key[1]]
			p.Role = r.members[key].Role
			out = append(out, p)
		}
	}
	return out, nil
}
