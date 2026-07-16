package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

const readinessTimeout = 2 * time.Second

// HealthDependency is deliberately limited to a read-only connectivity probe.
type HealthDependency interface{ PingContext(context.Context) error }

type HealthHandler struct{ dependencies map[string]HealthDependency }

func NewHealthHandler(dependencies map[string]HealthDependency) http.Handler {
	copy := make(map[string]HealthDependency, len(dependencies))
	for name, dependency := range dependencies {
		copy[name] = dependency
	}
	return HealthHandler{dependencies: copy}
}

func (h HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/health/live":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	case "/health/ready":
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		failed := make([]string, 0)
		for name, dependency := range h.dependencies {
			if dependency == nil || dependency.PingContext(ctx) != nil {
				failed = append(failed, name)
			}
		}
		sort.Strings(failed)
		if len(failed) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "not_ready", "dependencies": failed})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	default:
		http.NotFound(w, r)
	}
}
