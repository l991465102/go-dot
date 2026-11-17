package godot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"
)

// TestSendRetryAndResults 验证重试复用事件 ID，逐条返回拒绝结果。
func TestSendRetryAndResults(t *testing.T) {
	var calls atomic.Int32
	var first string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Events []Event `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("X-Write-Key") != "key" {
			t.Error("missing key")
		}
		if calls.Add(1) == 1 {
			first = body.Events[0].EventID
			w.WriteHeader(503)
			return
		}
		if first != body.Events[0].EventID {
			t.Error("event ID changed on retry")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(response{Accepted: 1, Rejected: 1, Results: []EventResult{{Index: 0, EventID: body.Events[0].EventID, Status: "accepted"}, {Index: 1, EventID: body.Events[1].EventID, Status: "rejected", Code: "PROPERTY_REQUIRED"}}})
	}))
	defer server.Close()
	client, err := NewClient(Options{BaseURL: server.URL, WriteKey: "key", MaxRetries: 1, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{{PointCode: "a"}, {PointCode: "b"}}
	got, err := client.Send(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Code != "PROPERTY_REQUIRED" || calls.Load() != 2 {
		t.Fatalf("results=%+v calls=%d", got, calls.Load())
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(events[0].EventID) {
		t.Fatal(events[0].EventID)
	}
}

// TestSendRejectsMalformedResponse 验证不完整响应不会被当作成功。
func TestSendRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		w.Write([]byte(`{"accepted":1,"rejected":0,"results":[]}`))
	}))
	defer server.Close()
	client, _ := NewClient(Options{BaseURL: server.URL, WriteKey: "key"})
	if _, err := client.Send(context.Background(), []Event{{PointCode: "a"}}); err == nil {
		t.Fatal("expected error")
	}
}
