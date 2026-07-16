package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthDependencyFunc func(context.Context) error

func (f healthDependencyFunc) PingContext(ctx context.Context) error { return f(ctx) }

func TestHealthLivenessDoesNotDependOnInfrastructure(t *testing.T) {
	h := NewHealthHandler(map[string]HealthDependency{"mysql": healthDependencyFunc(func(context.Context) error { return errors.New("down") })})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHealthReadinessFailsAndRecoversWithDependencies(t *testing.T) {
	available := false
	h := NewHealthHandler(map[string]HealthDependency{"redis": healthDependencyFunc(func(ctx context.Context) error {
		_, ok := ctx.Deadline()
		require.True(t, ok)
		if !available {
			return errors.New("down")
		}
		return nil
	})})
	for _, expected := range []int{http.StatusServiceUnavailable, http.StatusOK} {
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		require.Equal(t, expected, recorder.Code)
		available = true
	}
}

func TestHealthReadinessUsesTwoSecondBound(t *testing.T) {
	var remaining time.Duration
	h := NewHealthHandler(map[string]HealthDependency{"mysql": healthDependencyFunc(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		remaining = time.Until(deadline)
		return nil
	})})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Greater(t, remaining, 1500*time.Millisecond)
	require.LessOrEqual(t, remaining, 2*time.Second)
}
