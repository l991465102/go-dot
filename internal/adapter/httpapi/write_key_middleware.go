package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
)

type collectProjectKey struct{}

type WriteKeyProjectRepository interface {
	FindProjectByWriteKeyHash(context.Context, string, time.Time) (project.Project, error)
}

type WriteKeyMiddleware struct {
	repo WriteKeyProjectRepository
	now  func() time.Time
}

func NewWriteKeyMiddleware(repo WriteKeyProjectRepository, now func() time.Time) WriteKeyMiddleware {
	if now == nil {
		now = time.Now
	}
	return WriteKeyMiddleware{repo: repo, now: now}
}

func HashWriteKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (m WriteKeyMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Write-Key")
		if key == "" || len(key) > 256 {
			writeError(w, http.StatusUnauthorized, "invalid write key")
			return
		}
		p, err := m.repo.FindProjectByWriteKeyHash(r.Context(), HashWriteKey(key), m.now().UTC())
		if errors.Is(err, project.ErrProjectNotFound) || errors.Is(err, project.ErrForbidden) || (err == nil && !p.Active) {
			writeError(w, http.StatusUnauthorized, "invalid write key")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "collection service unavailable")
			return
		}
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !originAllowed(p.AllowedOrigins, origin) {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		} else if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		ctx := context.WithValue(r.Context(), collectProjectKey{}, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CollectionProjectFromContext(ctx context.Context) (project.Project, bool) {
	p, ok := ctx.Value(collectProjectKey{}).(project.Project)
	return p, ok
}

func originAllowed(allowed []string, origin string) bool {
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), origin) {
			return true
		}
	}
	return false
}
