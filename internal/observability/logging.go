package observability

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var sensitiveKeys = map[string]struct{}{
	"password": {}, "access_token": {}, "refresh_token": {}, "write_key": {}, "x-write-key": {},
	"user_id": {}, "anonymous_id": {}, "properties": {}, "token": {}, "authorization": {}, "cookie": {}, "set-cookie": {},
}

func RedactFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if _, secret := sensitiveKeys[strings.ToLower(strings.TrimSpace(key))]; secret {
				out[key] = "[REDACTED]"
			} else {
				out[key] = RedactFields(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = RedactFields(typed[i])
		}
		return out
	default:
		return value
	}
}

// JSONLogger serializes one redacted JSON object per line.
type JSONLogger struct {
	mu        sync.Mutex
	writer    io.Writer
	component string
}

func NewJSONLogger(writer io.Writer, component string) *JSONLogger {
	return &JSONLogger{writer: writer, component: component}
}
func (l *JSONLogger) Log(level, message string, fields map[string]any) error {
	record := map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano), "level": level, "component": l.component, "message": message}
	for key, value := range fields {
		record[key] = value
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return json.NewEncoder(l.writer).Encode(RedactFields(record))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (l *JSONLogger) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		tracked := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(tracked, r)
		_ = l.Log("info", "http request", map[string]any{"request_id": r.Header.Get("X-Request-ID"), "method": r.Method, "path": r.URL.Path, "status": tracked.status, "duration_ms": time.Since(start).Milliseconds()})
	})
}
