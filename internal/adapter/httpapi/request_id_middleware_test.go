package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestIDPropagatesValidValueAndGeneratesForInvalidValue(t *testing.T) {
	var seen []string
	h := NewRequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, RequestIDFromContext(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, supplied := range []string{"client-123", "contains spaces"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", supplied)
		h.ServeHTTP(recorder, req)
		require.Equal(t, seen[len(seen)-1], recorder.Header().Get("X-Request-ID"))
		require.NotEmpty(t, seen[len(seen)-1])
	}
	require.Equal(t, "client-123", seen[0])
	require.NotEqual(t, "contains spaces", seen[1])
}
