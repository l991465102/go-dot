package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lzw7758/go-dot/internal/auth"
)

type principalKey struct{}

type AuthMiddleware struct{ service *auth.Service }

func NewAuthMiddleware(service *auth.Service) AuthMiddleware { return AuthMiddleware{service: service} }

func (m AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAuthPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" || raw == r.Header.Get("Authorization") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		principal, err := m.service.ParseAccessToken(raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := m.service.User(r.Context(), principal.UserID)
		if err != nil || user.Status != "ACTIVE" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		principal.MustChangePassword = user.MustChangePassword
		if principal.MustChangePassword && isManagementWrite(r) && !isFirstLoginAllowedPath(r.URL.Path) {
			writeError(w, http.StatusForbidden, "password change required")
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func PrincipalFromContext(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}
func isPublicAuthPath(path string) bool {
	return path == "/api/v1/auth/login" || path == "/api/v1/auth/refresh"
}
func isFirstLoginAllowedPath(path string) bool {
	return path == "/api/v1/auth/change-password" || path == "/api/v1/auth/logout"
}
func isManagementWrite(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
