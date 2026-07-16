package httpapi

import (
	"net/http"
	"regexp"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, message string) {
	requestID := RequestIDFromContext(r.Context())
	if requestID == "" {
		requestID = r.Header.Get("X-Request-ID")
	}
	if !requestIDPattern.MatchString(requestID) {
		requestID = newRequestID()
	}
	w.Header().Set("X-Request-ID", requestID)
	writeJSON(w, status, map[string]string{"error": message, "request_id": requestID})
}
