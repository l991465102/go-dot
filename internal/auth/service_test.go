package auth_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/auth"
)

func TestInitialAdminMustChangePassword(t *testing.T) {
	svc, repo := newAuthFixture(t)
	require.NoError(t, svc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
	session, err := svc.Login(context.Background(), "admin", "admin")
	require.NoError(t, err)
	require.True(t, session.MustChangePassword)
	require.Equal(t, "admin", repo.UserByName("admin").Username)
}

func TestChangePasswordClearsFirstLoginRestriction(t *testing.T) {
	svc, repo := newAuthFixture(t)
	require.NoError(t, svc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
	user := repo.UserByName("admin")
	require.NoError(t, svc.ChangePassword(context.Background(), user.ID, "admin", "Admin-Changed-2026!"))

	session, err := svc.Login(context.Background(), "admin", "Admin-Changed-2026!")
	require.NoError(t, err)
	require.False(t, session.MustChangePassword)
	_, err = svc.Login(context.Background(), "admin", "admin")
	require.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestRefreshRotatesStoredTokenAndLogoutRevokesIt(t *testing.T) {
	svc, repo := newAuthFixture(t)
	require.NoError(t, svc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
	session, err := svc.Login(context.Background(), "admin", "admin")
	require.NoError(t, err)
	require.NotEqual(t, session.RefreshToken, repo.StoredRefreshToken())

	rotated, err := svc.Refresh(context.Background(), session.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, session.RefreshToken, rotated.RefreshToken)
	_, err = svc.Refresh(context.Background(), session.RefreshToken)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
	require.NoError(t, svc.Logout(context.Background(), rotated.RefreshToken))
	_, err = svc.Refresh(context.Background(), rotated.RefreshToken)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestRefreshSuccessorInsertFailureLeavesOldTokenUsable(t *testing.T) {
	svc, repo := newAuthFixture(t)
	require.NoError(t, svc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
	session, err := svc.Login(context.Background(), "admin", "admin")
	require.NoError(t, err)
	repo.failNextRotation = errors.New("insert successor: connection lost")

	_, err = svc.Refresh(context.Background(), session.RefreshToken)
	require.ErrorContains(t, err, "connection lost")
	_, err = svc.Refresh(context.Background(), session.RefreshToken)
	require.NoError(t, err)
}

func TestConcurrentRefreshCreatesOnlyOneSuccessor(t *testing.T) {
	svc, _ := newAuthFixture(t)
	require.NoError(t, svc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
	session, err := svc.Login(context.Background(), "admin", "admin")
	require.NoError(t, err)

	var successes atomic.Int32
	var invalid atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Refresh(context.Background(), session.RefreshToken)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, auth.ErrInvalidToken) {
				invalid.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
	require.EqualValues(t, 7, invalid.Load())
}

func TestPasswordEncodingContainsExplicitArgon2idParameters(t *testing.T) {
	encoded, err := auth.HashPassword("secret")
	require.NoError(t, err)
	require.Contains(t, encoded, "$argon2id$v=19$m=65536,t=3,p=2$")
	require.True(t, auth.CheckPassword("secret", encoded))
	require.False(t, auth.CheckPassword("wrong", encoded))
}

func newAuthFixture(t *testing.T) (*auth.Service, *authMemoryRepository) {
	t.Helper()
	repo := &authMemoryRepository{users: map[uint64]auth.User{}, names: map[string]uint64{}, tokens: map[string]auth.RefreshToken{}}
	svc := auth.NewService(repo, []byte("test-jwt-secret-that-is-long-enough"), 15*time.Minute, 7*24*time.Hour)
	return svc, repo
}

type authMemoryRepository struct {
	mu               sync.Mutex
	nextID           uint64
	users            map[uint64]auth.User
	names            map[string]uint64
	tokens           map[string]auth.RefreshToken
	failNextRotation error
}

func (r *authMemoryRepository) CreateInitialUser(_ context.Context, user auth.User) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.names[user.Username]; ok {
		return r.users[id], nil
	}
	r.nextID++
	user.ID = r.nextID
	r.users[user.ID], r.names[user.Username] = user, user.ID
	return user, nil
}
func (r *authMemoryRepository) FindUserByUsername(_ context.Context, username string) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.names[username]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return r.users[id], nil
}
func (r *authMemoryRepository) FindUserByID(_ context.Context, id uint64) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}
func (r *authMemoryRepository) UpdatePassword(_ context.Context, id uint64, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.users[id]
	u.PasswordHash, u.MustChangePassword = hash, false
	r.users[id] = u
	return nil
}
func (r *authMemoryRepository) TouchLogin(context.Context, uint64, time.Time) error { return nil }
func (r *authMemoryRepository) StoreRefreshToken(_ context.Context, token auth.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[token.Hash] = token
	return nil
}
func (r *authMemoryRepository) RotateRefreshToken(_ context.Context, hash string, successor auth.RefreshToken, now time.Time) (auth.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[hash]
	if !ok || t.RevokedAt != nil || !t.ExpiresAt.After(now) {
		return auth.RefreshToken{}, auth.ErrInvalidToken
	}
	if r.failNextRotation != nil {
		err := r.failNextRotation
		r.failNextRotation = nil
		return auth.RefreshToken{}, err
	}
	n := now
	t.RevokedAt = &n
	r.tokens[hash] = t
	successor.UserID = t.UserID
	r.tokens[successor.Hash] = successor
	return t, nil
}
func (r *authMemoryRepository) RevokeRefreshToken(_ context.Context, hash string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[hash]
	if !ok {
		return auth.ErrInvalidToken
	}
	t.RevokedAt = &now
	r.tokens[hash] = t
	return nil
}
func (r *authMemoryRepository) UserByName(name string) auth.User {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.users[r.names[name]]
}
func (r *authMemoryRepository) StoredRefreshToken() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for hash := range r.tokens {
		return hash
	}
	return ""
}
