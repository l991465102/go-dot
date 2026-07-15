package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	collectv1 "github.com/lzw7758/go-dot/api/collect/v1"
	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/project"
)

type CollectService interface {
	Collect(context.Context, project.Project, ingestion.CollectRequest) (ingestion.CollectResult, error)
}

type CollectController struct {
	service      CollectService
	maxBodyBytes int64
}

func NewCollectController(service CollectService, maxBodyBytes int) CollectController {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 256 << 10
	}
	return CollectController{service: service, maxBodyBytes: int64(maxBodyBytes)}
}

func (c CollectController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/collect/events" {
		http.NotFound(w, r)
		return
	}
	if r.ContentLength > c.maxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	var req collectv1.EventsReq
	reader := http.MaxBytesReader(w, r.Body, c.maxBodyBytes)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}
	p, ok := CollectionProjectFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid write key")
		return
	}
	result, err := c.service.Collect(r.Context(), p, mapCollectRequest(req))
	if errors.Is(err, ingestion.ErrInvalidBatch) {
		writeError(w, http.StatusBadRequest, "events must contain 1 to 100 items")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "collection service unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, mapCollectResponse(result))
}

func NewCollectHandler(controller CollectController, writeKey WriteKeyMiddleware, rateLimit RateLimitMiddleware) http.Handler {
	secured := writeKey.Wrap(rateLimit.Wrap(controller))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions && r.URL.Path == "/api/v1/collect/events" {
			origin := r.Header.Get("Origin")
			if origin == "" || r.Header.Get("Access-Control-Request-Method") != http.MethodPost {
				writeError(w, http.StatusBadRequest, "invalid preflight request")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Write-Key, X-Device-ID")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Add("Vary", "Origin")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		secured.ServeHTTP(w, r)
	})
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
func mapCollectRequest(req collectv1.EventsReq) ingestion.CollectRequest {
	events := make([]ingestion.Event, len(req.Events))
	for i, event := range req.Events {
		events[i] = ingestion.Event{EventID: event.EventID, PointCode: event.PointCode, EventTime: event.EventTime, UserID: event.UserID, AnonymousID: event.AnonymousID, SessionID: event.SessionID, Platform: event.Platform, AppVersion: event.AppVersion, Channel: event.Channel, Properties: event.Properties}
	}
	return ingestion.CollectRequest{ProjectID: req.ProjectID, Events: events}
}
func mapCollectResponse(result ingestion.CollectResult) collectv1.EventsRes {
	results := make([]collectv1.EventResult, len(result.Results))
	for i, item := range result.Results {
		results[i] = collectv1.EventResult{Index: item.Index, EventID: item.EventID, Status: item.Status, Code: item.Code, Message: item.Message}
	}
	return collectv1.EventsRes{BatchID: result.BatchID, Accepted: result.Accepted, Rejected: result.Rejected, Results: results}
}
