package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/adapter/httpapi"
	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/project"
)

func TestFirstPasswordHTTPFlow(t *testing.T) {
	authRepo := newAuthRepo()
	authSvc := auth.NewService(authRepo, []byte("http-test-secret-that-is-long-enough"), 15*time.Minute, 7*24*time.Hour)
	require.NoError(t, authSvc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
	projectSvc := project.NewService(newProjectRepo())
	handler := httpapi.NewHandler(authSvc, projectSvc, false)

	login := performJSON(t, handler, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "admin"}, "")
	require.Equal(t, http.StatusOK, login.Code)
	var session struct {
		AccessToken        string `json:"access_token"`
		MustChangePassword bool   `json:"must_change_password"`
	}
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &session))
	require.True(t, session.MustChangePassword)

	create := performJSON(t, handler, http.MethodPost, "/api/v1/projects", map[string]string{"code": "checkout", "name": "Checkout"}, session.AccessToken)
	require.Equal(t, http.StatusForbidden, create.Code)

	change := performJSON(t, handler, http.MethodPost, "/api/v1/auth/change-password", map[string]string{"current_password": "admin", "new_password": "Admin-Changed-2026!"}, session.AccessToken)
	require.Equal(t, http.StatusNoContent, change.Code)

	create = performJSON(t, handler, http.MethodPost, "/api/v1/projects", map[string]string{"code": "checkout", "name": "Checkout"}, session.AccessToken)
	require.Equal(t, http.StatusCreated, create.Code)
}

func TestViewerWriteRequestReturnsForbidden(t *testing.T) {
	authRepo := newAuthRepo()
	authSvc := auth.NewService(authRepo, []byte("http-test-secret-that-is-long-enough"), 15*time.Minute, 7*24*time.Hour)
	require.NoError(t, authSvc.EnsureInitialAdmin(context.Background(), "viewer", "initial"))
	viewer := authRepo.byName("viewer")
	require.NoError(t, authSvc.ChangePassword(context.Background(), viewer.ID, "initial", "changed"))
	session, err := authSvc.Login(context.Background(), "viewer", "changed")
	require.NoError(t, err)
	projectRepo := newProjectRepo()
	projectRepo.members[[2]uint64{viewer.ID, 9}] = project.Member{UserID: viewer.ID, ProjectID: 9, Role: project.Viewer, Active: true}
	handler := httpapi.NewHandler(authSvc, project.NewService(projectRepo), false)

	response := performJSON(t, handler, http.MethodPost, "/api/v1/projects/9/rotate-write-key", nil, session.AccessToken)
	require.Equal(t, http.StatusForbidden, response.Code)
}

func performJSON(t *testing.T, handler http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

type testAuthRepo struct {
	mu     sync.Mutex
	next   uint64
	users  map[uint64]auth.User
	names  map[string]uint64
	tokens map[string]auth.RefreshToken
}

func newAuthRepo() *testAuthRepo {
	return &testAuthRepo{users: map[uint64]auth.User{}, names: map[string]uint64{}, tokens: map[string]auth.RefreshToken{}}
}
func (r *testAuthRepo) CreateInitialUser(_ context.Context, u auth.User) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.names[u.Username]; ok {
		return r.users[id], nil
	}
	r.next++
	u.ID = r.next
	r.users[u.ID] = u
	r.names[u.Username] = u.ID
	return u, nil
}
func (r *testAuthRepo) FindUserByUsername(_ context.Context, n string) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.names[n]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return r.users[id], nil
}
func (r *testAuthRepo) FindUserByID(_ context.Context, id uint64) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}
func (r *testAuthRepo) UpdatePassword(_ context.Context, id uint64, h string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.users[id]
	u.PasswordHash = h
	u.MustChangePassword = false
	r.users[id] = u
	return nil
}
func (r *testAuthRepo) TouchLogin(context.Context, uint64, time.Time) error { return nil }
func (r *testAuthRepo) StoreRefreshToken(_ context.Context, t auth.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[t.Hash] = t
	return nil
}
func (r *testAuthRepo) ConsumeRefreshToken(_ context.Context, h string, n time.Time) (auth.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[h]
	if !ok || t.RevokedAt != nil || !t.ExpiresAt.After(n) {
		return auth.RefreshToken{}, auth.ErrInvalidToken
	}
	t.RevokedAt = &n
	r.tokens[h] = t
	return t, nil
}
func (r *testAuthRepo) RevokeRefreshToken(_ context.Context, h string, n time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[h]
	if !ok {
		return auth.ErrInvalidToken
	}
	t.RevokedAt = &n
	r.tokens[h] = t
	return nil
}
func (r *testAuthRepo) byName(n string) auth.User {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.users[r.names[n]]
}

type testProjectRepo struct {
	mu       sync.Mutex
	next     uint64
	projects map[uint64]project.Project
	members  map[[2]uint64]project.Member
}

func newProjectRepo() *testProjectRepo {
	return &testProjectRepo{projects: map[uint64]project.Project{}, members: map[[2]uint64]project.Member{}}
}
func (r *testProjectRepo) CreateProject(_ context.Context, p project.Project, m project.Member) (project.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	p.ID = r.next
	m.ProjectID = p.ID
	r.projects[p.ID] = p
	r.members[[2]uint64{m.UserID, p.ID}] = m
	return p, nil
}
func (r *testProjectRepo) FindMember(_ context.Context, u, p uint64) (project.Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[[2]uint64{u, p}]
	if !ok {
		return project.Member{}, project.ErrForbidden
	}
	return m, nil
}
func (r *testProjectRepo) RotateWriteKey(_ context.Context, id uint64, h string, e time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.projects[id]
	p.PreviousWriteKeyHash = p.WriteKeyHash
	p.PreviousWriteKeyExpiresAt = &e
	p.WriteKeyHash = h
	r.projects[id] = p
	return nil
}
func (r *testProjectRepo) ListProjects(_ context.Context, u uint64) ([]project.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []project.Project
	for k := range r.members {
		if k[0] == u {
			out = append(out, r.projects[k[1]])
		}
	}
	return out, nil
}
