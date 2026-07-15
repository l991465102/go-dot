package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, message string) {
	requestID := r.Header.Get("X-Request-ID")
	if !requestIDPattern.MatchString(requestID) {
		var raw [12]byte
		if _, err := rand.Read(raw[:]); err == nil {
			requestID = hex.EncodeToString(raw[:])
		} else {
			requestID = "unavailable"
		}
	}
	w.Header().Set("X-Request-ID", requestID)
	writeJSON(w, status, map[string]string{"error": message, "request_id": requestID})
}
